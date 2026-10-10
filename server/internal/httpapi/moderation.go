package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

func moderationMutator(searchers ...posts.Searcher) (posts.ModerationMutator, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	mutator, ok := searchers[0].(posts.ModerationMutator)
	return mutator, ok
}

func moderationReader(searchers ...posts.Searcher) (posts.ModerationReader, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	reader, ok := searchers[0].(posts.ModerationReader)
	return reader, ok
}

// moderationActor resolves the capability owner for moderation endpoints
// through the identity contract. The second result reports whether the actor
// holds the write capability that guards moderation; every resolved actor
// holds it once authenticated, while unauthenticated requests never do.
func moderationActor(r *http.Request) (string, bool) {
	actor, ok := actorFromRequest(r)
	if !ok || strings.TrimSpace(actor.ID) == "" {
		return "", false
	}
	if !auth.HasWriteCapability(actor) {
		return "", false
	}
	return strings.TrimSpace(actor.ID), true
}

func moderatePosts(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := moderationActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		mutator, ok := moderationMutator(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "moderation unavailable")
			return
		}
		var request posts.ModerationRequest
		if err := decodeJSONBody(w, r, &request, "moderation"); err != nil {
			return
		}
		validated, err := posts.ValidateModerationRequest(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.ModeratePosts(r.Context(), actor, validated)
		if err != nil {
			writeModerationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func listModerationQueue(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderationActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		reader, ok := moderationReader(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "moderation unavailable")
			return
		}
		state := strings.TrimSpace(r.URL.Query().Get("state"))
		if state == "" {
			writeJSONError(w, http.StatusBadRequest, "state is required")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"), r.URL.Query().Has("limit"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
			return
		}
		page, err := reader.ListModerationQueue(r.Context(), state, limit)
		if err != nil {
			writeModerationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func listPostModerationActions(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderationActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		reader, ok := moderationReader(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "moderation unavailable")
			return
		}
		id := strings.TrimSpace(chi.URLParam(r, "id"))
		actions, err := reader.ListModerationActions(r.Context(), id)
		if err != nil {
			writeModerationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Actions []posts.ModerationAction `json:"actions"`
		}{Actions: actions})
	}
}

func writeModerationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, posts.ErrInvalidModeration), errors.Is(err, posts.ErrInvalidQuery):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, posts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "post not found")
	case errors.Is(err, posts.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "moderation unavailable")
	default:
		writeJSONError(w, http.StatusInternalServerError, "moderation failed")
	}
}
