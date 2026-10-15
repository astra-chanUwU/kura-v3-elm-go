package media

import (
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireFFmpegTools(t *testing.T) {
	t.Helper()
	if extendedToolPath("KURA_FFPROBE_PATH", "ffprobe") == "" || extendedToolPath("KURA_FFMPEG_PATH", "ffmpeg") == "" {
		t.Skip("extended media pipeline requires ffprobe and ffmpeg")
	}
}

func putExtendedOriginal(t *testing.T, store *LocalStore, key, mediaType, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "extended", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), key, mediaType, bytes.NewReader(data)); err != nil {
		t.Fatalf("Put original: %v", err)
	}
	return data
}

func TestDerivativeExtendedPosters(t *testing.T) {
	requireFFmpegTools(t)
	for _, tc := range []struct{ name, key, mediaType string }{
		{"static.avif", "uploads/pic.avif", "image/avif"},
		{"animated.webp", "uploads/anim.webp", "image/webp"},
		{"h264-aac.mp4", "uploads/clip.mp4", "video/mp4"},
		{"vp9-opus.webm", "uploads/clip.webm", "video/webm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := NewLocalStore(root)
			recorder := newFakeVariantRecorder()
			original := putExtendedOriginal(t, store, tc.key, tc.mediaType, tc.name)
			if err := handleDerivative(t, testProcessor(store, recorder), "7", tc.key, "thumb-320"); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			variant, ok := recorder.rows[variantKey(7, "thumb-320")]
			if !ok || variant.MediaType != "image/jpeg" || variant.Status != "ready" || variant.Width != 32 || variant.Height != 24 {
				t.Fatalf("variant=%#v ok=%v", variant, ok)
			}
			thumb, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(variant.ObjectKey)))
			if err != nil {
				t.Fatal(err)
			}
			if _, format, err := image.DecodeConfig(bytes.NewReader(thumb)); err != nil || format != "jpeg" {
				t.Fatalf("poster format=%q err=%v", format, err)
			}
			if stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tc.key))); err != nil || !bytes.Equal(stored, original) {
				t.Fatal("original was mutated")
			}
		})
	}
}

func TestDerivativeExtendedWithoutFFmpegFailsClearlyButBasicWorks(t *testing.T) {
	t.Setenv("KURA_FFPROBE_PATH", "/nonexistent/ffprobe")
	t.Setenv("KURA_FFMPEG_PATH", "/nonexistent/ffmpeg")
	store := NewLocalStore(t.TempDir())
	recorder := newFakeVariantRecorder()
	putExtendedOriginal(t, store, "uploads/clip.mp4", "video/mp4", "h264-aac.mp4")
	err := handleDerivative(t, testProcessor(store, recorder), "7", "uploads/clip.mp4", "thumb-320")
	if err == nil || !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("missing-tool error=%v", err)
	}
	if len(recorder.rows) != 0 {
		t.Fatalf("rows=%#v", recorder.rows)
	}
	// Basic formats never need ffmpeg.
	putOriginal(t, store, "uploads/plain.png", "png", 64, 48)
	if err := handleDerivative(t, testProcessor(store, recorder), "8", "uploads/plain.png", "thumb-320"); err != nil {
		t.Fatalf("basic PNG without ffmpeg: %v", err)
	}
}

func TestFingerprintExtendedImagesAndVideoExclusion(t *testing.T) {
	requireFFmpegTools(t)
	hash := "sha256:" + strings.Repeat("e", 64)
	for _, tc := range []struct{ name, key, mediaType string }{
		{"static.avif", "uploads/pic.avif", "image/avif"},
		{"animated.webp", "uploads/anim.webp", "image/webp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewLocalStore(t.TempDir())
			recorder := newFakeFingerprintRecorder()
			putExtendedOriginal(t, store, tc.key, tc.mediaType, tc.name)
			processor := &FingerprintProcessor{Originals: store, Fingerprints: recorder}
			if err := handleFingerprint(t, processor, "7", tc.key, hash); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if _, ok := recorder.rows[7]; !ok {
				t.Fatal("fingerprint not recorded")
			}
		})
	}
	for _, tc := range []struct{ name, key, mediaType string }{
		{"h264-aac.mp4", "uploads/clip.mp4", "video/mp4"},
		{"vp9-opus.webm", "uploads/clip.webm", "video/webm"},
	} {
		t.Run("exclude "+tc.name, func(t *testing.T) {
			store := NewLocalStore(t.TempDir())
			recorder := newFakeFingerprintRecorder()
			putExtendedOriginal(t, store, tc.key, tc.mediaType, tc.name)
			processor := &FingerprintProcessor{Originals: store, Fingerprints: recorder}
			err := handleFingerprint(t, processor, "7", tc.key, hash)
			if err == nil || !strings.Contains(err.Error(), "fingerprint unsupported") || !strings.Contains(err.Error(), "video is excluded") {
				t.Fatalf("video exclusion error=%v", err)
			}
			if len(recorder.rows) != 0 {
				t.Fatalf("video recorded fingerprint: %#v", recorder.rows)
			}
		})
	}
}
