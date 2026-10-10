package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

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
		var cursor *posts.CollectionCursor
		if r.URL.Query().Has("cursor") {
			decoded, decodeErr := posts.DecodeCollectionCursor(r.URL.Query().Get("cursor"), parsedID)
			if decodeErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid collection cursor")
				return
			}
			cursor = &decoded
		}
		result, err := reader.SearchCollectionPosts(r.Context(), collectionID, cursor, limit)
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

func writeCollectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, posts.ErrCollectionChanged):
		writeJSON(w, http.StatusConflict, struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}{Error: "collection changed", Code: "collection_changed"})
	case errors.Is(err, posts.ErrInvalidCursor):
		writeJSONError(w, http.StatusBadRequest, "invalid collection cursor")
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
