package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/profiles"
	"github.com/go-chi/chi/v5"
)

func getOwnProfile(svc *profiles.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
			return
		}
		actor, ok := actorFromRequest(r)
		if !ok || actor.UserID <= 0 {
			writeJSONError(w, http.StatusNotFound, "profile not found")
			return
		}
		profile, err := svc.Own(r.Context(), actor.UserID)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profile.WithViewer(actor.UserID))
	}
}

func getProfile(svc *profiles.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
			return
		}
		username := chi.URLParam(r, "username")
		if !profiles.ValidPathSegment(username) {
			writeJSONError(w, http.StatusNotFound, "profile not found")
			return
		}
		profile, err := svc.View(r.Context(), username)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		viewer, _ := actorFromRequest(r)
		writeJSON(w, http.StatusOK, profile.WithViewer(viewer.UserID))
	}
}

func patchOwnProfile(svc *profiles.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
			return
		}
		actor, ok := actorFromRequest(r)
		if !ok || actor.UserID <= 0 {
			writeJSONError(w, http.StatusForbidden, "you can only edit your own profile")
			return
		}
		var input profiles.Input
		if err := decodeJSONBody(w, r, &input, "profile"); err != nil {
			return
		}
		profile, err := svc.Update(r.Context(), actor.UserID, input)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profile.WithViewer(actor.UserID))
	}
}

func postProfileAvatar(svc *profiles.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
			return
		}
		actor, ok := actorFromRequest(r)
		if !ok || actor.UserID <= 0 {
			writeJSONError(w, http.StatusForbidden, "you can only edit your own profile")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, (2<<20)+8192)
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			writeJSONError(w, http.StatusBadRequest, "picture could not be read")
			return
		}
		version, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("expected_version")), 10, 64)
		if err != nil || version < 1 {
			writeJSONError(w, http.StatusBadRequest, "expected_version must be a positive integer")
			return
		}
		file, _, err := r.FormFile("avatar")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "picture is required")
			return
		}
		defer file.Close()
		data, err := profiles.ReadLimited(file)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		profile, err := svc.ReplaceAvatar(r.Context(), actor.UserID, version, data)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profile.WithViewer(actor.UserID))
	}
}

func deleteProfileAvatar(svc *profiles.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
			return
		}
		actor, ok := actorFromRequest(r)
		if !ok || actor.UserID <= 0 {
			writeJSONError(w, http.StatusForbidden, "you can only edit your own profile")
			return
		}
		var body struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if err := decodeJSONBody(w, r, &body, "profile"); err != nil {
			return
		}
		profile, err := svc.ClearAvatar(r.Context(), actor.UserID, body.ExpectedVersion)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profile.WithViewer(actor.UserID))
	}
}

func getProfileAvatar(svc *profiles.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", "Authorization")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
			return
		}
		username := chi.URLParam(r, "username")
		if !profiles.ValidPathSegment(username) {
			writeJSONError(w, http.StatusNotFound, "profile not found")
			return
		}
		data, err := svc.AvatarBytes(r.Context(), username)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
	}
}

func listProfileLists(svc *profiles.Service, lists posts.ProfileListReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if svc == nil || lists == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
			return
		}
		username := chi.URLParam(r, "username")
		if !profiles.ValidPathSegment(username) {
			writeJSONError(w, http.StatusNotFound, "profile not found")
			return
		}
		profile, err := svc.View(r.Context(), username)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		limit, err := posts.NormalizeProfileListLimit(0, false)
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, parseErr := strconv.Atoi(raw)
			if parseErr != nil {
				writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 24")
				return
			}
			limit, err = posts.NormalizeProfileListLimit(parsed, true)
		}
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 24")
			return
		}
		var cursor *posts.ProfileListCursor
		if r.URL.Query().Has("cursor") {
			decoded, decodeErr := posts.DecodeProfileListCursor(r.URL.Query().Get("cursor"), profile.UserID)
			if decodeErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid list cursor")
				return
			}
			cursor = &decoded
		}
		page, err := lists.ListProfileLists(r.Context(), profile.UserID, cursor, limit)
		if err != nil {
			if errors.Is(err, posts.ErrInvalidCursor) {
				writeJSONError(w, http.StatusBadRequest, "invalid list cursor")
				return
			}
			if errors.Is(err, posts.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "profile not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "lists unavailable")
			return
		}
		if page.Lists == nil {
			page.Lists = []posts.ProfileList{}
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, profiles.ErrUsernameTaken):
		writeJSONError(w, http.StatusConflict, "username is already in use")
	case errors.Is(err, profiles.ErrConflict):
		current, ok := profiles.ConflictProfile(err)
		if !ok {
			writeJSONError(w, http.StatusConflict, "profile changed")
			return
		}
		writeJSON(w, http.StatusConflict, struct {
			Error    string           `json:"error"`
			Conflict bool             `json:"conflict"`
			Profile  profiles.Profile `json:"profile"`
		}{Error: "profile changed", Conflict: true, Profile: current.WithViewer(current.UserID)})
	case errors.Is(err, profiles.ErrForbidden):
		writeJSONError(w, http.StatusForbidden, "you can only edit your own profile")
	case errors.Is(err, profiles.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "profile not found")
	case errors.Is(err, profiles.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "profiles unavailable")
	case errors.Is(err, profiles.ErrAvatar), errors.Is(err, profiles.ErrInvalidUsername), errors.Is(err, profiles.ErrReservedName), errors.Is(err, profiles.ErrInvalidWebsite), errors.Is(err, profiles.ErrInvalidProfile):
		writeJSONError(w, http.StatusBadRequest, publicProfileMessage(err))
	default:
		if errors.Is(err, io.ErrUnexpectedEOF) {
			writeJSONError(w, http.StatusBadRequest, "picture could not be read")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "profile update failed")
	}
}

func publicProfileMessage(err error) string {
	message := err.Error()
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		message = message[i+1:]
	}
	message = strings.TrimSpace(message)
	if message == "" || strings.Contains(message, "\n") {
		return "profile is not valid"
	}
	return message
}
