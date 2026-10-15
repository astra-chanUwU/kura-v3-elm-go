package posts

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func extendedPostsFixtureBasicJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func extendedPostsFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "media", "testdata", "extended", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requireFFmpegForUpload(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("extended upload requires ffprobe")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("extended upload requires ffmpeg")
	}
}

func TestStageUploadAcceptsExtendedFormats(t *testing.T) {
	requireFFmpegForUpload(t)
	for _, tc := range []struct {
		fixture, mediaType, extension string
		width, height                 int
	}{
		{"static.avif", "image/avif", "avif", 32, 24},
		{"animated.webp", "image/webp", "webp", 32, 24},
		{"h264-aac.mp4", "video/mp4", "mp4", 32, 24},
		{"vp9-opus.webm", "video/webm", "webm", 32, 24},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Setenv("MEDIA_ROOT", t.TempDir())
			data := extendedPostsFixture(t, tc.fixture)
			// Spoofed name: content inspection decides type/dimensions.
			staged, err := stageUpload(UploadRequest{File: bytes.NewReader(data), Filename: "photo.jpg"})
			if err != nil {
				t.Fatalf("stageUpload: %v", err)
			}
			defer staged.cleanup()
			if staged.mediaType != tc.mediaType || staged.extension != tc.extension || staged.width != tc.width || staged.height != tc.height {
				t.Fatalf("staged=%#v", staged)
			}
			if stored, err := os.ReadFile(staged.tempPath); err != nil || !bytes.Equal(stored, data) {
				t.Fatal("staged bytes differ from immutable original")
			}
		})
	}
}

func TestStageUploadExtendedWithoutFFmpegFailsClearly(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("KURA_FFPROBE_PATH", "/nonexistent/ffprobe")
	data := extendedPostsFixture(t, "h264-aac.mp4")
	staged, err := stageUpload(UploadRequest{File: bytes.NewReader(data), Filename: "clip.mp4"})
	if err == nil {
		staged.cleanup()
		t.Fatal("expected rejection without ffprobe")
	}
	if !errors.Is(err, ErrInvalidUpload) || !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("err=%v", err)
	}
	// JPEG still stages without any ffmpeg tool.
	jpeg := extendedPostsFixtureBasicJPEG(t)
	staged, err = stageUpload(UploadRequest{File: bytes.NewReader(jpeg), Filename: "a.jpg"})
	if err != nil {
		t.Fatalf("basic JPEG without ffmpeg: %v", err)
	}
	staged.cleanup()
}

func TestExportExtensionExtendedFormats(t *testing.T) {
	for mediaType, want := range map[string]string{"image/avif": "avif", "video/mp4": "mp4", "video/webm": "webm", "image/webp": "webp"} {
		if got := exportExtension(mediaType); got != want {
			t.Fatalf("exportExtension(%q)=%q want %q", mediaType, got, want)
		}
	}
}
