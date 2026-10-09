package media

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
)

type fakeVariantRecorder struct {
	rows map[string]Variant
}

func newFakeVariantRecorder() *fakeVariantRecorder {
	return &fakeVariantRecorder{rows: make(map[string]Variant)}
}

func variantKey(postID int64, name string) string {
	return strconv.FormatInt(postID, 10) + ":" + name
}

func (f *fakeVariantRecorder) UpsertVariant(_ context.Context, variant Variant) error {
	f.rows[variantKey(variant.PostID, variant.Name)] = variant
	return nil
}

// testPicture renders a deterministic gradient picture for encoding tests.
func testPicture(width, height int) image.Image {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return picture
}

func encodePicture(t *testing.T, format string, picture image.Image) []byte {
	t.Helper()
	var encoded bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&encoded, picture)
	case "jpeg":
		err = jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 90})
	case "gif":
		err = gif.Encode(&encoded, picture, nil)
	default:
		t.Fatalf("unknown test format %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return encoded.Bytes()
}

func putOriginal(t *testing.T, store *LocalStore, key, format string, width, height int) []byte {
	t.Helper()
	data := encodePicture(t, format, testPicture(width, height))
	if _, err := store.Put(context.Background(), key, "image/"+format, bytes.NewReader(data)); err != nil {
		t.Fatalf("Put original: %v", err)
	}
	return data
}

func testProcessor(store *LocalStore, recorder *fakeVariantRecorder) *DerivativeProcessor {
	return &DerivativeProcessor{Originals: store, Derivatives: store, Variants: recorder}
}

func handleDerivative(t *testing.T, processor *DerivativeProcessor, postID, originalKey, variant string) error {
	t.Helper()
	request, err := EnqueueDerivative(postID, originalKey, variant)
	if err != nil {
		t.Fatalf("EnqueueDerivative: %v", err)
	}
	return processor.Handle(context.Background(), jobs.Job{ID: "1", Kind: request.Kind, Payload: request.Payload})
}

func TestEnqueueDerivativeKeys(t *testing.T) {
	request, err := EnqueueDerivative("7", "uploads/abc.png", "thumb-320")
	if err != nil {
		t.Fatalf("EnqueueDerivative: %v", err)
	}
	if request.Kind != KindDerivative {
		t.Fatalf("kind=%q want %q", request.Kind, KindDerivative)
	}
	if request.IdempotencyKey != "derivative:7:thumb-320" {
		t.Fatalf("idempotency key=%q", request.IdempotencyKey)
	}
	if _, err := EnqueueDerivative("0", "uploads/abc.png", ""); err == nil {
		t.Fatal("expected bad post id to fail")
	}
	if _, err := EnqueueDerivative("7", "", ""); err == nil {
		t.Fatal("expected empty key to fail")
	}
	if _, err := EnqueueDerivative("7", "uploads/abc.png", "thumb-999"); err == nil {
		t.Fatal("expected unknown variant to fail")
	}
}

func TestDerivativeCreatesBoundedThumbnail(t *testing.T) {
	root := t.TempDir()
	store := NewLocalStore(root)
	recorder := newFakeVariantRecorder()
	originalKey := "uploads/photo.png"
	original := putOriginal(t, store, originalKey, "png", 640, 480)

	if err := handleDerivative(t, testProcessor(store, recorder), "7", originalKey, "thumb-320"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	variant, ok := recorder.rows[variantKey(7, "thumb-320")]
	if !ok {
		t.Fatal("variant metadata was not recorded")
	}
	if variant.ObjectKey != "derivatives/thumb-320/photo.jpg" {
		t.Fatalf("object key=%q", variant.ObjectKey)
	}
	if variant.MediaType != "image/jpeg" || variant.Status != "ready" {
		t.Fatalf("unexpected variant: %#v", variant)
	}
	if variant.Width != 320 || variant.Height != 240 {
		t.Fatalf("dimensions=%dx%d want 320x240", variant.Width, variant.Height)
	}
	if !strings.HasPrefix(variant.URL, "/media/derivatives/thumb-320/photo.jpg") {
		t.Fatalf("url=%q", variant.URL)
	}
	thumbnail, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(variant.ObjectKey)))
	if err != nil {
		t.Fatalf("thumbnail missing on disk: %v", err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(thumbnail))
	if err != nil || format != "jpeg" {
		t.Fatalf("thumbnail not JPEG: %v %q", err, format)
	}
	if config.Width != 320 || config.Height != 240 {
		t.Fatalf("thumbnail pixels=%dx%d", config.Width, config.Height)
	}
	if stored, err := os.ReadFile(filepath.Join(root, "uploads", "photo.png")); err != nil || !bytes.Equal(stored, original) {
		t.Fatal("original was mutated by derivative processing")
	}
}

func TestDerivativeIsSafeToRetry(t *testing.T) {
	root := t.TempDir()
	store := NewLocalStore(root)
	recorder := newFakeVariantRecorder()
	originalKey := "uploads/retry.png"
	original := putOriginal(t, store, originalKey, "png", 400, 400)
	processor := testProcessor(store, recorder)

	if err := handleDerivative(t, processor, "9", originalKey, "thumb-320"); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	first := recorder.rows[variantKey(9, "thumb-320")]
	before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(first.ObjectKey)))
	if err != nil {
		t.Fatalf("thumbnail missing: %v", err)
	}
	if err := handleDerivative(t, processor, "9", originalKey, "thumb-320"); err != nil {
		t.Fatalf("retry Handle: %v", err)
	}
	second := recorder.rows[variantKey(9, "thumb-320")]
	if second.ObjectKey != first.ObjectKey || second.Width != first.Width || second.Size != first.Size {
		t.Fatalf("retry changed variant: %#v vs %#v", first, second)
	}
	after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(second.ObjectKey)))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retry overwrote thumbnail bytes")
	}
	if stored, err := os.ReadFile(filepath.Join(root, "uploads", "retry.png")); err != nil || !bytes.Equal(stored, original) {
		t.Fatal("original was mutated by retry")
	}
}

func TestDerivativeSupportsRepositoryFormats(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			store := NewLocalStore(t.TempDir())
			recorder := newFakeVariantRecorder()
			originalKey := "uploads/format." + format
			putOriginal(t, store, originalKey, format, 500, 250)
			if err := handleDerivative(t, testProcessor(store, recorder), "3", originalKey, "thumb-160"); err != nil {
				t.Fatalf("Handle %s: %v", format, err)
			}
			variant := recorder.rows[variantKey(3, "thumb-160")]
			if variant.Width != 160 || variant.Height != 80 {
				t.Fatalf("%s dimensions=%dx%d want 160x80", format, variant.Width, variant.Height)
			}
		})
	}
}

func TestDerivativeNeverUpscales(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	recorder := newFakeVariantRecorder()
	originalKey := "uploads/small.png"
	putOriginal(t, store, originalKey, "png", 100, 80)
	if err := handleDerivative(t, testProcessor(store, recorder), "5", originalKey, "thumb-320"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	variant := recorder.rows[variantKey(5, "thumb-320")]
	if variant.Width != 100 || variant.Height != 80 {
		t.Fatalf("dimensions=%dx%d want 100x80", variant.Width, variant.Height)
	}
}

func TestDerivativeReportsFailures(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	recorder := newFakeVariantRecorder()
	processor := testProcessor(store, recorder)

	if err := handleDerivative(t, processor, "7", "uploads/missing.png", "thumb-320"); err == nil {
		t.Fatal("expected missing original to fail")
	}
	if _, err := store.Put(context.Background(), "uploads/corrupt.png", "image/png", strings.NewReader("not an image")); err != nil {
		t.Fatalf("Put corrupt: %v", err)
	}
	if err := handleDerivative(t, processor, "7", "uploads/corrupt.png", "thumb-320"); err == nil {
		t.Fatal("expected corrupt original to fail")
	} else if !strings.Contains(err.Error(), "supported image") {
		t.Fatalf("corrupt error=%v", err)
	}
	if len(recorder.rows) != 0 {
		t.Fatalf("failures must not record variants: %#v", recorder.rows)
	}
	badPayload := jobs.Job{ID: "1", Kind: KindDerivative, Payload: json.RawMessage(`{"post_id":"abc"}`)}
	if err := processor.Handle(context.Background(), badPayload); err == nil {
		t.Fatal("expected bad payload to fail")
	}
	if err := (&DerivativeProcessor{}).Handle(context.Background(), badPayload); err == nil {
		t.Fatal("expected unconfigured processor to fail")
	}
}

func TestLocalStoreOpenRoundTrip(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	if _, err := store.Put(context.Background(), "uploads/read.jpg", "image/jpeg", strings.NewReader("bytes")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	body, err := store.Open(context.Background(), "uploads/read.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := io.ReadAll(body)
	closeErr := body.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || string(data) != "bytes" {
		t.Fatalf("Open bytes=%q err=%v", data, err)
	}
	if _, err := store.Open(context.Background(), "uploads/missing.jpg"); err == nil {
		t.Fatal("expected missing key to fail")
	}
	if _, err := store.Open(context.Background(), "../escape"); err == nil {
		t.Fatal("expected traversal key to fail")
	}
}

func TestS3StoreOpenRoundTrip(t *testing.T) {
	store, _ := newFakeS3Store(t, "kura-media", "", nil)
	if _, err := store.Put(context.Background(), "uploads/read.jpg", "image/jpeg", strings.NewReader("bytes")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	body, err := store.Open(context.Background(), "uploads/read.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := io.ReadAll(body)
	closeErr := body.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || string(data) != "bytes" {
		t.Fatalf("Open bytes=%q err=%v", data, err)
	}
	if _, err := store.Open(context.Background(), "uploads/missing.jpg"); err == nil {
		t.Fatal("expected missing key to fail")
	}
	if _, err := store.Open(context.Background(), "../escape"); err == nil {
		t.Fatal("expected traversal key to fail")
	}
}
