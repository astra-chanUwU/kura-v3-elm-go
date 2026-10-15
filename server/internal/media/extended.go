package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"
)

const (
	extendedMaxBytes       = 25 << 20
	extendedMaxPixels      = 64_000_000
	extendedMaxFrames      = 10_000
	extendedMaxVideo       = 300 * time.Second
	extendedCommandTimeout = 10 * time.Second
	extendedProbeMaxOutput = 64 << 10
	extendedPosterMaxBytes = 8 << 20
	extendedStderrMaxBytes = 8 << 10
)

// ExtendedDescriptor is the server-verified media description used by the
// optional extended-media ingest path. Duration is zero when the container
// does not provide a meaningful duration (for example a static image).
type ExtendedDescriptor struct {
	MediaType  string        `json:"media_type"`
	Extension  string        `json:"extension"`
	Width      int           `json:"width"`
	Height     int           `json:"height"`
	Duration   time.Duration `json:"duration,omitempty"`
	Animated   bool          `json:"animated,omitempty"`
	VideoCodec string        `json:"video_codec,omitempty"`
	AudioCodec string        `json:"audio_codec,omitempty"`
}

var (
	ErrExtendedUnsupported  = errors.New("unsupported extended media")
	ErrExtendedAnimatedAVIF = errors.New("animated AVIF is not supported")
	errExtendedOutputLimit  = errors.New("media tool output exceeded its limit")
)

type extendedCommandRunner interface {
	Run(context.Context, string, []string, int, int) ([]byte, []byte, error)
}

type execExtendedRunner struct{}

func (execExtendedRunner) Run(ctx context.Context, executable string, args []string, stdoutLimit, stderrLimit int) ([]byte, []byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, extendedCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, executable, args...)
	cmd.Stdin = nil
	stdout := &boundedBuffer{limit: stdoutLimit, failOnOverflow: true}
	stderr := &boundedBuffer{limit: stderrLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if cmdCtx.Err() != nil {
		return stdout.Bytes(), stderr.Bytes(), cmdCtx.Err()
	}
	if stdout.Overflowed() {
		return stdout.Bytes(), stderr.Bytes(), errExtendedOutputLimit
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

type boundedBuffer struct {
	mu             sync.Mutex
	buf            bytes.Buffer
	limit          int
	overflow       bool
	failOnOverflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - b.buf.Len()
	if remaining < len(p) {
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.overflow = true
		if b.failOnOverflow {
			return remaining, errExtendedOutputLimit
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

func (b *boundedBuffer) Overflowed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.overflow
}

type extendedFormat struct {
	mediaType string
	extension string
	demuxer   string
	animated  bool
}

// InspectExtended validates the content and returns authoritative media
// metadata. JPEG, PNG, GIF and static WebP use Go decoders; animated WebP,
// static AVIF, MP4 and WebM use ffprobe when it is installed.
func InspectExtended(ctx context.Context, data []byte) (ExtendedDescriptor, error) {
	return inspectExtended(ctx, data, execExtendedRunner{}, extendedToolPath("KURA_FFPROBE_PATH", "ffprobe"))
}

func inspectExtended(ctx context.Context, data []byte, runner extendedCommandRunner, ffprobe string) (ExtendedDescriptor, error) {
	if err := validateExtendedBytes(ctx, data); err != nil {
		return ExtendedDescriptor{}, err
	}
	if desc, ok, err := inspectBasicImage(ctx, data, runner, ffprobe); ok || err != nil {
		return desc, err
	}
	format, err := detectExtendedFormat(data)
	if err != nil {
		return ExtendedDescriptor{}, err
	}
	return inspectProbeFormat(ctx, data, format, runner, ffprobe)
}

func inspectProbeFormat(ctx context.Context, data []byte, format extendedFormat, runner extendedCommandRunner, ffprobe string) (ExtendedDescriptor, error) {
	if ffprobe == "" {
		return ExtendedDescriptor{}, errors.New("extended media requires ffprobe; install ffmpeg 9.0.2 or configure KURA_FFPROBE_PATH")
	}
	input, cleanup, err := extendedTempFile(data)
	if err != nil {
		return ExtendedDescriptor{}, fmt.Errorf("prepare media for inspection: %w", err)
	}
	defer cleanup()
	args := []string{"-v", "error", "-protocol_whitelist", "file", "-f", format.demuxer}
	if format.demuxer == "mov" {
		args = append(args, "-enable_drefs", "0", "-use_absolute_path", "0")
	}
	args = append(args, "-i", input)
	if format.mediaType == "image/avif" || format.mediaType == "image/webp" {
		args = append(args, "-count_frames")
	}
	args = append(args,
		"-show_entries", "format=format_name,duration:stream=codec_type,codec_name,width,height,duration,nb_frames,nb_read_frames",
		"-of", "json",
	)
	stdout, stderr, runErr := runner.Run(ctx, ffprobe, args, extendedProbeMaxOutput, extendedStderrMaxBytes)
	if runErr != nil {
		return ExtendedDescriptor{}, toolError("ffprobe", runErr, stderr)
	}
	var probe extendedProbe
	if err := json.Unmarshal(stdout, &probe); err != nil {
		return ExtendedDescriptor{}, fmt.Errorf("ffprobe returned invalid metadata: %w", err)
	}
	return validateProbe(format, probe)
}

// DecodeExtendedPoster returns the first decoded frame. It re-inspects input
// and compares descriptors so callers cannot select a demuxer or dimensions.
func DecodeExtendedPoster(ctx context.Context, data []byte, descriptor ExtendedDescriptor) (image.Image, error) {
	return decodeExtendedPoster(ctx, data, descriptor, execExtendedRunner{}, extendedToolPath("KURA_FFPROBE_PATH", "ffprobe"), extendedToolPath("KURA_FFMPEG_PATH", "ffmpeg"))
}

func decodeExtendedPoster(ctx context.Context, data []byte, descriptor ExtendedDescriptor, runner extendedCommandRunner, ffprobe, ffmpeg string) (image.Image, error) {
	verified, err := inspectExtended(ctx, data, runner, ffprobe)
	if err != nil {
		return nil, err
	}
	if descriptor != verified {
		return nil, errors.New("media descriptor does not match inspected input")
	}
	if verified.MediaType == "image/jpeg" || verified.MediaType == "image/png" || verified.MediaType == "image/gif" || (verified.MediaType == "image/webp" && !verified.Animated) {
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode media poster: %w", err)
		}
		return decoded, nil
	}
	if ffmpeg == "" {
		return nil, errors.New("extended media poster requires ffmpeg; install ffmpeg 9.0.2 or configure KURA_FFMPEG_PATH")
	}
	format, err := detectExtendedFormat(data)
	if err != nil {
		return nil, err
	}
	input, cleanup, err := extendedTempFile(data)
	if err != nil {
		return nil, fmt.Errorf("prepare media for poster decode: %w", err)
	}
	defer cleanup()
	args := []string{"-v", "error", "-nostdin", "-threads", "1", "-protocol_whitelist", "file", "-f", format.demuxer}
	if format.demuxer == "mov" {
		args = append(args, "-enable_drefs", "0", "-use_absolute_path", "0")
	}
	args = append(args, "-i", input, "-map", "0:v:0", "-frames:v", "1", "-threads", "1", "-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1")
	stdout, stderr, runErr := runner.Run(ctx, ffmpeg, args, extendedPosterMaxBytes, extendedStderrMaxBytes)
	if runErr != nil {
		return nil, toolError("ffmpeg", runErr, stderr)
	}
	poster, formatName, err := image.Decode(bytes.NewReader(stdout))
	if err != nil || formatName != "jpeg" {
		if err == nil {
			err = errors.New("poster output was not JPEG")
		}
		return nil, fmt.Errorf("decode media poster: %w", err)
	}
	bounds := poster.Bounds()
	if validateDimensions(bounds.Dx(), bounds.Dy()) != nil || bounds.Dx() != verified.Width || bounds.Dy() != verified.Height {
		return nil, errors.New("poster dimensions do not match inspected media")
	}
	return poster, nil
}

func inspectBasicImage(ctx context.Context, data []byte, runner extendedCommandRunner, ffprobe string) (ExtendedDescriptor, bool, error) {
	if IsWebP(data) {
		if animatedWebP, duration, err := inspectWebPAnimation(data); err != nil {
			return ExtendedDescriptor{}, true, err
		} else if animatedWebP {
			format := extendedFormat{"image/webp", ".webp", "webp_anim", true}
			desc, err := inspectProbeFormat(ctx, data, format, runner, ffprobe)
			if err == nil && duration > 0 {
				desc.Duration = duration
			}
			return desc, true, err
		}
		config, err := ValidateStaticWebP(data)
		if err != nil {
			return ExtendedDescriptor{}, true, err
		}
		if err := validateDimensions(config.Width, config.Height); err != nil {
			return ExtendedDescriptor{}, true, err
		}
		return imageDescriptor("image/webp", ".webp", config.Width, config.Height, false, 0), true, nil
	}
	config, formatName, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ExtendedDescriptor{}, false, nil
	}
	mediaType, extension := "", ""
	switch formatName {
	case "jpeg":
		mediaType, extension = "image/jpeg", ".jpg"
	case "png":
		mediaType, extension = "image/png", ".png"
	case "gif":
		mediaType, extension = "image/gif", ".gif"
	default:
		return ExtendedDescriptor{}, false, nil
	}
	if err := validateDimensions(config.Width, config.Height); err != nil {
		return ExtendedDescriptor{}, true, err
	}
	if formatName == "gif" {
		animated, duration, err := inspectGIF(data)
		if err != nil {
			return ExtendedDescriptor{}, true, err
		}
		decoded, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil || len(decoded.Image) == 0 {
			if err == nil {
				err = errors.New("GIF has no decodable frames")
			}
			return ExtendedDescriptor{}, true, fmt.Errorf("invalid GIF image: %w", err)
		}
		if (len(decoded.Image) > 1) != animated {
			return ExtendedDescriptor{}, true, errors.New("GIF frame count does not match its structure")
		}
		return imageDescriptor(mediaType, extension, config.Width, config.Height, animated, duration), true, nil
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return ExtendedDescriptor{}, true, fmt.Errorf("invalid %s image: %w", formatName, err)
	}
	return imageDescriptor(mediaType, extension, config.Width, config.Height, false, 0), true, nil
}

func detectExtendedFormat(data []byte) (extendedFormat, error) {
	if IsWebP(data) {
		animated, _, err := inspectWebPAnimation(data)
		if err != nil {
			return extendedFormat{}, err
		}
		if animated {
			return extendedFormat{"image/webp", ".webp", "webp_anim", true}, nil
		}
		return extendedFormat{}, fmt.Errorf("static WebP should use the built-in image decoder")
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		boxSize := int(binary.BigEndian.Uint32(data[:4]))
		if boxSize < 16 || boxSize > len(data) || (boxSize-8)%4 != 0 {
			return extendedFormat{}, fmt.Errorf("invalid ISO-BMFF file type box")
		}
		brands := make(map[string]bool)
		brands[string(data[8:12])] = true
		for offset := 16; offset+4 <= boxSize; offset += 4 {
			brands[string(data[offset:offset+4])] = true
		}
		if brands["avis"] {
			return extendedFormat{}, ErrExtendedAnimatedAVIF
		}
		if brands["avif"] {
			return extendedFormat{"image/avif", ".avif", "mov", false}, nil
		}
		for _, brand := range []string{"isom", "iso2", "iso3", "iso4", "iso5", "iso6", "mp41", "mp42", "avc1", "dash", "M4V "} {
			if brands[brand] {
				return extendedFormat{"video/mp4", ".mp4", "mov", false}, nil
			}
		}
		return extendedFormat{}, fmt.Errorf("unsupported ISO-BMFF media brand")
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return extendedFormat{"video/webm", ".webm", "matroska", false}, nil
	}
	return extendedFormat{}, ErrExtendedUnsupported
}

func validateExtendedBytes(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > extendedMaxBytes {
		return fmt.Errorf("media must be between 1 byte and 25 MiB")
	}
	return nil
}

func validateProbe(format extendedFormat, probe extendedProbe) (ExtendedDescriptor, error) {
	if format.mediaType == "video/mp4" && !strings.Contains(probe.Format.Name, "mp4") {
		return ExtendedDescriptor{}, errors.New("input is not a supported MP4 container")
	}
	if format.mediaType == "video/webm" && !strings.Contains(probe.Format.Name, "webm") {
		return ExtendedDescriptor{}, errors.New("input is not a WebM container")
	}
	if len(probe.Streams) == 0 || len(probe.Streams) > 2 {
		return ExtendedDescriptor{}, errors.New("media must contain one video stream and at most one audio stream")
	}
	var video *extendedProbeStream
	audioCount := 0
	for i := range probe.Streams {
		stream := &probe.Streams[i]
		switch stream.CodecType {
		case "video":
			if video != nil {
				return ExtendedDescriptor{}, errors.New("media must contain exactly one video stream")
			}
			video = stream
		case "audio":
			audioCount++
			if audioCount > 1 {
				return ExtendedDescriptor{}, errors.New("media may contain at most one audio stream")
			}
		default:
			return ExtendedDescriptor{}, errors.New("media contains an unsupported stream")
		}
	}
	if video == nil {
		return ExtendedDescriptor{}, errors.New("media has no video stream")
	}
	if err := validateDimensions(video.Width, video.Height); err != nil {
		return ExtendedDescriptor{}, err
	}
	desc := imageDescriptor(format.mediaType, format.extension, video.Width, video.Height, format.animated || strings.HasPrefix(format.mediaType, "video/"), 0)
	desc.VideoCodec = video.CodecName
	var audio *extendedProbeStream
	for i := range probe.Streams {
		if probe.Streams[i].CodecType == "audio" {
			audio = &probe.Streams[i]
		}
	}
	if format.mediaType == "image/webp" {
		if video.CodecName != "webp_anim" && video.CodecName != "webp" {
			return ExtendedDescriptor{}, errors.New("animated WebP must contain a WebP image stream")
		}
		if audio != nil {
			return ExtendedDescriptor{}, errors.New("animated WebP cannot contain audio")
		}
		if probeFrameCount(*video) < 2 {
			return ExtendedDescriptor{}, errors.New("animated WebP must contain at least two verified frames")
		}
		return desc, nil
	}
	if format.mediaType == "image/avif" {
		if video.CodecName != "av1" || audio != nil {
			return ExtendedDescriptor{}, errors.New("AVIF must contain one AV1 image and no audio")
		}
		frames := probeFrameCount(*video)
		if frames > 1 {
			return ExtendedDescriptor{}, ErrExtendedAnimatedAVIF
		}
		if frames == 0 {
			return ExtendedDescriptor{}, errors.New("AVIF frame count could not be verified")
		}
		return desc, nil
	}
	allowedVideo := (format.mediaType == "video/mp4" && video.CodecName == "h264") || (format.mediaType == "video/webm" && (video.CodecName == "vp8" || video.CodecName == "vp9"))
	if !allowedVideo {
		return ExtendedDescriptor{}, errors.New("video codec is not supported; MP4 requires H.264 and WebM requires VP8 or VP9")
	}
	if audio != nil {
		allowedAudio := (format.mediaType == "video/mp4" && audio.CodecName == "aac") || (format.mediaType == "video/webm" && (audio.CodecName == "opus" || audio.CodecName == "vorbis"))
		if !allowedAudio {
			return ExtendedDescriptor{}, errors.New("audio codec is not supported for this video container")
		}
		desc.AudioCodec = audio.CodecName
	}
	duration, err := probeDuration(probe, *video, audio)
	if err != nil {
		return ExtendedDescriptor{}, err
	}
	if duration <= 0 || duration > extendedMaxVideo {
		return ExtendedDescriptor{}, errors.New("video duration must be greater than zero and at most 300 seconds")
	}
	desc.Duration = duration
	return desc, nil
}

func validateDimensions(width, height int) error {
	if width <= 0 || height <= 0 || width > extendedMaxPixels/height {
		return errors.New("media dimensions must be positive and at most 64 megapixels")
	}
	return nil
}

func imageDescriptor(mediaType, extension string, width, height int, animated bool, duration time.Duration) ExtendedDescriptor {
	return ExtendedDescriptor{MediaType: mediaType, Extension: extension, Width: width, Height: height, Animated: animated, Duration: duration}
}

func probeDuration(probe extendedProbe, video extendedProbeStream, audio *extendedProbeStream) (time.Duration, error) {
	values := []string{video.Duration, probe.Format.Duration}
	if audio != nil {
		values = append(values, audio.Duration)
	}
	var seconds float64
	for _, raw := range values {
		if strings.TrimSpace(raw) == "" || raw == "N/A" {
			continue
		}
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
			return 0, errors.New("media duration is invalid")
		}
		if parsed > seconds {
			seconds = parsed
		}
	}
	if seconds <= 0 || seconds > extendedMaxVideo.Seconds() {
		return 0, errors.New("video duration must be greater than zero and at most 300 seconds")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func probeFrameCount(stream extendedProbeStream) int64 {
	for _, raw := range []string{stream.NBReadFrames, stream.NBFrames} {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

type extendedProbe struct {
	Format struct {
		Name     string `json:"format_name"`
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []extendedProbeStream `json:"streams"`
}

type extendedProbeStream struct {
	CodecType    string `json:"codec_type"`
	CodecName    string `json:"codec_name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Duration     string `json:"duration"`
	NBFrames     string `json:"nb_frames"`
	NBReadFrames string `json:"nb_read_frames"`
}

func inspectWebPAnimation(data []byte) (bool, time.Duration, error) {
	if !IsWebP(data) {
		return false, 0, nil
	}
	riffSize := binary.LittleEndian.Uint32(data[4:8])
	if riffSize < 4 || uint64(riffSize)+8 != uint64(len(data)) {
		return false, 0, fmt.Errorf("invalid WebP RIFF size")
	}
	offset := 12
	animated, seenAnim, seenVP8X := false, false, false
	frames, images := 0, 0
	var decodedPixels int64
	var duration time.Duration
	for offset < len(data) {
		if len(data)-offset < 8 {
			return false, 0, errors.New("truncated WebP chunk header")
		}
		kind := string(data[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start, end := offset+8, uint64(offset+8)+size
		paddedEnd := end + size%2
		if end > uint64(len(data)) || paddedEnd > uint64(len(data)) {
			return false, 0, errors.New("WebP chunk exceeds input")
		}
		switch kind {
		case "VP8X":
			if offset != 12 || seenVP8X || size != 10 {
				return false, 0, errors.New("invalid WebP extended header")
			}
			seenVP8X = true
			if data[start]&0x02 != 0 {
				animated = true
			}
		case "ANIM":
			if !seenVP8X || !animated || seenAnim || size != 6 {
				return false, 0, errors.New("invalid WebP animation control")
			}
			seenAnim = true
		case "ANMF":
			if !seenAnim || size < 16 {
				return false, 0, errors.New("invalid WebP animation frame")
			}
			frames++
			if frames > extendedMaxFrames {
				return false, 0, errors.New("animated WebP exceeds 10000 frames")
			}
			width := int(uint32(data[start+6])|uint32(data[start+7])<<8|uint32(data[start+8])<<16) + 1
			height := int(uint32(data[start+9])|uint32(data[start+10])<<8|uint32(data[start+11])<<16) + 1
			if err := validateDimensions(width, height); err != nil {
				return false, 0, err
			}
			decodedPixels += int64(width) * int64(height)
			if decodedPixels > extendedMaxPixels {
				return false, 0, errors.New("animated WebP decoded frames exceed 64 megapixels")
			}
			duration += time.Duration(uint32(data[start+12])|uint32(data[start+13])<<8|uint32(data[start+14])<<16) * time.Millisecond
		case "VP8 ", "VP8L":
			if animated {
				return false, 0, errors.New("animated WebP contains a standalone image chunk")
			}
			images++
		}
		offset = int(paddedEnd)
	}
	if animated && (!seenAnim || frames == 0) {
		return false, 0, errors.New("animated WebP has no animation frames")
	}
	if !animated && images != 1 {
		return false, 0, errors.New("static WebP must contain one image chunk")
	}
	return animated, duration, nil
}

// inspectGIF walks GIF blocks without retaining every decoded frame. It
// validates the complete block stream, counts image frames, and sums GCE delay.
func inspectGIF(data []byte) (bool, time.Duration, error) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return false, 0, errors.New("invalid GIF header")
	}
	packed := data[10]
	offset := 13
	if packed&0x80 != 0 {
		offset += 3 * (1 << ((packed & 7) + 1))
	}
	if offset > len(data) {
		return false, 0, errors.New("truncated GIF color table")
	}
	frames := 0
	var decodedPixels int64
	var pendingDelay time.Duration
	var duration time.Duration
	trailer := false
	for offset < len(data) {
		marker := data[offset]
		offset++
		switch marker {
		case 0x3b:
			trailer = true
			if offset != len(data) {
				return false, 0, errors.New("trailing bytes after GIF trailer")
			}
		case 0x21:
			if offset >= len(data) {
				return false, 0, errors.New("truncated GIF extension")
			}
			label := data[offset]
			offset++
			if label == 0xf9 {
				if offset+6 > len(data) || data[offset] != 4 || data[offset+5] != 0 {
					return false, 0, errors.New("invalid GIF graphic control extension")
				}
				pendingDelay = time.Duration(binary.LittleEndian.Uint16(data[offset+2:offset+4])) * 10 * time.Millisecond
				offset += 6
			} else {
				var err error
				offset, err = skipGIFSubBlocks(data, offset)
				if err != nil {
					return false, 0, err
				}
			}
		case 0x2c:
			if offset+9 > len(data) {
				return false, 0, errors.New("truncated GIF image descriptor")
			}
			width := int(binary.LittleEndian.Uint16(data[offset+4 : offset+6]))
			height := int(binary.LittleEndian.Uint16(data[offset+6 : offset+8]))
			if err := validateDimensions(width, height); err != nil {
				return false, 0, err
			}
			decodedPixels += int64(width) * int64(height)
			if decodedPixels > extendedMaxPixels {
				return false, 0, errors.New("GIF decoded frame data exceeds 64 megapixels")
			}
			localPacked := data[offset+8]
			offset += 9
			if localPacked&0x80 != 0 {
				offset += 3 * (1 << ((localPacked & 7) + 1))
			}
			if offset >= len(data) {
				return false, 0, errors.New("truncated GIF image data")
			}
			offset++ // LZW minimum code size.
			var err error
			offset, err = skipGIFSubBlocks(data, offset)
			if err != nil {
				return false, 0, err
			}
			frames++
			if frames > extendedMaxFrames {
				return false, 0, errors.New("GIF exceeds 10000 frames")
			}
			duration += pendingDelay
			pendingDelay = 0
		default:
			return false, 0, errors.New("invalid GIF block marker")
		}
		if offset > len(data) {
			return false, 0, errors.New("truncated GIF block")
		}
	}
	if !trailer || frames == 0 {
		return false, 0, errors.New("GIF has no complete image frames")
	}
	return frames > 1, duration, nil
}

func skipGIFSubBlocks(data []byte, offset int) (int, error) {
	for {
		if offset >= len(data) {
			return 0, errors.New("truncated GIF data sub-block")
		}
		size := int(data[offset])
		offset++
		if size == 0 {
			return offset, nil
		}
		if offset+size > len(data) {
			return 0, errors.New("truncated GIF data sub-block")
		}
		offset += size
	}
}

func extendedToolPath(envName, name string) string {
	if configured := strings.TrimSpace(os.Getenv(envName)); configured != "" {
		return configured
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func extendedTempFile(data []byte) (string, func(), error) {
	file, err := os.CreateTemp("", "kura-extended-*")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return filepath.Clean(path), cleanup, nil
}

func toolError(name string, err error, stderr []byte) error {
	message := strings.TrimSpace(string(stderr))
	if message != "" {
		return fmt.Errorf("%s failed: %w: %s", name, err, message)
	}
	return fmt.Errorf("%s failed: %w", name, err)
}
