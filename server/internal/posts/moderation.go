package posts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Moderation states. Every post is published by default; only published,
// non-deleted posts are publicly visible. Hidden and rejected posts are
// indistinguishable from missing rows on every read path, including direct
// /media/* access.
const (
	ModerationPublished = "published"
	ModerationPending   = "pending"
	ModerationHidden    = "hidden"
	ModerationRejected  = "rejected"
)

// DefaultModerationState is the moderation_state of pre-moderation rows and
// new uploads.
const DefaultModerationState = ModerationPublished

// Moderation actions and the state each one transitions a post into.
const (
	ModerationActionApprove = "approve"
	ModerationActionPend    = "pend"
	ModerationActionHide    = "hide"
	ModerationActionReject  = "reject"
)

const (
	maxModerationPosts        = 100
	maxModerationReasonLength = 512
	maxModerationActorLength  = 120
)

// ModerationTarget identifies one post to transition. Moderation carries no
// optimistic version: transitions are serialized by the row lock and every
// applied transition appends an immutable audit row.
type ModerationTarget struct {
	ID string `json:"id"`
}

// ModerationRequest is the bounded, server-owned moderation command body.
// Actor travels separately (see ModerationMutator) so the capability owner
// is never taken from the request payload.
type ModerationRequest struct {
	Posts  []ModerationTarget `json:"posts"`
	Action string             `json:"action"`
	Reason string             `json:"reason"`
}

// ModerationResult reports one post's transition. Changed is false when the
// post already held the target state; no audit row is recorded then,
// mirroring the tag-edit no-op convention.
type ModerationResult struct {
	ID            string `json:"id"`
	Action        string `json:"action"`
	PreviousState string `json:"previous_state"`
	NewState      string `json:"new_state"`
	Changed       bool   `json:"changed"`
}

// ModerationResponse is the response body for POST /api/posts/moderation.
type ModerationResponse struct {
	Posts []ModerationResult `json:"posts"`
}

// ModerationAction is one immutable audit entry.
type ModerationAction struct {
	ID            string `json:"id"`
	PostID        string `json:"post_id"`
	Action        string `json:"action"`
	PreviousState string `json:"previous_state"`
	NewState      string `json:"new_state"`
	Actor         string `json:"actor"`
	Reason        string `json:"reason"`
	CreatedAt     string `json:"created_at"`
}

// ModerationQueueItem pairs a queue member's stable summary with the state
// it was queued under.
type ModerationQueueItem struct {
	PostSummary
	ModerationState string `json:"moderation_state"`
}

// ModerationQueuePage is the response body for GET /api/posts/moderation.
// Queues are bounded and newest-first with no cursor.
type ModerationQueuePage struct {
	Posts []ModerationQueueItem `json:"posts"`
}

// MediaVisibility describes how a /media/* path maps to post rows.
type MediaVisibility struct {
	// Referenced is true when at least one post row references the path,
	// including soft-deleted rows.
	Referenced bool
	// Visible is true when at least one non-deleted post referencing the
	// path is published. Unreferenced paths are not post media.
	Visible bool
}

// ModerationMutator applies capability-checked moderation transitions.
type ModerationMutator interface {
	ModeratePosts(ctx context.Context, actor string, request ModerationRequest) (ModerationResponse, error)
}

// ModerationReader serves the capability-checked moderation queue and the
// per-post audit history.
type ModerationReader interface {
	ListModerationActions(ctx context.Context, postID string) ([]ModerationAction, error)
	ListModerationQueue(ctx context.Context, state string, limit int) (ModerationQueuePage, error)
}

// MediaVisibilityChecker maps a /media/* URL path to post visibility so
// hidden, rejected, or deleted media never leaks through direct URLs.
type MediaVisibilityChecker interface {
	LookupMediaVisibility(ctx context.Context, mediaPath string) (MediaVisibility, error)
}

// ValidModerationState reports whether state is a known moderation state.
func ValidModerationState(state string) bool {
	switch state {
	case ModerationPublished, ModerationPending, ModerationHidden, ModerationRejected:
		return true
	default:
		return false
	}
}

// IsPubliclyVisibleModerationState is the single centralized visibility
// predicate behind search, post detail, collections, tag/reaction mutations,
// and direct media access.
func IsPubliclyVisibleModerationState(state string) bool {
	return state == ModerationPublished
}

// ModerationStateForAction maps an action name to its target state.
func ModerationStateForAction(action string) (string, bool) {
	switch action {
	case ModerationActionApprove:
		return ModerationPublished, true
	case ModerationActionPend:
		return ModerationPending, true
	case ModerationActionHide:
		return ModerationHidden, true
	case ModerationActionReject:
		return ModerationRejected, true
	default:
		return "", false
	}
}

// ValidateModerationRequest normalizes ids, action, and reason. It is shared
// by HTTP and storage-facing callers so invalid commands never open a
// transaction.
func ValidateModerationRequest(request ModerationRequest) (ModerationRequest, error) {
	if len(request.Posts) == 0 || len(request.Posts) > maxModerationPosts {
		return ModerationRequest{}, fmt.Errorf("%w: posts must contain 1 to 100 targets", ErrInvalidModeration)
	}
	seen := make(map[string]struct{}, len(request.Posts))
	ids := make([]ModerationTarget, len(request.Posts))
	for index, target := range request.Posts {
		id := strings.TrimSpace(target.ID)
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 || id != strconv.FormatInt(parsed, 10) {
			return ModerationRequest{}, fmt.Errorf("%w: posts[%d].id must be a positive integer", ErrInvalidModeration, index)
		}
		if _, duplicate := seen[id]; duplicate {
			return ModerationRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidModeration, id)
		}
		seen[id] = struct{}{}
		ids[index] = ModerationTarget{ID: id}
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if _, ok := ModerationStateForAction(action); !ok {
		return ModerationRequest{}, fmt.Errorf("%w: action must be one of approve, pend, hide, reject", ErrInvalidModeration)
	}
	reason := strings.TrimSpace(request.Reason)
	if utf8.RuneCountInString(reason) > maxModerationReasonLength {
		return ModerationRequest{}, fmt.Errorf("%w: reason must be at most %d characters", ErrInvalidModeration, maxModerationReasonLength)
	}
	return ModerationRequest{Posts: ids, Action: action, Reason: reason}, nil
}

// ValidateModerationActor normalizes the capability-owner key recorded on
// audit rows. It mirrors the actor length bound in 009_moderation.sql.
func ValidateModerationActor(actor string) (string, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || utf8.RuneCountInString(actor) > maxModerationActorLength {
		return "", fmt.Errorf("%w: actor must contain 1 to %d characters", ErrInvalidModeration, maxModerationActorLength)
	}
	return actor, nil
}

// ValidateModerationQueue bounds the state-filtered queue listing.
func ValidateModerationQueue(state string, limit int) (string, error) {
	state = strings.ToLower(strings.TrimSpace(state))
	if !ValidModerationState(state) {
		return "", fmt.Errorf("%w: state must be one of published, pending, hidden, rejected", ErrInvalidModeration)
	}
	if limit < 1 || limit > 60 {
		return "", ErrInvalidQuery
	}
	return state, nil
}
