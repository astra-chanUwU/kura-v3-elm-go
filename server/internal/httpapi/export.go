package httpapi

import (
	"context"
	"errors"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func exportLibrary(exporter posts.LibraryExporter, collection bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request := posts.ExportRequest{Metadata: true}
		if value, present := r.URL.Query()["metadata"]; present {
			if len(value) != 1 || (value[0] != "true" && value[0] != "false") {
				writeJSONError(w, 400, "metadata must be true or false")
				return
			}
			request.Metadata = value[0] == "true"
		}
		if collection {
			request.CollectionID = chi.URLParam(r, "id")
			raw, ok := r.URL.Query()["expected_version"]
			if !ok || len(raw) != 1 {
				writeJSONError(w, 400, "expected_version is required")
				return
			}
			v, err := strconv.ParseInt(raw[0], 10, 64)
			if err != nil {
				writeJSONError(w, 400, "invalid expected_version")
				return
			}
			request.ExpectedVersion = &v
		} else {
			raw := r.URL.Query().Get("ids")
			if len(raw) > 25000 {
				writeJSONError(w, 400, "too many post IDs")
				return
			}
			request.PostIDs = strings.Split(raw, ",")
		}
		if err := posts.ValidateExportRequest(request); err != nil {
			writeJSONError(w, 400, "invalid export request")
			return
		}
		if exporter == nil {
			writeJSONError(w, 503, "export unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		archive, err := exporter.ExportLibrary(ctx, request)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, 404, "export post or collection not found")
			case errors.Is(err, posts.ErrCollectionChanged):
				writeJSON(w, 409, map[string]string{"error": "collection changed", "code": "collection_changed"})
			case errors.Is(err, posts.ErrExportTooLarge):
				writeJSONError(w, 413, err.Error())
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, 503, "export unavailable")
			default:
				writeJSONError(w, 500, "export could not read every original")
			}
			return
		}
		defer archive.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="kura-export.zip"`)
		w.Header().Set("Content-Length", strconv.FormatInt(archive.Size, 10))
		w.Header().Set("Cache-Control", "no-store")
		io.Copy(w, archive.File)
	}
}
