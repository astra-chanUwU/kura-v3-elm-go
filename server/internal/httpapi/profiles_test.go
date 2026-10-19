package httpapi

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/profiles"
	"github.com/go-chi/chi/v5"
)

type httpMemStore struct {
	rows map[int64]profiles.Record
}

func (m *httpMemStore) GetByUsername(_ context.Context, username string) (profiles.Record, error) {
	for _, rec := range m.rows {
		if rec.Username == username && !rec.Disabled {
			return rec, nil
		}
	}
	return profiles.Record{}, profiles.ErrNotFound
}

func (m *httpMemStore) GetByUserID(_ context.Context, userID int64) (profiles.Record, error) {
	rec, ok := m.rows[userID]
	if !ok || rec.Disabled {
		return profiles.Record{}, profiles.ErrNotFound
	}
	return rec, nil
}

func (m *httpMemStore) Update(_ context.Context, userID int64, input profiles.Input) (profiles.Record, error) {
	rec, ok := m.rows[userID]
	if !ok || rec.Disabled {
		return profiles.Record{}, profiles.ErrNotFound
	}
	normalized, err := profiles.NormalizeProfile(input)
	if err != nil {
		return profiles.Record{}, err
	}
	if rec.Version != normalized.ExpectedVersion {
		return profiles.Record{}, &profiles.VersionConflict{Current: rec}
	}
	for id, other := range m.rows {
		if id != userID && other.Username == normalized.Username {
			return profiles.Record{}, profiles.ErrUsernameTaken
		}
	}
	rec.Username = normalized.Username
	rec.DisplayName = normalized.DisplayName
	rec.Bio = normalized.Bio
	rec.Website = normalized.Website
	rec.Version++
	m.rows[userID] = rec
	return rec, nil
}

func (m *httpMemStore) SetAvatar(_ context.Context, userID, expected int64, key string) (profiles.Record, string, error) {
	rec := m.rows[userID]
	if rec.Version != expected {
		return profiles.Record{}, "", &profiles.VersionConflict{Current: rec}
	}
	previous := rec.AvatarKey
	rec.AvatarKey = key
	rec.Version++
	m.rows[userID] = rec
	return rec, previous, nil
}

func (m *httpMemStore) Username(context.Context, int64) (string, bool, error) {
	return "", false, nil
}

type httpMemMedia struct {
	objects map[string][]byte
}

func (m *httpMemMedia) Put(_ context.Context, key, contentType string, content io.Reader) (media.Object, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return media.Object{}, err
	}
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = data
	return media.Object{Key: key, ContentType: contentType, Created: true}, nil
}
func (m *httpMemMedia) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (m *httpMemMedia) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func TestProfileEditsStayOnTheSignedInAccount(t *testing.T) {
	store := &httpMemStore{rows: map[int64]profiles.Record{
		1: {UserID: 1, Handle: "login-a", Username: "ada", Version: 2},
		2: {UserID: 2, Handle: "login-b", Username: "bea", Version: 4, Bio: "kept"},
	}}
	svc := profiles.NewService(store, &httpMemMedia{})
	router := chi.NewRouter()
	router.Patch("/profile", patchOwnProfile(svc))
	router.Get("/profiles/{username}", getProfile(svc))
	router.Post("/profile/avatar", postProfileAvatar(svc))

	body := []byte(`{"display_name":"Ada","username":"ada2","bio":"","website":"https://example.com","expected_version":2}`)
	request := withActor(httptest.NewRequest(http.MethodPatch, "/profile", bytes.NewReader(body)), Actor{ID: "user:1", UserID: 1, Role: "viewer"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.rows[1].Username != "ada2" || store.rows[1].Handle != "login-a" || store.rows[2].Bio != "kept" {
		t.Fatalf("self edit status=%d body=%s rows=%#v", response.Code, response.Body.String(), store.rows)
	}
	foreign := []byte(`{"display_name":"Stolen","username":"bea","bio":"nope","website":"","expected_version":4,"user_id":2}`)
	request = withActor(httptest.NewRequest(http.MethodPatch, "/profile", bytes.NewReader(foreign)), Actor{ID: "user:1", UserID: 1, Role: "viewer"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || store.rows[2].Bio != "kept" || store.rows[2].DisplayName != "" {
		t.Fatalf("foreign field status=%d rows=%#v", response.Code, store.rows)
	}
	stale := []byte(`{"display_name":"Ada","username":"ada2","bio":"draft stays server-side","website":"","expected_version":2}`)
	request = withActor(httptest.NewRequest(http.MethodPatch, "/profile", bytes.NewReader(stale)), Actor{ID: "user:1", UserID: 1, Role: "viewer"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte(`"conflict":true`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"username":"ada2"`)) {
		t.Fatalf("stale status=%d body=%s", response.Code, response.Body.String())
	}
	if store.rows[1].Bio != "" {
		t.Fatalf("stale edit applied %#v", store.rows[1])
	}
	taken := []byte(`{"display_name":"","username":"ada2","bio":"","website":"","expected_version":4}`)
	request = withActor(httptest.NewRequest(http.MethodPatch, "/profile", bytes.NewReader(taken)), Actor{ID: "user:2", UserID: 2, Role: "editor"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || store.rows[2].Username != "bea" {
		t.Fatalf("taken status=%d row=%#v body=%s", response.Code, store.rows[2], response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/profiles/ada", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("old username status=%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/profiles/ada2", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte("login-a")) {
		t.Fatalf("public profile status=%d body=%s", response.Code, response.Body.String())
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	_ = writer.WriteField("expected_version", strconv.FormatInt(store.rows[1].Version, 10))
	part, err := writer.CreateFormFile("avatar", "x.svg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("<svg></svg>"))
	_ = writer.Close()
	request = withActor(httptest.NewRequest(http.MethodPost, "/profile/avatar", bytes.NewReader(payload.Bytes())), Actor{ID: "user:1", UserID: 1, Role: "viewer"})
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || store.rows[1].AvatarKey != "" {
		t.Fatalf("svg avatar status=%d key=%q body=%s", response.Code, store.rows[1].AvatarKey, response.Body.String())
	}
}
