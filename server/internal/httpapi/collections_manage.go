package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

type collectionManageBody struct {
	Name            string `json:"name,omitempty"`
	ExpectedVersion *int64 `json:"expected_version"`
}

type collectionDeleteBody struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

// RenameCollection handles PATCH /api/collections/{id}.
func RenameCollection(manager posts.CollectionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if manager == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		var body collectionManageBody
		if err := decodeJSONBody(w, r, &body, "collection rename"); err != nil {
			return
		}
		if body.ExpectedVersion == nil {
			writeJSONError(w, http.StatusBadRequest, "expected_version is required")
			return
		}
		request, err := posts.ValidateCollectionManageRequest(posts.CollectionManageRequest{Name: body.Name, ExpectedVersion: *body.ExpectedVersion}, true)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !validCollectionPathID(chi.URLParam(r, "id")) {
			writeJSONError(w, http.StatusBadRequest, "invalid collection id")
			return
		}
		result, err := manager.RenameCollection(r.Context(), chi.URLParam(r, "id"), request)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// DeleteCollection handles DELETE /api/collections/{id}.
func DeleteCollection(manager posts.CollectionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if manager == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "collections unavailable")
			return
		}
		var body collectionDeleteBody
		if err := decodeJSONBody(w, r, &body, "collection delete"); err != nil {
			return
		}
		if body.ExpectedVersion == nil {
			writeJSONError(w, http.StatusBadRequest, "expected_version is required")
			return
		}
		if *body.ExpectedVersion < 0 {
			writeJSONError(w, http.StatusBadRequest, "expected_version must be non-negative")
			return
		}
		collectionID := chi.URLParam(r, "id")
		if !validCollectionPathID(collectionID) {
			writeJSONError(w, http.StatusBadRequest, "invalid collection id")
			return
		}
		result, err := manager.DeleteCollection(r.Context(), collectionID, *body.ExpectedVersion)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func validCollectionPathID(raw string) bool {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return err == nil && id > 0 && raw == strconv.FormatInt(id, 10)
}
