package posts

import (
	"context"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

// AccountStore returns the account store over the backend's shared pool.
// It never opens a second pool.
func (s *PostgresBackend) AccountStore() auth.Store {
	if s == nil || s.pool == nil {
		return nil
	}
	return auth.NewPostgresStore(s.pool)
}

// Ping verifies the backend database is reachable. Private mode uses it
// to fail closed at startup instead of serving a gated library whose
// account store cannot answer.
func (s *PostgresBackend) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	return s.pool.Ping(ctx)
}

// UsesExternalMediaProvider reports whether writers would persist object
// URLs outside the authenticated /media handler. Same-origin S3 storage is
// not external. A public CDN or endpoint URL is.
func (s *PostgresBackend) UsesExternalMediaProvider() bool {
	if s == nil {
		return false
	}
	if s.store != nil {
		return media.PublishesExternalObjectURLs(s.store)
	}
	cfg, ok := media.S3ConfigFromEnv()
	if !ok {
		return false
	}
	if media.PrivateLibraryEnabled() {
		return false
	}
	prefix := strings.TrimRight(strings.TrimSpace(cfg.URLPrefix), "/")
	return prefix != media.SameOriginMediaPrefix
}

// HasExternalMediaURLs rejects persisted media outside the local gated
// namespace, including protocol-relative and mixed-case external URLs.
func (s *PostgresBackend) HasExternalMediaURLs(ctx context.Context) (bool, error) {
	if s == nil || s.pool == nil {
		return false, ErrUnavailable
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM posts
  WHERE original_url NOT LIKE '/media/%'
     OR (preview_url NOT LIKE '/media/%' AND preview_url !~ '^/static/icons/[A-Za-z0-9_.-]+\.svg$')
  UNION ALL
  SELECT 1 FROM asset_variants
  WHERE url NOT LIKE '/media/%'
)`).Scan(&exists)
	return exists, err
}
