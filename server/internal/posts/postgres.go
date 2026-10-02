package posts

import (
	"context"

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

// SearchPosts returns summaries in the stable API shape.
func (s *PostgresSearcher) SearchPosts(ctx context.Context, query string) (SearchResponse, error) {
	if s == nil || s.pool == nil {
		return SearchResponse{}, ErrUnavailable
	}

	response := SearchResponse{Posts: make([]PostSummary, 0)}
	rows, err := s.pool.Query(ctx, SearchPostsSQL, query)
	if err != nil {
		return SearchResponse{}, err
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
		); err != nil {
			return SearchResponse{}, err
		}
		response.Posts = append(response.Posts, post)
	}
	if err := rows.Err(); err != nil {
		return SearchResponse{}, err
	}

	return response, nil
}
