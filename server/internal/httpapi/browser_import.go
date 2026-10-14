package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func browserImportDuplicates(importer posts.BrowserImporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if importer == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "imports unavailable")
			return
		}
		raw := r.URL.Query().Get("hashes")
		hashes := strings.Split(raw, ",")
		if raw == "" || len(hashes) > 100 {
			writeJSONError(w, http.StatusBadRequest, "hashes must contain 1 to 100 values")
			return
		}
		for i := range hashes {
			hashes[i] = strings.TrimSpace(hashes[i])
			if !isBrowserSHA256(hashes[i]) {
				writeJSONError(w, http.StatusBadRequest, "each hash must be sha256 followed by 64 lowercase hexadecimal characters")
				return
			}
		}
		matches, err := importer.FindBrowserImportDuplicates(r.Context(), hashes)
		if err != nil {
			if errors.Is(err, posts.ErrInvalidUpload) {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			} else {
				writeJSONError(w, http.StatusInternalServerError, "duplicate lookup failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
	}
}

func isBrowserSHA256(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, c := range value[len("sha256:"):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func browserImport(importer posts.BrowserImporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if importer == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "imports unavailable")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBytes)
		parseErr := r.ParseMultipartForm(1 << 20)
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }()
		}
		if parseErr != nil {
			if strings.Contains(strings.ToLower(parseErr.Error()), "request body too large") {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "upload exceeds 25 MiB")
			} else {
				writeJSONError(w, http.StatusBadRequest, "invalid import multipart form")
			}
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "import file is required")
			return
		}
		defer file.Close()
		expectedHash := strings.TrimSpace(r.FormValue("expected_hash"))
		if !isBrowserSHA256(expectedHash) {
			writeJSONError(w, http.StatusBadRequest, "expected_hash must be sha256 followed by 64 lowercase hexadecimal characters")
			return
		}
		result, err := importer.ImportBrowser(r.Context(), posts.BrowserImportRequest{
			UploadRequest: posts.UploadRequest{File: file, Filename: header.Filename, Tags: r.Form["tags"], Source: r.FormValue("source"), Artist: r.FormValue("artist")},
			ExpectedHash:  expectedHash, CollectionID: r.FormValue("collection_id"), DuplicatePolicy: r.FormValue("duplicate_policy"),
		})
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidUpload), errors.Is(err, posts.ErrInvalidCollections):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrDuplicateImport):
				writeJSONError(w, http.StatusConflict, "matching image already exists")
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "collection or matching post not found")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "imports unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "import failed")
			}
			return
		}
		status := http.StatusCreated
		if result.Duplicate {
			status = http.StatusOK
		}
		writeJSON(w, status, result)
	}
}
