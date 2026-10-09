package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeS3Transport struct {
	mu           sync.Mutex
	objects      map[string][]byte
	contentTypes map[string]string
	putCount     int
	headCount    int
	deleteCount  int
	bucket       string
	forceHeadErr int
	forcePutErr  int
	capturedAuth string
	hangHead     bool
}

func (f *fakeS3Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.hangHead && req.Method == http.MethodHead {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(5 * time.Second):
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Method == http.MethodHead || req.Method == http.MethodPut || req.Method == http.MethodDelete {
		if auth := req.Header.Get("Authorization"); auth != "" {
			f.capturedAuth = auth
		} else if req.Method == http.MethodPut && f.capturedAuth == "" {
			// capture even empty for verification; don't overwrite on HEAD if already set
			// keep first captured
		}
	}
	prefix := "/" + f.bucket + "/"
	if !strings.HasPrefix(req.URL.Path, prefix) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("missing bucket prefix")),
			Request:    req,
		}, nil
	}
	key := strings.TrimPrefix(req.URL.Path, prefix)
	if decoded, err := url.PathUnescape(key); err == nil {
		key = decoded
	}
	switch req.Method {
	case http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}
		h := http.Header{}
		h.Set("Content-Length", fmt.Sprint(len(data)))
		if ct, ok := f.contentTypes[key]; ok && ct != "" {
			h.Set("Content-Type", ct)
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        h,
			Body:          io.NopCloser(bytes.NewReader(data)),
			ContentLength: int64(len(data)),
			Request:       req,
		}, nil
	case http.MethodHead:
		f.headCount++
		if f.forceHeadErr != 0 {
			return &http.Response{
				StatusCode: f.forceHeadErr,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("forced error")),
				Request:    req,
			}, nil
		}
		data, ok := f.objects[key]
		if !ok {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}
		h := http.Header{}
		h.Set("Content-Length", fmt.Sprint(len(data)))
		if ct, ok := f.contentTypes[key]; ok && ct != "" {
			h.Set("Content-Type", ct)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	case http.MethodPut:
		f.putCount++
		if f.forcePutErr != 0 {
			return &http.Response{
				StatusCode: f.forcePutErr,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("forced put error")),
				Request:    req,
			}, nil
		}
		body, _ := io.ReadAll(req.Body)
		f.objects[key] = body
		f.contentTypes[key] = req.Header.Get("Content-Type")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	case http.MethodDelete:
		f.deleteCount++
		delete(f.objects, key)
		delete(f.contentTypes, key)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	default:
		return &http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("unsupported")),
			Request:    req,
		}, nil
	}
}

func newFakeS3Store(t *testing.T, bucket string, urlPrefix string, transport *fakeS3Transport) (*S3Store, *fakeS3Transport) {
	t.Helper()
	if transport == nil {
		transport = &fakeS3Transport{
			objects:      make(map[string][]byte),
			contentTypes: make(map[string]string),
			bucket:       bucket,
		}
	}
	// Ensure bucket field matches
	transport.bucket = bucket
	if transport.objects == nil {
		transport.objects = make(map[string][]byte)
	}
	if transport.contentTypes == nil {
		transport.contentTypes = make(map[string]string)
	}
	endpoint := "http://fake-s3.test"
	if bucket == "my-bucket" {
		// keeps generic
	}
	cfg := S3Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "secret",
		URLPrefix:       urlPrefix,
		HTTPClient:      &http.Client{Transport: transport},
	}
	store, err := NewS3Store(cfg)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	return store, transport
}

func TestS3StoreValidatesConfig(t *testing.T) {
	if _, err := NewS3Store(S3Config{Endpoint: "", Bucket: "b"}); err == nil {
		t.Fatal("expected endpoint required")
	}
	if _, err := NewS3Store(S3Config{Endpoint: "http://example.com", Bucket: ""}); err == nil {
		t.Fatal("expected bucket required")
	}
	if _, err := NewS3Store(S3Config{Endpoint: "not-a-url", Bucket: "b"}); err == nil {
		t.Fatal("expected invalid endpoint")
	}
	if _, err := NewS3Store(S3Config{Endpoint: "ftp://example.com", Bucket: "b"}); err == nil {
		t.Fatal("expected scheme validation")
	}
	if _, err := NewS3Store(S3Config{Endpoint: "http://example.com", Bucket: "bad/bucket"}); err == nil {
		t.Fatal("expected bucket validation")
	}
	cfg := S3Config{Endpoint: "http://example.com/", Bucket: "my-bucket", Region: "", URLPrefix: "https://cdn.example.com/media/"}
	store, err := NewS3Store(cfg)
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if store.urlPrefix != "https://cdn.example.com/media" {
		t.Fatalf("url prefix should be trimmed, got %q", store.urlPrefix)
	}
	if store.region != "us-east-1" {
		t.Fatalf("region default expected us-east-1, got %q", store.region)
	}
}

func TestS3StorePutIsContentAddressedAndIdempotent(t *testing.T) {
	store, fake := newFakeS3Store(t, "kura-media", "https://cdn.example.com", nil)
	first, err := store.Put(context.Background(), "uploads/abc.jpg", "image/jpeg", strings.NewReader("bytes"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !first.Created || first.URL != "https://cdn.example.com/uploads/abc.jpg" || first.Size != 5 {
		t.Fatalf("unexpected first object: %#v", first)
	}
	if fake.putCount != 1 {
		t.Fatalf("putCount=%d want 1", fake.putCount)
	}
	second, err := store.Put(context.Background(), "uploads/abc.jpg", "image/jpeg", strings.NewReader("different"))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if second.Created || second.Size != 5 {
		t.Fatalf("existing object was overwritten: %#v", second)
	}
	if fake.putCount != 1 {
		t.Fatalf("second put should not issue PUT, putCount=%d", fake.putCount)
	}
	fake.mu.Lock()
	stored := string(fake.objects["uploads/abc.jpg"])
	fake.mu.Unlock()
	if stored != "bytes" {
		t.Fatalf("stored bytes=%q", stored)
	}
}

func TestS3StoreRejectsTraversalAndDeletes(t *testing.T) {
	store, fake := newFakeS3Store(t, "kura-media", "", nil)
	if _, err := store.Put(context.Background(), "../escape", "application/octet-stream", strings.NewReader("x")); err == nil {
		t.Fatal("expected traversal key to fail")
	}
	if err := store.Delete(context.Background(), "../escape"); err == nil {
		t.Fatal("expected traversal delete to fail")
	}
	object, err := store.Put(context.Background(), "uploads/remove.jpg", "image/jpeg", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if object.URL == "" || object.Size != 1 {
		t.Fatalf("unexpected object: %#v", object)
	}
	if err := store.Delete(context.Background(), object.Key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.deleteCount != 1 {
		t.Fatalf("deleteCount=%d", fake.deleteCount)
	}
	if err := store.Delete(context.Background(), object.Key); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	again, err := store.Put(context.Background(), "uploads/remove.jpg", "image/jpeg", strings.NewReader("y"))
	if err != nil {
		t.Fatalf("Put after delete: %v", err)
	}
	if !again.Created {
		t.Fatalf("expected Created after delete, got %#v", again)
	}
}

func TestS3StoreRequiresContent(t *testing.T) {
	store, _ := newFakeS3Store(t, "kura-media", "", nil)
	if _, err := store.Put(context.Background(), "uploads/abc.jpg", "image/jpeg", nil); err == nil {
		t.Fatal("expected nil content to fail")
	}
}

func TestS3StoreContextCancellation(t *testing.T) {
	store, _ := newFakeS3Store(t, "kura-media", "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, "uploads/cancel.jpg", "image/jpeg", strings.NewReader("x")); err == nil {
		t.Fatal("expected cancelled Put to fail")
	}
	if err := store.Delete(ctx, "uploads/cancel.jpg"); err == nil {
		t.Fatal("expected cancelled Delete to fail")
	}
}

func TestS3StoreContextCancellationDuringRequest(t *testing.T) {
	transport := &fakeS3Transport{
		objects:      make(map[string][]byte),
		contentTypes: make(map[string]string),
		bucket:       "kura-media",
		hangHead:     true,
	}
	store, _ := newFakeS3Store(t, "kura-media", "", transport)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := store.Put(ctx, "uploads/hang.jpg", "image/jpeg", strings.NewReader("bytes"))
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
		if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Put did not respect context cancellation")
	}
}

func TestS3StoreURLPrefixFallback(t *testing.T) {
	store, _ := newFakeS3Store(t, "my-bucket", "", nil)
	obj, err := store.Put(context.Background(), "uploads/fallback.jpg", "image/jpeg", strings.NewReader("hi"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	expectedPrefix := "http://fake-s3.test/my-bucket"
	if !strings.HasPrefix(obj.URL, expectedPrefix) {
		t.Fatalf("URL %q should start with %q", obj.URL, expectedPrefix)
	}
	if !strings.HasSuffix(obj.URL, "/uploads/fallback.jpg") {
		t.Fatalf("URL %q should end with key", obj.URL)
	}
}

func TestS3StoreHandlesHeadFailure(t *testing.T) {
	transport := &fakeS3Transport{
		objects:      make(map[string][]byte),
		contentTypes: make(map[string]string),
		bucket:       "kura-media",
		forceHeadErr: http.StatusInternalServerError,
	}
	store, _ := newFakeS3Store(t, "kura-media", "", transport)
	if _, err := store.Put(context.Background(), "uploads/fail.jpg", "image/jpeg", strings.NewReader("x")); err == nil {
		t.Fatal("expected HEAD error to propagate")
	}
	transport.forceHeadErr = 0
	transport.forcePutErr = http.StatusInternalServerError
	if _, err := store.Put(context.Background(), "uploads/fail-put.jpg", "image/jpeg", strings.NewReader("x")); err == nil {
		t.Fatal("expected PUT error to propagate")
	}
}

func TestS3StoreImplementsStoreInterface(t *testing.T) {
	var _ Store = (*S3Store)(nil)
	var _ Store = (*LocalStore)(nil)
	store, _ := newFakeS3Store(t, "b", "", nil)
	var _ Store = store
}

func TestS3StoreValidatesKeyOnDelete(t *testing.T) {
	store, _ := newFakeS3Store(t, "b", "", nil)
	if err := store.Delete(context.Background(), ""); err == nil {
		t.Fatal("expected empty key to fail")
	}
	if err := store.Delete(context.Background(), "/absolute"); err == nil {
		t.Fatal("expected absolute key to fail")
	}
	if err := store.Delete(context.Background(), "../escape"); err == nil {
		t.Fatal("expected traversal delete to fail")
	}
	if err := store.Delete(context.Background(), "a/../../b"); err == nil {
		t.Fatal("expected traversal delete to fail")
	}
}

func TestS3StoreSigningHeaderPresent(t *testing.T) {
	store, fake := newFakeS3Store(t, "kura-media", "", nil)
	_, err := store.Put(context.Background(), "uploads/signed.jpg", "image/jpeg", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	fake.mu.Lock()
	auth := fake.capturedAuth
	fake.mu.Unlock()
	if auth == "" {
		t.Fatal("expected Authorization header when credentials are set")
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Fatalf("unexpected auth header %q", auth)
	}
}

func TestS3StoreWithoutCredentialsSkipsSigning(t *testing.T) {
	transport := &fakeS3Transport{
		objects:      make(map[string][]byte),
		contentTypes: make(map[string]string),
		bucket:       "kura-media",
	}
	cfg := S3Config{
		Endpoint:   "http://fake-s3.test",
		Bucket:     "kura-media",
		Region:     "us-east-1",
		URLPrefix:  "",
		HTTPClient: &http.Client{Transport: transport},
	}
	store, err := NewS3Store(cfg)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	_, err = store.Put(context.Background(), "uploads/noauth.jpg", "image/jpeg", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	transport.mu.Lock()
	auth := transport.capturedAuth
	transport.mu.Unlock()
	if auth != "" {
		t.Fatalf("expected no Authorization header without credentials, got %q", auth)
	}
}

var _ = fmt.Sprint
