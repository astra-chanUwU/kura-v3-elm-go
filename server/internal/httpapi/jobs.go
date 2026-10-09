package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

// jobReader serves one job row for GET /api/jobs/{id} polling. Any searcher
// that also implements jobs.Reader serves job status; routers whose
// searcher predates jobs answer 503 until a later slice wires a jobs store.
// Production wiring is deferred the same way: main.go keeps passing only the
// posts searcher, so the endpoint is reachable but reports unavailable until
// then.
func jobReader(searchers ...posts.Searcher) (jobs.Reader, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	reader, ok := searchers[0].(jobs.Reader)
	return reader, ok
}

// getJobStatus polls one durable job by id. Reads stay public like other
// GET endpoints, so local polling works without credentials while the write
// middleware still guards mutations.
func getJobStatus(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reader, ok := jobReader(searchers...)
		if !ok {
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
		writeJSON(w, http.StatusOK, job)
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
