package posts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

const maxUploadBytes int64 = 25 << 20

// CreateUpload stores one supported image under MEDIA_ROOT and creates its
// searchable post row. The temp-file and database steps are deliberately
// ordered so a partially received upload is never visible as a post.
func (s *PostgresSearcher) CreateUpload(ctx context.Context, request UploadRequest) (PostDetail, error) {
	if s == nil || s.pool == nil {
		return PostDetail{}, ErrUnavailable
	}
	if request.File == nil {
		return PostDetail{}, fmt.Errorf("%w: file is required", ErrInvalidUpload)
	}
	filename := strings.TrimSpace(filepath.Base(request.Filename))
	if filename == "" || filename == "." || strings.ContainsRune(filename, '\x00') {
		return PostDetail{}, fmt.Errorf("%w: filename is invalid", ErrInvalidUpload)
	}
	tags, err := normalizeUploadTags(request.Tags)
	if err != nil {
		return PostDetail{}, err
	}
	source := strings.TrimSpace(request.Source)
	if source == "" {
		source = filepath.ToSlash(filepath.Join("uploads", filename))
	}
	if utf8.RuneCountInString(source) > 512 || utf8.RuneCountInString(strings.TrimSpace(request.Artist)) > 512 {
		return PostDetail{}, fmt.Errorf("%w: source and artist must be at most 512 characters", ErrInvalidUpload)
	}
	artist := strings.TrimSpace(request.Artist)

	root := uploadMediaRoot()
	uploadDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return PostDetail{}, err
	}
	temp, err := os.CreateTemp(uploadDir, ".upload-*")
	if err != nil {
		return PostDetail{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(request.File, maxUploadBytes+1))
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return PostDetail{}, err
	}
	if written == 0 || written > maxUploadBytes {
		return PostDetail{}, fmt.Errorf("%w: image must be between 1 byte and 25 MiB", ErrInvalidUpload)
	}

	imageFile, err := os.Open(tempPath)
	if err != nil {
		return PostDetail{}, err
	}
	config, format, err := image.DecodeConfig(imageFile)
	closeErr := imageFile.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return PostDetail{}, fmt.Errorf("%w: file is not a supported image", ErrInvalidUpload)
	}
	mediaType, extension, ok := uploadFormat(format)
	if !ok {
		return PostDetail{}, fmt.Errorf("%w: format %q is not supported", ErrInvalidUpload, format)
	}

	digest := hex.EncodeToString(hasher.Sum(nil))
	storedName := digest + "." + extension
	store := s.store
	if store == nil {
		store = media.NewLocalStore(root)
	}
	objectFile, err := os.Open(tempPath)
	if err != nil {
		return PostDetail{}, err
	}
	object, err := store.Put(ctx, filepath.ToSlash(filepath.Join("uploads", storedName)), mediaType, objectFile)
	objectCloseErr := objectFile.Close()
	if err == nil {
		err = objectCloseErr
	}
	if err != nil {
		return PostDetail{}, err
	}
	keepObject := false
	defer func() {
		if object.Created && !keepObject {
			_ = store.Delete(context.Background(), object.Key)
		}
	}()

	mediaURL := object.URL
	searchText := strings.Join(append(append([]string{}, tags...), source, artist), " ")
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PostDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err := tx.QueryRow(ctx, insertUploadedPostSQL, mediaURL, mediaURL, mediaType, config.Width, config.Height, searchText, source, artist, "sha256:"+digest, written, tags).Scan(&id); err != nil {
		return PostDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PostDetail{}, err
	}
	keepObject = true
	return s.GetPostDetail(ctx, id)
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
			if utf8.RuneCountInString(tag) > maxTagLength {
				return nil, fmt.Errorf("%w: tags must be at most 128 characters", ErrInvalidUpload)
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
