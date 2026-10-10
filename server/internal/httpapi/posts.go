package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

const (
	maxSearchQueryLength = 256
	defaultSearchLimit   = 60
	maxSearchLimit       = 60
)

func searchPosts(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		limit, err := parseLimit(r.URL.Query().Get("limit"), r.URL.Query().Has("limit"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
			return
		}
		var cursor *posts.Cursor
		if rawCursor := r.URL.Query().Get("cursor"); rawCursor != "" {
			decoded, decodeErr := posts.DecodeCursor(rawCursor, query)
			if decodeErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid search cursor")
				return
			}
			cursor = &decoded
		} else if r.URL.Query().Has("cursor") {
			writeJSONError(w, http.StatusBadRequest, "invalid search cursor")
			return
		}

		if len([]rune(query)) > maxSearchQueryLength {
			writeJSONError(w, http.StatusBadRequest, "search query is too long (maximum 256 characters)")
			return
		}
		if _, parseErr := posts.ParseQuery(query); parseErr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid search query")
			return
		}
		if len(searchers) == 0 || searchers[0] == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "post search unavailable")
			return
		}

		result, err := searchers[0].SearchPosts(r.Context(), query, cursor, limit)
		if err != nil {
			if errors.Is(err, posts.ErrInvalidCursor) {
				writeJSONError(w, http.StatusBadRequest, "invalid search cursor")
				return
			}
			if errors.Is(err, posts.ErrInvalidQuery) {
				writeJSONError(w, http.StatusBadRequest, "invalid search query")
				return
			}
			if errors.Is(err, posts.ErrUnavailable) {
				writeJSONError(w, http.StatusServiceUnavailable, "post search unavailable")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "post search failed")
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func postDetail(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(chi.URLParam(r, "id"))
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 {
			writeJSONError(w, http.StatusNotFound, "post not found")
			return
		}
		if len(searchers) == 0 || searchers[0] == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "post detail unavailable")
			return
		}
		detailer, ok := searchers[0].(posts.PostDetailer)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "post detail unavailable")
			return
		}
		detail, err := detailer.GetPostDetail(r.Context(), strconv.FormatInt(parsed, 10))
		if err != nil {
			if errors.Is(err, posts.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "post not found")
				return
			}
			if errors.Is(err, posts.ErrUnavailable) {
				writeJSONError(w, http.StatusServiceUnavailable, "post detail unavailable")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "post detail failed")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

func parseLimit(raw string, present bool) (int, error) {
	if !present {
		return defaultSearchLimit, nil
	}
	if raw == "" {
		return 0, errors.New("invalid limit")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid limit")
		}
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxSearchLimit {
		return 0, errors.New("invalid limit")
	}
	return limit, nil
}
