package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/go-chi/chi/v5"
)

type InvitationService interface {
	CreateInvitation(ctx context.Context, actor auth.Actor, role string) (auth.Invitation, string, error)
	ListInvitations(ctx context.Context, actor auth.Actor) ([]auth.Invitation, error)
	RevokeInvitation(ctx context.Context, actor auth.Actor, id int64) error
	PreviewInvitation(ctx context.Context, token string) (auth.Invitation, error)
	RegisterWithInvitation(ctx context.Context, token, handle, password string) (auth.LoginResult, error)
}

type invitationJSON struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	Used      bool      `json:"used"`
}

func toInvitationJSON(inv auth.Invitation) invitationJSON {
	return invitationJSON{ID: inv.ID, Role: inv.Role, CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt, Revoked: inv.Revoked, Used: inv.Used}
}

func invitationService(cfg AccountsConfig) (InvitationService, bool) {
	svc, ok := cfg.Service.(InvitationService)
	return svc, ok && svc != nil
}

func publicAuthRequestAllowed(cfg AccountsConfig, w http.ResponseWriter, r *http.Request) bool {
	if !isJSONRequest(r) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "request requires application/json")
		return false
	}
	if !checkCookieCSRF(r, cfg.access()) {
		writeJSONError(w, http.StatusForbidden, "untrusted origin")
		return false
	}
	return true
}

func publicAuthLimiter(cfg AccountsConfig) *LoginLimiter {
	if cfg.Limiter != nil {
		return cfg.Limiter
	}
	return NewLoginLimiter()
}

func ListInvitationsHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		actor, ok := cfg.requireAdmin(w, r)
		if !ok {
			return
		}
		svc, ok := invitationService(cfg)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "invitations unavailable")
			return
		}
		items, err := svc.ListInvitations(r.Context(), actor)
		if err != nil {
			writeAccountError(w, err)
			return
		}
		out := make([]invitationJSON, 0, len(items))
		for _, inv := range items {
			out = append(out, toInvitationJSON(inv))
		}
		writeJSON(w, http.StatusOK, struct {
			Invitations []invitationJSON `json:"invitations"`
		}{out})
	}
}

func CreateInvitationHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		actor, ok := cfg.requireAdmin(w, r)
		if !ok {
			return
		}
		if !isJSONRequest(r) {
			writeJSONError(w, http.StatusUnsupportedMediaType, "request requires application/json")
			return
		}
		svc, ok := invitationService(cfg)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "invitations unavailable")
			return
		}
		var body struct {
			Role string `json:"role"`
		}
		if !decodeBoundedJSON(w, r, &body) {
			return
		}
		inv, token, err := svc.CreateInvitation(r.Context(), actor, body.Role)
		if err != nil {
			writeAccountError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			Invitation invitationJSON `json:"invitation"`
			Token      string         `json:"token"`
		}{toInvitationJSON(inv), token})
	}
}

func RevokeInvitationHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		actor, ok := cfg.requireAdmin(w, r)
		if !ok {
			return
		}
		svc, ok := invitationService(cfg)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "invitations unavailable")
			return
		}
		id, err := auth.ParseCanonicalUserID(chi.URLParam(r, "id"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invitation id must be a positive integer")
			return
		}
		if err := svc.RevokeInvitation(r.Context(), actor, id); err != nil {
			if errors.Is(err, auth.ErrInvitationUnavailable) {
				writeJSONError(w, http.StatusNotFound, "invitation not found")
			} else {
				writeAccountError(w, err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func PreviewInvitationHandler(cfg AccountsConfig) http.HandlerFunc {
	limiter := publicAuthLimiter(cfg)
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !publicAuthRequestAllowed(cfg, w, r) {
			return
		}
		release, ok := limiter.Allow(clientIP(r))
		if !ok {
			writeJSONError(w, http.StatusTooManyRequests, "too many authentication attempts")
			return
		}
		defer release()
		var body struct {
			Token string `json:"token"`
		}
		if !decodeBoundedJSON(w, r, &body) {
			return
		}
		svc, ok := invitationService(cfg)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "invitations unavailable")
			return
		}
		inv, err := svc.PreviewInvitation(r.Context(), body.Token)
		if err != nil {
			if errors.Is(err, auth.ErrInvitationUnavailable) {
				writeJSONError(w, http.StatusGone, "invitation unavailable")
			} else {
				writeJSONError(w, http.StatusInternalServerError, "invitation preview failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Role        string    `json:"role"`
			ExpiresAt   time.Time `json:"expires_at"`
			LibraryName string    `json:"library_name"`
		}{inv.Role, inv.ExpiresAt, "Kura"})
	}
}

func RegisterInvitationHandler(cfg AccountsConfig) http.HandlerFunc {
	limiter := publicAuthLimiter(cfg)
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !publicAuthRequestAllowed(cfg, w, r) {
			return
		}
		release, ok := limiter.Allow(clientIP(r))
		if !ok {
			writeJSONError(w, http.StatusTooManyRequests, "too many authentication attempts")
			return
		}
		defer release()
		var body struct {
			Token    string `json:"token"`
			Handle   string `json:"handle"`
			Password string `json:"password"`
		}
		if !decodeBoundedJSON(w, r, &body) {
			return
		}
		svc, ok := invitationService(cfg)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "invitations unavailable")
			return
		}
		result, err := svc.RegisterWithInvitation(r.Context(), body.Token, body.Handle, body.Password)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvitationUnavailable):
				writeJSONError(w, http.StatusGone, "invitation unavailable")
			case errors.Is(err, auth.ErrDuplicateHandle):
				writeJSONError(w, http.StatusConflict, "handle already exists")
			case errors.Is(err, auth.ErrInvalidHandle), errors.Is(err, auth.ErrReservedHandle), errors.Is(err, auth.ErrInvalidPassword):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			default:
				writeJSONError(w, http.StatusInternalServerError, "registration failed")
			}
			return
		}
		cfg.setSessionCookie(w, r, result.RawToken, result.ExpiresAt)
		writeJSON(w, http.StatusCreated, cfg.sessionDocument(r.Context(), result.Actor, result.User, "cookie"))
	}
}
