package httpapi

import (
	"errors"
	"net/http"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func readMetadata(reader posts.MetadataReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "metadata reading unavailable")
			return
		}
		var request posts.MetadataReadRequest
		if err := decodeJSONBody(w, r, &request, "metadata read"); err != nil {
			return
		}
		if err := posts.ValidateMetadataRead(request); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := reader.ReadMetadata(r.Context(), request)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidMetadata):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "metadata reading unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "metadata read failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// EditMetadata serves the protected, versioned source/artist mutation.
func EditMetadata(mutator posts.MetadataMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "metadata editing unavailable")
			return
		}
		var request posts.MetadataRequest
		if err := decodeJSONBody(w, r, &request, "metadata edit"); err != nil {
			return
		}
		validated, err := posts.ValidateMetadataRequest(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.UpdateMetadata(r.Context(), validated)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidMetadata):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrMetadataConflict):
				writeJSONError(w, http.StatusConflict, "post metadata version is stale")
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "metadata editing unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "metadata edit failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
