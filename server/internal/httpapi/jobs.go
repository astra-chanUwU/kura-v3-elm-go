package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/go-chi/chi/v5"
)

// getJobStatus polls one durable job by id. Reads stay public like other
// GET endpoints, so local polling works without credentials while the write
// middleware still guards mutations. The body is the public view: no
// payload, object key, idempotency key, or worker lease, and last_error is
// a sanitized sentence plus an action that names the next step.
func getJobStatus(reader jobs.Reader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "jobs unavailable")
			return
		}
		raw := strings.TrimSpace(chi.URLParam(r, "id"))
		id, err := jobs.ParseID(raw)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "job not found")
			return
		}
		job, err := reader.GetJob(r.Context(), id)
		if err != nil {
			writeJobError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job.Public())
	}
}

func writeJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jobs.ErrInvalidJob):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, jobs.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "job not found")
	case errors.Is(err, jobs.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "jobs unavailable")
	case errors.Is(err, jobs.ErrLeaseConflict):
		writeJSONError(w, http.StatusConflict, "job lease conflict")
	default:
		writeJSONError(w, http.StatusInternalServerError, "job lookup failed")
	}
}
