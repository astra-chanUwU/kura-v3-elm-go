package posts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const maxCollectionNameLength = 120
const maxCollectionPosts = 200

type Collection struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	PostIDs []string `json:"post_ids"`
	// Version is the membership/order version: every effective
	// add/remove/reorder increments it, no-ops leave it unchanged.
	Version int64 `json:"version"`
}

type CreateCollectionRequest struct {
	Name string `json:"name"`
}

type AddCollectionPostsRequest struct {
	PostIDs []string `json:"post_ids"`
}

type ReorderCollectionRequest struct {
	PostIDs []string `json:"post_ids"`
}

type CollectionReader interface {
	ListCollections(context.Context) ([]Collection, error)
}

// CollectionPostsPage is the response body for
// GET /api/collections/{id}/posts. NextCursor is null when there is no
// next page. CollectionVersion is the membership/order version observed by
// the same database snapshot that produced the page; clients echo it back
// inside the opaque cursor so concurrent changes surface as conflicts
// instead of silent gaps or duplicates.
type CollectionPostsPage struct {
	Posts             []PostSummary `json:"posts"`
	NextCursor        *string       `json:"next_cursor"`
	CollectionVersion int64         `json:"collection_version"`
}

// CollectionPostSearcher returns visible collection members in their saved
// collection order using versioned keyset pagination. A nil cursor starts
// from the beginning; a non-nil cursor continues after its (position,
// post_id) key at the exact collection version it was issued for.
type CollectionPostSearcher interface {
	SearchCollectionPosts(context.Context, string, *CollectionCursor, int) (CollectionPostsPage, error)
}

type CollectionMutator interface {
	CollectionReader
	CreateCollection(context.Context, CreateCollectionRequest) (Collection, error)
	AddCollectionPosts(context.Context, string, AddCollectionPostsRequest) (Collection, error)
	RemoveCollectionPost(context.Context, string, string) error
	ReorderCollection(context.Context, string, ReorderCollectionRequest) (Collection, error)
}

func ValidateCreateCollection(request CreateCollectionRequest) (CreateCollectionRequest, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" || len([]rune(name)) > maxCollectionNameLength {
		return CreateCollectionRequest{}, fmt.Errorf("%w: name must contain 1 to %d characters", ErrInvalidCollections, maxCollectionNameLength)
	}
	return CreateCollectionRequest{Name: name}, nil
}

func ValidateAddCollectionPosts(request AddCollectionPostsRequest) (AddCollectionPostsRequest, error) {
	if len(request.PostIDs) == 0 || len(request.PostIDs) > maxCollectionPosts {
		return AddCollectionPostsRequest{}, fmt.Errorf("%w: post_ids must contain 1 to %d ids", ErrInvalidCollections, maxCollectionPosts)
	}
	seen := make(map[string]struct{}, len(request.PostIDs))
	ids := make([]string, len(request.PostIDs))
	for i, raw := range request.PostIDs {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
			return AddCollectionPostsRequest{}, fmt.Errorf("%w: post_ids[%d] must be a positive integer", ErrInvalidCollections, i)
		}
		if _, ok := seen[raw]; ok {
			return AddCollectionPostsRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidCollections, raw)
		}
		seen[raw] = struct{}{}
		ids[i] = raw
	}
	return AddCollectionPostsRequest{PostIDs: ids}, nil
}

func ValidateReorderCollection(request ReorderCollectionRequest) (ReorderCollectionRequest, error) {
	// No length cap here: maxCollectionPosts bounds a single add request,
	// not the total collection size, and reorder carries full membership.
	// Oversized reorder payloads are still bounded by decodeJSONBody.
	seen := make(map[string]struct{}, len(request.PostIDs))
	ids := make([]string, len(request.PostIDs))
	for i, raw := range request.PostIDs {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
			return ReorderCollectionRequest{}, fmt.Errorf("%w: post_ids[%d] must be a positive integer", ErrInvalidCollections, i)
		}
		if _, ok := seen[raw]; ok {
			return ReorderCollectionRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidCollections, raw)
		}
		seen[raw] = struct{}{}
		ids[i] = raw
	}
	return ReorderCollectionRequest{PostIDs: ids}, nil
}
