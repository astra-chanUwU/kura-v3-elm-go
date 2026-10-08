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

func mediaVisibilityChecker(searchers ...posts.Searcher) (posts.MediaVisibilityChecker, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	checker, ok := searchers[0].(posts.MediaVisibilityChecker)
	return checker, ok
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

// gatedMedia serves /media/* files while enforcing the centralized
// visibility rule: a path referenced by post media is served only when a
// non-deleted referencing post is published. Unreferenced paths keep the
// previous public behavior; without a checker the handler also serves,
// preserving routers whose searcher predates moderation.
func gatedMedia(searchers ...posts.Searcher) http.Handler {
	fileServer := http.StripPrefix("/media/", http.FileServer(http.Dir(mediaRoot())))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checker, ok := mediaVisibilityChecker(searchers...)
		if !ok {
			fileServer.ServeHTTP(w, r)
			return
		}
		subpath := chi.URLParam(r, "*")
		if subpath == "" {
			subpath = strings.TrimPrefix(r.URL.Path, "/media/")
		}
		subpath = strings.TrimSpace(subpath)
		if subpath == "" {
			writeJSONError(w, http.StatusNotFound, "media not found")
			return
		}
		visibility, err := checker.LookupMediaVisibility(r.Context(), "/media/"+subpath)
		if err != nil {
			if errors.Is(err, posts.ErrUnavailable) {
				writeJSONError(w, http.StatusServiceUnavailable, "media unavailable")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "media lookup failed")
			return
		}
		if visibility.Referenced && !visibility.Visible {
			writeJSONError(w, http.StatusNotFound, "media not found")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
