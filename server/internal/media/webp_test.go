package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWebPFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestValidateStaticWebPLossyAndLosslessAlpha(t *testing.T) {
	lossy := readWebPFixture(t, "lossy.webp")
	config, err := ValidateStaticWebP(lossy)
	if err != nil {
		t.Fatalf("lossy: %v", err)
	}
	if config.Width != 32 || config.Height != 24 {
		t.Fatalf("lossy dimensions=%dx%d want 32x24", config.Width, config.Height)
	}
	decoded, format, err := image.Decode(bytes.NewReader(lossy))
	if err != nil || format != "webp" {
		t.Fatalf("lossy decode format=%q err=%v", format, err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 24 {
		t.Fatalf("lossy decoded bounds=%v", decoded.Bounds())
	}

	lossless := readWebPFixture(t, "lossless-alpha.webp")
	config, err = ValidateStaticWebP(lossless)
	if err != nil {
		t.Fatalf("lossless-alpha: %v", err)
	}
	if config.Width != 16 || config.Height != 12 {
		t.Fatalf("lossless dimensions=%dx%d want 16x12", config.Width, config.Height)
	}
	decoded, format, err = image.Decode(bytes.NewReader(lossless))
	if err != nil || format != "webp" {
		t.Fatalf("lossless decode format=%q err=%v", format, err)
	}
	// The fixture carries real alpha: at least one pixel is not opaque.
	foundAlpha := false
	bounds := decoded.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y && !foundAlpha; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := color.NRGBAModel.Convert(decoded.At(x, y)).RGBA()
			if a != 0xffff {
				foundAlpha = true
				break
			}
		}
	}
	if !foundAlpha {
		t.Fatal("lossless-alpha fixture decoded as fully opaque")
	}
}

func TestValidateStaticWebPRejectsAnimated(t *testing.T) {
	animated := readWebPFixture(t, "animated.webp")
	if _, _, err := image.DecodeConfig(bytes.NewReader(animated)); err != nil {
		t.Fatalf("precondition: DecodeConfig should accept animated VP8X header: %v", err)
	}
	if _, err := ValidateStaticWebP(animated); !errors.Is(err, ErrAnimatedWebP) {
		t.Fatalf("animated err=%v want ErrAnimatedWebP", err)
	}
}

func TestValidateStaticWebPRejectsMalformed(t *testing.T) {
	lossy := readWebPFixture(t, "lossy.webp")

	impossibleRIFF := bytes.Clone(lossy)
	binary.LittleEndian.PutUint32(impossibleRIFF[4:8], 0xfffffff0)

	overrunChunk := bytes.Clone(lossy)
	// VP8 chunk size lives at offset 16; inflate it past EOF.
	binary.LittleEndian.PutUint32(overrunChunk[16:20], 0x0000fff0)

	// Truncated payload with RIFF/chunk sizes rewritten to stay internally
	// consistent, so only a full decode can reject it.
	truncated := bytes.Clone(lossy[:40])
	binary.LittleEndian.PutUint32(truncated[4:8], uint32(len(truncated)-8))
	binary.LittleEndian.PutUint32(truncated[16:20], uint32(len(truncated)-20))

	cases := map[string][]byte{
		"empty":           {},
		"not webp":        []byte("RIFF\x04\x00\x00\x00WAVE"),
		"truncated":       lossy[:20],
		"impossible RIFF": impossibleRIFF,
		"chunk overrun":   overrunChunk,
		"truncated frame": truncated,
	}
	for name, data := range cases {
		if _, err := ValidateStaticWebP(data); err == nil {
			t.Fatalf("%s: expected rejection", name)
		} else if errors.Is(err, ErrAnimatedWebP) {
			t.Fatalf("%s: err=%v should not be animated", name, err)
		}
	}
}

func TestDerivativeWebPThumbnails(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		key     string
		width   int
		height  int
	}{
		{"lossy.webp", "uploads/photo.webp", 32, 24},
		{"lossless-alpha.webp", "uploads/alpha.webp", 16, 12},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			root := t.TempDir()
			store := NewLocalStore(root)
			recorder := newFakeVariantRecorder()
			data := readWebPFixture(t, tc.fixture)
			if _, err := store.Put(context.Background(), tc.key, "image/webp", bytes.NewReader(data)); err != nil {
				t.Fatalf("Put: %v", err)
			}
			if err := handleDerivative(t, testProcessor(store, recorder), "11", tc.key, "thumb-320"); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			variant := recorder.rows[variantKey(11, "thumb-320")]
			if variant.MediaType != "image/jpeg" || variant.Status != "ready" {
				t.Fatalf("variant=%#v", variant)
			}
			if variant.Width != tc.width || variant.Height != tc.height {
				t.Fatalf("thumb dimensions=%dx%d want %dx%d", variant.Width, variant.Height, tc.width, tc.height)
			}
			thumb, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(variant.ObjectKey)))
			if err != nil {
				t.Fatal(err)
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(thumb))
			if err != nil || format != "jpeg" || config.Width != tc.width || config.Height != tc.height {
				t.Fatalf("thumb format=%q config=%v err=%v", format, config, err)
			}
			stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tc.key)))
			if err != nil || !bytes.Equal(stored, data) {
				t.Fatal("original WebP was mutated")
			}
		})
	}
}

func TestDerivativeRejectsAnimatedWebP(t *testing.T) {
	// Animated WebP is no longer rejected outright: with ffmpeg tools it
	// yields a first-frame poster; without them it fails clearly and
	// records nothing. The static validator still rejects it (above).
	if extendedToolPath("KURA_FFPROBE_PATH", "ffprobe") != "" && extendedToolPath("KURA_FFMPEG_PATH", "ffmpeg") != "" {
		t.Skip("animated WebP derivative succeeds when ffmpeg is installed; covered by extended pipeline tests")
	}
	store := NewLocalStore(t.TempDir())
	recorder := newFakeVariantRecorder()
	data := readWebPFixture(t, "animated.webp")
	if _, err := store.Put(context.Background(), "uploads/anim.webp", "image/webp", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	err := handleDerivative(t, testProcessor(store, recorder), "12", "uploads/anim.webp", "thumb-320")
	if err == nil || !strings.Contains(err.Error(), "supported image") {
		t.Fatalf("animated derivative err=%v", err)
	}
	if len(recorder.rows) != 0 {
		t.Fatalf("animated derivative recorded variants: %#v", recorder.rows)
	}
}
