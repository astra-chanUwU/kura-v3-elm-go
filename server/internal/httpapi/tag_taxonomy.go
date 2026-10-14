package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func writeTagTaxonomyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, posts.ErrTagTaxonomyChanged):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "tag taxonomy changed", "code": "tag_taxonomy_changed"})
	case errors.Is(err, posts.ErrInvalidTagTaxonomy), errors.Is(err, posts.ErrInvalidTags), errors.Is(err, posts.ErrTagOperationTooLarge), errors.Is(err, posts.ErrInvalidCursor):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, posts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, posts.ErrTagRetired), errors.Is(err, posts.ErrTagTargetExists):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, posts.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "tag taxonomy unavailable")
	default:
		writeJSONError(w, http.StatusInternalServerError, "tag taxonomy failed")
	}
}

func listManageTags(manager posts.TagTaxonomyManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderationActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if manager == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "tag taxonomy unavailable")
			return
		}
		query := r.URL.Query()
		for key, values := range query {
			if key != "prefix" && key != "limit" && key != "cursor" {
				writeJSONError(w, http.StatusBadRequest, "unknown tag management query parameter")
				return
			}
			if len(values) != 1 {
				writeJSONError(w, http.StatusBadRequest, "tag management query parameters must appear once")
				return
			}
		}
		limit := 60
		if values, present := r.URL.Query()["limit"]; present {
			if len(values) != 1 {
				writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
				return
			}
			v, err := strconv.Atoi(values[0])
			if err != nil || v < 1 || v > 60 {
				writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
				return
			}
			limit = v
		}
		prefix := r.URL.Query().Get("prefix")
		if _, err := posts.ValidateTagManagePrefix(prefix, limit); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		var cursor *posts.TagManageCursor
		if r.URL.Query().Has("cursor") {
			decoded, err := posts.DecodeTagManageCursor(r.URL.Query().Get("cursor"), prefix)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid tag manage cursor")
				return
			}
			cursor = &decoded
		}
		page, err := manager.ListManageTags(r.Context(), prefix, limit, cursor)
		if err != nil {
			writeTagTaxonomyError(w, err)
			return
		}
		if page.Tags == nil {
			page.Tags = []posts.TagManageEntry{}
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func previewTagOperation(manager posts.TagTaxonomyManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := moderationActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if manager == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "tag taxonomy unavailable")
			return
		}
		var req posts.TagManageRequest
		if err := decodeJSONBody(w, r, &req, "tag operation preview"); err != nil {
			return
		}
		result, err := manager.PreviewTagOperation(r.Context(), req)
		if err != nil {
			writeTagTaxonomyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func applyTagOperation(manager posts.TagTaxonomyManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := moderationActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "moderation capability required")
			return
		}
		if manager == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "tag taxonomy unavailable")
			return
		}
		var req posts.TagManageRequest
		if err := decodeJSONBody(w, r, &req, "tag operation"); err != nil {
			return
		}
		if req.ExpectedVersion == nil {
			writeJSONError(w, http.StatusBadRequest, "expected_version is required")
			return
		}
		result, err := manager.ApplyTagOperation(r.Context(), actor, req)
		if err != nil {
			writeTagTaxonomyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
