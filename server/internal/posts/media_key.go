package posts

import (
	"context"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

// StoredMediaKeyResolver maps one stored same-origin media URL to the object
// key recorded by the upload, derivative, or import writer. Implementations
// must not fetch the URL and must not accept a key supplied by the caller.
type StoredMediaKeyResolver interface {
	ResolveStoredMediaKey(ctx context.Context, mediaPath string) (key, mediaType string, err error)
}

// storedWriterMediaSQL reads keys written with the URL. Variant rows come
// from the derivative writer. Originals come from the derivative job payload
// the upload and browser-import writers enqueue in the same transaction.
// A preview that still equals the original uses that same original key; a
// thumbnail URL does not, so it cannot fall through to the original object.
const storedWriterMediaSQL = `
SELECT object_key, media_type
FROM (
    SELECT v.object_key AS object_key, v.media_type AS media_type
    FROM asset_variants v
    WHERE v.url = $1::text
    UNION
    SELECT j.payload->>'original_key' AS object_key, p.media_type AS media_type
    FROM posts p
    JOIN jobs j
      ON j.kind = 'derivative'
     AND j.payload->>'post_id' = p.id::text
    WHERE p.original_url = $1::text
       OR (p.preview_url = $1::text AND p.preview_url = p.original_url)
) stored
WHERE btrim(object_key) <> ''
`

// storedMediaTypeSQL covers same-origin references that use the writer URL
// grammar but have no job or variant row, such as seeded /media/demo files.
const storedMediaTypeSQL = `
SELECT media_type
FROM (
    SELECT media_type FROM posts
    WHERE original_url = $1::text OR preview_url = $1::text
    UNION
    SELECT media_type FROM asset_variants
    WHERE url = $1::text
) stored
WHERE btrim(media_type) <> ''
`

// ResolveStoredMediaKey returns the store key for a persisted /media URL.
// Writer-recorded keys must equal the key encoded by that URL. External
// URLs and traversal are rejected here, before any store is opened.
func (s *PostgresBackend) ResolveStoredMediaKey(ctx context.Context, mediaPath string) (string, string, error) {
	if s == nil || s.pool == nil {
		return "", "", ErrUnavailable
	}
	mapped, err := media.KeyFromStoredMediaURL(mediaPath)
	if err != nil {
		return "", "", ErrNotFound
	}
	rows, err := s.pool.Query(ctx, storedWriterMediaSQL, mediaPath)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	var mediaType string
	found := false
	for rows.Next() {
		var key, kind string
		if err := rows.Scan(&key, &kind); err != nil {
			return "", "", err
		}
		key = strings.TrimSpace(key)
		kind = strings.TrimSpace(kind)
		if key != mapped {
			return "", "", ErrNotFound
		}
		if mediaType == "" {
			mediaType = kind
		} else if kind != "" && kind != mediaType {
			return "", "", ErrNotFound
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	if found {
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		return mapped, mediaType, nil
	}
	typeRows, err := s.pool.Query(ctx, storedMediaTypeSQL, mediaPath)
	if err != nil {
		return "", "", err
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var kind string
		if err := typeRows.Scan(&kind); err != nil {
			return "", "", err
		}
		kind = strings.TrimSpace(kind)
		if kind == "" {
			continue
		}
		if mediaType == "" {
			mediaType = kind
		} else if kind != mediaType {
			return "", "", ErrNotFound
		}
		found = true
	}
	if err := typeRows.Err(); err != nil {
		return "", "", err
	}
	if !found {
		return "", "", ErrNotFound
	}
	return mapped, mediaType, nil
}
