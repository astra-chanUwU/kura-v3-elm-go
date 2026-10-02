// Package posts contains the browser-facing post contracts.
package posts

import "context"

// PostSummary is the stable, small representation used by MediaGrid.
//
// The API deliberately exposes media URLs and display metadata instead of a
// database row. Additional post details belong behind a detail endpoint.
type PostSummary struct {
	ID          string `json:"id"`
	PreviewURL  string `json:"preview_url"`
	OriginalURL string `json:"original_url"`
	MediaType   string `json:"media_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

// SearchResponse is the response body for GET /api/posts.
//
// Pagination is intentionally left out of this first slice. A cursor can be
// added to this envelope without changing individual PostSummary values.
type SearchResponse struct {
	Posts []PostSummary `json:"posts"`
}

// Searcher is the storage boundary used by the HTTP layer. PostgreSQL-backed
// implementations belong in a separate adapter and must return PostSummary
// values rather than leaking storage rows into the API.
type Searcher interface {
	SearchPosts(ctx context.Context, query string) (SearchResponse, error)
}
