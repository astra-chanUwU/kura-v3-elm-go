package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
	"github.com/go-chi/chi/v5"
)

func recoveryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "backup-1")
	_ = os.MkdirAll(dir, 0700)
	data := []byte("dump")
	if err := os.WriteFile(filepath.Join(dir, "database.dump"), data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	m := map[string]any{"version": 1, "created_at": "2026-10-15T12:00:00Z", "files": []map[string]any{{"path": "database.dump", "size": len(data), "sha256": hex.EncodeToString(sum[:])}}}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRecoveryHandlersRequireAdminAndReturnNoPaths(t *testing.T) {
	root := recoveryFixture(t)
	jobDir := filepath.Join(root, ".kura-backup-jobs")
	if err := os.Mkdir(jobDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "backup-live.json"), []byte(`{"id":"backup-live","status":"running"}`), 0600); err != nil {
		t.Fatal(err)
	}
	service := recovery.New(root)
	req := httptest.NewRequest(http.MethodGet, "/api/recovery/backups", nil)
	w := httptest.NewRecorder()
	RecoveryList(service).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status %d", w.Code)
	}
	req = withActor(req, Actor{ID: "user:12", Role: auth.RoleAdmin})
	w = httptest.NewRecorder()
	RecoveryList(service).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	var listing map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	activeJob, ok := listing["active_job"].(map[string]any)
	if !ok || activeJob["id"] != "backup-live" || activeJob["status"] != "running" {
		t.Fatalf("active job was not exposed through the admin list: %v", listing["active_job"])
	}
	if _, ok := listing["/"].(string); ok {
		t.Fatal("unexpected path field")
	}
	b, _ := json.Marshal(listing)
	if string(b) == "" {
		t.Fatal("empty listing")
	}
}

func TestRecoveryCreateRequiresAdmin(t *testing.T) {
	service := recovery.New(t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/recovery/backups", nil)
	w := httptest.NewRecorder()
	RecoveryCreate(service).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status %d", w.Code)
	}
	req = withActor(httptest.NewRequest(http.MethodPost, "/api/recovery/backups", nil), Actor{ID: "user:3", Role: auth.RoleViewer})
	w = httptest.NewRecorder()
	RecoveryCreate(service).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer status %d", w.Code)
	}
	req = withActor(httptest.NewRequest(http.MethodPost, "/api/recovery/backups", strings.NewReader("{\"output\":\"/tmp\"}")), Actor{ID: "admin", Role: auth.RoleAdmin})
	w = httptest.NewRecorder()
	RecoveryCreate(service).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("body status %d", w.Code)
	}
	for _, body := range []string{" \n{\"output\":\"/tmp\"}", strings.Repeat(" ", 1025)} {
		req = withActor(httptest.NewRequest(http.MethodPost, "/api/recovery/backups", strings.NewReader(body)), Actor{ID: "admin", Role: auth.RoleAdmin})
		w = httptest.NewRecorder()
		RecoveryCreate(service).ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("nonempty or oversized body accepted: %d", w.Code)
		}
	}
}

func TestRecoveryVerifyRouteReturnsReceipt(t *testing.T) {
	service := recovery.New(recoveryFixture(t))
	r := httptest.NewRequest(http.MethodPost, "/api/recovery/backups/backup-1/verify", nil)
	r = withActor(r, Actor{ID: "admin", Role: auth.RoleAdmin})
	route := chi.NewRouteContext()
	route.URLParams.Add("id", "backup-1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
	w := httptest.NewRecorder()
	RecoveryVerify(service).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("verify status %d: %s", w.Code, w.Body.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["id"] != "backup-1" || receipt["verified_at"] == nil {
		t.Fatalf("receipt=%v", receipt)
	}
}
