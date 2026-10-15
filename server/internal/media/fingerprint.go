package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/bits"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"

	_ "golang.org/x/image/webp"
)

// KindFingerprint is the durable job kind for visual-similarity
// fingerprinting. Workers claim it like any other kind; the payload selects
// one stored original. It is independent of KindDerivative: thumbnails and
// fingerprints never share rows, keys, or metadata.
const KindFingerprint = "fingerprint"

// FingerprintAlgorithm is the only supported similarity algorithm. The id
// is frozen in API responses, job payloads, and the fingerprints table.
const FingerprintAlgorithm = "dhash-v1"

// maxFingerprintBytes bounds how much of an original one fingerprint job
// buffers while decoding. It mirrors the 25 MiB upload bound so similarity
// indexing cannot widen the ingest envelope.
const maxFingerprintBytes int64 = 25 << 20

// maxFingerprintPixels caps decoded dimensions before full decode at 64
// megapixels. Config is validated first so a hostile header cannot force a
// giant allocation; images over the cap fail the job with a clear error.
const maxFingerprintPixels = 64 * 1024 * 1024

// Fingerprint is one computed dHash-v1 row. OriginalHash binds the hash to
// the exact posts.hash bytes it was computed from, so a stale fingerprint
// can never match re-uploaded or replaced bytes under the same post id.
type Fingerprint struct {
	PostID       int64
	Algorithm    string
	Hash         uint64
	OriginalHash string
}

// FingerprintRecorder persists fingerprint rows. The PostgreSQL
// implementation below upserts; tests substitute a fake.
type FingerprintRecorder interface {
	UpsertFingerprint(ctx context.Context, fingerprint Fingerprint) error
}

// FingerprintProcessor executes fingerprint jobs: it reads the original
// through the Reader boundary, computes a deterministic 64-bit difference
// hash, and records it bound to the original content hash. Every step is
// retry-safe: reads never mutate, and the fingerprint row upserts on
// post_id. Failures are returned so the worker reports them through the
// durable job record (last_error with retry/backoff). Thumbnails are
// unrelated: this processor never writes derivatives.
type FingerprintProcessor struct {
	Originals    Reader
	Fingerprints FingerprintRecorder
}

// fingerprintPayload is the validated job payload for KindFingerprint.
type fingerprintPayload struct {
	PostID       int64
	OriginalKey  string
	OriginalHash string
	Algorithm    string
}

// EnqueueFingerprint builds the jobs.EnqueueRequest for one post's
// similarity fingerprint. The idempotency key binds post, algorithm, and
// original content hash, so retries of the same logical work collapse to
// one job row while a re-uploaded original (new hash) gets a new row.
func EnqueueFingerprint(postID, originalKey, originalHash string) (jobs.EnqueueRequest, error) {
	payload, err := validateFingerprintInput(postID, originalKey, originalHash, "")
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	encoded, err := json.Marshal(map[string]string{
		"post_id":       strconv.FormatInt(payload.PostID, 10),
		"original_key":  payload.OriginalKey,
		"original_hash": payload.OriginalHash,
		"algorithm":     payload.Algorithm,
	})
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	return jobs.ValidateEnqueue(jobs.EnqueueRequest{
		Kind:           KindFingerprint,
		Payload:        encoded,
		IdempotencyKey: FingerprintIdempotencyKey(payload.PostID, payload.OriginalHash),
	})
}

// FingerprintIdempotencyKey scopes one logical fingerprint job per
// (post, algorithm, original bytes). Callers outside media (upload
// enqueue, similarity backfill) must use this constructor so every writer
// addresses the same row.
func FingerprintIdempotencyKey(postID int64, originalHash string) string {
	return "fingerprint:" + strconv.FormatInt(postID, 10) + ":" + FingerprintAlgorithm + ":" + strings.TrimSpace(originalHash)
}

// Handle runs one claimed fingerprint job. Its signature matches
// jobs.Handler, so a worker can execute it directly.
func (p *FingerprintProcessor) Handle(ctx context.Context, job jobs.Job) error {
	if p == nil || p.Originals == nil || p.Fingerprints == nil {
		return errors.New("fingerprint processor is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var raw map[string]string
	if err := json.Unmarshal(job.Payload, &raw); err != nil {
		return fmt.Errorf("fingerprint payload must be valid JSON: %w", err)
	}
	payload, err := validateFingerprintInput(raw["post_id"], raw["original_key"], raw["original_hash"], raw["algorithm"])
	if err != nil {
		return err
	}
	fingerprint, err := p.process(ctx, payload)
	if err != nil {
		return err
	}
	if err := p.Fingerprints.UpsertFingerprint(ctx, fingerprint); err != nil {
		return fmt.Errorf("record fingerprint: %w", err)
	}
	return nil
}

func (p *FingerprintProcessor) process(ctx context.Context, payload fingerprintPayload) (Fingerprint, error) {
	if err := ctx.Err(); err != nil {
		return Fingerprint{}, err
	}
	body, err := p.Originals.Open(ctx, payload.OriginalKey)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("open original: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(body, maxFingerprintBytes+1))
	closeErr := body.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Fingerprint{}, fmt.Errorf("read original: %w", err)
	}
	if int64(len(data)) == 0 || int64(len(data)) > maxFingerprintBytes {
		return Fingerprint{}, fmt.Errorf("original must be between 1 byte and 25 MiB")
	}
	// Inspect first: this validates dimensions before any full decode
	// (64 MP cap in the helper), detects animated WebP via its chunks
	// rather than the static-WebP DecodeConfig that rejects animation,
	// and identifies video so it is excluded honestly instead of being
	// hashed as if it were a still image.
	desc, err := InspectExtended(ctx, data)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("decode original: file is not a supported image: %w", err)
	}
	if strings.HasPrefix(desc.MediaType, "video/") {
		// dhash-v1 is an image fingerprint. Video is deliberately
		// excluded; this terminal wording lets the job fail bounded
		// (jobs cap attempts) rather than requeue forever, and upload
		// does not enqueue fingerprint jobs for video at all.
		return Fingerprint{}, fmt.Errorf("fingerprint unsupported for %s: dhash-v1 covers images only, video is excluded", desc.MediaType)
	}
	if err := validateFingerprintDimensions(desc.Width, desc.Height); err != nil {
		return Fingerprint{}, err
	}
	if err := ctx.Err(); err != nil {
		return Fingerprint{}, err
	}
	// First-frame honest: for animated GIF/WebP this hashes frame one
	// only (DecodeExtendedPoster), never a blended or later frame.
	source, err := DecodeExtendedPoster(ctx, data, desc)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("decode original: file is not a supported image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Fingerprint{}, err
	}
	return Fingerprint{
		PostID:       payload.PostID,
		Algorithm:    FingerprintAlgorithm,
		Hash:         ComputeDHash(source),
		OriginalHash: payload.OriginalHash,
	}, nil
}

func validateFingerprintInput(postID, originalKey, originalHash, algorithm string) (fingerprintPayload, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(postID), 10, 64)
	if err != nil || id <= 0 {
		return fingerprintPayload{}, errors.New("fingerprint post_id must be a positive integer")
	}
	key := strings.TrimSpace(originalKey)
	if key == "" || strings.ContainsRune(key, '\x00') {
		return fingerprintPayload{}, errors.New("fingerprint original_key is required")
	}
	hash := strings.TrimSpace(originalHash)
	if !isCanonicalSHA256(hash) {
		return fingerprintPayload{}, errors.New("fingerprint original_hash must be sha256 followed by 64 lowercase hexadecimal characters")
	}
	name := strings.TrimSpace(algorithm)
	if name == "" {
		name = FingerprintAlgorithm
	}
	if name != FingerprintAlgorithm {
		return fingerprintPayload{}, fmt.Errorf("fingerprint algorithm %q is not supported", name)
	}
	return fingerprintPayload{PostID: id, OriginalKey: key, OriginalHash: hash, Algorithm: name}, nil
}

// validateFingerprintDimensions rejects empty images and images over the
// 64 megapixel cap before the full decode allocates pixels.
func validateFingerprintDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("decode original: image has no pixels")
	}
	if int64(width)*int64(height) > maxFingerprintPixels {
		return fmt.Errorf("decode original: image %dx%d exceeds the 64 megapixel fingerprint cap", width, height)
	}
	return nil
}

func isCanonicalSHA256(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, c := range value[len("sha256:"):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ComputeDHash returns the deterministic 64-bit difference hash (dHash-v1)
// of img. The image is sampled to a 9x8 luminance grid with bilinear
// interpolation, then each of the 64 horizontal neighbor pairs emits one
// bit in row-major order (rows top to bottom, within a row left to right):
// bit 63 (most significant) compares grid (0,0) > (1,0), and bit 0 (least
// significant) compares grid (7,7) > (8,7). A bit is 1 when the left pixel
// is strictly brighter than its right neighbor, else 0.
//
// Luminance uses Rec. 601 luma (0.299 R + 0.587 G + 0.114 B) on pixels
// composited over a documented white alpha background: a fully
// transparent pixel reads as white (255), and partial alpha blends the
// channel toward white proportionally. This keeps two renders of the same
// picture with different alpha encodings hashing alike without changing
// opaque pixels. Bilinear (not nearest-neighbor) resampling keeps the hash
// stable across small resizes and recompression, which is the goal: near
// copies that were resized or recompressed match at small Hamming
// distances. Crops, rotations, and semantic edits are out of scope.
func ComputeDHash(img image.Image) uint64 {
	grid := luminance9x8(img)
	var hash uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			hash <<= 1
			if grid[y][x] > grid[y][x+1] {
				hash |= 1
			}
		}
	}
	return hash
}

// DHashDistance is the Hamming distance between two dHash-v1 values: the
// count of differing bits (0-64). Similarity reads accept distances 0-16;
// identical files are distance 0, resized/recompressed near copies land in
// the low single digits, and unrelated pictures land far higher.
func DHashDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// luminance9x8 resamples img to a 9x8 luma grid with bilinear interpolation
// over the full source bounds.
func luminance9x8(img image.Image) [8][9]uint8 {
	bounds := img.Bounds()
	srcWidth, srcHeight := bounds.Dx(), bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 {
		return [8][9]uint8{}
	}
	var grid [8][9]uint8
	for ty := 0; ty < 8; ty++ {
		for tx := 0; tx < 9; tx++ {
			// Center-mapping: target pixel centers spread evenly over
			// the source, keeping edges aligned for any source size.
			sx := (float64(tx)+0.5)*float64(srcWidth)/9 - 0.5
			sy := (float64(ty)+0.5)*float64(srcHeight)/8 - 0.5
			grid[ty][tx] = bilinearLuma(img, bounds, sx, sy)
		}
	}
	return grid
}

func bilinearLuma(img image.Image, bounds image.Rectangle, sx, sy float64) uint8 {
	x0 := int(floor(sx))
	y0 := int(floor(sy))
	fx := sx - float64(x0)
	fy := sy - float64(y0)
	top := lerpPixel(img, bounds, x0, y0, x0+1, y0, fx)
	bottom := lerpPixel(img, bounds, x0, y0+1, x0+1, y0+1, fx)
	return uint8(top*(1-fy) + bottom*fy + 0.5)
}

func lerpPixel(img image.Image, bounds image.Rectangle, x0, y0, x1, y1 int, f float64) float64 {
	left := pixelLuma(img, bounds, x0, y0)
	right := pixelLuma(img, bounds, x1, y1)
	return float64(left)*(1-f) + float64(right)*f
}

// pixelLuma returns the white-composited Rec. 601 luma of the source pixel
// at (x, y), clamped to the source bounds.
func pixelLuma(img image.Image, bounds image.Rectangle, x, y int) uint8 {
	if x < bounds.Min.X {
		x = bounds.Min.X
	}
	if x >= bounds.Max.X {
		x = bounds.Max.X - 1
	}
	if y < bounds.Min.Y {
		y = bounds.Min.Y
	}
	if y >= bounds.Max.Y {
		y = bounds.Max.Y - 1
	}
	r, g, b, a := img.At(x, y).RGBA()
	// RGBA() returns 16-bit premultiplied channels. Composite onto a
	// documented white background: out = premultiplied + white * (1 -
	// alpha). Fully transparent reads as white, opaque pixels are
	// unchanged, and partial alpha blends toward white proportionally.
	const max = 0xffff
	nr := (r + (max - a)) >> 8
	ng := (g + (max - a)) >> 8
	nb := (b + (max - a)) >> 8
	return uint8((299*nr + 587*ng + 114*nb + 500) / 1000)
}

func floor(value float64) int {
	if value < 0 {
		return int(value) - 1
	}
	return int(value)
}

// FormatHashBits renders a dHash-v1 value as the 64-character bit string
// stored in the BIT(64) column (most significant bit first).
func FormatHashBits(hash uint64) string {
	var out [64]byte
	for i := 0; i < 64; i++ {
		if hash>>(63-i)&1 == 1 {
			out[i] = '1'
		} else {
			out[i] = '0'
		}
	}
	return string(out[:])
}

// ParseHashBits parses a 64-character BIT(64) text value back to uint64.
func ParseHashBits(value string) (uint64, error) {
	if len(value) != 64 {
		return 0, errors.New("fingerprint bits must be 64 characters")
	}
	var hash uint64
	for i := 0; i < 64; i++ {
		hash <<= 1
		switch value[i] {
		case '1':
			hash |= 1
		case '0':
		default:
			return 0, errors.New("fingerprint bits must contain only 0 and 1")
		}
	}
	return hash, nil
}

const upsertFingerprintSQL = `
INSERT INTO similarity_fingerprints
    (post_id, algorithm, fingerprint, original_hash, updated_at)
VALUES
    ($1, $2, $3::bit(64), $4, now())
ON CONFLICT (post_id)
DO UPDATE SET
    algorithm = EXCLUDED.algorithm,
    fingerprint = EXCLUDED.fingerprint,
    original_hash = EXCLUDED.original_hash,
    updated_at = now()
`

// PostgresFingerprintStore records fingerprint rows against PostgreSQL. The
// upsert keeps retries idempotent: one row per post.
type PostgresFingerprintStore struct {
	pool *pgxpool.Pool
}

// NewPostgresFingerprintStore wraps an existing pool. A nil pool keeps
// UpsertFingerprint returning an error so miswiring surfaces as job
// failure rather than silent metadata loss.
func NewPostgresFingerprintStore(pool *pgxpool.Pool) *PostgresFingerprintStore {
	return &PostgresFingerprintStore{pool: pool}
}

// UpsertFingerprint records one computed fingerprint, bound to the exact
// original bytes it was computed from.
func (s *PostgresFingerprintStore) UpsertFingerprint(ctx context.Context, fingerprint Fingerprint) error {
	if s == nil || s.pool == nil {
		return errors.New("fingerprint store is unavailable")
	}
	if fingerprint.PostID <= 0 {
		return errors.New("fingerprint post id is invalid")
	}
	algorithm := strings.TrimSpace(fingerprint.Algorithm)
	if algorithm == "" {
		algorithm = FingerprintAlgorithm
	}
	if algorithm != FingerprintAlgorithm {
		return fmt.Errorf("fingerprint algorithm %q is not supported", fingerprint.Algorithm)
	}
	if !isCanonicalSHA256(fingerprint.OriginalHash) {
		return errors.New("fingerprint original hash is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, upsertFingerprintSQL,
		fingerprint.PostID, algorithm, FormatHashBits(fingerprint.Hash), fingerprint.OriginalHash,
	)
	return err
}
