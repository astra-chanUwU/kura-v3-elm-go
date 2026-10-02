package posts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresSearcher executes the browser-facing post summary query against
// PostgreSQL. It keeps the pool behind the Searcher interface used by HTTP.
type PostgresSearcher struct {
	pool *pgxpool.Pool
}

// NewPostgresSearcher creates a searcher for databaseURL. The pool connects on
// demand, so a server can still start and serve /health while PostgreSQL is
// unavailable.
func NewPostgresSearcher(ctx context.Context, databaseURL string) (*PostgresSearcher, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &PostgresSearcher{pool: pool}, nil
}

// Close releases PostgreSQL connections owned by the searcher.
func (s *PostgresSearcher) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// SearchPosts returns summaries in the stable API shape and uses id keyset
// pagination to avoid offset work as the result set grows.
func (s *PostgresSearcher) SearchPosts(ctx context.Context, query string, cursor *Cursor, limit int) (SearchPage, error) {
	if s == nil || s.pool == nil {
		return SearchPage{}, ErrUnavailable
	}
	if limit < 1 || limit > 60 {
		return SearchPage{}, ErrInvalidQuery
	}

	var cursorID any
	if cursor != nil {
		cursorID = cursor.ID
	}
	page := SearchPage{Posts: make([]PostSummary, 0, limit)}
	rows, err := s.pool.Query(ctx, SearchPostsSQL, query, cursorID, limit+1)
	if err != nil {
		return SearchPage{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var post PostSummary
		if err := rows.Scan(
			&post.ID,
			&post.PreviewURL,
			&post.OriginalURL,
			&post.MediaType,
			&post.Width,
			&post.Height,
			&post.Tags,
		); err != nil {
			return SearchPage{}, err
		}
		page.Posts = append(page.Posts, post)
	}
	if err := rows.Err(); err != nil {
		return SearchPage{}, err
	}
	if len(page.Posts) <= limit {
		return page, nil
	}
	page.Posts = page.Posts[:limit]
	lastID := page.Posts[len(page.Posts)-1].ID
	// Postgres IDs are positive; malformed fixture data cannot create a token.
	var id int64
	if _, err := fmt.Sscan(lastID, &id); err != nil || id <= 0 {
		return SearchPage{}, ErrInvalidQuery
	}
	token, err := EncodeCursor(query, id)
	if err != nil {
		return SearchPage{}, err
	}
	page.NextCursor = &token
	return page, nil
}

// GetPostDetail returns visible post metadata for the Inspector. Deleted rows
// are intentionally indistinguishable from missing rows.
func (s *PostgresSearcher) GetPostDetail(ctx context.Context, id string) (PostDetail, error) {
	if s == nil || s.pool == nil {
		return PostDetail{}, ErrUnavailable
	}
	var detail PostDetail
	err := s.pool.QueryRow(ctx, GetPostDetailSQL, id).Scan(
		&detail.ID,
		&detail.PreviewURL,
		&detail.OriginalURL,
		&detail.MediaType,
		&detail.Width,
		&detail.Height,
		&detail.Source,
		&detail.Artist,
		&detail.Hash,
		&detail.FileSize,
		&detail.CreatedAt,
		&detail.Tags,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostDetail{}, ErrNotFound
	}
	if err != nil {
		return PostDetail{}, err
	}
	return detail, nil
}
