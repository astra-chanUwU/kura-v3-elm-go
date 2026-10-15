package posts

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/jackc/pgx/v5"
)

const MaxExportPosts = 1000
const MaxExportBytes int64 = 250 << 20

var ErrInvalidExport = errors.New("invalid export request")
var ErrExportTooLarge = errors.New("export exceeds 1000 posts or 250 MiB; choose a smaller selection")

// ExportArchive owns a fully prepared archive. Nothing is returned to HTTP
// until all originals were copied successfully. Close removes its private file.
type ExportArchive struct {
	File *os.File
	Size int64
}

func (a *ExportArchive) Close() error {
	name := a.File.Name()
	err := a.File.Close()
	removeErr := os.Remove(name)
	if err != nil {
		return err
	}
	return removeErr
}

type ExportRequest struct {
	PostIDs         []string
	CollectionID    string
	ExpectedVersion *int64
	Metadata        bool
}
type LibraryExporter interface {
	ExportLibrary(context.Context, ExportRequest) (*ExportArchive, error)
}

func ValidateExportRequest(r ExportRequest) error {
	if (len(r.PostIDs) == 0) == (r.CollectionID == "") || len(r.PostIDs) > MaxExportPosts {
		return ErrInvalidExport
	}
	if r.CollectionID != "" {
		if _, err := collectionIDValue(r.CollectionID); err != nil {
			return ErrInvalidExport
		}
		if r.ExpectedVersion == nil || *r.ExpectedVersion < 0 {
			return ErrInvalidExport
		}
	}
	seen := map[string]bool{}
	for _, id := range r.PostIDs {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || seen[id] {
			return ErrInvalidExport
		}
		seen[id] = true
	}
	return nil
}

type exportPost struct {
	ID          string   `json:"id"`
	Filename    string   `json:"filename"`
	Tags        []string `json:"tags"`
	Source      string   `json:"source"`
	Artist      string   `json:"artist"`
	Hash        string   `json:"hash"`
	MediaType   string   `json:"media_type"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	FileSize    int64    `json:"file_size"`
	Favorite    bool     `json:"favorite"`
	Score       int      `json:"score"`
	OriginalURL string   `json:"-"`
	Key         string   `json:"-"`
}

func (s *PostgresBackend) ExportLibrary(ctx context.Context, request ExportRequest) (*ExportArchive, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	if err := ValidateExportRequest(request); err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if request.CollectionID != "" {
		var version int64
		err = tx.QueryRow(ctx, collectionVersionSQL, request.CollectionID).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if version != *request.ExpectedVersion {
			return nil, ErrCollectionChanged
		}
	}
	ids := make([]int64, 0, len(request.PostIDs))
	for _, id := range request.PostIDs {
		n, _ := strconv.ParseInt(id, 10, 64)
		ids = append(ids, n)
	}
	rows, err := tx.Query(ctx, `SELECT p.id::text,p.original_url,p.media_type,p.width,p.height,p.tags,p.source,p.artist,p.hash,p.file_size,p.favorite,p.score,
 COALESCE((SELECT j.payload->>'original_key' FROM jobs j WHERE j.kind='derivative' AND j.payload->>'post_id'=p.id::text ORDER BY j.id LIMIT 1),'')
 FROM posts p LEFT JOIN collection_posts cp ON cp.post_id=p.id AND cp.collection_id=$2::bigint
 WHERE p.deleted_at IS NULL AND p.moderation_state='published' AND
 (($2::bigint IS NULL AND p.id=ANY($1::bigint[])) OR ($2::bigint IS NOT NULL AND cp.post_id IS NOT NULL))
 ORDER BY CASE WHEN $2::bigint IS NULL THEN array_position($1::bigint[],p.id) ELSE cp.position END,p.id LIMIT $3`, ids, nullableCollectionID(request.CollectionID), MaxExportPosts+1)
	if err != nil {
		return nil, err
	}
	items := []exportPost{}
	for rows.Next() {
		var p exportPost
		if err = rows.Scan(&p.ID, &p.OriginalURL, &p.MediaType, &p.Width, &p.Height, &p.Tags, &p.Source, &p.Artist, &p.Hash, &p.FileSize, &p.Favorite, &p.Score, &p.Key); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > MaxExportPosts {
		return nil, ErrExportTooLarge
	}
	if request.CollectionID == "" && len(items) != len(ids) {
		return nil, ErrNotFound
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	reader := s.store
	if reader == nil {
		reader = media.NewStoreFromEnv(uploadMediaRoot())
	}
	file, err := os.CreateTemp("", "kura-export-*.zip")
	if err != nil {
		return nil, err
	}
	archive := &ExportArchive{File: file}
	success := false
	defer func() {
		if !success {
			archive.Close()
		}
	}()
	zw := zip.NewWriter(file)
	defer zw.Close()
	var total int64
	for i := range items {
		p := &items[i]
		if p.Key == "" && strings.HasPrefix(p.OriginalURL, "/media/") {
			p.Key = strings.TrimPrefix(p.OriginalURL, "/media/")
		}
		if p.Key == "" || strings.HasPrefix(p.Key, "/") || path.Clean(p.Key) != p.Key || strings.Contains(p.Key, "\\") || strings.HasPrefix(p.Key, "../") {
			return nil, fmt.Errorf("original media unavailable for post %s", p.ID)
		}
		extension := exportExtension(p.MediaType)
		if extension == "" {
			extension = "bin"
		}
		p.Filename = "originals/" + p.ID + "." + extension
		original, err := reader.Open(ctx, p.Key)
		if err != nil {
			return nil, fmt.Errorf("original media unavailable for post %s", p.ID)
		}
		entry, err := zw.Create(p.Filename)
		if err != nil {
			original.Close()
			return nil, err
		}
		digest := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(entry, digest), io.LimitReader(&exportContextReader{ctx: ctx, reader: original}, min(maxUploadBytes, MaxExportBytes-total)+1))
		closeErr := original.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if n > maxUploadBytes || total+n > MaxExportBytes {
			return nil, ErrExportTooLarge
		}
		if (p.FileSize > 0 && p.FileSize != n) || (p.Hash != "" && p.Hash != "sha256:"+hex.EncodeToString(digest.Sum(nil))) {
			return nil, fmt.Errorf("original media integrity check failed for post %s", p.ID)
		}
		p.FileSize = n
		p.Hash = "sha256:" + hex.EncodeToString(digest.Sum(nil))
		total += n
	}
	if request.Metadata {
		entry, err := zw.Create("metadata.json")
		if err != nil {
			return nil, err
		}
		if err = json.NewEncoder(entry).Encode(struct {
			Version int          `json:"version"`
			Posts   []exportPost `json:"posts"`
		}{1, items}); err != nil {
			return nil, err
		}
	}
	if err = zw.Close(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	archive.Size = info.Size()
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	// Recheck visibility after staging so a newly hidden/trashed original is
	// not published merely because it was public at the first query.
	exportedIDs := make([]int64, 0, len(items))
	for _, p := range items {
		n, _ := strconv.ParseInt(p.ID, 10, 64)
		exportedIDs = append(exportedIDs, n)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM posts WHERE id=ANY($1::bigint[]) AND deleted_at IS NULL AND moderation_state='published'`, exportedIDs).Scan(&count); err != nil {
		return nil, err
	}
	if count != len(items) {
		return nil, ErrNotFound
	}
	if request.CollectionID != "" {
		var v int64
		if err = s.pool.QueryRow(ctx, collectionVersionSQL, request.CollectionID).Scan(&v); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if v != *request.ExpectedVersion {
			return nil, ErrCollectionChanged
		}
	}
	success = true
	return archive, nil
}
func exportExtension(mediaType string) string {
	if extension, ok := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif", "image/webp": "webp", "image/avif": "avif", "video/mp4": "mp4", "video/webm": "webm", "image/svg+xml": "svg"}[mediaType]; ok {
		return extension
	}
	return "bin"
}

func nullableCollectionID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

type exportContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *exportContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
