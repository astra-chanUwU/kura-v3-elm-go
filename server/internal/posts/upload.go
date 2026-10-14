package posts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	_ "golang.org/x/image/webp"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/jackc/pgx/v5"
)

const maxUploadBytes int64 = 25 << 20

// CreateUpload stores one supported image under MEDIA_ROOT and creates its
// searchable post row plus the default thumb-320 derivative job in one
// transaction. The temp-file and database steps are deliberately ordered
// so a partially received upload is never visible as a post.
func (s *PostgresBackend) CreateUpload(ctx context.Context, request UploadRequest) (PostDetail, error) {
	if s == nil || s.pool == nil {
		return PostDetail{}, ErrUnavailable
	}
	staged, err := stageUpload(request)
	if err != nil {
		return PostDetail{}, err
	}
	defer staged.cleanup()
	result, err := s.persistStagedUpload(ctx, staged)
	if err != nil {
		return PostDetail{}, err
	}
	return result.post, nil
}

type stagedUpload struct {
	tempPath  string
	filename  string
	digest    string
	mediaType string
	extension string
	width     int
	height    int
	fileSize  int64
	tags      []string
	source    string
	artist    string
	search    string
}

func (u *stagedUpload) cleanup() {
	if u != nil && u.tempPath != "" {
		_ = os.Remove(u.tempPath)
	}
}

// stageUpload performs the common bounded upload validation and writes an
// immutable temporary copy. Callers can then coordinate persistence without
// re-reading a multipart body or duplicating image validation.
func stageUpload(request UploadRequest) (*stagedUpload, error) {
	if request.File == nil {
		return nil, fmt.Errorf("%w: file is required", ErrInvalidUpload)
	}
	filename := strings.TrimSpace(filepath.Base(request.Filename))
	if filename == "" || filename == "." || strings.ContainsRune(filename, '\x00') {
		return nil, fmt.Errorf("%w: filename is invalid", ErrInvalidUpload)
	}
	tags, err := normalizeUploadTags(request.Tags)
	if err != nil {
		return nil, err
	}
	source := strings.TrimSpace(request.Source)
	if source == "" {
		source = filepath.ToSlash(filepath.Join("uploads", filename))
	}
	if utf8.RuneCountInString(source) > 512 || utf8.RuneCountInString(strings.TrimSpace(request.Artist)) > 512 {
		return nil, fmt.Errorf("%w: source and artist must be at most 512 characters", ErrInvalidUpload)
	}
	artist := strings.TrimSpace(request.Artist)

	root := uploadMediaRoot()
	uploadDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(uploadDir, ".upload-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(request.File, maxUploadBytes+1))
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if written == 0 || written > maxUploadBytes {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("%w: image must be between 1 byte and 25 MiB", ErrInvalidUpload)
	}

	imageFile, err := os.Open(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	config, format, err := image.DecodeConfig(imageFile)
	closeErr := imageFile.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("%w: file is not a supported image", ErrInvalidUpload)
	}
	mediaType, extension, ok := uploadFormat(format)
	if !ok {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("%w: format %q is not supported", ErrInvalidUpload, format)
	}
	if format == "webp" {
		data, err := os.ReadFile(tempPath)
		if err != nil {
			_ = os.Remove(tempPath)
			return nil, err
		}
		if int64(len(data)) != written {
			_ = os.Remove(tempPath)
			return nil, fmt.Errorf("%w: file is not a supported image", ErrInvalidUpload)
		}
		webpConfig, err := media.ValidateStaticWebP(data)
		if err != nil {
			_ = os.Remove(tempPath)
			if errors.Is(err, media.ErrAnimatedWebP) {
				return nil, fmt.Errorf("%w: animated WebP is not supported", ErrInvalidUpload)
			}
			return nil, fmt.Errorf("%w: file is not a supported image", ErrInvalidUpload)
		}
		config = webpConfig
	}

	digest := hex.EncodeToString(hasher.Sum(nil))
	searchText := strings.Join(append(append([]string{}, tags...), source, artist), " ")
	keepTemp = true
	return &stagedUpload{tempPath: tempPath, filename: filename, digest: digest, mediaType: mediaType, extension: extension, width: config.Width, height: config.Height, fileSize: written, tags: tags, source: source, artist: artist, search: searchText}, nil
}

type persistedUpload struct {
	id   string
	post PostDetail
}

func (s *PostgresBackend) persistStagedUpload(ctx context.Context, staged *stagedUpload) (persistedUpload, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return persistedUpload{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockTaxonomyTx(ctx, tx); err != nil {
		return persistedUpload{}, err
	}
	redirects, err := loadRedirects(ctx, tx)
	if err != nil {
		return persistedUpload{}, err
	}
	staged.tags = canonicalizeTags(staged.tags, redirects)
	staged.search = canonicalSearchText(staged.tags, staged.source, staged.artist)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "kura:browser-import:"+staged.digest); err != nil {
		return persistedUpload{}, err
	}
	object, err := s.storeStagedUpload(ctx, staged)
	if err != nil {
		return persistedUpload{}, err
	}
	keepObject := false
	defer func() {
		if !keepObject {
			s.cleanupStagedObject(object)
		}
	}()
	id, err := s.insertStagedUpload(ctx, tx, staged, object)
	if err != nil {
		return persistedUpload{}, err
	}
	if len(staged.tags) > 0 {
		if err := bumpTaxonomyTx(ctx, tx); err != nil {
			return persistedUpload{}, err
		}
	}
	post, err := s.getPostDetail(ctx, tx, id)
	if err != nil {
		return persistedUpload{}, err
	}
	if err := commitRetainingObject(&keepObject, func() error { return tx.Commit(ctx) }); err != nil {
		return persistedUpload{}, err
	}
	return persistedUpload{id: id, post: post}, nil
}

// commitRetainingObject applies the conservative cleanup policy for an
// ambiguous commit: once Commit is attempted, the database may have made the
// row durable even if the client receives an error, so its media object stays.
func commitRetainingObject(retained *bool, commit func() error) error {
	*retained = true
	return commit()
}

type stagedMediaObject struct {
	key     string
	url     string
	created bool
	store   media.Store
}

func (s *PostgresBackend) storeStagedUpload(ctx context.Context, staged *stagedUpload) (stagedMediaObject, error) {
	store := s.store
	if store == nil {
		store = media.NewStoreFromEnv(uploadMediaRoot())
	}
	objectFile, err := os.Open(staged.tempPath)
	if err != nil {
		return stagedMediaObject{}, err
	}
	object, err := store.Put(ctx, filepath.ToSlash(filepath.Join("uploads", staged.digest+"."+staged.extension)), staged.mediaType, objectFile)
	closeErr := objectFile.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return stagedMediaObject{}, err
	}
	return stagedMediaObject{key: object.Key, url: object.URL, created: object.Created, store: store}, nil
}

func (s *PostgresBackend) cleanupStagedObject(object stagedMediaObject) {
	if object.created && object.store != nil {
		_ = object.store.Delete(context.Background(), object.key)
	}
}

func (s *PostgresBackend) insertStagedUpload(ctx context.Context, tx pgx.Tx, staged *stagedUpload, object stagedMediaObject) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, insertUploadedPostSQL, object.url, object.url, staged.mediaType, staged.width, staged.height, staged.search, staged.source, staged.artist, "sha256:"+staged.digest, staged.fileSize, staged.tags).Scan(&id); err != nil {
		return "", err
	}
	derivativeRequest, err := media.EnqueueDerivative(id, object.key, media.DefaultDerivativeVariant)
	if err != nil {
		return "", err
	}
	if _, err := jobs.EnqueueInTx(ctx, tx, derivativeRequest); err != nil {
		return "", err
	}
	return id, nil
}

func normalizeUploadTags(tags []string) ([]string, error) {
	if len(tags) > maxTagEditTags {
		return nil, fmt.Errorf("%w: at most 100 tags may be supplied", ErrInvalidUpload)
	}
	clean := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		for _, part := range strings.Split(raw, ",") {
			tag := strings.TrimSpace(part)
			if tag == "" {
				continue
			}
			if !utf8.ValidString(tag) || strings.ContainsRune(tag, '\x00') || utf8.RuneCountInString(tag) > maxTagLength {
				return nil, fmt.Errorf("%w: tags must be valid UTF-8 without NUL and at most 128 characters", ErrInvalidUpload)
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			if len(clean) == maxTagEditTags {
				return nil, fmt.Errorf("%w: at most 100 tags may be supplied", ErrInvalidUpload)
			}
			seen[tag] = struct{}{}
			clean = append(clean, tag)
		}
	}
	return clean, nil
}

func uploadFormat(format string) (mediaType, extension string, ok bool) {
	switch strings.ToLower(format) {
	case "jpeg":
		return "image/jpeg", "jpg", true
	case "png":
		return "image/png", "png", true
	case "gif":
		return "image/gif", "gif", true
	case "webp":
		return "image/webp", "webp", true
	default:
		return "", "", false
	}
}

func uploadMediaRoot() string {
	if configured := strings.TrimSpace(os.Getenv("MEDIA_ROOT")); configured != "" {
		return configured
	}
	return filepath.Join("..", "web", "static", "media")
}
