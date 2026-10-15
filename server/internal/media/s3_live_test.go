package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestS3LiveRoundTripAcceptance is an opt-in acceptance check for a local,
// disposable S3-compatible bucket. It never targets public endpoints and it
// only deletes the random keys created during this test.
func TestS3LiveRoundTripAcceptance(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("KURA_S3_TEST_ENDPOINT"))
	if endpoint == "" {
		t.Skip("set KURA_S3_TEST_ENDPOINT and the remaining KURA_S3_TEST_* values to run local S3 acceptance")
	}

	bucket := strings.TrimSpace(os.Getenv("KURA_S3_TEST_BUCKET"))
	accessKey := strings.TrimSpace(os.Getenv("KURA_S3_TEST_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("KURA_S3_TEST_SECRET_ACCESS_KEY"))
	if bucket == "" || accessKey == "" || secretKey == "" {
		t.Fatal("local S3 acceptance requires KURA_S3_TEST_BUCKET, KURA_S3_TEST_ACCESS_KEY_ID, and KURA_S3_TEST_SECRET_ACCESS_KEY")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("KURA_S3_TEST_ENDPOINT must be an HTTP(S) endpoint without credentials, query, or fragment")
	}
	host := parsed.Hostname()
	if !isLoopbackS3TestHost(host) {
		t.Fatal("S3 live acceptance is restricted to localhost or a loopback IP")
	}

	region := strings.TrimSpace(os.Getenv("KURA_S3_TEST_REGION"))
	urlPrefix := strings.TrimRight(strings.TrimSpace(os.Getenv("KURA_S3_TEST_URL_PREFIX")), "/")
	store, err := NewS3Store(S3Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		Region:          region,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		URLPrefix:       urlPrefix,
	})
	if err != nil {
		t.Fatalf("configure local S3 store: %v", err)
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("create unique test key: %v", err)
	}
	prefix := "kura-acceptance/" + hex.EncodeToString(nonce[:])
	originalKey := prefix + "/uploads/original.bin"
	thumbKey := prefix + "/derivatives/thumb-320/poster.jpg"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := store.Delete(cleanupCtx, originalKey); err != nil {
			t.Errorf("remove unique acceptance original: %v", err)
		}
		if err := store.Delete(cleanupCtx, thumbKey); err != nil {
			t.Errorf("remove unique acceptance thumbnail: %v", err)
		}
	})

	original := []byte("synthetic original bytes; no real media")
	first, err := store.Put(ctx, originalKey, "application/octet-stream", bytes.NewReader(original))
	if err != nil {
		t.Fatalf("put original: %v", err)
	}
	if !first.Created || first.Key != originalKey || first.Size != int64(len(original)) {
		t.Fatalf("first original put metadata = created:%t key:%q size:%d", first.Created, first.Key, first.Size)
	}
	assertProviderURL(t, first, endpoint, bucket, urlPrefix)

	retry, err := store.Put(ctx, originalKey, "application/octet-stream", bytes.NewReader([]byte("replacement bytes must not win")))
	if err != nil {
		t.Fatalf("repeat original put: %v", err)
	}
	if retry.Created || retry.Size != int64(len(original)) {
		t.Fatalf("repeat original put metadata = created:%t size:%d; original must remain immutable", retry.Created, retry.Size)
	}
	assertObjectBytes(t, ctx, store, originalKey, original)

	thumbnail := []byte("synthetic JPEG poster bytes")
	thumb, err := store.Put(ctx, thumbKey, "image/jpeg", bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatalf("put derivative: %v", err)
	}
	if !thumb.Created || thumb.Key == first.Key || thumb.ContentType != "image/jpeg" {
		t.Fatalf("derivative metadata = created:%t key:%q type:%q", thumb.Created, thumb.Key, thumb.ContentType)
	}
	assertProviderURL(t, thumb, endpoint, bucket, urlPrefix)
	assertObjectBytes(t, ctx, store, thumbKey, thumbnail)

	if err := store.Delete(ctx, thumbKey); err != nil {
		t.Fatalf("delete derivative: %v", err)
	}
	if _, err := store.Open(ctx, thumbKey); err == nil {
		t.Fatal("deleted derivative is still readable")
	}
	if err := store.Delete(ctx, originalKey); err != nil {
		t.Fatalf("delete original: %v", err)
	}
	if _, err := store.Open(ctx, originalKey); err == nil {
		t.Fatal("deleted original is still readable")
	}
}

func isLoopbackS3TestHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func assertProviderURL(t *testing.T, object Object, endpoint, bucket, urlPrefix string) {
	t.Helper()
	prefix := urlPrefix
	if prefix == "" {
		prefix = strings.TrimRight(endpoint, "/") + "/" + bucket
	}
	want := prefix + "/" + object.Key
	if object.URL != want {
		t.Fatalf("provider URL = %q, want %q", object.URL, want)
	}
}

func assertObjectBytes(t *testing.T, ctx context.Context, store *S3Store, key string, want []byte) {
	t.Helper()
	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("open %s: %v", key, err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		t.Fatalf("read %s: %v", key, readErr)
	}
	if closeErr != nil {
		t.Fatalf("close %s: %v", key, closeErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round-trip bytes differ for %s", key)
	}
}
