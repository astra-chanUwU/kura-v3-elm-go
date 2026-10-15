package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
	"github.com/go-chi/chi/v5"
)

const RecoveryVerifyTimeout = 120 * time.Second

// RecoveryList and RecoveryVerify are exposed for router composition. Register
// them at GET /api/recovery/backups and POST /api/recovery/backups/{id}/verify.
func RecoveryList(service *recovery.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !recoveryAdmin(w, r) {
			return
		}
		if service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "recovery unavailable")
			return
		}
		listing, err := service.List(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "backup listing unavailable")
			return
		}
		writeJSON(w, http.StatusOK, listing)
	}
}

func RecoveryCreate(service *recovery.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !recoveryAdmin(w, r) {
			return
		}
		if service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "recovery unavailable")
			return
		}
		buf := make([]byte, 1)
		n, _ := r.Body.Read(buf)
		if n > 0 && strings.TrimSpace(string(buf[:n])) != "" {
			writeJSONError(w, http.StatusBadRequest, "backup creation does not accept a request body")
			return
		}
		job, err := service.Start(r.Context())
		if err != nil {
			writeRecoveryCreateError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	}
}

func RecoveryJob(service *recovery.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !recoveryAdmin(w, r) {
			return
		}
		if service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "recovery unavailable")
			return
		}
		job, err := service.Job(chi.URLParam(r, "id"))
		if err != nil {
			writeRecoveryCreateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	}
}

func RecoveryCancel(service *recovery.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !recoveryAdmin(w, r) {
			return
		}
		if service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "recovery unavailable")
			return
		}
		job, err := service.Cancel(chi.URLParam(r, "id"))
		if err != nil {
			writeRecoveryCreateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	}
}

func writeRecoveryCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recovery.ErrUnconfigured):
		writeJSONError(w, http.StatusServiceUnavailable, "backup root is not configured")
	case errors.Is(err, recovery.ErrUnsupported):
		writeJSONError(w, http.StatusServiceUnavailable, "S3 backups are not supported")
	case errors.Is(err, recovery.ErrActive), errors.Is(err, recovery.ErrBusy):
		writeJSONError(w, http.StatusConflict, "a backup is already running")
	case errors.Is(err, recovery.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "backup not found")
	case errors.Is(err, recovery.ErrInvalid):
		writeJSONError(w, http.StatusUnprocessableEntity, "backup verification failed")
	default:
		writeJSONError(w, http.StatusServiceUnavailable, "backup creation unavailable")
	}
}

func RecoveryVerify(service *recovery.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !recoveryAdmin(w, r) {
			return
		}
		if service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "recovery unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), RecoveryVerifyTimeout)
		defer cancel()
		receipt, err := service.Verify(ctx, chi.URLParam(r, "id"))
		if err != nil {
			switch {
			case errors.Is(err, recovery.ErrUnconfigured):
				writeJSONError(w, http.StatusServiceUnavailable, "backup root is not configured")
			case errors.Is(err, recovery.ErrBusy):
				writeJSONError(w, http.StatusConflict, "backup verification is busy; retry later")
			case errors.Is(err, recovery.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "backup not found")
			case errors.Is(err, recovery.ErrInvalid):
				writeJSONError(w, http.StatusUnprocessableEntity, "backup verification failed")
			case errors.Is(err, recovery.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
				writeJSONError(w, http.StatusGatewayTimeout, "backup verification timed out")
			case errors.Is(err, recovery.ErrCanceled), errors.Is(err, context.Canceled):
				writeJSONError(w, http.StatusRequestTimeout, "backup verification canceled")
			default:
				writeJSONError(w, http.StatusServiceUnavailable, "backup verification unavailable")
			}
			return
		}
		writeJSON(w, http.StatusOK, receipt)
	}
}

func recoveryAdmin(w http.ResponseWriter, r *http.Request) bool {
	actor, ok := actorFromRequest(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "admin capability required")
		return false
	}
	if !auth.HasAdminCapability(actor) {
		writeJSONError(w, http.StatusForbidden, "admin capability required")
		return false
	}
	return true
}
