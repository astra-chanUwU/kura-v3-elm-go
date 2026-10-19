package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	libhealth "github.com/astra-chanUwU/kura-v3-elm-go/server/internal/health"
)

func LibraryHealthSummary(service *libhealth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !healthReady(w, r, service) {
			return
		}
		summary, err := service.Summary(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "library health is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}
}

func LibraryHealthScan(service *libhealth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !healthReady(w, r, service) {
			return
		}
		writeJSON(w, http.StatusOK, service.ScanStatus())
	}
}

func LibraryHealthStartScan(service *libhealth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !healthReady(w, r, service) {
			return
		}
		if !emptyHealthBody(w, r) {
			return
		}
		err := service.StartScan()
		switch {
		case err == nil:
			writeJSON(w, http.StatusAccepted, service.ScanStatus())
		case errors.Is(err, libhealth.ErrUnsupported):
			writeJSONError(w, http.StatusServiceUnavailable, libhealth.ErrUnsupported.Error())
		case errors.Is(err, libhealth.ErrBusy):
			writeJSONError(w, http.StatusConflict, libhealth.ErrBusy.Error())
		default:
			writeJSONError(w, http.StatusServiceUnavailable, "library scan is unavailable")
		}
	}
}

func LibraryHealthCancelScan(service *libhealth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !healthReady(w, r, service) {
			return
		}
		if err := service.CancelScan(); err != nil {
			writeJSONError(w, http.StatusConflict, "there is no library scan to cancel")
			return
		}
		writeJSON(w, http.StatusOK, service.ScanStatus())
	}
}

func LibraryHealthThumbnails(service *libhealth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !healthReady(w, r, service) {
			return
		}
		page, err := service.Thumbnails(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "thumbnail inventory is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func LibraryHealthRepair(service *libhealth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !healthReady(w, r, service) {
			return
		}
		var body struct {
			PostID string `json:"post_id"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 256))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "repair accepts only a post id")
			return
		}
		jobID, status, err := service.Repair(r.Context(), strings.TrimSpace(body.PostID))
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, map[string]string{"job_id": jobID, "status": status})
		case errors.Is(err, libhealth.ErrBadPost):
			writeJSONError(w, http.StatusBadRequest, "repair accepts only a post id")
		case errors.Is(err, libhealth.ErrUnsupported):
			writeJSONError(w, http.StatusServiceUnavailable, libhealth.ErrUnsupported.Error())
		case errors.Is(err, libhealth.ErrUnavailable):
			writeJSONError(w, http.StatusConflict, "original is unavailable")
		default:
			writeJSONError(w, http.StatusServiceUnavailable, "thumbnail repair is unavailable")
		}
	}
}

func healthReady(w http.ResponseWriter, r *http.Request, service *libhealth.Service) bool {
	if !recoveryAdmin(w, r) {
		return false
	}
	if service == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "library health is unavailable")
		return false
	}
	return true
}

func emptyHealthBody(w http.ResponseWriter, r *http.Request) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8))
	if err != nil || len(bytesTrim(body)) != 0 {
		writeJSONError(w, http.StatusBadRequest, "library scan does not accept a request body")
		return false
	}
	return true
}

func bytesTrim(body []byte) string {
	return strings.TrimSpace(string(body))
}
