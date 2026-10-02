// Package posts contains the browser-facing post contracts.
package posts

import "context"

// PostSummary is the stable, small representation used by MediaGrid.
//
// The API deliberately exposes media URLs and display metadata instead of a
// database row. Additional post details belong behind a detail endpoint.
type PostSummary struct {
	ID          string   `json:"id"`
	PreviewURL  string   `json:"preview_url"`
	OriginalURL string   `json:"original_url"`
	MediaType   string   `json:"media_type"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Tags        []string `json:"tags"`
}

// PostDetail is the stable read-only representation used by the Inspector.
type PostDetail struct {
	PostSummary
	Source    string `json:"source"`
	Artist    string `json:"artist"`
	Hash      string `json:"hash"`
	FileSize  int64  `json:"file_size"`
	CreatedAt string `json:"created_at"`
}

// SearchPage is the response body for GET /api/posts. NextCursor is null
// when there is no next page; an empty query always returns a nil cursor.
type SearchPage struct {
	Posts      []PostSummary `json:"posts"`
	NextCursor *string       `json:"next_cursor"`
}

// SearchResponse remains an alias for callers that used the first slice's
// envelope. New code should use SearchPage.
type SearchResponse = SearchPage

// Searcher is the storage boundary used by the HTTP layer. PostgreSQL-backed
// implementations belong in a separate adapter and must return PostSummary
// values rather than leaking storage rows into the API.
type Searcher interface {
	SearchPosts(ctx context.Context, query string, cursor *Cursor, limit int) (SearchPage, error)
}

// PostDetailer is the storage boundary for read-only Inspector metadata.
type PostDetailer interface {
	GetPostDetail(ctx context.Context, id string) (PostDetail, error)
}
