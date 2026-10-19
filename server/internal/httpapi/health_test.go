package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	libhealth "github.com/astra-chanUwU/kura-v3-elm-go/server/internal/health"
)

type httpReader struct{}

func (httpReader) Counts(context.Context) (libhealth.Counts, error) {
	return libhealth.Counts{MissingThumbnails: 1, FailedThumbnails: 0, Backlog: 2}, nil
}
func (httpReader) Originals(context.Context, int) ([]libhealth.Original, error) {
	return nil, nil
}
func (httpReader) OriginalKey(context.Context, string) (string, error) {
	return "uploads/a.png", nil
}
func (httpReader) MissingThumbnails(context.Context, int) (libhealth.Page, error) {
	return libhealth.Page{PostIDs: []string{"9"}}, nil
}

type httpRepair struct{ calls int }

func (r *httpRepair) RepairThumbnail(context.Context, string, string) (string, string, error) {
	r.calls++
	return "job-1", "pending", nil
}

func TestLibraryHealthRequiresAdminAndHidesStorageDetails(t *testing.T) {
	repair := &httpRepair{}
	service := libhealth.New(httpReader{}, nil, repair, nil, libhealth.MediaLocal)
	handler := LibraryHealthSummary(service)

	denied := httptest.NewRecorder()
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/health/library", nil), Actor{ID: "viewer", Role: auth.RoleViewer})
	handler.ServeHTTP(denied, req)
	if denied.Code != http.StatusForbidden || strings.Contains(denied.Body.String(), "uploads") {
		t.Fatalf("viewer status %d body %s", denied.Code, denied.Body.String())
	}

	allowed := httptest.NewRecorder()
	admin := withActor(httptest.NewRequest(http.MethodGet, "/api/health/library", nil), Actor{ID: "admin", Role: auth.RoleAdmin})
	handler.ServeHTTP(allowed, admin)
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), `"known":false`) {
		t.Fatalf("admin body %d %s", allowed.Code, allowed.Body.String())
	}
	if strings.Contains(allowed.Body.String(), "uploads/") || strings.Contains(allowed.Body.String(), `\\`) {
		t.Fatal("summary exposed a storage key")
	}

	editor := httptest.NewRecorder()
	LibraryHealthRepair(service).ServeHTTP(editor, withActor(httptest.NewRequest(http.MethodPost, "/api/health/library/repairs", strings.NewReader(`{"post_id":"9"}`)), Actor{ID: "editor", Role: auth.RoleEditor}))
	if editor.Code != http.StatusForbidden {
		t.Fatalf("editor repair %d", editor.Code)
	}

	first := httptest.NewRecorder()
	LibraryHealthRepair(service).ServeHTTP(first, withActor(httptest.NewRequest(http.MethodPost, "/api/health/library/repairs", strings.NewReader(`{"post_id":"9"}`)), Actor{ID: "admin", Role: auth.RoleAdmin}))
	second := httptest.NewRecorder()
	LibraryHealthRepair(service).ServeHTTP(second, withActor(httptest.NewRequest(http.MethodPost, "/api/health/library/repairs", strings.NewReader(`{"post_id":"9"}`)), Actor{ID: "admin", Role: auth.RoleAdmin}))
	if first.Code != http.StatusOK || second.Body.String() != first.Body.String() || repair.calls != 2 || !strings.Contains(first.Body.String(), `"job_id":"job-1"`) {
		t.Fatalf("repair %d %s / %d %s calls %d", first.Code, first.Body.String(), second.Code, second.Body.String(), repair.calls)
	}

	bad := httptest.NewRecorder()
	LibraryHealthRepair(service).ServeHTTP(bad, withActor(httptest.NewRequest(http.MethodPost, "/api/health/library/repairs", strings.NewReader(`{"path":"C:/secret"}`)), Actor{ID: "admin", Role: auth.RoleAdmin}))
	if bad.Code != http.StatusBadRequest || strings.Contains(bad.Body.String(), "secret") {
		t.Fatalf("bad repair %d %s", bad.Code, bad.Body.String())
	}
}

func TestLivenessHealthStaysSeparate(t *testing.T) {
	rec := httptest.NewRecorder()
	health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "missing_originals") {
		t.Fatalf("liveness %d %s", rec.Code, rec.Body.String())
	}
}
