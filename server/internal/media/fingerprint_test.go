package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
)

// similarityScene renders a deterministic low-frequency picture: a smooth
// gradient with a dark disc and a bright bar. Variant 1 mirrors the
// layout, so the two scenes are genuinely different pictures while
// resizes and recompressions of one variant stay near copies.
func similarityScene(variant, width, height int) image.Image {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var value int
			if variant == 0 {
				value = x*200/width + y*40/height
			} else {
				value = (width-x)*200/width + (height-y)*40/height
			}
			picture.Set(x, y, color.RGBA{R: uint8(value % 256), G: uint8((value * 3 / 4) % 256), B: uint8((100 + value/3) % 256), A: 255})
		}
	}
	disc := func(cx, cy, radius int, c color.RGBA) {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy < radius*radius {
					picture.Set(x, y, c)
				}
			}
		}
	}
	bar := func(top bool, c color.RGBA) {
		for x := 0; x < width; x++ {
			if top {
				for y := 0; y < height/5; y++ {
					picture.Set(x, y, c)
				}
			} else {
				for y := height * 4 / 5; y < height; y++ {
					picture.Set(x, y, c)
				}
			}
		}
	}
	if variant == 0 {
		disc(width/4, height/2, height/5, color.RGBA{15, 15, 15, 255})
		bar(false, color.RGBA{235, 235, 235, 255})
	} else {
		disc(3*width/4, height/2, height/5, color.RGBA{235, 235, 235, 255})
		bar(true, color.RGBA{15, 15, 15, 255})
	}
	return picture
}

func encodeScene(t *testing.T, variant, width, height int, format string, quality int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&encoded, similarityScene(variant, width, height))
	case "jpeg":
		err = jpeg.Encode(&encoded, similarityScene(variant, width, height), &jpeg.Options{Quality: quality})
	case "gif":
		err = gif.Encode(&encoded, similarityScene(variant, width, height), nil)
	default:
		t.Fatalf("unknown test format %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return encoded.Bytes()
}

func decodeScene(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return img
}

func hashScene(t *testing.T, data []byte) uint64 {
	t.Helper()
	return ComputeDHash(decodeScene(t, data))
}

func TestComputeDHashIsDeterministic(t *testing.T) {
	data := encodeScene(t, 0, 96, 72, "png", 0)
	if first, second := hashScene(t, data), hashScene(t, data); first != second {
		t.Fatalf("same bytes hashed to %016x then %016x", first, second)
	}
}

func TestDHashMatchesResizedAndRecompressedCopies(t *testing.T) {
	base := hashScene(t, encodeScene(t, 0, 96, 72, "png", 0))
	recompressed := hashScene(t, encodeScene(t, 0, 96, 72, "jpeg", 70))
	halfSize := hashScene(t, encodeScene(t, 0, 48, 36, "png", 0))
	quarterSize := hashScene(t, encodeScene(t, 0, 24, 18, "jpeg", 50))
	if distance := DHashDistance(base, recompressed); distance > 6 {
		t.Fatalf("recompressed copy distance=%d want <= 6", distance)
	}
	if distance := DHashDistance(base, halfSize); distance > 2 {
		t.Fatalf("half-size copy distance=%d want <= 2", distance)
	}
	if distance := DHashDistance(base, quarterSize); distance > 10 {
		t.Fatalf("quarter-size recompressed copy distance=%d want <= 10", distance)
	}
}

func TestDHashKeepsUnrelatedImagesFar(t *testing.T) {
	base := hashScene(t, encodeScene(t, 0, 96, 72, "png", 0))
	other := hashScene(t, encodeScene(t, 1, 96, 72, "png", 0))
	if distance := DHashDistance(base, other); distance < 24 {
		t.Fatalf("unrelated scene distance=%d want >= 24", distance)
	}
	// A flat field hashes near zero, exactly like smooth gradients do, so
	// "unrelated" here means structured differently: use a checkerboard.
	checker := image.NewRGBA(image.Rect(0, 0, 96, 72))
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			if (x/12+y/12)%2 == 0 {
				checker.Set(x, y, color.RGBA{255, 255, 255, 255})
			} else {
				checker.Set(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	if distance := DHashDistance(base, ComputeDHash(checker)); distance < 24 {
		t.Fatalf("checkerboard distance=%d want >= 24", distance)
	}
}

func TestDHashDocumentsWhiteAlphaBackground(t *testing.T) {
	transparent := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	white := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			white.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	if ComputeDHash(transparent) != ComputeDHash(white) {
		t.Fatal("fully transparent pixels must read as white (documented alpha background)")
	}
	opaque := image.NewRGBA(image.Rect(0, 0, 64, 48))
	withAlpha := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			opaque.Set(x, y, color.RGBA{R: 40, G: 120, B: 200, A: 255})
			withAlpha.Set(x, y, color.NRGBA{R: 40, G: 120, B: 200, A: 255})
		}
	}
	if ComputeDHash(opaque) != ComputeDHash(withAlpha) {
		t.Fatal("fully opaque pixels must hash identically with or without alpha")
	}
}

func TestHashBitsRoundTrip(t *testing.T) {
	for _, value := range []uint64{0, 1, 0xad564b8483cd0000, 0xffffffffffffffff} {
		parsed, err := ParseHashBits(FormatHashBits(value))
		if err != nil || parsed != value {
			t.Fatalf("bits round trip of %016x gave %016x, %v", value, parsed, err)
		}
	}
	if formatted := FormatHashBits(0); len(formatted) != 64 || strings.Trim(formatted, "0") != "" {
		t.Fatalf("zero hash bits=%q", formatted)
	}
	for _, bad := range []string{"", "0101", strings.Repeat("0", 63) + "2", strings.Repeat("0", 65)} {
		if _, err := ParseHashBits(bad); err == nil {
			t.Fatalf("expected bits %q to fail", bad)
		}
	}
}

func TestEnqueueFingerprintKeys(t *testing.T) {
	hash := "sha256:" + strings.Repeat("a", 64)
	request, err := EnqueueFingerprint("7", "uploads/abc.png", hash)
	if err != nil {
		t.Fatalf("EnqueueFingerprint: %v", err)
	}
	if request.Kind != KindFingerprint {
		t.Fatalf("kind=%q want %q", request.Kind, KindFingerprint)
	}
	if want := "fingerprint:7:dhash-v1:" + hash; request.IdempotencyKey != want {
		t.Fatalf("idempotency key=%q want %q", request.IdempotencyKey, want)
	}
	if FingerprintIdempotencyKey(7, hash) != request.IdempotencyKey {
		t.Fatal("public idempotency constructor must match the enqueue key")
	}
	for _, input := range [][3]string{
		{"0", "uploads/abc.png", hash},
		{"abc", "uploads/abc.png", hash},
		{"7", "", hash},
		{"7", "uploads/abc.png", "not-a-hash"},
		{"7", "uploads/abc.png", "sha256:" + strings.Repeat("A", 64)},
	} {
		if _, err := EnqueueFingerprint(input[0], input[1], input[2]); err == nil {
			t.Fatalf("expected EnqueueFingerprint(%v) to fail", input)
		}
	}
}

type fakeFingerprintRecorder struct {
	rows map[int64]Fingerprint
}

func newFakeFingerprintRecorder() *fakeFingerprintRecorder {
	return &fakeFingerprintRecorder{rows: make(map[int64]Fingerprint)}
}

func (f *fakeFingerprintRecorder) UpsertFingerprint(_ context.Context, fingerprint Fingerprint) error {
	f.rows[fingerprint.PostID] = fingerprint
	return nil
}

func putFingerprintOriginal(t *testing.T, store *LocalStore, key, format string, data []byte) {
	t.Helper()
	if _, err := store.Put(context.Background(), key, "image/"+format, bytes.NewReader(data)); err != nil {
		t.Fatalf("Put original: %v", err)
	}
}

func handleFingerprint(t *testing.T, processor *FingerprintProcessor, postID, originalKey, originalHash string) error {
	t.Helper()
	request, err := EnqueueFingerprint(postID, originalKey, originalHash)
	if err != nil {
		t.Fatalf("EnqueueFingerprint: %v", err)
	}
	return processor.Handle(context.Background(), jobs.Job{ID: "1", Kind: request.Kind, Payload: request.Payload})
}

func TestFingerprintProcessorRecordsBoundHash(t *testing.T) {
	root := t.TempDir()
	store := NewLocalStore(root)
	recorder := newFakeFingerprintRecorder()
	originalKey := "uploads/sim.png"
	original := encodeScene(t, 0, 96, 72, "png", 0)
	putFingerprintOriginal(t, store, originalKey, "png", original)
	originalHash := "sha256:" + strings.Repeat("b", 64)

	processor := &FingerprintProcessor{Originals: store, Fingerprints: recorder}
	if err := handleFingerprint(t, processor, "7", originalKey, originalHash); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	row, ok := recorder.rows[7]
	if !ok {
		t.Fatal("fingerprint was not recorded")
	}
	if row.Algorithm != FingerprintAlgorithm || row.OriginalHash != originalHash {
		t.Fatalf("unexpected binding: %#v", row)
	}
	if want := hashScene(t, original); row.Hash != want {
		t.Fatalf("hash=%016x want %016x", row.Hash, want)
	}
	if stored, err := os.ReadFile(filepath.Join(root, "uploads", "sim.png")); err != nil || !bytes.Equal(stored, original) {
		t.Fatal("original was mutated by fingerprint processing")
	}
	// Retry-safe: a second run upserts the same single row.
	if err := handleFingerprint(t, processor, "7", originalKey, originalHash); err != nil {
		t.Fatalf("retry Handle: %v", err)
	}
	if len(recorder.rows) != 1 || recorder.rows[7].Hash != row.Hash {
		t.Fatalf("retry changed rows: %#v", recorder.rows)
	}
}

func TestFingerprintProcessorAcceptsRepositoryFormats(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			store := NewLocalStore(t.TempDir())
			recorder := newFakeFingerprintRecorder()
			originalKey := "uploads/format." + format
			data := encodeScene(t, 0, 64, 48, format, 85)
			putFingerprintOriginal(t, store, originalKey, format, data)
			processor := &FingerprintProcessor{Originals: store, Fingerprints: recorder}
			if err := handleFingerprint(t, processor, "3", originalKey, "sha256:"+strings.Repeat("c", 64)); err != nil {
				t.Fatalf("Handle %s: %v", format, err)
			}
			if recorder.rows[3].Hash != hashScene(t, data) {
				t.Fatalf("%s hash mismatch", format)
			}
		})
	}
}

func TestFingerprintProcessorReportsFailures(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	recorder := newFakeFingerprintRecorder()
	processor := &FingerprintProcessor{Originals: store, Fingerprints: recorder}
	hash := "sha256:" + strings.Repeat("d", 64)

	if err := handleFingerprint(t, processor, "7", "uploads/missing.png", hash); err == nil {
		t.Fatal("expected missing original to fail")
	} else if !strings.Contains(err.Error(), "open original") {
		t.Fatalf("missing original error=%v", err)
	}
	putFingerprintOriginal(t, store, "uploads/corrupt.png", "png", []byte("not an image"))
	if err := handleFingerprint(t, processor, "7", "uploads/corrupt.png", hash); err == nil {
		t.Fatal("expected corrupt original to fail")
	} else if !strings.Contains(err.Error(), "supported image") {
		t.Fatalf("corrupt error=%v", err)
	}
	// Animated WebP now fingerprints its first frame when ffmpeg tools
	// are installed, and fails with a clear tool-named error without
	// them; either way it must not be misread as a static WebP.
	animated, err := os.ReadFile(filepath.Join("testdata", "animated.webp"))
	if err != nil {
		t.Fatalf("read animated fixture: %v", err)
	}
	putFingerprintOriginal(t, store, "uploads/animated.webp", "webp", animated)
	animErr := handleFingerprint(t, processor, "7", "uploads/animated.webp", hash)
	if extendedToolPath("KURA_FFPROBE_PATH", "ffprobe") != "" && extendedToolPath("KURA_FFMPEG_PATH", "ffmpeg") != "" {
		if animErr != nil {
			t.Fatalf("animated WebP with ffmpeg should fingerprint first frame: %v", animErr)
		}
		delete(recorder.rows, 7)
	} else if animErr == nil || !strings.Contains(animErr.Error(), "supported image") {
		t.Fatalf("animated without ffmpeg error=%v", animErr)
	}
	badPayload := jobs.Job{ID: "1", Kind: KindFingerprint, Payload: []byte(`{"post_id":"abc"}`)}
	if err := processor.Handle(context.Background(), badPayload); err == nil {
		t.Fatal("expected bad payload to fail")
	}
	if err := (&FingerprintProcessor{}).Handle(context.Background(), badPayload); err == nil {
		t.Fatal("expected unconfigured processor to fail")
	}
	if len(recorder.rows) != 0 {
		t.Fatalf("failures must not record fingerprints: %#v", recorder.rows)
	}
}

func TestValidateFingerprintDimensions(t *testing.T) {
	if err := validateFingerprintDimensions(96, 72); err != nil {
		t.Fatalf("small image rejected: %v", err)
	}
	if err := validateFingerprintDimensions(0, 72); err == nil {
		t.Fatal("expected empty dimensions to fail")
	}
	// 64MP is 67108864 pixels: 8192x8192 sits exactly at the cap and
	// passes, while 8193x8192 exceeds it.
	if err := validateFingerprintDimensions(8192, 8192); err != nil {
		t.Fatalf("at-cap image rejected: %v", err)
	}
	if err := validateFingerprintDimensions(8193, 8192); err == nil {
		t.Fatal("expected over-cap dimensions to fail")
	} else if !strings.Contains(err.Error(), "64 megapixel") {
		t.Fatalf("cap error=%v", err)
	}
}
