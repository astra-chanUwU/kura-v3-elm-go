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

// MoveCollectionPostRequest moves one member before BeforePostID, or to
// the end when BeforePostID is null. ExpectedVersion is the collection
// version the caller observed; a mismatch fails with ErrCollectionChanged.
type MoveCollectionPostRequest struct {
	PostID          string  `json:"post_id"`
	BeforePostID    *string `json:"before_post_id"`
	ExpectedVersion int64   `json:"expected_version"`
}

// MoveCollectionPostResult is the compact move response: it never echoes
// the full member list, so large collections stay cheap to reorder.
type MoveCollectionPostResult struct {
	ID           string `json:"id"`
	CollectionID string `json:"collection_id"`
	Version      int64  `json:"version"`
	Changed      bool   `json:"changed"`
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

// CollectionPostMover is the narrow, independent capability for moving a
// single collection member. It is deliberately not part of
// CollectionMutator so backends predating the move endpoint keep serving
// the existing collection routes and only the move route is unavailable.
type CollectionPostMover interface {
	MoveCollectionPost(context.Context, string, MoveCollectionPostRequest) (MoveCollectionPostResult, error)
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

func ValidateMoveCollectionPostRequest(request MoveCollectionPostRequest) (MoveCollectionPostRequest, error) {
	if request.ExpectedVersion < 0 {
		return MoveCollectionPostRequest{}, fmt.Errorf("%w: expected_version must be non-negative", ErrInvalidCollections)
	}
	parsed, err := strconv.ParseInt(request.PostID, 10, 64)
	if err != nil || parsed <= 0 || request.PostID != strconv.FormatInt(parsed, 10) {
		return MoveCollectionPostRequest{}, fmt.Errorf("%w: post_id must be a positive integer", ErrInvalidCollections)
	}
	validated := MoveCollectionPostRequest{PostID: request.PostID, ExpectedVersion: request.ExpectedVersion}
	if request.BeforePostID != nil {
		before := *request.BeforePostID
		parsedBefore, err := strconv.ParseInt(before, 10, 64)
		if err != nil || parsedBefore <= 0 || before != strconv.FormatInt(parsedBefore, 10) {
			return MoveCollectionPostRequest{}, fmt.Errorf("%w: before_post_id must be a positive integer or null", ErrInvalidCollections)
		}
		validated.BeforePostID = &before
	}
	return validated, nil
}

// moveCollectionOrder computes the full new order for a single-post move
// over existing, which must already be in deterministic (position,
// post_id) order and must include every member, visible or not. It
// returns the new order and whether it differs from existing. A missing
// post or neighbor fails with ErrNotFound; moving a post before itself
// or to its current place returns the existing order unchanged.
func moveCollectionOrder(existing []int64, postID int64, beforePostID *int64) ([]int64, bool, error) {
	from := -1
	for i, id := range existing {
		if id == postID {
			from = i
			break
		}
	}
	if from < 0 {
		return nil, false, ErrNotFound
	}
	if beforePostID != nil && *beforePostID == postID {
		return append([]int64(nil), existing...), false, nil
	}
	if beforePostID != nil {
		found := false
		for _, id := range existing {
			if id == *beforePostID {
				found = true
				break
			}
		}
		if !found {
			return nil, false, ErrNotFound
		}
	}
	rest := make([]int64, 0, len(existing)-1)
	rest = append(rest, existing[:from]...)
	rest = append(rest, existing[from+1:]...)
	insertAt := len(rest)
	if beforePostID != nil {
		for i, id := range rest {
			if id == *beforePostID {
				insertAt = i
				break
			}
		}
	}
	next := make([]int64, 0, len(existing))
	next = append(next, rest[:insertAt]...)
	next = append(next, postID)
	next = append(next, rest[insertAt:]...)
	changed := false
	for i := range next {
		if next[i] != existing[i] {
			changed = true
			break
		}
	}
	return next, changed, nil
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
