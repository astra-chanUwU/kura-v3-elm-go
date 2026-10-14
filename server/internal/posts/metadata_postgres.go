package posts

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
)

const lockPostsForMetadataSQL = `
SELECT id::text, source, artist, tags, metadata_version
FROM posts
WHERE id = ANY($1::bigint[])
  AND deleted_at IS NULL
  AND moderation_state = 'published'
ORDER BY posts.id
FOR UPDATE
`

const updatePostMetadataSQL = `
UPDATE posts
SET source = $1, artist = $2, search_text = $3, metadata_version = $4
WHERE id = $5::bigint AND deleted_at IS NULL AND moderation_state = 'published'
`

const insertPostMetadataRevisionSQL = `
INSERT INTO post_metadata_revisions
    (post_id, version, previous_source, previous_artist, source, artist)
VALUES ($1::bigint, $2, $3, $4, $5, $6)
`

type currentMetadata struct {
	id      string
	source  string
	artist  string
	tags    []string
	version int
}

// UpdateMetadata atomically changes source and/or artist for a bounded set of
// visible posts, checking independent optimistic versions and appending one
// immutable history row for every changed post.
func (s *PostgresBackend) UpdateMetadata(ctx context.Context, request MetadataRequest) (MetadataResponse, error) {
	if s == nil || s.pool == nil {
		return MetadataResponse{}, ErrUnavailable
	}
	validated, err := ValidateMetadataRequest(request)
	if err != nil {
		return MetadataResponse{}, err
	}
	ids := make([]int64, 0, len(validated.Posts))
	for _, target := range validated.Posts {
		var id int64
		id, _ = strconv.ParseInt(target.ID, 10, 64) // validated above
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MetadataResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, lockPostsForMetadataSQL, ids)
	if err != nil {
		return MetadataResponse{}, err
	}
	current := make(map[string]currentMetadata, len(ids))
	for rows.Next() {
		var row currentMetadata
		if err := rows.Scan(&row.id, &row.source, &row.artist, &row.tags, &row.version); err != nil {
			rows.Close()
			return MetadataResponse{}, err
		}
		current[row.id] = row
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return MetadataResponse{}, rowsErr
	}
	if len(current) != len(validated.Posts) {
		return MetadataResponse{}, ErrNotFound
	}

	for _, target := range validated.Posts {
		row := current[target.ID]
		if row.version != target.Version {
			return MetadataResponse{}, ErrMetadataConflict
		}
	}

	result := MetadataResponse{Posts: make([]MetadataResult, 0, len(validated.Posts))}
	for _, target := range validated.Posts {
		row := current[target.ID]
		nextSource, nextArtist := row.source, row.artist
		if validated.Source != nil {
			nextSource = *validated.Source
		}
		if validated.Artist != nil {
			nextArtist = *validated.Artist
		}
		item := MetadataResult{ID: target.ID, Version: row.version, Source: row.source, Artist: row.artist}
		if nextSource == row.source && nextArtist == row.artist {
			result.Posts = append(result.Posts, item)
			continue
		}
		if row.version == math.MaxInt {
			return MetadataResponse{}, ErrInvalidMetadata
		}
		nextVersion := row.version + 1
		searchText := canonicalSearchText(row.tags, nextSource, nextArtist)
		if _, err := tx.Exec(ctx, updatePostMetadataSQL, nextSource, nextArtist, searchText, nextVersion, target.ID); err != nil {
			return MetadataResponse{}, err
		}
		if _, err := tx.Exec(ctx, insertPostMetadataRevisionSQL, target.ID, nextVersion, row.source, row.artist, nextSource, nextArtist); err != nil {
			return MetadataResponse{}, err
		}
		item.Version, item.Source, item.Artist, item.Changed = nextVersion, nextSource, nextArtist, true
		result.Posts = append(result.Posts, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return MetadataResponse{}, err
	}
	return result, nil
}

func canonicalSearchText(tags []string, source, artist string) string {
	parts := make([]string, 0, len(tags)+2)
	for _, tag := range tags {
		if tag != "" {
			parts = append(parts, tag)
		}
	}
	if source != "" {
		parts = append(parts, source)
	}
	if artist != "" {
		parts = append(parts, artist)
	}
	return strings.Join(parts, " ")
}
