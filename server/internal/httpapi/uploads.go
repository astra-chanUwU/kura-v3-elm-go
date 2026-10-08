package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

const maxUploadRequestBytes int64 = 26 << 20

func uploadPost(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(searchers) == 0 || searchers[0] == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "uploads unavailable")
			return
		}
		uploader, ok := searchers[0].(posts.UploadCreator)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "uploads unavailable")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBytes)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "request body too large") {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "upload exceeds 25 MiB")
			} else {
				writeJSONError(w, http.StatusBadRequest, "invalid upload multipart form")
			}
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "upload file is required")
			return
		}
		defer file.Close()
		result, err := uploader.CreateUpload(r.Context(), posts.UploadRequest{
			File:     file,
			Filename: header.Filename,
			Tags:     r.Form["tags"],
			Source:   r.FormValue("source"),
			Artist:   r.FormValue("artist"),
		})
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidUpload):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "uploads unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "upload failed")
			}
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}
