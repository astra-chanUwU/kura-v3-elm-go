package client

// PostSummary is the stable browser-facing representation from GET /api/posts.
type PostSummary struct {
	ID          string   `json:"id"`
	PreviewURL  string   `json:"preview_url"`
	OriginalURL string   `json:"original_url"`
	MediaType   string   `json:"media_type"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Tags        []string `json:"tags"`
}

// SearchResponse is the envelope for GET /api/posts.
// next_cursor is optional and forwards pagination when the API adds it.
// collection_version is present only on collection page responses.
type SearchResponse struct {
	Posts             []PostSummary `json:"posts"`
	NextCursor        *string       `json:"next_cursor,omitempty"`
	CollectionVersion *int64        `json:"collection_version,omitempty"`
}

// Collection is the stable browser-facing representation from
// GET /api/collections. Version is the membership/order version and is
// zero for responses from servers that predate versioned pagination.
type Collection struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	PostIDs []string `json:"post_ids"`
	Version int64    `json:"version,omitempty"`
}

// CollectionsResponse is the envelope for GET /api/collections.
type CollectionsResponse struct {
	Collections []Collection `json:"collections"`
}

type CollectionSummary struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          int64  `json:"version"`
	MemberCount      int64  `json:"member_count"`
	VisiblePostCount int64  `json:"visible_post_count"`
}

type CollectionSummariesResponse struct {
	Collections []CollectionSummary `json:"collections"`
}

type CollectionManageRequest struct {
	Name            string `json:"name,omitempty"`
	ExpectedVersion int64  `json:"expected_version"`
}

type CollectionManageResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
	Changed bool   `json:"changed"`
}

type MoveCollectionPostRequest struct {
	PostID          string  `json:"post_id"`
	BeforePostID    *string `json:"before_post_id"`
	ExpectedVersion int64   `json:"expected_version"`
}

type MoveCollectionPostResult struct {
	ID           string `json:"id"`
	CollectionID string `json:"collection_id"`
	Version      int64  `json:"version"`
	Changed      bool   `json:"changed"`
}

// CreateCollectionRequest is the payload for POST /api/collections.
type CreateCollectionRequest struct {
	Name string `json:"name"`
}

// AddCollectionPostsRequest is the payload for
// POST /api/collections/{id}/posts.
type AddCollectionPostsRequest struct {
	PostIDs []string `json:"post_ids"`
}

// ReorderCollectionRequest is the payload for
// POST /api/collections/{id}/order.
type ReorderCollectionRequest struct {
	PostIDs []string `json:"post_ids"`
}

// PostDetail is the stable read-only representation from GET /api/posts/{id}.
// It extends PostSummary with Inspector metadata and revision histories.
type PostDetail struct {
	PostSummary
	Source           string             `json:"source"`
	Artist           string             `json:"artist"`
	Hash             string             `json:"hash"`
	FileSize         int64              `json:"file_size"`
	CreatedAt        string             `json:"created_at"`
	TagVersion       int                `json:"tag_version"`
	Favorite         bool               `json:"favorite"`
	Score            int                `json:"score"`
	ReactionVersion  int                `json:"reaction_version"`
	History          []Revision         `json:"history"`
	ReactionHistory  []ReactionRevision `json:"reaction_history"`
	DerivativeJobID  string             `json:"derivative_job_id,omitempty"`
	DerivativeStatus string             `json:"derivative_status,omitempty"`
}

// Revision is an immutable tag-edit entry.
type Revision struct {
	Version     int       `json:"version"`
	Kind        string    `json:"kind"`
	AddedTags   []string  `json:"added_tags"`
	RemovedTags []string  `json:"removed_tags"`
	TargetTags  *[]string `json:"target_tags"`
	Revertible  bool      `json:"revertible"`
	CreatedAt   string    `json:"created_at"`
}

// ReactionRevision is an immutable favorite/score entry.
type ReactionRevision struct {
	Version   int    `json:"version"`
	Favorite  bool   `json:"favorite"`
	Score     int    `json:"score"`
	CreatedAt string `json:"created_at"`
}

// TagTarget identifies a post with its expected tag version.
type TagTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// TagEditRequest is the payload for POST /api/posts/tags.
type TagEditRequest struct {
	Posts  []TagTarget `json:"posts"`
	Add    []string    `json:"add"`
	Remove []string    `json:"remove"`
}

// TagEditResult is one post result from a tag mutation.
type TagEditResult struct {
	ID      string   `json:"id"`
	Version int      `json:"version"`
	Tags    []string `json:"tags"`
	Changed bool     `json:"changed"`
}

// TagEditResponse is the envelope for tag mutations.
type TagEditResponse struct {
	Posts []TagEditResult `json:"posts"`
}

// TagRevertRequest is the payload for POST /api/posts/tags/revert.
type TagRevertRequest struct {
	Posts         []TagTarget `json:"posts"`
	TargetVersion int         `json:"target_version"`
}

// TagRevertResponse is the envelope for tag reverts.
type TagRevertResponse struct {
	Posts []TagEditResult `json:"posts"`
}

// ReactionTarget identifies a post with its expected reaction version.
type ReactionTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// ReactionRequest is the payload for POST /api/posts/reactions.
type ReactionRequest struct {
	Posts    []ReactionTarget `json:"posts"`
	Favorite *bool            `json:"favorite,omitempty"`
	Score    *int             `json:"score,omitempty"`
}

// ReactionResult is one post result from a reaction mutation.
type ReactionResult struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Favorite bool   `json:"favorite"`
	Score    int    `json:"score"`
	Changed  bool   `json:"changed"`
}

// ReactionResponse is the envelope for reaction mutations.
type ReactionResponse struct {
	Posts []ReactionResult `json:"posts"`
}
