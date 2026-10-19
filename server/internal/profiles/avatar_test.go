package profiles

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

func TestNormalizeAvatarCenterCropsPNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for x := 0; x < 8; x++ {
		src.Set(x, 0, color.RGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	out, err := NormalizeAvatar(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(out))
	if err != nil || format != "jpeg" {
		t.Fatalf("format=%s err=%v", format, err)
	}
	if decoded.Bounds().Dx() != decoded.Bounds().Dy() || decoded.Bounds().Dx() != 4 {
		t.Fatalf("bounds=%v", decoded.Bounds())
	}
}

func TestNormalizeAvatarRejectsUnsafeFiles(t *testing.T) {
	animated, err := os.ReadFile(filepath.Join("..", "media", "testdata", "animated.webp"))
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]byte{
		[]byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"),
		[]byte("GIF89a"),
		[]byte("MZ-executable"),
		[]byte{0x7f, 'E', 'L', 'F', 0, 0},
		animated,
		bytes.Repeat([]byte{0xff, 0xd8, 0xff}, 800000),
	}
	for _, data := range cases {
		if _, err := NormalizeAvatar(data); !errors.Is(err, ErrAvatar) {
			t.Fatalf("data %q err=%v", data[:min(8, len(data))], err)
		}
	}
	if _, err := NormalizeAvatar(animated); !errors.Is(err, media.ErrAnimatedWebP) && !errors.Is(err, ErrAvatar) {
		t.Fatal(err)
	}
	static, err := os.ReadFile(filepath.Join("..", "media", "testdata", "lossy.webp"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := NormalizeAvatar(static)
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || format != "jpeg" {
		t.Fatalf("webp normalized format=%s err=%v", format, err)
	}
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeAvatar(jpegBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
}
