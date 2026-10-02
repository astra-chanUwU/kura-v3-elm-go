package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
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

// NewRouter returns the HTTP surface. A nil searcher keeps the scaffold's
// health endpoint usable before PostgreSQL is configured. Local media is
// served from MEDIA_ROOT when configured, or ../web/static/media when the
// server is started with the documented `go -C server run` command.
func NewRouter(searchers ...posts.Searcher) http.Handler {
	r := chi.NewRouter()
	r.Use(localDevCORS)
	r.Get("/health", health)
	r.Get("/api/posts", searchPosts(searchers...))
	r.Handle("/media/*", http.StripPrefix("/media/", http.FileServer(http.Dir(mediaRoot()))))
	return r
}

func mediaRoot() string {
	if configured := strings.TrimSpace(os.Getenv("MEDIA_ROOT")); configured != "" {
		return configured
	}
	return filepath.Join("..", "web", "static", "media")
}

func localDevCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:8000" || origin == "http://127.0.0.1:8000" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", http.MethodGet)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

func searchPosts(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		limit, err := parseLimit(r.URL.Query().Get("limit"), r.URL.Query().Has("limit"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
			return
		}
		if query == "" && r.URL.Query().Has("cursor") {
			writeJSONError(w, http.StatusBadRequest, "cursor cannot be used with an empty query")
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
		if strings.TrimSpace(query) == "" {
			writeJSON(w, http.StatusOK, posts.SearchPage{Posts: []posts.PostSummary{}, NextCursor: nil})
			return
		}
		if len(searchers) == 0 || searchers[0] == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "post search unavailable")
			return
		}

		result, err := searchers[0].SearchPosts(r.Context(), query, cursor, limit)
		if err != nil {
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}
