package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

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
