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
	Source          string             `json:"source"`
	Artist          string             `json:"artist"`
	Hash            string             `json:"hash"`
	FileSize        int64              `json:"file_size"`
	CreatedAt       string             `json:"created_at"`
	TagVersion      int                `json:"tag_version"`
	Favorite        bool               `json:"favorite"`
	Score           int                `json:"score"`
	ReactionVersion int                `json:"reaction_version"`
	History         []Revision         `json:"history"`
	ReactionHistory []ReactionRevision `json:"reaction_history"`
}

// Revision is an immutable tag-edit entry exposed by the Inspector.
type Revision struct {
	Version     int      `json:"version"`
	Kind        string   `json:"kind"`
	AddedTags   []string `json:"added_tags"`
	RemovedTags []string `json:"removed_tags"`
	CreatedAt   string   `json:"created_at"`
}

// ReactionRevision is an immutable favorite/score entry exposed by the
// Inspector. Reaction history is kept separate from tag history so each
// version sequence can remain optimistic and independently editable.
type ReactionRevision struct {
	Version   int    `json:"version"`
	Favorite  bool   `json:"favorite"`
	Score     int    `json:"score"`
	CreatedAt string `json:"created_at"`
}

type TagTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type TagEditRequest struct {
	Posts  []TagTarget `json:"posts"`
	Add    []string    `json:"add"`
	Remove []string    `json:"remove"`
}

type TagEditResult struct {
	ID      string   `json:"id"`
	Version int      `json:"version"`
	Tags    []string `json:"tags"`
	Changed bool     `json:"changed"`
}

type TagEditResponse struct {
	Posts []TagEditResult `json:"posts"`
}

type ReactionTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type ReactionRequest struct {
	Posts    []ReactionTarget `json:"posts"`
	Favorite *bool            `json:"favorite"`
	Score    *int             `json:"score"`
}

type ReactionResult struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Favorite bool   `json:"favorite"`
	Score    int    `json:"score"`
	Changed  bool   `json:"changed"`
}

type ReactionResponse struct {
	Posts []ReactionResult `json:"posts"`
}

// PostReaction* aliases keep the contract discoverable to callers that use
// the longer resource name.
type PostReactionTarget = ReactionTarget
type PostReactionRequest = ReactionRequest
type PostReactionResult = ReactionResult
type PostReactionResponse = ReactionResponse

// SearchPage is the response body for GET /api/posts. NextCursor is null
// when there is no next page. Empty queries browse newest posts.
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

// TagMutator is the storage boundary for atomic optimistic tag edits.
type TagMutator interface {
	EditTags(ctx context.Context, request TagEditRequest) (TagEditResponse, error)
}

// ReactionMutator is the storage boundary for atomic optimistic favorite and
// score edits.
type ReactionMutator interface {
	EditReactions(ctx context.Context, request ReactionRequest) (ReactionResponse, error)
}
