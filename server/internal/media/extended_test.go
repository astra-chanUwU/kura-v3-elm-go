package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeExtendedRunner struct {
	stdout []byte
	stderr []byte
	err    error
	calls  [][]string
	poster []byte
}

func (f *fakeExtendedRunner) Run(ctx context.Context, executable string, args []string, stdoutLimit, stderrLimit int) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	f.calls = append(f.calls, append([]string{executable}, args...))
	if strings.Contains(executable, "ffmpeg") {
		return bytes.Clone(f.poster), nil, f.err
	}
	return bytes.Clone(f.stdout), bytes.Clone(f.stderr), f.err
}

func TestInspectExtendedBasicImagesDoNotNeedFFprobe(t *testing.T) {
	for _, tc := range []struct {
		name, mediaType, extension string
		encode                     func(*testing.T) []byte
	}{
		{"png", "image/png", ".png", pngFixture},
		{"jpeg", "image/jpeg", ".jpg", jpegFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desc, err := inspectExtended(context.Background(), tc.encode(t), &fakeExtendedRunner{}, "")
			if err != nil {
				t.Fatal(err)
			}
			if desc.MediaType != tc.mediaType || desc.Extension != tc.extension || desc.Width != 8 || desc.Height != 6 {
				t.Fatalf("descriptor=%#v", desc)
			}
		})
	}
	webp, err := os.ReadFile(filepath.Join("testdata", "lossy.webp"))
	if err != nil {
		t.Fatal(err)
	}
	desc, err := inspectExtended(context.Background(), webp, &fakeExtendedRunner{}, "")
	if err != nil || desc.MediaType != "image/webp" || desc.Width != 32 || desc.Height != 24 {
		t.Fatalf("descriptor=%#v err=%v", desc, err)
	}
	animatedGIF := animatedGIFFixture(t)
	desc, err = inspectExtended(context.Background(), animatedGIF, &fakeExtendedRunner{}, "")
	if err != nil || desc.MediaType != "image/gif" || !desc.Animated || desc.Duration != 350*time.Millisecond {
		t.Fatalf("GIF descriptor=%#v err=%v", desc, err)
	}
}

func TestInspectExtendedAnimatedWebPUsesProbe(t *testing.T) {
	data := extendedFixture(t, "animated.webp")
	runner := &fakeExtendedRunner{stdout: []byte(`{"format":{"format_name":"webp_anim"},"streams":[{"codec_type":"video","codec_name":"webp_anim","width":32,"height":24,"nb_read_frames":"2"}]}`)}
	desc, err := inspectExtended(context.Background(), data, runner, "ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	if desc.MediaType != "image/webp" || !desc.Animated || desc.Duration != 300*time.Millisecond || desc.Width != 32 || desc.Height != 24 {
		t.Fatalf("descriptor=%#v", desc)
	}
	if len(runner.calls) != 1 || !containsArg(runner.calls[0], "-f") || !containsArg(runner.calls[0], "webp_anim") {
		t.Fatalf("ffprobe arguments=%v", runner.calls)
	}
}

func TestInspectExtendedVideoAndDecodePoster(t *testing.T) {
	data := extendedFixture(t, "h264-aac.mp4")
	probe, _ := json.Marshal(map[string]any{
		"format": map[string]string{"format_name": "mov,mp4,m4a,3gp,3g2,mj2", "duration": "1.000000"},
		"streams": []map[string]any{
			{"codec_type": "video", "codec_name": "h264", "width": 32, "height": 24, "duration": "1.000000", "nb_read_frames": "1"},
			{"codec_type": "audio", "codec_name": "aac", "duration": "1.000000"},
		},
	})
	runner := &fakeExtendedRunner{stdout: probe, poster: jpegFixtureSize(t, 32, 24)}
	desc, err := inspectExtended(context.Background(), data, runner, "ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	if desc.MediaType != "video/mp4" || desc.VideoCodec != "h264" || desc.AudioCodec != "aac" || desc.Duration != time.Second {
		t.Fatalf("descriptor=%#v", desc)
	}
	poster, err := decodeExtendedPoster(context.Background(), data, desc, runner, "ffprobe", "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if poster.Bounds().Dx() != 32 || poster.Bounds().Dy() != 24 {
		t.Fatalf("poster bounds=%v", poster.Bounds())
	}
	last := runner.calls[len(runner.calls)-1]
	for _, forbidden := range []string{"-filter_complex", "-vf", "-safe", "-protocol_whitelist"} {
		if forbidden == "-protocol_whitelist" && containsArg(last, forbidden) {
			continue
		}
		if containsArg(last, forbidden) {
			t.Fatalf("unexpected ffmpeg argument %q in %v", forbidden, last)
		}
	}
	if !containsArg(last, "-frames:v") || !containsArg(last, "1") {
		t.Fatalf("poster command is not single-frame: %v", last)
	}
}

func TestInspectExtendedRejectsUnsupportedStreamsAndAnimatedAVIF(t *testing.T) {
	data := extendedFixture(t, "h264-aac.mp4")
	runner := &fakeExtendedRunner{stdout: []byte(`{"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"1"},"streams":[{"codec_type":"video","codec_name":"hevc","width":32,"height":24,"duration":"1"}]}`)}
	if _, err := inspectExtended(context.Background(), data, runner, "ffprobe"); err == nil || !strings.Contains(err.Error(), "codec") {
		t.Fatalf("unsupported codec error=%v", err)
	}
	avif := extendedFixture(t, "static.avif")
	if _, err := detectExtendedFormat(avif); err != nil {
		t.Fatalf("static AVIF detection: %v", err)
	}
	animated := bytes.Clone(avif)
	copy(animated[8:12], []byte("ftyp"))
	copy(animated[8:12], []byte("ftyp"))
	// Add the AVIF animation brand to the compatible-brand slot without
	// changing the actual checked-in static fixture.
	if len(animated) >= 20 {
		copy(animated[16:20], []byte("avis"))
	}
	if _, err := detectExtendedFormat(animated); !errors.Is(err, ErrExtendedAnimatedAVIF) {
		t.Fatalf("animated AVIF error=%v", err)
	}
}

func TestExtendedMediaLimitsAndToolFailures(t *testing.T) {
	if _, err := inspectExtended(context.Background(), make([]byte, extendedMaxBytes+1), &fakeExtendedRunner{}, ""); err == nil {
		t.Fatal("oversized input accepted")
	}
	webm := extendedFixture(t, "vp9-opus.webm")
	if _, err := inspectExtended(context.Background(), webm, &fakeExtendedRunner{}, ""); err == nil || !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("missing ffprobe error=%v", err)
	}
	runner := &fakeExtendedRunner{stdout: bytes.Repeat([]byte("x"), extendedProbeMaxOutput+1)}
	if _, err := inspectProbeFormat(context.Background(), webm, extendedFormat{"video/webm", ".webm", "matroska", false}, runner, "ffprobe"); err == nil {
		t.Fatal("malformed probe JSON accepted")
	}
	buf := &boundedBuffer{limit: 4, failOnOverflow: true}
	if _, err := buf.Write([]byte("12345")); !errors.Is(err, errExtendedOutputLimit) || len(buf.Bytes()) != 4 {
		t.Fatalf("bounded write err=%v bytes=%q", err, buf.Bytes())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspectExtended(ctx, webm, &fakeExtendedRunner{}, "ffprobe"); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error=%v", err)
	}
	runner = &fakeExtendedRunner{err: context.DeadlineExceeded}
	if _, err := inspectProbeFormat(context.Background(), webm, extendedFormat{"video/webm", ".webm", "matroska", false}, runner, "ffprobe"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("tool timeout error=%v", err)
	}
}

func TestExtendedMediaRealFixtures(t *testing.T) {
	if os.Getenv("KURA_EXTENDED_INTEGRATION_TESTS") != "1" {
		t.Skip("set KURA_EXTENDED_INTEGRATION_TESTS=1 to exercise installed ffmpeg tools")
	}
	for _, name := range []string{"animated.webp", "static.avif", "h264-aac.mp4", "vp9-opus.webm"} {
		t.Run(name, func(t *testing.T) {
			data := extendedFixture(t, name)
			desc, err := InspectExtended(context.Background(), data)
			if err != nil {
				t.Fatal(err)
			}
			poster, err := DecodeExtendedPoster(context.Background(), data, desc)
			if err != nil {
				t.Fatal(err)
			}
			if poster.Bounds().Dx() != desc.Width || poster.Bounds().Dy() != desc.Height {
				t.Fatalf("poster=%v descriptor=%#v", poster.Bounds(), desc)
			}
		})
	}
}

func pngFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, fixtureImage()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, fixtureImage(), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegFixtureSize(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func animatedGIFFixture(t *testing.T) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	first, second := image.NewPaletted(image.Rect(0, 0, 8, 6), palette), image.NewPaletted(image.Rect(0, 0, 8, 6), palette)
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			first.SetColorIndex(x, y, 1)
			if (x+y)%2 == 0 {
				second.SetColorIndex(x, y, 1)
			}
		}
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 25}, LoopCount: 0}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fixtureImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 30), B: 128, A: 255})
		}
	}
	return img
}

func extendedFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "extended", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
