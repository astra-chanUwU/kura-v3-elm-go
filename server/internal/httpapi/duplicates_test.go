package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeDuplicateReader struct {
	groups posts.DuplicateGroupsPage
	groupsErr error
	groupsLimit int
	members posts.DuplicateMembersPage
	membersErr error
	membersHash string
	membersLimit int
}

func (f *fakeDuplicateReader) ListDuplicateGroups(_ context.Context, limit int, _ *posts.DuplicateGroupsCursor) (posts.DuplicateGroupsPage, error) {
	f.groupsLimit = limit
	if f.groupsErr != nil {
		return posts.DuplicateGroupsPage{}, f.groupsErr
	}
	return f.groups, nil
}

func (f *fakeDuplicateReader) ListDuplicateMembers(_ context.Context, hash string, limit int, _ *posts.DuplicateMembersCursor) (posts.DuplicateMembersPage, error) {
	f.membersHash, f.membersLimit = hash, limit
	if f.membersErr != nil {
		return posts.DuplicateMembersPage{}, f.membersErr
	}
	return f.members, nil
}

func duplicateHashForTest(ch byte) string {
	return "sha256:" + strings.Repeat(string([]byte{ch}), 64)
}

func TestDuplicateGroupsValidation(t *testing.T) {
	fake := &fakeDuplicateReader{}
	r := NewRouterWithDependencies("", Dependencies{Duplicates: fake})
	for _, raw := range []string{"/api/posts/duplicates?limit=0", "/api/posts/duplicates?limit=31", "/api/posts/duplicates?limit=nope", "/api/posts/duplicates?limit="} {
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("GET %s: got status %d", raw, resp.Code)
		}
	}
	for _, raw := range []string{"/api/posts/duplicates?cursor=bad", "/api/posts/duplicates?cursor="} {
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("GET %s: got status %d", raw, resp.Code)
		}
	}
	// A member cursor must never open the group listing.
	member, err := posts.EncodeDuplicateMembersCursor(duplicateHashForTest('a'), 1)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/posts/duplicates?cursor="+member, nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("member cursor as group cursor: got status %d", resp.Code)
	}
	if fake.groupsLimit != 0 {
		t.Fatal("invalid requests reached the reader")
	}
}

func TestDuplicateGroupsDefaultsAndMissingCapability(t *testing.T) {
	fake := &fakeDuplicateReader{groups: posts.DuplicateGroupsPage{Groups: []posts.DuplicateGroup{}}}
	r := NewRouterWithDependencies("", Dependencies{Duplicates: fake})
	req := httptest.NewRequest(http.MethodGet, "/api/posts/duplicates", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || fake.groupsLimit != 20 {
		t.Fatalf("default groups limit: status=%d limit=%d", resp.Code, fake.groupsLimit)
	}
	var body posts.DuplicateGroupsPage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Groups == nil || body.NextCursor != nil {
		t.Fatalf("groups envelope not normalized: %#v", body)
	}

	empty := NewRouterWithDependencies("", Dependencies{})
	req = httptest.NewRequest(http.MethodGet, "/api/posts/duplicates", nil)
	resp = httptest.NewRecorder()
	empty.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing capability: got status %d", resp.Code)
	}
}

func TestDuplicateGroupsErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{
		{posts.ErrInvalidCursor, http.StatusBadRequest},
		{posts.ErrInvalidQuery, http.StatusBadRequest},
		{posts.ErrUnavailable, http.StatusServiceUnavailable},
		{errors.New("db down"), http.StatusInternalServerError},
	} {
		fake := &fakeDuplicateReader{groupsErr: tc.err}
		r := NewRouterWithDependencies("", Dependencies{Duplicates: fake})
		req := httptest.NewRequest(http.MethodGet, "/api/posts/duplicates", nil)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != tc.code {
			t.Errorf("err %v: got status %d, want %d", tc.err, resp.Code, tc.code)
		}
	}
}

func TestDuplicateMembersValidationAndPrivacy(t *testing.T) {
	hash := duplicateHashForTest('c')
	fake := &fakeDuplicateReader{members: posts.DuplicateMembersPage{Hash: hash, Count: 2, Posts: []posts.PostSummary{{ID: "1"}}}}
	r := NewRouterWithDependencies("secret", Dependencies{Duplicates: fake})
	// Public read: no token required even when writes require auth.
	req := httptest.NewRequest(http.MethodGet, "/api/posts/duplicates/"+hash, nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || fake.membersHash != hash || fake.membersLimit != 60 {
		t.Fatalf("public member read: status=%d hash=%q limit=%d", resp.Code, fake.membersHash, fake.membersLimit)
	}
	// Escaped colon decodes to the same group.
	escaped := "/api/posts/duplicates/" + "sha256%3A" + strings.Repeat("c", 64)
	req = httptest.NewRequest(http.MethodGet, escaped, nil)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("escaped colon: got status %d", resp.Code)
	}
	for _, raw := range []string{
		"/api/posts/duplicates/not-a-hash",
		"/api/posts/duplicates/sha256:" + strings.Repeat("G", 64),
		"/api/posts/duplicates/" + hash + "?limit=0",
		"/api/posts/duplicates/" + hash + "?limit=61",
		"/api/posts/duplicates/" + hash + "?cursor=bad",
		"/api/posts/duplicates/" + hash + "?cursor=",
	} {
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("GET %s: got status %d", raw, resp.Code)
		}
	}
	// Foreign-hash and group-route cursors are rejected.
	other := duplicateHashForTest('d')
	foreign, err := posts.EncodeDuplicateMembersCursor(other, 1)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/posts/duplicates/"+hash+"?cursor="+foreign, nil)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("foreign member cursor: got status %d", resp.Code)
	}
	group, err := posts.EncodeDuplicateGroupsCursor(hash)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/posts/duplicates/"+hash+"?cursor="+group, nil)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("group cursor as member cursor: got status %d", resp.Code)
	}
}

func TestDuplicateMembersErrorMapping(t *testing.T) {
	hash := duplicateHashForTest('e')
	for _, tc := range []struct {
		err  error
		code int
	}{
		{posts.ErrNotFound, http.StatusNotFound},
		{posts.ErrInvalidCursor, http.StatusBadRequest},
		{posts.ErrInvalidQuery, http.StatusBadRequest},
		{posts.ErrUnavailable, http.StatusServiceUnavailable},
		{errors.New("db down"), http.StatusInternalServerError},
	} {
		fake := &fakeDuplicateReader{membersErr: tc.err}
		r := NewRouterWithDependencies("", Dependencies{Duplicates: fake})
		req := httptest.NewRequest(http.MethodGet, "/api/posts/duplicates/"+hash, nil)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != tc.code {
			t.Errorf("err %v: got status %d, want %d", tc.err, resp.Code, tc.code)
		}
	}
	empty := NewRouterWithDependencies("", Dependencies{})
	req := httptest.NewRequest(http.MethodGet, "/api/posts/duplicates/"+hash, nil)
	resp := httptest.NewRecorder()
	empty.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing member capability: got status %d", resp.Code)
	}
}
