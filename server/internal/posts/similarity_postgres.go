package posts

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

var (
	_ SimilarityReader  = (*PostgresBackend)(nil)
	_ SimilarityIndexer = (*PostgresBackend)(nil)
)

// NewFingerprintProcessor builds the similarity handler over the backend's
// media provider and a fingerprint recorder sharing the backend's pool.
// Uploads fall back to the environment media store when no explicit
// provider was configured, and the processor mirrors that fallback.
func (s *PostgresBackend) NewFingerprintProcessor() *media.FingerprintProcessor {
	if s == nil {
		return nil
	}
	store := s.store
	if store == nil {
		store = media.NewStoreFromEnv(uploadMediaRoot())
	}
	return &media.FingerprintProcessor{
		Originals:    store,
		Fingerprints: media.NewPostgresFingerprintStore(s.pool),
	}
}

// HandleMediaJob dispatches one claimed durable job to its kind handler.
// The root main wires this as the single worker handler so derivative and
// fingerprint jobs share one worker; unknown kinds fail the job with a
// clear error instead of succeeding silently. The existing derivative path
// is unchanged: the same processor handles KindDerivative rows.
func (s *PostgresBackend) HandleMediaJob(ctx context.Context, job jobs.Job) error {
	switch strings.TrimSpace(job.Kind) {
	case media.KindDerivative:
		return s.NewDerivativeProcessor().Handle(ctx, job)
	case media.KindFingerprint:
		return s.NewFingerprintProcessor().Handle(ctx, job)
	default:
		return jobs.ErrInvalidJob
	}
}

// similarityVisibleImage is the eligibility predicate shared by the status
// page and its counts: published, non-deleted image posts. Video and
// non-image rows are ineligible until a later worker teaches uploads and
// fingerprinting new decoders.
const similarityVisibleImage = `deleted_at IS NULL
  AND moderation_state = 'published'
  AND media_type LIKE 'image/%'`

const similarAnchorSQL = `
SELECT id::text, hash
FROM posts
WHERE id = $1::bigint
  AND deleted_at IS NULL
  AND moderation_state = 'published'
  AND media_type LIKE 'image/%'
`

const similarAnchorFingerprintSQL = `
SELECT fingerprint::text, original_hash
FROM similarity_fingerprints
WHERE post_id = $1::bigint
  AND algorithm = 'dhash-v1'
`

// similarCandidatesSQL returns visible candidates within maxDistance of
// the anchor hash, ordered by (distance ASC, id ASC) for deterministic
// keyset paging. Every candidate re-checks the original-hash binding, so
// stale fingerprints never match wrong bytes. $1 is the anchor id, $2 is
// the anchor bits, $3 is the max distance, $4/$5 are the cursor
// (lastDistance, lastID) and are NULL on the first page, $6 is limit+1.
const similarCandidatesSQL = `
SELECT
    p.id::text,
    p.preview_url,
    p.original_url,
    p.media_type,
    p.width,
    p.height,
    p.tags,
    p.rating,
    p.rating_version,
    bit_count(f.fingerprint # $2::bit(64)) AS distance
FROM similarity_fingerprints f
JOIN posts p ON p.id = f.post_id
WHERE f.algorithm = 'dhash-v1'
  AND f.original_hash = p.hash
  AND p.deleted_at IS NULL
  AND p.moderation_state = 'published'
  AND p.media_type LIKE 'image/%'
  AND p.id <> $1::bigint
  AND bit_count(f.fingerprint # $2::bit(64)) <= $3::integer
  AND ($4::integer IS NULL OR
       bit_count(f.fingerprint # $2::bit(64)) > $4::integer OR
       (bit_count(f.fingerprint # $2::bit(64)) = $4::integer AND p.id > $5::bigint))
ORDER BY distance ASC, p.id ASC
LIMIT $6::integer
`

// ListSimilarPosts serves the public near-copy read. A non-image,
// hidden, deleted, or missing anchor is indistinguishable from a missing
// row (ErrNotFound); candidates are eligible visible images only.
// A visible anchor with no current fingerprint (never indexed, or indexed
// from older bytes) reads as indexed=false with no matches: no hash leaks
// and no jobs are enqueued on GET. Anchor, candidates, and thumbnail
// overlays come from one repeatable-read transaction on a single pooled
// connection; the candidate scan is bounded by the radius and limit+1, so
// the library body is never loaded and no N^2 pairs are computed.
func (s *PostgresBackend) ListSimilarPosts(ctx context.Context, anchorID string, maxDistance, limit int, cursor string) (SimilarPage, error) {
	if !isCanonicalPostID(anchorID) {
		return SimilarPage{}, ErrNotFound
	}
	if maxDistance < 0 || maxDistance > MaxSimilarDistance {
		return SimilarPage{}, ErrInvalidQuery
	}
	if limit < 1 || limit > MaxSimilarLimit {
		return SimilarPage{}, ErrInvalidQuery
	}
	if s == nil || s.pool == nil {
		return SimilarPage{}, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return SimilarPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var resolvedID, anchorHash string
	if err := tx.QueryRow(ctx, similarAnchorSQL, anchorID).Scan(&resolvedID, &anchorHash); err != nil {
		if isNoRows(err) {
			return SimilarPage{}, ErrNotFound
		}
		return SimilarPage{}, err
	}
	page := SimilarPage{PostID: resolvedID, Algorithm: SimilarityAlgorithm, Indexed: false, Matches: []SimilarMatch{}}
	// Bind the cursor before the fingerprint lookup so foreign or
	// malformed tokens fail even for unindexed anchors.
	var lastDistance, lastID any
	if strings.TrimSpace(cursor) != "" {
		decoded, err := DecodeSimilarCursor(cursor, resolvedID, anchorHash, maxDistance)
		if err != nil {
			return SimilarPage{}, ErrInvalidCursor
		}
		lastDistance, lastID = decoded.LastDistance, decoded.LastID
	}
	var anchorBits string
	var storedOriginalHash string
	if err := tx.QueryRow(ctx, similarAnchorFingerprintSQL, resolvedID).Scan(&anchorBits, &storedOriginalHash); err != nil {
		if isNoRows(err) {
			if err := tx.Commit(ctx); err != nil {
				return SimilarPage{}, err
			}
			return page, nil
		}
		return SimilarPage{}, err
	}
	if storedOriginalHash != anchorHash {
		if err := tx.Commit(ctx); err != nil {
			return SimilarPage{}, err
		}
		return page, nil
	}
	if _, err := media.ParseHashBits(anchorBits); err != nil {
		if err := tx.Commit(ctx); err != nil {
			return SimilarPage{}, err
		}
		return page, nil
	}
	rows, err := tx.Query(ctx, similarCandidatesSQL, resolvedID, anchorBits, maxDistance, lastDistance, lastID, limit+1)
	if err != nil {
		return SimilarPage{}, err
	}
	type candidate struct {
		post     PostSummary
		distance int
	}
	candidates := make([]candidate, 0, limit+1)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.post.ID, &item.post.PreviewURL, &item.post.OriginalURL,
			&item.post.MediaType, &item.post.Width, &item.post.Height,
			&item.post.Tags, &item.post.Rating, &item.post.RatingVersion,
			&item.distance,
		); err != nil {
			rows.Close()
			return SimilarPage{}, err
		}
		if item.post.Tags == nil {
			item.post.Tags = []string{}
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return SimilarPage{}, err
	}
	hasMore := len(candidates) > limit
	if hasMore {
		candidates = candidates[:limit]
	}
	summaries := make([]PostSummary, 0, len(candidates))
	for _, item := range candidates {
		summaries = append(summaries, item.post)
	}
	if err := overlayReadyPreviewsTx(ctx, tx, summaries); err != nil {
		return SimilarPage{}, err
	}
	page.Indexed = true
	page.Matches = make([]SimilarMatch, 0, len(candidates))
	for index, item := range candidates {
		page.Matches = append(page.Matches, SimilarMatch{Post: summaries[index], Distance: item.distance})
	}
	if hasMore {
		last := candidates[len(candidates)-1]
		parsed, err := parsePostIDValue(last.post.ID)
		if err != nil {
			return SimilarPage{}, ErrInvalidQuery
		}
		token, err := EncodeSimilarCursor(resolvedID, anchorHash, maxDistance, last.distance, parsed)
		if err != nil {
			return SimilarPage{}, err
		}
		page.NextCursor = &token
	}
	if err := tx.Commit(ctx); err != nil {
		return SimilarPage{}, err
	}
	return page, nil
}

const similarityIndexCandidateSQL = `
SELECT
    p.id::text,
    p.hash,
    p.original_url,
    COALESCE((
        SELECT j.payload->>'original_key'
        FROM jobs j
        WHERE j.kind = 'derivative'
          AND j.payload->>'post_id' = p.id::text
        ORDER BY j.id
        LIMIT 1
    ), '') AS original_key
FROM posts p
WHERE p.id = $1::bigint
  AND p.deleted_at IS NULL
  AND p.moderation_state = 'published'
  AND p.media_type LIKE 'image/%'
`

// IndexSimilarityPosts enqueues one fingerprint job per id. Every id is
// validated and every post is confirmed an eligible visible image
// (published, non-deleted, image/*) before any job is enqueued: one
// ineligible, hidden, deleted, or missing id rejects the whole batch
// with ErrNotFound and enqueues nothing. The batch commits atomically.
// An explicit call also resets a terminally failed fingerprint job for
// the same original bytes back to pending; pending, running, and
// succeeded jobs are returned unchanged, and no other caller requeues
// failed work. No image bytes are read here; workers do the decoding.
func (s *PostgresBackend) IndexSimilarityPosts(ctx context.Context, ids []string) (SimilarityIndexResponse, error) {
	clean, err := ValidateSimilarityIndexIDs(ids)
	if err != nil {
		return SimilarityIndexResponse{}, err
	}
	if s == nil || s.pool == nil {
		return SimilarityIndexResponse{}, ErrUnavailable
	}
	type target struct {
		id          string
		hash        string
		originalKey string
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SimilarityIndexResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	targets := make([]target, 0, len(clean))
	for _, id := range clean {
		var item target
		var originalURL, payloadKey string
		if err := tx.QueryRow(ctx, similarityIndexCandidateSQL, id).Scan(&item.id, &item.hash, &originalURL, &payloadKey); err != nil {
			if isNoRows(err) {
				return SimilarityIndexResponse{}, ErrNotFound
			}
			return SimilarityIndexResponse{}, err
		}
		key := strings.TrimSpace(payloadKey)
		if key == "" && strings.HasPrefix(originalURL, "/media/") {
			key = strings.TrimPrefix(originalURL, "/media/")
		}
		if strings.TrimSpace(key) == "" {
			return SimilarityIndexResponse{}, ErrInvalidQuery
		}
		item.originalKey = key
		targets = append(targets, item)
	}
	response := SimilarityIndexResponse{Posts: make([]SimilarityIndexResult, 0, len(targets))}
	for _, item := range targets {
		request, err := media.EnqueueFingerprint(item.id, item.originalKey, item.hash)
		if err != nil {
			return SimilarityIndexResponse{}, err
		}
		job, err := jobs.EnqueueOrResetFailedInTx(ctx, tx, request)
		if err != nil {
			return SimilarityIndexResponse{}, err
		}
		response.Posts = append(response.Posts, SimilarityIndexResult{ID: item.id, JobID: job.ID, Status: job.Status})
	}
	if err := tx.Commit(ctx); err != nil {
		return SimilarityIndexResponse{}, err
	}
	return response, nil
}

const similarityEligibleCountSQL = `
SELECT COUNT(*)
FROM posts
WHERE ` + similarityVisibleImage + `
`

const similarityIndexedCountSQL = `
SELECT COUNT(*)
FROM similarity_fingerprints f
JOIN posts p ON p.id = f.post_id
WHERE f.algorithm = 'dhash-v1'
  AND f.original_hash = p.hash
  AND ` + similarityVisibleImage + `
`

// similarityStatusPageSQL pages eligible posts with their index state and
// the backing job for the current original bytes. The job join uses the
// same idempotency key the enqueue path writes, so jobs for older bytes
// never attach to the current row. Failed jobs are reported as-is; the
// read never retries them.
const similarityStatusPageSQL = `
SELECT
    p.id::text,
    f.post_id IS NOT NULL AS indexed,
    j.id::text,
    j.status
FROM posts p
LEFT JOIN similarity_fingerprints f
    ON f.post_id = p.id
    AND f.algorithm = 'dhash-v1'
    AND f.original_hash = p.hash
LEFT JOIN jobs j
    ON j.kind = 'fingerprint'
    AND j.idempotency_key = ('fingerprint:' || p.id::text || ':dhash-v1:' || p.hash)
WHERE ` + similarityVisibleImage + `
  AND ($1::bigint IS NULL OR p.id > $1::bigint)
ORDER BY p.id ASC
LIMIT $2::integer
`

// GetSimilarityStatus pages ALL eligible visible image posts by id
// ascending with their index state. Counts and page come from one
// read-only repeatable-read snapshot on one pooled connection. The page
// carries no media URLs and no hashes.
func (s *PostgresBackend) GetSimilarityStatus(ctx context.Context, limit int, cursor string) (SimilarityStatusPage, error) {
	if limit < 1 || limit > MaxSimilarityStatusLimit {
		return SimilarityStatusPage{}, ErrInvalidQuery
	}
	var lastID any
	if strings.TrimSpace(cursor) != "" {
		decoded, err := DecodeSimilarityStatusCursor(cursor)
		if err != nil {
			return SimilarityStatusPage{}, ErrInvalidCursor
		}
		lastID = decoded.LastID
	}
	if s == nil || s.pool == nil {
		return SimilarityStatusPage{}, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return SimilarityStatusPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var eligible, indexed int64
	if err := tx.QueryRow(ctx, similarityEligibleCountSQL).Scan(&eligible); err != nil {
		return SimilarityStatusPage{}, err
	}
	if err := tx.QueryRow(ctx, similarityIndexedCountSQL).Scan(&indexed); err != nil {
		return SimilarityStatusPage{}, err
	}
	rows, err := tx.Query(ctx, similarityStatusPageSQL, lastID, limit+1)
	if err != nil {
		return SimilarityStatusPage{}, err
	}
	items := make([]SimilarityStatusItem, 0, limit+1)
	for rows.Next() {
		var item SimilarityStatusItem
		var jobID, status pgtype.Text
		if err := rows.Scan(&item.ID, &item.Indexed, &jobID, &status); err != nil {
			rows.Close()
			return SimilarityStatusPage{}, err
		}
		if jobID.Valid {
			value := jobID.String
			item.JobID = &value
		}
		if status.Valid {
			value := status.String
			item.Status = &value
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return SimilarityStatusPage{}, err
	}
	page := SimilarityStatusPage{
		Algorithm:     SimilarityAlgorithm,
		IndexedCount:  int(indexed),
		EligibleCount: int(eligible),
		Posts:         make([]SimilarityStatusItem, 0, limit),
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page.Posts = append(page.Posts, items...)
	if hasMore {
		parsed, err := parsePostIDValue(items[len(items)-1].ID)
		if err != nil {
			return SimilarityStatusPage{}, ErrInvalidQuery
		}
		token, err := EncodeSimilarityStatusCursor(parsed)
		if err != nil {
			return SimilarityStatusPage{}, err
		}
		page.NextCursor = &token
	}
	if err := tx.Commit(ctx); err != nil {
		return SimilarityStatusPage{}, err
	}
	return page, nil
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
