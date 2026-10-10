package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
)

type capabilitiesResponse struct {
	WritesRequireAuth bool    `json:"writes_require_auth"`
	CanWrite          bool    `json:"can_write"`
	Actor             *string `json:"actor"`
}

func capabilities(apiToken string) http.HandlerFunc {
	expected := strings.TrimSpace(apiToken)
	return func(w http.ResponseWriter, r *http.Request) {
		writesRequireAuth := expected != ""
		rawAuth := r.Header.Get("Authorization")
		hasAuth := len(r.Header.Values("Authorization")) > 0
		provided := bearerToken(rawAuth)

		if !writesRequireAuth {
			local := auth.LocalActorID
			writeJSON(w, http.StatusOK, capabilitiesResponse{
				WritesRequireAuth: false,
				CanWrite:          true,
				Actor:             &local,
			})
			return
		}
		if !hasAuth {
			writeJSON(w, http.StatusOK, capabilitiesResponse{
				WritesRequireAuth: true,
				CanWrite:          false,
				Actor:             nil,
			})
			return
		}
		if provided == "" || !sameToken(provided, expected) {
			writeJSONError(w, http.StatusUnauthorized, "write capability required")
			return
		}
		system := auth.SystemActorID
		writeJSON(w, http.StatusOK, capabilitiesResponse{
			WritesRequireAuth: true,
			CanWrite:          true,
			Actor:             &system,
		})
	}
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
	}{Status: "ok"})
}
