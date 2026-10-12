package httpapi

import (
	"errors"
	"net/http"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func editTags(mutator posts.TagMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "tag editing unavailable")
			return
		}
		var request posts.TagEditRequest
		if err := decodeJSONBody(w, r, &request, "tag edit"); err != nil {
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

func revertTags(mutator posts.TagReverter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mutator == nil {
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
