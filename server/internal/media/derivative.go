package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
)

// KindDerivative is the durable job kind for thumbnail processing. Workers
// claim it like any other kind; the payload selects one bounded variant of
// one stored original.
const KindDerivative = "derivative"

// Supported thumbnail variants and their long-edge bound in pixels. The
// decode set stays the repository's existing stdlib set (JPEG, PNG, GIF,
// as accepted by uploads); no ffmpeg, libvips, or richer formats are added.
var derivativeBounds = map[string]int{
	"thumb-160": 160,
	"thumb-320": 320,
}

// DefaultDerivativeVariant is used when the job payload names no variant.
const DefaultDerivativeVariant = "thumb-320"

// maxOriginalBytes bounds how much of an original one job buffers while
// decoding. It mirrors the 25 MiB upload bound so derivatives cannot widen
// the ingest envelope.
const maxOriginalBytes int64 = 25 << 20

// Variant is the recorded metadata for one processed derivative. Originals
// are never mutated: the variant lives under its own derivatives/ key and
// the row upserts on (post_id, variant) so retries collapse to one row.
type Variant struct {
	PostID    int64
	Name      string
	ObjectKey string
	URL       string
	MediaType string
	Width     int
	Height    int
	Size      int64
	Status    string
	LastError string
}

// VariantRecorder persists variant metadata. The PostgreSQL implementation
// below upserts; tests substitute a fake.
type VariantRecorder interface {
	UpsertVariant(ctx context.Context, variant Variant) error
}

// DerivativeProcessor executes derivative jobs: it reads the original
// through the Store boundary, writes one bounded thumbnail, and records
// its metadata. Every step is retry-safe: reads never mutate, Put never
// overwrites an existing key, and the variant row upserts. Failures are
// returned so the worker reports them through the durable job record
// (last_error with retry/backoff).
type DerivativeProcessor struct {
	Originals   Reader
	Derivatives Store
	Variants    VariantRecorder
}

// derivativePayload is the validated job payload for KindDerivative.
type derivativePayload struct {
	PostID      int64
	OriginalKey string
	Variant     string
	MaxDim      int
}

// EnqueueDerivative builds the jobs.EnqueueRequest for one thumbnail. The
// idempotency key scopes retries of the same logical work to one job row
// per (post, variant).
func EnqueueDerivative(postID, originalKey, variant string) (jobs.EnqueueRequest, error) {
	payload, err := validateDerivativeInput(postID, originalKey, variant)
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	encoded, err := json.Marshal(map[string]string{
		"post_id":      strconv.FormatInt(payload.PostID, 10),
		"original_key": payload.OriginalKey,
		"variant":      payload.Variant,
	})
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	return jobs.ValidateEnqueue(jobs.EnqueueRequest{
		Kind:           KindDerivative,
		Payload:        encoded,
		IdempotencyKey: "derivative:" + strconv.FormatInt(payload.PostID, 10) + ":" + payload.Variant,
	})
}

// Handle runs one claimed derivative job. Its signature matches
// jobs.Handler, so a worker can execute it directly.
func (p *DerivativeProcessor) Handle(ctx context.Context, job jobs.Job) error {
	if p == nil || p.Originals == nil || p.Derivatives == nil || p.Variants == nil {
		return errors.New("derivative processor is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var raw map[string]string
	if err := json.Unmarshal(job.Payload, &raw); err != nil {
		return fmt.Errorf("derivative payload must be valid JSON: %w", err)
	}
	payload, err := validateDerivativeInput(raw["post_id"], raw["original_key"], raw["variant"])
	if err != nil {
		return err
	}
	variant, err := p.process(ctx, payload)
	if err != nil {
		return err
	}
	if err := p.Variants.UpsertVariant(ctx, variant); err != nil {
		return fmt.Errorf("record derivative variant: %w", err)
	}
	return nil
}

func (p *DerivativeProcessor) process(ctx context.Context, payload derivativePayload) (Variant, error) {
	body, err := p.Originals.Open(ctx, payload.OriginalKey)
	if err != nil {
		return Variant{}, fmt.Errorf("open original: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxOriginalBytes+1))
	closeErr := body.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Variant{}, fmt.Errorf("read original: %w", err)
	}
	if int64(len(data)) == 0 || int64(len(data)) > maxOriginalBytes {
		return Variant{}, fmt.Errorf("original must be between 1 byte and 25 MiB")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Variant{}, fmt.Errorf("decode original: file is not a supported image: %w", err)
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return Variant{}, errors.New("decode original: image has no pixels")
	}
	thumbWidth, thumbHeight := boundedDimensions(width, height, payload.MaxDim)
	thumbnail := scaleNearestNeighbor(source, bounds, thumbWidth, thumbHeight)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, thumbnail, &jpeg.Options{Quality: 80}); err != nil {
		return Variant{}, fmt.Errorf("encode thumbnail: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Variant{}, err
	}
	key := derivativeKey(payload.OriginalKey, payload.Variant)
	object, err := p.Derivatives.Put(ctx, key, "image/jpeg", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		return Variant{}, fmt.Errorf("store thumbnail: %w", err)
	}
	return Variant{
		PostID:    payload.PostID,
		Name:      payload.Variant,
		ObjectKey: object.Key,
		URL:       object.URL,
		MediaType: "image/jpeg",
		Width:     thumbWidth,
		Height:    thumbHeight,
		Size:      object.Size,
		Status:    "ready",
	}, nil
}

func validateDerivativeInput(postID, originalKey, variant string) (derivativePayload, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(postID), 10, 64)
	if err != nil || id <= 0 {
		return derivativePayload{}, errors.New("derivative post_id must be a positive integer")
	}
	key := strings.TrimSpace(originalKey)
	if key == "" || strings.ContainsRune(key, '\x00') {
		return derivativePayload{}, errors.New("derivative original_key is required")
	}
	name := strings.TrimSpace(variant)
	if name == "" {
		name = DefaultDerivativeVariant
	}
	maxDim, ok := derivativeBounds[name]
	if !ok {
		return derivativePayload{}, fmt.Errorf("derivative variant %q is not supported", name)
	}
	return derivativePayload{PostID: id, OriginalKey: key, Variant: name, MaxDim: maxDim}, nil
}

// derivativeKey derives a deterministic thumbnail key from the original
// key so retries address the same object and Put stays idempotent.
// Thumbnails never share the uploads/ namespace, keeping originals
// immutable by construction.
func derivativeKey(originalKey, variant string) string {
	base := path.Base(path.Clean("/" + originalKey))
	if index := strings.LastIndexByte(base, '.'); index > 0 {
		base = base[:index]
	}
	if strings.TrimSpace(base) == "" {
		base = "original"
	}
	return "derivatives/" + variant + "/" + base + ".jpg"
}

// boundedDimensions scales width x height so the long edge fits maxDim.
// Images already within bounds keep their size: thumbnails never upscale.
func boundedDimensions(width, height, maxDim int) (int, int) {
	longest := width
	if height > longest {
		longest = height
	}
	if longest <= maxDim {
		return width, height
	}
	scaledWidth := width * maxDim / longest
	scaledHeight := height * maxDim / longest
	if scaledWidth < 1 {
		scaledWidth = 1
	}
	if scaledHeight < 1 {
		scaledHeight = 1
	}
	return scaledWidth, scaledHeight
}

// scaleNearestNeighbor shrinks source to target dimensions with pure
// stdlib sampling. It only downscales bounded thumbnails, where the
// quality tradeoff is acceptable and adds no native dependency.
func scaleNearestNeighbor(source image.Image, bounds image.Rectangle, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	srcWidth, srcHeight := bounds.Dx(), bounds.Dy()
	for y := 0; y < height; y++ {
		srcY := bounds.Min.Y + y*srcHeight/height
		for x := 0; x < width; x++ {
			srcX := bounds.Min.X + x*srcWidth/width
			dst.Set(x, y, source.At(srcX, srcY))
		}
	}
	return dst
}

const upsertVariantSQL = `
INSERT INTO asset_variants
    (post_id, variant, object_key, url, media_type, width, height, file_size, status, last_error, updated_at)
VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (post_id, variant)
DO UPDATE SET
    object_key = EXCLUDED.object_key,
    url = EXCLUDED.url,
    media_type = EXCLUDED.media_type,
    width = EXCLUDED.width,
    height = EXCLUDED.height,
    file_size = EXCLUDED.file_size,
    status = EXCLUDED.status,
    last_error = EXCLUDED.last_error,
    updated_at = now()
`

// PostgresVariantStore records variant metadata against PostgreSQL. The
// upsert keeps retries idempotent: one row per (post_id, variant).
type PostgresVariantStore struct {
	pool *pgxpool.Pool
}

// NewPostgresVariantStore wraps an existing pool. A nil pool keeps
// UpsertVariant returning an error so miswiring surfaces as job failure
// rather than silent metadata loss.
func NewPostgresVariantStore(pool *pgxpool.Pool) *PostgresVariantStore {
	return &PostgresVariantStore{pool: pool}
}

// UpsertVariant records one processed variant.
func (s *PostgresVariantStore) UpsertVariant(ctx context.Context, variant Variant) error {
	if s == nil || s.pool == nil {
		return errors.New("variant store is unavailable")
	}
	if variant.PostID <= 0 || strings.TrimSpace(variant.Name) == "" || strings.TrimSpace(variant.ObjectKey) == "" {
		return errors.New("variant metadata is incomplete")
	}
	if variant.Width <= 0 || variant.Height <= 0 || variant.Size < 0 {
		return errors.New("variant dimensions are invalid")
	}
	status := strings.TrimSpace(variant.Status)
	if status == "" {
		status = "ready"
	}
	if status != "ready" && status != "failed" {
		return errors.New("variant status must be ready or failed")
	}
	_, err := s.pool.Exec(ctx, upsertVariantSQL,
		variant.PostID, variant.Name, variant.ObjectKey, variant.URL,
		variant.MediaType, variant.Width, variant.Height, variant.Size,
		status, variant.LastError,
	)
	return err
}
