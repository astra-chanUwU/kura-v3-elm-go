package httpapi

import (
	"errors"
	"net/http"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func listTrash(store posts.PostTrashStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := moderationActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "trash capability required")
			return
		}
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "trash unavailable")
			return
		}
		query := r.URL.Query()
		for key := range query {
			if key != "limit" && key != "cursor" {
				writeJSONError(w, http.StatusBadRequest, "unknown trash query parameter")
				return
			}
			if len(query[key]) != 1 {
				writeJSONError(w, http.StatusBadRequest, "trash query parameters must appear once")
				return
			}
		}
		limit := 60
		if query.Has("limit") {
			parsed, err := parseLimit(query.Get("limit"), true)
			if err != nil || parsed > 60 {
				writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
				return
			}
			limit = parsed
		}
		var cursor *posts.TrashCursor
		if query.Has("cursor") {
			decoded, err := posts.DecodeTrashCursor(query.Get("cursor"))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid trash cursor")
				return
			}
			cursor = &decoded
		}
		page, err := store.ListTrash(r.Context(), actor, limit, cursor)
		if err != nil {
			writeTrashError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func trashPosts(store posts.PostTrashStore) http.HandlerFunc {
	return changeTrash(store, true)
}

func restorePosts(store posts.PostTrashStore) http.HandlerFunc {
	return changeTrash(store, false)
}

func changeTrash(store posts.PostTrashStore, trash bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := moderationActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "trash capability required")
			return
		}
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "trash unavailable")
			return
		}
		var request posts.TrashRequest
		if err := decodeJSONBody(w, r, &request, "trash"); err != nil {
			return
		}
		validated, err := posts.ValidateTrashRequest(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		var response posts.TrashResponse
		if trash {
			response, err = store.Trash(r.Context(), actor, validated)
		} else {
			response, err = store.Restore(r.Context(), actor, validated)
		}
		if err != nil {
			writeTrashError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func writeTrashError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, posts.ErrInvalidTrash), errors.Is(err, posts.ErrInvalidCursor), errors.Is(err, posts.ErrInvalidQuery):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, posts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "post not found")
	case errors.Is(err, posts.ErrConflict):
		writeJSONError(w, http.StatusConflict, "collection membership changed; retry the request")
	case errors.Is(err, posts.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "trash unavailable")
	default:
		writeJSONError(w, http.StatusInternalServerError, "trash operation failed")
	}
}
