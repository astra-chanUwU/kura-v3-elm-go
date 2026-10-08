package posts

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateModerationRequest(t *testing.T) {
	got, err := ValidateModerationRequest(ModerationRequest{
		Posts:  []ModerationTarget{{ID: " 2004 "}},
		Action: " Hide ",
		Reason: "  spam  ",
	})
	if err != nil {
		t.Fatalf("valid moderation rejected: %v", err)
	}
	if len(got.Posts) != 1 || got.Posts[0].ID != "2004" || got.Action != "hide" || got.Reason != "spam" {
		t.Fatalf("moderation not normalized: %#v", got)
	}
	for name, request := range map[string]ModerationRequest{
		"empty posts":   {Posts: nil, Action: "hide"},
		"unknown act":   {Posts: []ModerationTarget{{ID: "1"}}, Action: "ban"},
		"empty action":  {Posts: []ModerationTarget{{ID: "1"}}},
		"zero id":       {Posts: []ModerationTarget{{ID: "0"}}, Action: "hide"},
		"padded id":     {Posts: []ModerationTarget{{ID: "01"}}, Action: "hide"},
		"non-numeric":   {Posts: []ModerationTarget{{ID: "bad"}}, Action: "hide"},
		"duplicate ids": {Posts: []ModerationTarget{{ID: "2"}, {ID: "2"}}, Action: "hide"},
		"long reason":   {Posts: []ModerationTarget{{ID: "2"}}, Action: "hide", Reason: strings.Repeat("x", maxModerationReasonLength+1)},
	} {
		if _, err := ValidateModerationRequest(request); !errors.Is(err, ErrInvalidModeration) {
			t.Errorf("%s: error = %v, want ErrInvalidModeration", name, err)
		}
	}
	over := make([]ModerationTarget, maxModerationPosts+1)
	for i := range over {
		over[i] = ModerationTarget{ID: "1"}
	}
	if _, err := ValidateModerationRequest(ModerationRequest{Posts: over, Action: "hide"}); !errors.Is(err, ErrInvalidModeration) {
		t.Errorf("over-length targets accepted: %v", err)
	}
	emptyReason, err := ValidateModerationRequest(ModerationRequest{Posts: []ModerationTarget{{ID: "3"}}, Action: "approve"})
	if err != nil || emptyReason.Reason != "" {
		t.Fatalf("empty reason rejected: %#v, %v", emptyReason, err)
	}
}

func TestModerationStateForAction(t *testing.T) {
	for action, want := range map[string]string{
		ModerationActionApprove: ModerationPublished,
		ModerationActionPend:    ModerationPending,
		ModerationActionHide:    ModerationHidden,
		ModerationActionReject:  ModerationRejected,
	} {
		got, ok := ModerationStateForAction(action)
		if !ok || got != want {
			t.Errorf("action %q -> (%q, %v), want (%q, true)", action, got, ok, want)
		}
	}
	if _, ok := ModerationStateForAction("ban"); ok {
		t.Error("unknown action mapped to a state")
	}
}

func TestIsPubliclyVisibleModerationState(t *testing.T) {
	if !IsPubliclyVisibleModerationState(ModerationPublished) {
		t.Error("published should be publicly visible")
	}
	for _, state := range []string{ModerationPending, ModerationHidden, ModerationRejected, "", "PUBLISHED", "deleted"} {
		if IsPubliclyVisibleModerationState(state) {
			t.Errorf("state %q should not be publicly visible", state)
		}
	}
}

func TestValidateModerationActor(t *testing.T) {
	got, err := ValidateModerationActor("  local  ")
	if err != nil || got != "local" {
		t.Fatalf("actor not normalized: %q, %v", got, err)
	}
	for _, actor := range []string{"", "   ", strings.Repeat("a", maxModerationActorLength+1)} {
		if _, err := ValidateModerationActor(actor); !errors.Is(err, ErrInvalidModeration) {
			t.Errorf("actor %q accepted", actor)
		}
	}
}

func TestValidateModerationQueue(t *testing.T) {
	for _, state := range []string{"hidden", " Hidden ", "REJECTED", "pending", "published"} {
		got, err := ValidateModerationQueue(state, 10)
		if err != nil || got != strings.ToLower(strings.TrimSpace(state)) {
			t.Errorf("state %q -> (%q, %v)", state, got, err)
		}
	}
	if _, err := ValidateModerationQueue("banned", 10); !errors.Is(err, ErrInvalidModeration) {
		t.Errorf("unknown queue state accepted: %v", err)
	}
	if _, err := ValidateModerationQueue("", 10); !errors.Is(err, ErrInvalidModeration) {
		t.Errorf("empty queue state accepted: %v", err)
	}
	for _, limit := range []int{0, 61} {
		if _, err := ValidateModerationQueue("hidden", limit); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("queue limit %d: error = %v, want ErrInvalidQuery", limit, err)
		}
	}
}

// TestVisibilitySQLCentralizesPublishedRule pins the single visibility
// predicate across every read and mutation guard: public paths admit only
// published, non-deleted rows.
func TestVisibilitySQLCentralizesPublishedRule(t *testing.T) {
	guarded := map[string]string{
		"SearchPostsSQL":           SearchPostsSQL,
		"SearchPostsScoreSQL":      SearchPostsScoreSQL,
		"GetPostDetailSQL":         GetPostDetailSQL,
		"lockPostForTagEditSQL":    lockPostForTagEditSQL,
		"updatePostTagsSQL":        updatePostTagsSQL,
		"lockPostForTagRevertSQL":  lockPostForTagRevertSQL,
		"lockPostForReactionSQL":   lockPostForReactionSQL,
		"updatePostReactionSQL":    updatePostReactionSQL,
		"listCollectionsSQL":       listCollectionsSQL,
		"addCollectionPostSQL":     addCollectionPostSQL,
		"collectionPostIDsSQL":     collectionPostIDsSQL,
		"searchCollectionPostsSQL": searchCollectionPostsSQL,
		"deleteCollectionPostSQL":  deleteCollectionPostSQL,
	}
	for name, sql := range guarded {
		if !strings.Contains(sql, "moderation_state = 'published'") {
			t.Errorf("%s does not filter to published moderation state", name)
		}
		if !strings.Contains(sql, "deleted_at IS NULL") {
			t.Errorf("%s lost its deleted_at guard", name)
		}
	}
	// Moderation transitions must reach non-published rows, so their lock
	// filters on deleted_at only while still selecting the current state.
	if strings.Contains(lockPostForModerationSQL, "moderation_state = 'published'") {
		t.Error("lockPostForModerationSQL must reach non-published rows")
	}
	if !strings.Contains(lockPostForModerationSQL, "SELECT moderation_state") {
		t.Error("lockPostForModerationSQL must read the current state")
	}
	if !strings.Contains(moderationActionsByPostSQL, "post_moderation_actions") {
		t.Error("audit history must read post_moderation_actions")
	}
	if !strings.Contains(moderationQueueSQL, "moderation_state = $1::text") {
		t.Error("moderation queue must filter on the requested state")
	}
	if !strings.Contains(mediaPostStatesSQL, "preview_url") || !strings.Contains(mediaPostStatesSQL, "original_url") {
		t.Error("media visibility must map both post media URLs")
	}
}
