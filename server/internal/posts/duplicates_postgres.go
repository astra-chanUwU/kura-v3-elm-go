package posts

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
)

var _ DuplicateReader = (*PostgresBackend)(nil)

// listDuplicateGroupHashesSQL returns visible canonical-hash groups with at
// least two members in hash ASC keyset order. $1 is the cursor hash (NULL
// on the first page); $2 is limit+1 for lookahead. The hash regex keeps
// blank and legacy non-canonical hashes out of the review surface, and the
// inline visibility predicate matches the existing hash index.
const listDuplicateGroupHashesSQL = `
SELECT hash, COUNT(*)
FROM posts
WHERE deleted_at IS NULL
  AND moderation_state = 'published'
  AND hash ~ '^sha256:[0-9a-f]{64}$'
  AND ($1::text IS NULL OR hash > $1::text)
GROUP BY hash
HAVING COUNT(*) >= 2
ORDER BY hash ASC
LIMIT $2
`

// listDuplicateGroupSamplesSQL returns up to three oldest-ID samples per
// requested hash in (hash, id) order. The window bounds each group so one
// large group cannot load the whole library.
const listDuplicateGroupSamplesSQL = `
SELECT hash, id::text, preview_url, original_url, media_type, width, height, tags, rating, rating_version
FROM (
    SELECT hash, id, preview_url, original_url, media_type, width, height, tags, rating, rating_version,
           ROW_NUMBER() OVER (PARTITION BY hash ORDER BY id ASC) AS rn
    FROM posts
    WHERE deleted_at IS NULL
      AND moderation_state = 'published'
      AND hash = ANY($1::text[])
) s
WHERE rn <= 3
ORDER BY hash ASC, s.id ASC
`

const countDuplicateMembersSQL = `
SELECT COUNT(*)
FROM posts
WHERE hash = $1::text
  AND deleted_at IS NULL
  AND moderation_state = 'published'
`

const listDuplicateMembersSQL = `
SELECT id::text, preview_url, original_url, media_type, width, height, tags, rating, rating_version
FROM posts
WHERE hash = $1::text
  AND deleted_at IS NULL
  AND moderation_state = 'published'
  AND ($2::bigint IS NULL OR id > $2::bigint)
ORDER BY posts.id ASC
LIMIT $3
`

// ListDuplicateGroups returns hash-ordered exact-copy groups with visible
// counts and oldest-ID samples. Count, samples, and thumbnail overlays come
// from one repeatable-read transaction on a single pooled connection.
func (s *PostgresBackend) ListDuplicateGroups(ctx context.Context, limit int, cursor *DuplicateGroupsCursor) (DuplicateGroupsPage, error) {
	if limit < 1 || limit > maxDuplicateGroupsLimit {
		return DuplicateGroupsPage{}, ErrInvalidQuery
	}
	var cursorHash any
	if cursor != nil {
		if !validCanonicalSHA256(cursor.Hash) {
			return DuplicateGroupsPage{}, ErrInvalidCursor
		}
		cursorHash = cursor.Hash
	}
	if s == nil || s.pool == nil {
		return DuplicateGroupsPage{}, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DuplicateGroupsPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, listDuplicateGroupHashesSQL, cursorHash, limit+1)
	if err != nil {
		return DuplicateGroupsPage{}, err
	}
	type groupCount struct {
		hash  string
		count int64
	}
	counts := make([]groupCount, 0, limit+1)
	for rows.Next() {
		var item groupCount
		if err := rows.Scan(&item.hash, &item.count); err != nil {
			rows.Close()
			return DuplicateGroupsPage{}, err
		}
		counts = append(counts, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return DuplicateGroupsPage{}, err
	}
	page := DuplicateGroupsPage{Groups: make([]DuplicateGroup, 0, limit)}
	hasMore := len(counts) > limit
	if hasMore {
		counts = counts[:limit]
	}
	hashes := make([]string, 0, len(counts))
	for _, item := range counts {
		hashes = append(hashes, item.hash)
	}
	samplesByHash := make(map[string][]PostSummary, len(counts))
	if len(hashes) > 0 {
		sampleRows, err := tx.Query(ctx, listDuplicateGroupSamplesSQL, hashes)
		if err != nil {
			return DuplicateGroupsPage{}, err
		}
		for sampleRows.Next() {
			var hash string
			var post PostSummary
			if err := sampleRows.Scan(&hash, &post.ID, &post.PreviewURL, &post.OriginalURL, &post.MediaType, &post.Width, &post.Height, &post.Tags, &post.Rating, &post.RatingVersion); err != nil {
				sampleRows.Close()
				return DuplicateGroupsPage{}, err
			}
			if post.Tags == nil {
				post.Tags = []string{}
			}
			samplesByHash[hash] = append(samplesByHash[hash], post)
		}
		sampleRows.Close()
		if err := sampleRows.Err(); err != nil {
			return DuplicateGroupsPage{}, err
		}
	}
	flattened := make([]PostSummary, 0, len(counts)*3)
	for _, item := range counts {
		flattened = append(flattened, samplesByHash[item.hash]...)
	}
	if err := overlayReadyPreviewsTx(ctx, tx, flattened); err != nil {
		return DuplicateGroupsPage{}, err
	}
	offset := 0
	for _, item := range counts {
		samples := samplesByHash[item.hash]
		if samples == nil {
			samples = []PostSummary{}
		} else {
			samples = flattened[offset : offset+len(samples)]
			offset += len(samples)
		}
		page.Groups = append(page.Groups, DuplicateGroup{Hash: item.hash, Count: int(item.count), Posts: samples})
	}
	if hasMore {
		token, err := EncodeDuplicateGroupsCursor(counts[len(counts)-1].hash)
		if err != nil {
			return DuplicateGroupsPage{}, err
		}
		page.NextCursor = &token
	}
	if err := tx.Commit(ctx); err != nil {
		return DuplicateGroupsPage{}, err
	}
	return page, nil
}

// ListDuplicateMembers returns every visible member of one hash group in id
// ASC keyset order. Count, page, and thumbnail overlays share one
// repeatable-read transaction. A visible count below two reports ErrNotFound
// so shrinking groups read as missing.
func (s *PostgresBackend) ListDuplicateMembers(ctx context.Context, hash string, limit int, cursor *DuplicateMembersCursor) (DuplicateMembersPage, error) {
	if !validCanonicalSHA256(hash) {
		return DuplicateMembersPage{}, ErrInvalidQuery
	}
	if limit < 1 || limit > maxDuplicateMembersLimit {
		return DuplicateMembersPage{}, ErrInvalidQuery
	}
	if cursor != nil && (cursor.Hash != hash || cursor.ID <= 0) {
		return DuplicateMembersPage{}, ErrInvalidCursor
	}
	if s == nil || s.pool == nil {
		return DuplicateMembersPage{}, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DuplicateMembersPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var count int64
	if err := tx.QueryRow(ctx, countDuplicateMembersSQL, hash).Scan(&count); err != nil {
		return DuplicateMembersPage{}, err
	}
	if count < 2 {
		return DuplicateMembersPage{}, ErrNotFound
	}
	var cursorID any
	if cursor != nil {
		cursorID = cursor.ID
	}
	rows, err := tx.Query(ctx, listDuplicateMembersSQL, hash, cursorID, limit+1)
	if err != nil {
		return DuplicateMembersPage{}, err
	}
	members := make([]PostSummary, 0, limit+1)
	for rows.Next() {
		var post PostSummary
		if err := rows.Scan(&post.ID, &post.PreviewURL, &post.OriginalURL, &post.MediaType, &post.Width, &post.Height, &post.Tags, &post.Rating, &post.RatingVersion); err != nil {
			rows.Close()
			return DuplicateMembersPage{}, err
		}
		if post.Tags == nil {
			post.Tags = []string{}
		}
		members = append(members, post)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return DuplicateMembersPage{}, err
	}
	page := DuplicateMembersPage{Hash: hash, Count: int(count), Posts: make([]PostSummary, 0, limit)}
	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}
	page.Posts = append(page.Posts, members...)
	if err := overlayReadyPreviewsTx(ctx, tx, page.Posts); err != nil {
		return DuplicateMembersPage{}, err
	}
	if hasMore {
		var lastID int64
		last := page.Posts[len(page.Posts)-1].ID
		parsed, err := parsePostIDValue(last)
		if err != nil {
			return DuplicateMembersPage{}, ErrInvalidQuery
		}
		lastID = parsed
		token, err := EncodeDuplicateMembersCursor(hash, lastID)
		if err != nil {
			return DuplicateMembersPage{}, err
		}
		page.NextCursor = &token
	}
	if err := tx.Commit(ctx); err != nil {
		return DuplicateMembersPage{}, err
	}
	return page, nil
}

func parsePostIDValue(raw string) (int64, error) {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
		return 0, ErrInvalidQuery
	}
	return parsed, nil
}
