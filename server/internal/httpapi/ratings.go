package httpapi

import (
	"errors"
	"net/http"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

// EditRatings serves the protected, atomic versioned content-rating edit.
func EditRatings(mutator posts.RatingMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "rating editing unavailable")
			return
		}
		var request posts.RatingRequest
		if err := decodeJSONBody(w, r, &request, "rating edit"); err != nil {
			return
		}
		validated, err := posts.ValidateRatingRequest(request)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := mutator.UpdateRatings(r.Context(), validated)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidRating):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrRatingConflict):
				writeJSONError(w, http.StatusConflict, "post rating version is stale")
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "rating editing unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "rating edit failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
