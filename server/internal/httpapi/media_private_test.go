package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type memoryStore struct {
	objects map[string][]byte
	opened  []string
}

func (m *memoryStore) Put(_ context.Context, key, _ string, content io.Reader) (media.Object, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return media.Object{}, err
	}
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = data
	return media.Object{Key: key, URL: "/media/" + key, Size: int64(len(data)), Created: true}, nil
}

func (m *memoryStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.opened = append(m.opened, key)
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memoryStore) Stat(_ context.Context, key string) (int64, error) {
	data, ok := m.objects[key]
	if !ok {
		return 0, errors.New("missing")
	}
	return int64(len(data)), nil
}

func (m *memoryStore) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	body, err := m.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	if _, err := io.CopyN(io.Discard, body, offset); err != nil {
		_ = body.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{Reader: io.LimitReader(body, length), Closer: body}, nil
}

func (m *memoryStore) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

type storedRef struct {
	key       string
	mediaType string
}

type privateLibrary struct {
	visibility map[string]posts.MediaVisibility
	refs       map[string]storedRef
	resolved   []string
}

func (p *privateLibrary) LookupMediaVisibility(_ context.Context, mediaPath string) (posts.MediaVisibility, error) {
	if p.visibility == nil {
		return posts.MediaVisibility{}, nil
	}
	return p.visibility[mediaPath], nil
}

func (p *privateLibrary) ResolveStoredMediaKey(_ context.Context, mediaPath string) (string, string, error) {
	p.resolved = append(p.resolved, mediaPath)
	ref, ok := p.refs[mediaPath]
	if !ok {
		return "", "", posts.ErrNotFound
	}
	return ref.key, ref.mediaType, nil
}

func privateMediaRouter(t *testing.T, library *privateLibrary, store media.Store) (http.Handler, *http.Cookie, *http.Cookie) {
	t.Helper()
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)
	deps := Dependencies{
		MediaVisibility:   library,
		MediaStore:        store,
		Accounts:          &cfg,
		ModerationPreview: &fakePreview{preview: posts.ModerationPreview{Body: io.NopCloser(strings.NewReader("jpeg-bytes")), MediaType: "image/jpeg", Length: 10}},
	}
	router := NewRouterWithDependencies("deploy-secret", deps)
	viewer := loginCookie(t, cfg, "viewer", "viewer-password-1")
	admin := loginCookie(t, cfg, "admin", "admin-password-1")
	return router, viewer, admin
}

func mediaRequest(method, path, query string, cookie *http.Cookie, bearer string) *http.Request {
	target := path
	if query != "" {
		target += "?" + query
	}
	req := httptest.NewRequest(method, target, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req
}

func TestPrivateMediaAuthorizesVisibilityAndIgnoresBrowserKeys(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "loose.jpg"), []byte("loose-local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "published.jpg"), []byte("local-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	published := "/media/uploads/published.jpg"
	hidden := "/media/uploads/hidden.jpg"
	rejected := "/media/uploads/rejected.jpg"
	deleted := "/media/uploads/deleted.jpg"
	store := &memoryStore{objects: map[string][]byte{
		"uploads/published.jpg": []byte("store-published"),
		"uploads/other.jpg":     []byte("store-other"),
	}}
	library := &privateLibrary{
		visibility: map[string]posts.MediaVisibility{
			published: {Referenced: true, Visible: true},
			hidden:    {Referenced: true, Visible: false},
			rejected:  {Referenced: true, Visible: false},
			deleted:   {Referenced: true, Visible: false},
		},
		refs: map[string]storedRef{
			published: {key: "uploads/published.jpg", mediaType: "image/jpeg"},
		},
	}
	router, viewer, admin := privateMediaRouter(t, library, store)

	serve := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(mediaRequest(http.MethodGet, published, "", nil, "")); rec.Code != http.StatusUnauthorized || len(store.opened) != 0 {
		t.Fatalf("anonymous media %d opened=%v", rec.Code, store.opened)
	}

	rec := serve(mediaRequest(http.MethodGet, published, "key=uploads/other.jpg", viewer, ""))
	if rec.Code != http.StatusOK || rec.Body.String() != "store-published" {
		t.Fatalf("viewer media %d body=%q", rec.Code, rec.Body.String())
	}
	if strings.Join(store.opened, ",") != "uploads/published.jpg" {
		t.Fatalf("opened %#v", store.opened)
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("cache headers %v", rec.Header())
	}
	vary := strings.Join(rec.Header().Values("Vary"), ",")
	if !strings.Contains(vary, "Cookie") || !strings.Contains(vary, "Authorization") {
		t.Fatalf("vary %q", vary)
	}

	store.opened = nil
	if rec := serve(mediaRequest(http.MethodGet, published, "", admin, "")); rec.Code != http.StatusOK || rec.Body.String() != "store-published" {
		t.Fatalf("admin cookie %d body=%q", rec.Code, rec.Body.String())
	}
	store.opened = nil
	if rec := serve(mediaRequest(http.MethodGet, published, "", nil, "deploy-secret")); rec.Code != http.StatusOK || rec.Body.String() != "store-published" {
		t.Fatalf("admin bearer %d body=%q", rec.Code, rec.Body.String())
	}
	store.opened = nil
	if rec := serve(mediaRequest(http.MethodHead, published, "", viewer, "")); rec.Code != http.StatusOK || rec.Body.Len() != 0 || len(store.opened) != 0 || rec.Header().Get("Content-Length") != "15" {
		t.Fatalf("head %d body=%d opened=%v", rec.Code, rec.Body.Len(), store.opened)
	}

	before := len(library.resolved)
	store.opened = nil
	for _, path := range []string{hidden, rejected, deleted} {
		for _, actor := range []struct {
			cookie *http.Cookie
			bearer string
		}{{viewer, ""}, {admin, ""}, {nil, "deploy-secret"}} {
			if rec := serve(mediaRequest(http.MethodGet, path, "", actor.cookie, actor.bearer)); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "store-") {
				t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
			}
		}
	}
	if len(store.opened) != 0 || len(library.resolved) != before {
		t.Fatalf("denied media opened=%v resolved=%v", store.opened, library.resolved[before:])
	}

	for _, path := range []string{"/media/../secret", "/media/uploads/../../secret", "/media/uploads/%2e%2e/secret", "/media/uploads/./published.jpg"} {
		store.opened = nil
		if rec := serve(mediaRequest(http.MethodGet, path, "", viewer, "")); rec.Code != http.StatusNotFound || len(store.opened) != 0 {
			t.Fatalf("traversal %s status=%d opened=%v", path, rec.Code, store.opened)
		}
	}

	store.opened = nil
	if rec := serve(mediaRequest(http.MethodGet, "/media/uploads/loose.jpg", "", viewer, "")); rec.Code != http.StatusOK || rec.Body.String() != "loose-local" || len(store.opened) != 0 {
		t.Fatalf("unreferenced local %d body=%q opened=%v", rec.Code, rec.Body.String(), store.opened)
	}

	store.opened = nil
	if rec := serve(mediaRequest(http.MethodPost, published, "", nil, "deploy-secret")); rec.Code != http.StatusMethodNotAllowed || len(store.opened) != 0 {
		t.Fatalf("post media %d opened=%v", rec.Code, store.opened)
	}

	library.refs[published] = storedRef{key: "uploads/other.jpg", mediaType: "image/jpeg"}
	store.opened = nil
	if rec := serve(mediaRequest(http.MethodGet, published, "", viewer, "")); rec.Code != http.StatusNotFound || len(store.opened) != 0 || strings.Contains(rec.Body.String(), "store-other") {
		t.Fatalf("mismatched writer key %d opened=%v body=%s", rec.Code, store.opened, rec.Body.String())
	}

	rec = serve(mediaRequest(http.MethodGet, "/api/posts/9/moderation/preview", "", admin, ""))
	if rec.Code != http.StatusOK || rec.Body.String() != "jpeg-bytes" || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("moderation preview %d cache=%q body=%q", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	if rec := serve(mediaRequest(http.MethodGet, "/api/posts/9/moderation/preview", "", viewer, "")); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer preview %d", rec.Code)
	}
}

func TestPublicModeDoesNotOpenConfiguredStore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "published.jpg"), []byte("public-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{objects: map[string][]byte{"uploads/published.jpg": []byte("store-bytes")}}
	library := &privateLibrary{visibility: map[string]posts.MediaVisibility{
		"/media/uploads/published.jpg": {Referenced: true, Visible: true},
	}}
	router := NewRouterWithDependencies("", Dependencies{MediaVisibility: library, MediaStore: store})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/uploads/published.jpg", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "public-file" || len(store.opened) != 0 {
		t.Fatalf("public media %d body=%q opened=%v", rec.Code, rec.Body.String(), store.opened)
	}
}

func TestPrivateMediaReadsDisposableS3Endpoint(t *testing.T) {
	var followed int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed++
		http.Error(w, "followed", http.StatusOK)
	}))
	t.Cleanup(target.Close)

	objects := map[string][]byte{}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/fixture-bucket/")
		if r.Method == http.MethodGet && key == "uploads/redirect.jpg" {
			http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
			return
		}
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			objects[key] = append([]byte(nil), data...)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet, http.MethodHead:
			data, ok := objects[key]
			if !ok {
				http.NotFound(w, r)
				return
			}
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(origin.Close)

	store, err := media.NewS3Store(media.S3Config{
		Endpoint:        origin.URL,
		Bucket:          "fixture-bucket",
		AccessKeyID:     "fixture-key",
		SecretAccessKey: "fixture-secret",
		URLPrefix:       media.SameOriginMediaPrefix,
	})
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.Put(context.Background(), "uploads/published.jpg", "image/jpeg", strings.NewReader("s3-published"))
	if err != nil {
		t.Fatal(err)
	}
	if object.URL != "/media/uploads/published.jpg" {
		t.Fatalf("stored url %q", object.URL)
	}
	if _, err := store.Put(context.Background(), "uploads/redirect.jpg", "image/jpeg", strings.NewReader("nope")); err != nil {
		t.Fatal(err)
	}
	library := &privateLibrary{
		visibility: map[string]posts.MediaVisibility{
			"/media/uploads/published.jpg": {Referenced: true, Visible: true},
			"/media/uploads/redirect.jpg":  {Referenced: true, Visible: true},
		},
		refs: map[string]storedRef{
			"/media/uploads/published.jpg": {key: "uploads/published.jpg", mediaType: "image/jpeg"},
			"/media/uploads/redirect.jpg":  {key: "uploads/redirect.jpg", mediaType: "image/jpeg"},
		},
	}
	router, viewer, _ := privateMediaRouter(t, library, store)
	rec := httptest.NewRecorder()
	req := mediaRequest(http.MethodGet, "/media/uploads/published.jpg", "key=uploads/redirect.jpg", viewer, "")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "s3-published" || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("s3 media %d cache=%q body=%q", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, mediaRequest(http.MethodGet, "/media/uploads/redirect.jpg", "", viewer, ""))
	if rec.Code != http.StatusNotFound || followed != 0 {
		t.Fatalf("redirect media %d followed=%d body=%s", rec.Code, followed, rec.Body.String())
	}
}
