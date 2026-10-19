package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

func searchCollectionPosts(reader posts.CollectionPostSearcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		collectionID := strings.TrimSpace(chi.URLParam(r, "id"))
		parsedID, parseErr := strconv.ParseInt(collectionID, 10, 64)
		if parseErr != nil || parsedID <= 0 || collectionID != strconv.FormatInt(parsedID, 10) {
			writeJSONError(w, http.StatusBadRequest, "invalid collection id")
			return
		}
		if reader == nil {
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

func listCollections(mutator posts.CollectionMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		result, err := mutator.ListCollections(r.Context())
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Collections []posts.Collection `json:"collections"`
		}{Collections: result})
	}
}

func listCollectionSummaries(reader posts.CollectionSummaryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		result, err := reader.ListCollectionSummaries(r.Context())
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		if result == nil {
			result = []posts.CollectionSummary{}
		}
		writeJSON(w, http.StatusOK, struct {
			Collections []posts.CollectionSummary `json:"collections"`
		}{Collections: result})
	}
}

func createCollection(mutator posts.CollectionMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
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
		validated.CreatedBy = nil
		if actor, ok := actorFromRequest(r); ok && actor.UserID > 0 {
			owner := actor.UserID
			validated.CreatedBy = &owner
		}
		result, err := mutator.CreateCollection(r.Context(), validated)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func addCollectionPosts(mutator posts.CollectionMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
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

func removeCollectionPost(mutator posts.CollectionMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
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

func reorderCollection(mutator posts.CollectionMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
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

// moveCollectionPostBody is the thin HTTP wire shape for a move. It keeps
// field presence observable: ExpectedVersion is a pointer so omitted or
// null is distinguishable from an explicit 0, and BeforePostID is raw so
// an omitted field is distinguishable from an explicit null (move to end)
// and from a non-string value.
type moveCollectionPostBody struct {
	PostID          string          `json:"post_id"`
	BeforePostID    json.RawMessage `json:"before_post_id"`
	ExpectedVersion *int64          `json:"expected_version"`
}

func moveCollectionPost(mover posts.CollectionPostMover) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mover == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		var body moveCollectionPostBody
		if err := decodeJSONBody(w, r, &body, "collection move"); err != nil {
			return
		}
		if body.ExpectedVersion == nil {
			writeJSONError(w, http.StatusBadRequest, "expected_version is required")
			return
		}
		if body.BeforePostID == nil {
			writeJSONError(w, http.StatusBadRequest, "before_post_id is required")
			return
		}
		var beforePostID *string
		if trimmed := bytes.TrimSpace(body.BeforePostID); !bytes.Equal(trimmed, []byte("null")) {
			var before string
			if err := json.Unmarshal(body.BeforePostID, &before); err != nil {
				writeJSONError(w, http.StatusBadRequest, "before_post_id must be a positive integer or null")
				return
			}
			beforePostID = &before
		}
		request := posts.MoveCollectionPostRequest{
			PostID:          body.PostID,
			BeforePostID:    beforePostID,
			ExpectedVersion: *body.ExpectedVersion,
		}
		validated, err := posts.ValidateMoveCollectionPostRequest(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mover.MoveCollectionPost(r.Context(), chi.URLParam(r, "id"), validated)
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
