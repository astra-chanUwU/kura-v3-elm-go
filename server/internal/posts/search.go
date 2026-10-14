// Package posts contains the browser-facing post contracts.
package posts

import (
	"context"
	"io"
)

// PostSummary is the stable, small representation used by MediaGrid.
//
// The API deliberately exposes media URLs and display metadata instead of a
// database row. Additional post details belong behind a detail endpoint.
type PostSummary struct {
	ID            string   `json:"id"`
	PreviewURL    string   `json:"preview_url"`
	OriginalURL   string   `json:"original_url"`
	MediaType     string   `json:"media_type"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Tags          []string `json:"tags"`
	Rating        Rating   `json:"rating"`
	RatingVersion int      `json:"rating_version"`
}

// PostDetail is the stable read-only representation used by the Inspector.
//
// DerivativeJobID and DerivativeStatus are additive derivative metadata:
// the durable job backing the post's thumbnail and its current status.
// While the thumbnail is processing the preview URL serves the original;
// once the thumb-320 variant is ready the preview URL serves it, while
// the original URL never changes.
type PostDetail struct {
	PostSummary
	Source           string             `json:"source"`
	Artist           string             `json:"artist"`
	Hash             string             `json:"hash"`
	FileSize         int64              `json:"file_size"`
	CreatedAt        string             `json:"created_at"`
	TagVersion       int                `json:"tag_version"`
	MetadataVersion  int                `json:"metadata_version"`
	RatingHistory    []RatingRevision   `json:"rating_history"`
	Favorite         bool               `json:"favorite"`
	Score            int                `json:"score"`
	ReactionVersion  int                `json:"reaction_version"`
	History          []Revision         `json:"history"`
	ReactionHistory  []ReactionRevision `json:"reaction_history"`
	MetadataHistory  []MetadataRevision `json:"metadata_history"`
	DerivativeJobID  string             `json:"derivative_job_id,omitempty"`
	DerivativeStatus string             `json:"derivative_status,omitempty"`
}

// Revision is an immutable tag-edit entry exposed by the Inspector.
// TargetTags preserves NULL versus valid empty tags: nil encodes as JSON null
// (legacy revision without complete state) while non-nil empty slice encodes as [].
// Revertible is additive metadata derived from target state presence.
type Revision struct {
	Version     int       `json:"version"`
	Kind        string    `json:"kind"`
	AddedTags   []string  `json:"added_tags"`
	RemovedTags []string  `json:"removed_tags"`
	TargetTags  *[]string `json:"target_tags"`
	Revertible  bool      `json:"revertible"`
	CreatedAt   string    `json:"created_at"`
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

// MetadataRevision is an immutable source/artist edit entry exposed by the
// Inspector. Metadata versions are independent of tag and reaction versions.
type MetadataRevision struct {
	Version        int    `json:"version"`
	PreviousSource string `json:"previous_source"`
	PreviousArtist string `json:"previous_artist"`
	Source         string `json:"source"`
	Artist         string `json:"artist"`
	CreatedAt      string `json:"created_at"`
}

// RatingRevision is an immutable content-rating change shown in the Inspector.
type RatingRevision struct {
	Version        int    `json:"version"`
	PreviousRating Rating `json:"previous_rating"`
	Rating         Rating `json:"rating"`
	CreatedAt      string `json:"created_at"`
}

type Rating string

const (
	RatingUnrated      Rating = "unrated"
	RatingSafe         Rating = "safe"
	RatingQuestionable Rating = "questionable"
	RatingExplicit     Rating = "explicit"
)

func (r Rating) Valid() bool {
	switch r {
	case RatingUnrated, RatingSafe, RatingQuestionable, RatingExplicit:
		return true
	default:
		return false
	}
}

// RatingEdit uses a pointer so absent and explicit-null versions are rejected
// while JSON integer zero remains a valid initial version.
type RatingEdit struct {
	ID      string `json:"id"`
	Version *int   `json:"version"`
}

type RatingRequest struct {
	Posts  []RatingEdit `json:"posts"`
	Rating Rating       `json:"rating"`
}

type RatingOutcome struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Rating  Rating `json:"rating"`
	Changed bool   `json:"changed"`
}

type RatingResult struct {
	Posts []RatingOutcome `json:"posts"`
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

type TagRevertRequest struct {
	Posts         []TagTarget `json:"posts"`
	TargetVersion int         `json:"target_version"`
}

type TagRevertResponse struct {
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

// MetadataTarget identifies one post for a versioned source/artist edit.
type MetadataTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// MetadataRequest applies the supplied source and/or artist to every target.
// Nil fields are omitted; a pointer to an empty string clears that field.
type MetadataRequest struct {
	Posts  []MetadataTarget `json:"posts"`
	Source *string          `json:"source,omitempty"`
	Artist *string          `json:"artist,omitempty"`
}

type MetadataResult struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Source  string `json:"source"`
	Artist  string `json:"artist"`
	Changed bool   `json:"changed"`
}

type MetadataResponse struct {
	Posts []MetadataResult `json:"posts"`
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

// Searcher is the narrow storage boundary for post summary queries. The
// PostgreSQL backend also implements other capability interfaces, while this
// contract keeps the HTTP search handler independent of those capabilities.
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

// TagReverter restores an immutable tag revision atomically.
type TagReverter interface {
	RevertTags(ctx context.Context, request TagRevertRequest) (TagRevertResponse, error)
}

// ReactionMutator is the storage boundary for atomic optimistic favorite and
// score edits.
type ReactionMutator interface {
	EditReactions(ctx context.Context, request ReactionRequest) (ReactionResponse, error)
}

// MetadataMutator applies atomic optimistic source and artist edits.
type MetadataMutator interface {
	UpdateMetadata(ctx context.Context, request MetadataRequest) (MetadataResponse, error)
}

// RatingMutator applies an atomic optimistic content-rating edit.
type RatingMutator interface {
	UpdateRatings(ctx context.Context, request RatingRequest) (RatingResult, error)
}

// UploadRequest is the storage-facing contract for a bounded local upload.
// The HTTP layer owns multipart parsing; the adapter owns durable media and
// post creation.
type UploadRequest struct {
	File     io.Reader
	Filename string
	Tags     []string
	Source   string
	Artist   string
}

// UploadCreator creates a visible post and stores its original media.
type UploadCreator interface {
	CreateUpload(ctx context.Context, request UploadRequest) (PostDetail, error)
}
