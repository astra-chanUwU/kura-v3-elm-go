package httpapi

import (
	"errors"
	"net/http"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func editReactions(mutator posts.ReactionMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "reaction editing unavailable")
			return
		}
		var request posts.ReactionRequest
		if err := decodeJSONBody(w, r, &request, "reaction"); err != nil {
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
