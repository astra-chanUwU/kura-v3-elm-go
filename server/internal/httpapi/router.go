package httpapi

import (
	"encoding/json"
	"errors"
	"io"
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
	r.Get("/api/posts/{id}", postDetail(searchers...))
	r.Post("/api/posts/tags", editTags(searchers...))
	r.Post("/api/posts/tags/revert", revertTags(searchers...))
	r.Post("/api/posts/reactions", editReactions(searchers...))
	r.Get("/api/collections", listCollections(searchers...))
	r.Post("/api/collections", createCollection(searchers...))
	r.Get("/api/collections/{id}/posts", searchCollectionPosts(searchers...))
	r.Post("/api/collections/{id}/posts", addCollectionPosts(searchers...))
	r.Delete("/api/collections/{id}/posts/{postID}", removeCollectionPost(searchers...))
	r.Post("/api/collections/{id}/order", reorderCollection(searchers...))
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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func collectionMutator(searchers ...posts.Searcher) (posts.CollectionMutator, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	mutator, ok := searchers[0].(posts.CollectionMutator)
	return mutator, ok
}

func collectionPostSearcher(searchers ...posts.Searcher) (posts.CollectionPostSearcher, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	reader, ok := searchers[0].(posts.CollectionPostSearcher)
	return reader, ok
}

func searchCollectionPosts(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		collectionID := strings.TrimSpace(chi.URLParam(r, "id"))
		parsedID, parseErr := strconv.ParseInt(collectionID, 10, 64)
		if parseErr != nil || parsedID <= 0 || collectionID != strconv.FormatInt(parsedID, 10) {
			writeJSONError(w, http.StatusBadRequest, "invalid collection id")
			return
		}
		reader, ok := collectionPostSearcher(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"), r.URL.Query().Has("limit"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
			return
		}
		result, err := reader.SearchCollectionPosts(r.Context(), collectionID, limit)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func listCollections(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reader, ok := collectionMutator(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		result, err := reader.ListCollections(r.Context())
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Collections []posts.Collection `json:"collections"`
		}{Collections: result})
	}
}

func createCollection(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mutator, ok := collectionMutator(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		var request posts.CreateCollectionRequest
		if err := decodeJSONBody(w, r, &request, "collection"); err != nil {
			return
		}
		validated, err := posts.ValidateCreateCollection(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.CreateCollection(r.Context(), validated)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func addCollectionPosts(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mutator, ok := collectionMutator(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		var request posts.AddCollectionPostsRequest
		if err := decodeJSONBody(w, r, &request, "collection posts"); err != nil {
			return
		}
		validated, err := posts.ValidateAddCollectionPosts(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.AddCollectionPosts(r.Context(), chi.URLParam(r, "id"), validated)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func removeCollectionPost(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mutator, ok := collectionMutator(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		if err := mutator.RemoveCollectionPost(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "postID")); err != nil {
			writeCollectionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func reorderCollection(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mutator, ok := collectionMutator(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		var request posts.ReorderCollectionRequest
		if err := decodeJSONBody(w, r, &request, "collection order"); err != nil {
			return
		}
		validated, err := posts.ValidateReorderCollection(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.ReorderCollection(r.Context(), chi.URLParam(r, "id"), validated)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any, label string) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid "+label+" JSON")
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid "+label+" JSON")
		return errors.New("trailing JSON")
	}
	return nil
}

func writeCollectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, posts.ErrInvalidCollections), errors.Is(err, posts.ErrInvalidQuery):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, posts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "collection or post not found")
	case errors.Is(err, posts.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
	default:
		writeJSONError(w, http.StatusInternalServerError, "collection operation failed")
	}
}

func editTags(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(searchers) == 0 || searchers[0] == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "tag editing unavailable")
			return
		}
		mutator, ok := searchers[0].(posts.TagMutator)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "tag editing unavailable")
			return
		}
		var request posts.TagEditRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid tag edit JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeJSONError(w, http.StatusBadRequest, "invalid tag edit JSON")
			return
		}
		validated, err := posts.ValidateTagEdit(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.EditTags(r.Context(), validated)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidTags):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrConflict):
				writeJSONError(w, http.StatusConflict, "post tag version is stale")
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "tag editing unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "tag edit failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func revertTags(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(searchers) == 0 || searchers[0] == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "tag reverting unavailable")
			return
		}
		mutator, ok := searchers[0].(posts.TagReverter)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "tag reverting unavailable")
			return
		}
		var request posts.TagRevertRequest
		if err := decodeJSONBody(w, r, &request, "tag revert"); err != nil {
			return
		}
		validated, err := posts.ValidateTagRevert(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.RevertTags(r.Context(), validated)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidTags):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrConflict):
				writeJSONError(w, http.StatusConflict, "post tag version is stale")
			case errors.Is(err, posts.ErrNotFound), errors.Is(err, posts.ErrRevisionNotFound):
				writeJSONError(w, http.StatusNotFound, "post or target revision not found")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "tag reverting unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "tag revert failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func editReactions(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(searchers) == 0 || searchers[0] == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "reaction editing unavailable")
			return
		}
		mutator, ok := searchers[0].(posts.ReactionMutator)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "reaction editing unavailable")
			return
		}
		var request posts.ReactionRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid reaction JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeJSONError(w, http.StatusBadRequest, "invalid reaction JSON")
			return
		}
		validated, err := posts.ValidateReactionRequest(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.EditReactions(r.Context(), validated)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidReactions):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrReactionConflict), errors.Is(err, posts.ErrConflict):
				writeJSONError(w, http.StatusConflict, "post reaction version is stale")
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "reaction editing unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "reaction edit failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
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
