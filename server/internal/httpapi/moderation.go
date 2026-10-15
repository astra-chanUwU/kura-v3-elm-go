package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

const maxModerationPreviewBytes = 8 << 20

// moderationActor resolves the capability owner through the identity
// contract for shared write-gated handlers (trash, tag taxonomy). Every
// resolved actor holds write once authenticated; unauthenticated never do.
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

// moderatorActor is the defense-in-depth check for moderation handlers
// only: it requires the moderate capability (account admins and legacy
// actors), so editors cannot moderate even if middleware changes.
func moderatorActor(r *http.Request) (string, bool) {
	actor, ok := actorFromRequest(r)
	if !ok || strings.TrimSpace(actor.ID) == "" {
		return "", false
	}
	if !auth.HasModerateCapability(actor) {
		return "", false
	}
	return strings.TrimSpace(actor.ID), true
}

func moderatePosts(mutator posts.ModerationMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := moderatorActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if mutator == nil {
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

func listModerationQueue(reader posts.ModerationReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderatorActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if reader == nil {
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

func listPostModerationActions(reader posts.ModerationReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderatorActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if reader == nil {
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

// listModerationQueuePaged is the keyset-paged queue handler. It prefers a
// posts.ModerationQueuePager when the reader provides one and falls back to
// the legacy bounded reader (no cursor) otherwise. A supplied cursor is
// strictly validated for the requested state here and in the pager.
// Coordinator route: api.Get("/posts/moderation", listModerationQueuePaged(deps.ModerationRead))
// replaces the legacy registration when paging is wired.
func listModerationQueuePaged(reader posts.ModerationReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderatorActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if reader == nil {
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
		var cursor *string
		if raw := r.URL.Query().Get("cursor"); r.URL.Query().Has("cursor") {
			if _, err := posts.DecodeModerationQueueCursor(raw, state); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid moderation queue cursor")
				return
			}
			cursor = &raw
		}
		pager, ok := reader.(posts.ModerationQueuePager)
		if !ok {
			if cursor != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid moderation queue cursor")
				return
			}
			page, err := reader.ListModerationQueue(r.Context(), state, limit)
			if err != nil {
				writeModerationError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, page)
			return
		}
		page, err := pager.ListModerationQueuePage(r.Context(), state, cursor, limit)
		if err != nil {
			if errors.Is(err, posts.ErrInvalidCursor) {
				writeJSONError(w, http.StatusBadRequest, "invalid moderation queue cursor")
				return
			}
			writeModerationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

// moderationPreview serves one protected thumb-320 JPEG. Authorization is
// resolved BEFORE any post lookup, so anonymous callers cannot probe for
// post existence. It never redirects and never serves original media.
// Coordinator route: api.Get("/posts/{id}/moderation/preview", moderationPreview(deps.ModerationPreview))
// with deps field ModerationPreview posts.ModerationPreviewReader.
func moderationPreview(reader posts.ModerationPreviewReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderatorActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if reader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "moderation preview unavailable")
			return
		}
		id := strings.TrimSpace(chi.URLParam(r, "id"))
		preview, err := reader.OpenModerationPreview(r.Context(), id)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrInvalidModeration):
				writeJSONError(w, http.StatusBadRequest, "invalid post id")
			case errors.Is(err, posts.ErrModerationPreviewUnavailable):
				writeJSONError(w, http.StatusConflict, "moderation preview unavailable")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "moderation preview unavailable")
			default:
				writeJSONError(w, http.StatusServiceUnavailable, "moderation preview unavailable")
			}
			return
		}
		if preview.Body == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "moderation preview unavailable")
			return
		}
		defer preview.Body.Close()
		if !strings.EqualFold(strings.TrimSpace(preview.MediaType), "image/jpeg") || preview.Length > maxModerationPreviewBytes {
			writeJSONError(w, http.StatusConflict, "moderation preview unavailable")
			return
		}
		body, err := io.ReadAll(io.LimitReader(preview.Body, maxModerationPreviewBytes+1))
		if err != nil || int64(len(body)) > maxModerationPreviewBytes {
			writeJSONError(w, http.StatusServiceUnavailable, "moderation preview unavailable")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
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
