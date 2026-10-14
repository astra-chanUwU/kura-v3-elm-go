package posts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readUploadWebPFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedLeftoverCount(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "uploads"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestStageUploadAcceptsStaticWebP(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		width   int
		height  int
	}{
		{"lossy.webp", 32, 24},
		{"lossless-alpha.webp", 16, 12},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("MEDIA_ROOT", root)
			data := readUploadWebPFixture(t, tc.fixture)
			// Spoofed name/MIME: decoding, not the supplied name, decides.
			staged, err := stageUpload(UploadRequest{File: bytes.NewReader(data), Filename: "photo.jpg"})
			if err != nil {
				t.Fatalf("stageUpload: %v", err)
			}
			defer staged.cleanup()
			if staged.mediaType != "image/webp" || staged.extension != "webp" {
				t.Fatalf("mediaType=%q extension=%q want image/webp/webp", staged.mediaType, staged.extension)
			}
			if staged.width != tc.width || staged.height != tc.height {
				t.Fatalf("dimensions=%dx%d want %dx%d", staged.width, staged.height, tc.width, tc.height)
			}
			stored, err := os.ReadFile(staged.tempPath)
			if err != nil || !bytes.Equal(stored, data) {
				t.Fatal("staged bytes differ from original WebP")
			}
		})
	}
}

func TestStageUploadWebPSpoofedExtensionAcceptedAsWebP(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	data := readUploadWebPFixture(t, "lossy.webp")
	staged, err := stageUpload(UploadRequest{File: bytes.NewReader(data), Filename: "image.png"})
	if err != nil {
		t.Fatal(err)
	}
	defer staged.cleanup()
	if staged.mediaType != "image/webp" || staged.extension != "webp" {
		t.Fatalf("spoofed png name recorded %q/%q", staged.mediaType, staged.extension)
	}
}

func TestStageUploadRejectsWebPBeforePersistence(t *testing.T) {
	lossy := readUploadWebPFixture(t, "lossy.webp")
	impossible := bytes.Clone(lossy)
	binary.LittleEndian.PutUint32(impossible[4:8], 0xfffffff0)
	truncatedFrame := bytes.Clone(lossy[:40])
	binary.LittleEndian.PutUint32(truncatedFrame[4:8], uint32(len(truncatedFrame)-8))
	binary.LittleEndian.PutUint32(truncatedFrame[16:20], uint32(len(truncatedFrame)-20))

	cases := []struct {
		name     string
		data     []byte
		wantAnim bool
	}{
		{"animated", readUploadWebPFixture(t, "animated.webp"), true},
		{"truncated", lossy[:20], false},
		{"impossible RIFF size", impossible, false},
		{"truncated frame", truncatedFrame, false},
		{"not an image", []byte("not an image at all"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("MEDIA_ROOT", root)
			staged, err := stageUpload(UploadRequest{File: bytes.NewReader(tc.data), Filename: "evil.webp"})
			if err == nil {
				staged.cleanup()
				t.Fatal("expected rejection")
			}
			if !errors.Is(err, ErrInvalidUpload) {
				t.Fatalf("err=%v want ErrInvalidUpload", err)
			}
			if tc.wantAnim && !strings.Contains(err.Error(), "animated WebP") {
				t.Fatalf("animated err=%q should name animation", err)
			}
			if staged != nil {
				t.Fatalf("rejected upload returned staged file: %#v", staged)
			}
			if left := stagedLeftoverCount(t, root); left != 0 {
				t.Fatalf("rejected upload left %d staged files", left)
			}
		})
	}
}

func TestExportExtensionWebP(t *testing.T) {
	if got := exportExtension("image/webp"); got != "webp" {
		t.Fatalf("exportExtension(image/webp)=%q want webp", got)
	}
	if got := exportExtension("image/jpeg"); got != "jpg" {
		t.Fatalf("exportExtension(image/jpeg)=%q want jpg", got)
	}
	if got := exportExtension("application/octet-stream"); got != "bin" {
		t.Fatalf("exportExtension(unknown)=%q want bin", got)
	}
}

func TestUploadFormatWebP(t *testing.T) {
	mediaType, extension, ok := uploadFormat("webp")
	if !ok || mediaType != "image/webp" || extension != "webp" {
		t.Fatalf("uploadFormat(webp)=%q %q %v", mediaType, extension, ok)
	}
}
