package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func suggestTags(reader posts.TagSuggester) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 12
		if values, present := r.URL.Query()["limit"]; present {
			if len(values) != 1 {
				writeJSONError(w, 400, "invalid limit")
				return
			}
			var err error
			limit, err = strconv.Atoi(values[0])
			if err != nil {
				writeJSONError(w, 400, "invalid limit")
				return
			}
		}
		prefix, err := posts.ValidateTagSuggestionQuery(r.URL.Query().Get("prefix"), limit)
		if err != nil {
			writeJSONError(w, 400, "invalid tag suggestion query")
			return
		}
		if reader == nil {
			writeJSONError(w, 503, "tag suggestions unavailable")
			return
		}
		items, err := reader.SuggestTags(r.Context(), prefix, limit)
		if err != nil {
			if errors.Is(err, posts.ErrUnavailable) {
				writeJSONError(w, 503, "tag suggestions unavailable")
			} else {
				writeJSONError(w, 500, "tag suggestions failed")
			}
			return
		}
		if items == nil {
			items = []posts.TagSuggestion{}
		}
		writeJSON(w, 200, struct {
			Tags []posts.TagSuggestion `json:"tags"`
		}{items})
	}
}
