package media

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKeyFromStoredMediaURLMatchesWriters(t *testing.T) {
	ctx := context.Background()
	local := NewLocalStore(t.TempDir())
	originalKey := "uploads/abc.png"
	original, err := local.Put(ctx, originalKey, "image/png", strings.NewReader("png"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := KeyFromStoredMediaURL(original.URL); err != nil || got != original.Key || got != originalKey {
		t.Fatalf("upload url %q mapped to %q (%v), key %q", original.URL, got, err, original.Key)
	}
	derivativeKey := derivativeKey(originalKey, DefaultDerivativeVariant)
	derivative, err := local.Put(ctx, derivativeKey, "image/jpeg", strings.NewReader("jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := KeyFromStoredMediaURL(derivative.URL); err != nil || got != derivative.Key {
		t.Fatalf("derivative url %q mapped to %q (%v), key %q", derivative.URL, got, err, derivative.Key)
	}

	// Browser import persists the same Store.Put URL as upload.
	importedKey := "uploads/imported.gif"
	imported, err := local.Put(ctx, importedKey, "image/gif", strings.NewReader("gif"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := KeyFromStoredMediaURL(imported.URL); err != nil || got != importedKey {
		t.Fatalf("import url %q mapped to %q (%v)", imported.URL, got, err)
	}

	privateS3, _ := newFakeS3Store(t, "kura-media", SameOriginMediaPrefix, nil)
	stored, err := privateS3.Put(ctx, originalKey, "image/png", strings.NewReader("png"))
	if err != nil {
		t.Fatal(err)
	}
	if stored.URL != SameOriginMediaPrefix+"/"+originalKey {
		t.Fatalf("private s3 url %q", stored.URL)
	}
	if got, err := KeyFromStoredMediaURL(stored.URL); err != nil || got != originalKey {
		t.Fatalf("private s3 url mapped to %q (%v)", got, err)
	}
	thumb, err := privateS3.Put(ctx, derivativeKey, "image/jpeg", strings.NewReader("jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := KeyFromStoredMediaURL(thumb.URL); err != nil || got != derivativeKey {
		t.Fatalf("private s3 derivative mapped to %q (%v)", got, err)
	}

	publicS3, _ := newFakeS3Store(t, "kura-media", "https://cdn.example.com", nil)
	public, err := publicS3.Put(ctx, originalKey, "image/png", strings.NewReader("png"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := KeyFromStoredMediaURL(public.URL); err == nil {
		t.Fatalf("public object url %q mapped without a same-origin reference", public.URL)
	}
	if !PublishesExternalObjectURLs(publicS3) || PublishesExternalObjectURLs(privateS3) || PublishesExternalObjectURLs(local) {
		t.Fatal("external URL classification mismatched the writer prefix")
	}
}

func TestKeyFromStoredMediaURLRejectsUnmappedReferences(t *testing.T) {
	for _, reference := range []string{
		"",
		"/static/icons/shelf.svg",
		"https://cdn.example.com/uploads/abc.jpg",
		"HTTPS://cdn.example.com/uploads/abc.jpg",
		"//cdn.example.com/uploads/abc.jpg",
		"data:image/png;base64,aaaa",
		"/media/../secret",
		"/media/uploads/../../secret",
		"/media/uploads/../secret",
		"/media/%2e%2e/secret",
		"/media/uploads/%2e%2e/secret",
		"/media/uploads/a.jpg?key=other",
		"/media/uploads/a.jpg#frag",
		"/media/C:/windows/secret",
		"/media//uploads/a.jpg",
		"/media/uploads/./a.jpg",
		"/media/uploads\\a.jpg",
		"/media/",
		"media/uploads/a.jpg",
	} {
		if _, err := KeyFromStoredMediaURL(reference); err == nil {
			t.Errorf("mapped %q", reference)
		}
	}
	if got, err := KeyFromStoredMediaURL("/media/demo/cat-original.jpg"); err != nil || got != "demo/cat-original.jpg" {
		t.Fatalf("demo reference mapped to %q (%v)", got, err)
	}
}

func TestPrivateStoreFromEnvIgnoresPublicPrefix(t *testing.T) {
	t.Setenv("KURA_PRIVATE_LIBRARY", "true")
	t.Setenv("S3_ENDPOINT", "http://127.0.0.1:9")
	t.Setenv("S3_BUCKET", "kura-media")
	t.Setenv("S3_URL_PREFIX", "https://cdn.example.com/public")
	t.Setenv("S3_ACCESS_KEY_ID", "fixture-key")
	t.Setenv("S3_SECRET_ACCESS_KEY", "fixture-secret")
	store := NewStoreFromEnv(t.TempDir())
	s3, ok := store.(*S3Store)
	if !ok {
		t.Fatalf("store type %T", store)
	}
	if s3.urlPrefix != SameOriginMediaPrefix || PublishesExternalObjectURLs(s3) {
		t.Fatalf("private prefix %q", s3.urlPrefix)
	}
}

func TestPublicStoreFromEnvKeepsConfiguredPrefix(t *testing.T) {
	t.Setenv("KURA_PRIVATE_LIBRARY", "false")
	t.Setenv("S3_ENDPOINT", "http://127.0.0.1:9")
	t.Setenv("S3_BUCKET", "kura-media")
	t.Setenv("S3_URL_PREFIX", "https://cdn.example.com/public")
	store := NewStoreFromEnv(t.TempDir())
	s3, ok := store.(*S3Store)
	if !ok {
		t.Fatalf("store type %T", store)
	}
	if s3.urlPrefix != "https://cdn.example.com/public" || !PublishesExternalObjectURLs(s3) {
		t.Fatalf("public prefix %q", s3.urlPrefix)
	}
}

func TestS3StoreDoesNotFollowRedirects(t *testing.T) {
	var followed int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("redirected"))
	}))
	t.Cleanup(target.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	store, err := NewS3Store(S3Config{
		Endpoint:        origin.URL,
		Bucket:          "kura-media",
		AccessKeyID:     "fixture-key",
		SecretAccessKey: "fixture-secret",
		URLPrefix:       SameOriginMediaPrefix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), "uploads/abc.jpg"); err == nil {
		t.Fatal("redirected GET was treated as the object")
	}
	if _, err := store.Put(context.Background(), "uploads/abc.jpg", "image/jpeg", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("redirected PUT was treated as stored")
	}
	if followed != 0 {
		t.Fatalf("redirect target received %d requests", followed)
	}
}
