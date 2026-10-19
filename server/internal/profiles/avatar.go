package profiles

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	_ "golang.org/x/image/webp"
)

const (
	maxAvatarBytes = 2 << 20
	maxAvatarEdge  = 4096
	avatarSide     = 512
)

func NormalizeAvatar(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxAvatarBytes {
		return nil, errAvatar("file must be a JPEG, PNG, or static WebP under 2 MiB")
	}
	kind, err := sniffAvatar(data)
	if err != nil {
		return nil, err
	}
	if kind == "webp" {
		if _, webpErr := media.ValidateStaticWebP(data); webpErr != nil {
			if errors.Is(webpErr, media.ErrAnimatedWebP) {
				return nil, errAvatar("animated images are not supported")
			}
			return nil, errAvatar("file is not a usable static WebP")
		}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != kind {
		return nil, errAvatar("file could not be decoded")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxAvatarEdge || config.Height > maxAvatarEdge {
		return nil, errAvatar("image dimensions are not supported")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errAvatar("file could not be decoded")
	}
	square := centerCropSquare(decoded)
	if square.Bounds().Dx() > avatarSide {
		square = scaleNearest(square, avatarSide)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, square, &jpeg.Options{Quality: 85}); err != nil {
		return nil, errAvatar("picture could not be stored")
	}
	return encoded.Bytes(), nil
}

func sniffAvatar(data []byte) (string, error) {
	switch {
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "jpeg", nil
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "png", nil
	case media.IsWebP(data):
		return "webp", nil
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return "", errAvatar("GIF images are not supported")
	case bytes.HasPrefix(bytes.TrimSpace(data), []byte("<")):
		return "", errAvatar("SVG images are not supported")
	case bytes.HasPrefix(data, []byte("MZ")) || bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}):
		return "", errAvatar("file is not an image")
	case bytes.HasPrefix(data, []byte("%PDF")) || bytes.HasPrefix(data, []byte("PK")):
		return "", errAvatar("file is not an image")
	default:
		return "", errAvatar("file must be a JPEG, PNG, or static WebP")
	}
}

func centerCropSquare(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	side := bounds.Dx()
	if bounds.Dy() < side {
		side = bounds.Dy()
	}
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), src, image.Point{X: bounds.Min.X + (bounds.Dx()-side)/2, Y: bounds.Min.Y + (bounds.Dy()-side)/2}, draw.Src)
	return dst
}

func scaleNearest(src *image.RGBA, size int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	for y := 0; y < size; y++ {
		sy := bounds.Min.Y + y*sh/size
		for x := 0; x < size; x++ {
			sx := bounds.Min.X + x*sw/size
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func errAvatar(message string) error {
	return fmtJoin(ErrAvatar, message)
}

func fmtJoin(sentinel error, message string) error {
	return errors.Join(sentinel, errors.New(message))
}

func ReadLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxAvatarBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAvatarBytes {
		return nil, errAvatar("file must be a JPEG, PNG, or static WebP under 2 MiB")
	}
	return data, nil
}
