package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"

	_ "golang.org/x/image/webp"
)

// ErrAnimatedWebP marks an animated WebP, which Kura rejects explicitly.
var ErrAnimatedWebP = errors.New("animated WebP is not supported")

// ErrInvalidWebP marks a malformed, truncated, or unusable WebP.
var ErrInvalidWebP = errors.New("invalid WebP")

// IsWebP reports whether data starts with the RIFF/WEBP signature.
func IsWebP(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

// ValidateStaticWebP validates a bounded static WebP and returns its
// decoded configuration. It rejects animated files (VP8X animation flag,
// ANIM, or ANMF chunks), impossible RIFF sizes, malformed or truncated
// chunks, and images the decoder cannot fully decode. Callers must bound
// len(data) before calling (uploads and derivatives cap at 25 MiB).
func ValidateStaticWebP(data []byte) (image.Config, error) {
	if !IsWebP(data) {
		return image.Config{}, fmt.Errorf("%w: missing RIFF/WEBP signature", ErrInvalidWebP)
	}
	riffSize := binary.LittleEndian.Uint32(data[4:8])
	if riffSize < 4 || uint64(riffSize)+8 != uint64(len(data)) {
		return image.Config{}, fmt.Errorf("%w: RIFF size %d does not match file size %d", ErrInvalidWebP, riffSize, len(data))
	}
	offset := 12
	seenVP8X := false
	canvasWidth, canvasHeight := 0, 0
	imageChunks := 0
	for offset < len(data) {
		if len(data)-offset < 8 {
			return image.Config{}, fmt.Errorf("%w: truncated chunk header at offset %d", ErrInvalidWebP, offset)
		}
		chunkID := string(data[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		dataStart := offset + 8
		dataEnd := uint64(dataStart) + uint64(chunkSize)
		if dataEnd > uint64(len(data)) {
			return image.Config{}, fmt.Errorf("%w: chunk %q overruns file", ErrInvalidWebP, chunkID)
		}
		paddedEnd := dataEnd
		if chunkSize%2 == 1 {
			paddedEnd++
		}
		if paddedEnd > uint64(len(data)) {
			return image.Config{}, fmt.Errorf("%w: chunk %q missing padding byte", ErrInvalidWebP, chunkID)
		}
		switch chunkID {
		case "ANIM", "ANMF":
			return image.Config{}, ErrAnimatedWebP
		case "VP8X":
			if offset != 12 || seenVP8X {
				return image.Config{}, fmt.Errorf("%w: VP8X must be the unique first chunk", ErrInvalidWebP)
			}
			seenVP8X = true
			if chunkSize != 10 {
				return image.Config{}, fmt.Errorf("%w: VP8X chunk size %d, want 10", ErrInvalidWebP, chunkSize)
			}
			flags := data[dataStart]
			if flags&0x02 != 0 {
				return image.Config{}, ErrAnimatedWebP
			}
			canvasWidth = int(uint32(data[dataStart+4])|uint32(data[dataStart+5])<<8|uint32(data[dataStart+6])<<16) + 1
			canvasHeight = int(uint32(data[dataStart+7])|uint32(data[dataStart+8])<<8|uint32(data[dataStart+9])<<16) + 1
		case "VP8 ", "VP8L":
			imageChunks++
			if imageChunks > 1 {
				return image.Config{}, fmt.Errorf("%w: multiple image chunks", ErrInvalidWebP)
			}
		default:
			// ALPH, ICCP, EXIF, XMP and other ancillary chunks are size-checked above.
		}
		offset = int(paddedEnd)
	}
	if offset != len(data) {
		return image.Config{}, fmt.Errorf("%w: trailing data after last chunk", ErrInvalidWebP)
	}
	if imageChunks != 1 {
		return image.Config{}, fmt.Errorf("%w: missing image chunk", ErrInvalidWebP)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, fmt.Errorf("%w: %v", ErrInvalidWebP, err)
	}
	if format != "webp" || config.Width <= 0 || config.Height <= 0 {
		return image.Config{}, fmt.Errorf("%w: unusable dimensions %dx%d", ErrInvalidWebP, config.Width, config.Height)
	}
	if seenVP8X && (config.Width != canvasWidth || config.Height != canvasHeight) {
		return image.Config{}, fmt.Errorf("%w: VP8X canvas %dx%d does not match decoded %dx%d", ErrInvalidWebP, canvasWidth, canvasHeight, config.Width, config.Height)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, fmt.Errorf("%w: %v", ErrInvalidWebP, err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height || bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return image.Config{}, fmt.Errorf("%w: decoded image has no usable pixels", ErrInvalidWebP)
	}
	return config, nil
}
