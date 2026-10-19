package posts

import (
	"context"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/health"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

const healthCountSQL = `
SELECT
    (SELECT count(*) FROM posts p
        WHERE p.deleted_at IS NULL
          AND NOT EXISTS (
            SELECT 1 FROM asset_variants v
            WHERE v.post_id = p.id AND v.variant = 'thumb-320' AND v.status = 'ready'
          )),
    (SELECT count(*) FROM jobs WHERE kind = 'derivative' AND status = 'failed'),
    (SELECT count(*) FROM jobs WHERE status IN ('pending', 'running'))
`

const healthOriginalsSQL = `
SELECT id::text, original_url
FROM posts
WHERE deleted_at IS NULL
ORDER BY id
LIMIT $1
`

const healthOriginalKeySQL = `
SELECT original_url
FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL
`

const healthMissingThumbsSQL = `
SELECT id::text
FROM posts p
WHERE p.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM asset_variants v
    WHERE v.post_id = p.id AND v.variant = 'thumb-320' AND v.status = 'ready'
  )
ORDER BY p.id
LIMIT $1
`

func (s *PostgresBackend) Counts(ctx context.Context) (health.Counts, error) {
	var counts health.Counts
	if s == nil || s.pool == nil {
		return counts, health.ErrUnavailable
	}
	err := s.pool.QueryRow(ctx, healthCountSQL).Scan(&counts.MissingThumbnails, &counts.FailedThumbnails, &counts.Backlog)
	return counts, err
}

func (s *PostgresBackend) Originals(ctx context.Context, limit int) ([]health.Original, error) {
	if s == nil || s.pool == nil {
		return nil, health.ErrUnavailable
	}
	if limit < 1 {
		limit = 1
	}
	rows, err := s.pool.Query(ctx, healthOriginalsSQL, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []health.Original
	for rows.Next() {
		var id, reference string
		if err := rows.Scan(&id, &reference); err != nil {
			return nil, err
		}
		key, _ := media.KeyFromStoredMediaURL(reference)
		out = append(out, health.Original{PostID: id, Key: key})
	}
	return out, rows.Err()
}

func (s *PostgresBackend) OriginalKey(ctx context.Context, postID string) (string, error) {
	if s == nil || s.pool == nil {
		return "", health.ErrUnavailable
	}
	var reference string
	if err := s.pool.QueryRow(ctx, healthOriginalKeySQL, postID).Scan(&reference); err != nil {
		return "", health.ErrUnavailable
	}
	key, err := media.KeyFromStoredMediaURL(reference)
	if err != nil {
		return "", health.ErrUnavailable
	}
	return key, nil
}

func (s *PostgresBackend) MissingThumbnails(ctx context.Context, limit int) (health.Page, error) {
	page := health.Page{PostIDs: []string{}}
	if s == nil || s.pool == nil {
		return page, health.ErrUnavailable
	}
	if limit < 1 {
		limit = 1
	}
	rows, err := s.pool.Query(ctx, healthMissingThumbsSQL, limit)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return page, err
		}
		page.PostIDs = append(page.PostIDs, id)
	}
	return page, rows.Err()
}
