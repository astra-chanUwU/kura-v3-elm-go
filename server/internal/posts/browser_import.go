package posts

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrDuplicateImport = errors.New("duplicate import")

type BrowserDuplicate struct {
	Hash   string `json:"hash"`
	PostID string `json:"post_id"`
}

type BrowserImportRequest struct {
	UploadRequest
	ExpectedHash    string
	CollectionID    string
	DuplicatePolicy string
}

type BrowserImportResult struct {
	Post            PostDetail `json:"post"`
	Duplicate       bool       `json:"duplicate"`
	CollectionID    *string    `json:"collection_id"`
	Attached        bool       `json:"attached"`
	AttachmentError string     `json:"attachment_error,omitempty"`
}

// BrowserImporter is the isolated capability used by the folder-import API.
type BrowserImporter interface {
	FindBrowserImportDuplicates(context.Context, []string) ([]BrowserDuplicate, error)
	ImportBrowser(context.Context, BrowserImportRequest) (BrowserImportResult, error)
}

var _ BrowserImporter = (*PostgresBackend)(nil)

func (s *PostgresBackend) FindBrowserImportDuplicates(ctx context.Context, hashes []string) ([]BrowserDuplicate, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	if err := validateBrowserDuplicateHashes(hashes); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT ON (p.hash) p.hash, p.id::text
FROM posts p
JOIN unnest($1::text[]) AS requested(hash) ON requested.hash = p.hash
WHERE p.deleted_at IS NULL AND p.moderation_state = 'published'
ORDER BY p.hash, p.id
`, hashes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := make([]BrowserDuplicate, 0)
	for rows.Next() {
		var match BrowserDuplicate
		if err := rows.Scan(&match.Hash, &match.PostID); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

func validateBrowserDuplicateHashes(hashes []string) error {
	if len(hashes) == 0 || len(hashes) > 100 {
		return fmt.Errorf("%w: hashes must contain 1 to 100 values", ErrInvalidUpload)
	}
	for i, hash := range hashes {
		if !validCanonicalSHA256(hash) {
			return fmt.Errorf("%w: hashes[%d] must be sha256 followed by 64 lowercase hexadecimal characters", ErrInvalidUpload, i)
		}
	}
	return nil
}

func validCanonicalSHA256(value string) bool {
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

func (s *PostgresBackend) ImportBrowser(ctx context.Context, request BrowserImportRequest) (BrowserImportResult, error) {
	if s == nil || s.pool == nil {
		return BrowserImportResult{}, ErrUnavailable
	}
	policy := strings.TrimSpace(request.DuplicatePolicy)
	if policy == "" {
		policy = "skip"
	}
	if policy != "skip" && policy != "error" {
		return BrowserImportResult{}, fmt.Errorf("%w: duplicate_policy must be skip or error", ErrInvalidUpload)
	}

	collectionID := strings.TrimSpace(request.CollectionID)
	var collectionNumeric int64
	if collectionID != "" {
		var err error
		collectionNumeric, err = collectionIDValue(collectionID)
		if err != nil {
			return BrowserImportResult{}, err
		}
		// Fail before staging or storing a large file when the requested target
		// does not exist. The transaction checks it again under the row lock.
		var exists int
		if err := s.pool.QueryRow(ctx, collectionExistsSQL, collectionNumeric).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
			return BrowserImportResult{}, ErrNotFound
		} else if err != nil {
			return BrowserImportResult{}, err
		}
	}

	staged, err := stageUpload(request.UploadRequest)
	if err != nil {
		return BrowserImportResult{}, err
	}
	defer staged.cleanup()
	hash := "sha256:" + staged.digest
	if !validCanonicalSHA256(request.ExpectedHash) || request.ExpectedHash != hash {
		return BrowserImportResult{}, fmt.Errorf("%w: expected_hash does not match uploaded file", ErrInvalidUpload)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BrowserImportResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A transaction-scoped advisory lock serializes same-content browser
	// imports. It is independent of collection/post row locks used elsewhere.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "kura:browser-import:"+staged.digest); err != nil {
		return BrowserImportResult{}, err
	}

	var existingID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM posts WHERE hash=$1 AND deleted_at IS NULL AND moderation_state='published' ORDER BY id LIMIT 1`, hash).Scan(&existingID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return BrowserImportResult{}, err
	}
	if err == nil {
		if policy == "error" {
			return BrowserImportResult{}, ErrDuplicateImport
		}
		if collectionID != "" {
			if err := attachBrowserImportTx(ctx, tx, collectionNumeric, existingID); err != nil {
				return BrowserImportResult{}, err
			}
		} else if err := lockVisiblePostForBrowserImport(ctx, tx, existingID); err != nil {
			return BrowserImportResult{}, err
		}
		post, err := s.getPostDetail(ctx, tx, existingID)
		if err != nil {
			return BrowserImportResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return BrowserImportResult{}, err
		}
		return BrowserImportResult{Post: post, Duplicate: true, CollectionID: optionalCollectionID(collectionID), Attached: collectionID != ""}, nil
	}

	// Lock collection before creating/locking the post, matching trash and
	// collection membership operations' established lock order.
	if collectionID != "" {
		var version int64
		if err := tx.QueryRow(ctx, lockCollectionSQL, collectionNumeric).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
			return BrowserImportResult{}, ErrNotFound
		} else if err != nil {
			return BrowserImportResult{}, err
		}
	}
	object, err := s.storeStagedUpload(ctx, staged)
	if err != nil {
		return BrowserImportResult{}, err
	}
	keepObject := false
	defer func() {
		if !keepObject {
			s.cleanupStagedObject(object)
		}
	}()
	id, err := s.insertStagedUpload(ctx, tx, staged, object)
	if err != nil {
		return BrowserImportResult{}, err
	}
	if collectionID != "" {
		if _, err := tx.Exec(ctx, addCollectionPostSQL, collectionNumeric, []int64{parsePostID(id)}); err != nil {
			return BrowserImportResult{}, err
		}
		if _, err := tx.Exec(ctx, bumpCollectionVersionSQL, collectionNumeric); err != nil {
			return BrowserImportResult{}, err
		}
	}
	post, err := s.getPostDetail(ctx, tx, id)
	if err != nil {
		return BrowserImportResult{}, err
	}
	if err := commitRetainingObject(&keepObject, func() error { return tx.Commit(ctx) }); err != nil {
		return BrowserImportResult{}, err
	}
	return BrowserImportResult{Post: post, Duplicate: false, CollectionID: optionalCollectionID(collectionID), Attached: collectionID != ""}, nil
}

func lockVisiblePostForBrowserImport(ctx context.Context, tx pgx.Tx, postID string) error {
	postNumeric, err := strconv.ParseInt(postID, 10, 64)
	if err != nil || postNumeric <= 0 {
		return ErrNotFound
	}
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM posts WHERE id=$1 AND deleted_at IS NULL AND moderation_state='published' FOR SHARE`, postNumeric).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

func attachBrowserImportTx(ctx context.Context, tx pgx.Tx, collectionID int64, postID string) error {
	var version int64
	if err := tx.QueryRow(ctx, lockCollectionSQL, collectionID).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	postNumeric, err := strconv.ParseInt(postID, 10, 64)
	if err != nil || postNumeric <= 0 {
		return ErrNotFound
	}
	if err := lockVisiblePostForBrowserImport(ctx, tx, postID); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, addCollectionPostSQL, collectionID, []int64{postNumeric})
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, bumpCollectionVersionSQL, collectionID); err != nil {
			return err
		}
	}
	return nil
}

func optionalCollectionID(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
