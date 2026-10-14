package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

func listDuplicateGroups(reader posts.DuplicateReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "duplicates unavailable")
			return
		}
		query := r.URL.Query()
		limit := 20
		if query.Has("limit") {
			parsed, err := parseDuplicateGroupsLimit(query.Get("limit"))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 30")
				return
			}
			limit = parsed
		}
		var cursor *posts.DuplicateGroupsCursor
		if query.Has("cursor") {
			decoded, err := posts.DecodeDuplicateGroupsCursor(query.Get("cursor"))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid duplicates cursor")
				return
			}
			cursor = &decoded
		}
		page, err := reader.ListDuplicateGroups(r.Context(), limit, cursor)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidCursor):
				writeJSONError(w, http.StatusBadRequest, "invalid duplicates cursor")
			case errors.Is(err, posts.ErrInvalidQuery):
				writeJSONError(w, http.StatusBadRequest, "invalid duplicates request")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "duplicates unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "duplicate lookup failed")
			}
			return
		}
		if page.Groups == nil {
			page.Groups = []posts.DuplicateGroup{}
		}
		for i := range page.Groups {
			if page.Groups[i].Posts == nil {
				page.Groups[i].Posts = []posts.PostSummary{}
			}
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func getDuplicateMembers(reader posts.DuplicateReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "duplicates unavailable")
			return
		}
		hash := strings.TrimSpace(chi.URLParam(r, "hash"))
		if unescaped, err := url.PathUnescape(hash); err == nil {
			hash = strings.TrimSpace(unescaped)
		}
		if !isBrowserSHA256(hash) {
			writeJSONError(w, http.StatusBadRequest, "invalid duplicate hash")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"), r.URL.Query().Has("limit"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
			return
		}
		var cursor *posts.DuplicateMembersCursor
		if r.URL.Query().Has("cursor") {
			decoded, decodeErr := posts.DecodeDuplicateMembersCursor(r.URL.Query().Get("cursor"), hash)
			if decodeErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid duplicate cursor")
				return
			}
			cursor = &decoded
		}
		page, err := reader.ListDuplicateMembers(r.Context(), hash, limit, cursor)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "duplicate group not found")
			case errors.Is(err, posts.ErrInvalidCursor):
				writeJSONError(w, http.StatusBadRequest, "invalid duplicate cursor")
			case errors.Is(err, posts.ErrInvalidQuery):
				writeJSONError(w, http.StatusBadRequest, "invalid duplicate request")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "duplicates unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "duplicate lookup failed")
			}
			return
		}
		if page.Posts == nil {
			page.Posts = []posts.PostSummary{}
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func parseDuplicateGroupsLimit(raw string) (int, error) {
	if raw == "" {
		return 0, errors.New("invalid limit")
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid limit")
		}
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 30 {
		return 0, errors.New("invalid limit")
	}
	return limit, nil
}
