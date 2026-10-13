package posts

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// CollectionManageRequest carries the observed version for an administrative
// collection rename or delete. Version zero is valid for a newly created row.
type CollectionManageRequest struct {
	Name            string `json:"name,omitempty"`
	ExpectedVersion int64  `json:"expected_version"`
}

// CollectionManageResult is the compact acknowledgement for a rename/delete.
// Delete includes the removed collection's last name and version.
type CollectionManageResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
	Changed bool   `json:"changed"`
}

// CollectionManager is an independent capability so legacy CollectionMutator
// implementations remain compatible.
type CollectionManager interface {
	RenameCollection(context.Context, string, CollectionManageRequest) (CollectionManageResult, error)
	DeleteCollection(context.Context, string, int64) (CollectionManageResult, error)
}

func ValidateCollectionManageRequest(request CollectionManageRequest, rename bool) (CollectionManageRequest, error) {
	if request.ExpectedVersion < 0 {
		return CollectionManageRequest{}, fmt.Errorf("%w: expected_version must be non-negative", ErrInvalidCollections)
	}
	if rename {
		name := strings.TrimSpace(request.Name)
		if name == "" || !utf8.ValidString(name) || len([]rune(name)) > maxCollectionNameLength {
			return CollectionManageRequest{}, fmt.Errorf("%w: name must contain 1 to %d characters", ErrInvalidCollections, maxCollectionNameLength)
		}
		request.Name = name
	}
	return request, nil
}

const lockManagedCollectionSQL = `SELECT name, version FROM collections WHERE id = $1::bigint FOR UPDATE`
const renameManagedCollectionSQL = `UPDATE collections SET name = $2, version = version + 1 WHERE id = $1::bigint RETURNING name, version`
const deleteManagedCollectionSQL = `DELETE FROM collections WHERE id = $1::bigint`

func (s *PostgresBackend) RenameCollection(ctx context.Context, rawID string, request CollectionManageRequest) (CollectionManageResult, error) {
	if s == nil || s.pool == nil {
		return CollectionManageResult{}, ErrUnavailable
	}
	validated, err := ValidateCollectionManageRequest(request, true)
	if err != nil {
		return CollectionManageResult{}, err
	}
	id, err := collectionIDValue(rawID)
	if err != nil {
		return CollectionManageResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CollectionManageResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var name string
	var version int64
	if err := tx.QueryRow(ctx, lockManagedCollectionSQL, id).Scan(&name, &version); errors.Is(err, pgx.ErrNoRows) {
		return CollectionManageResult{}, ErrNotFound
	} else if err != nil {
		return CollectionManageResult{}, err
	}
	if version != validated.ExpectedVersion {
		return CollectionManageResult{}, ErrCollectionChanged
	}
	result := CollectionManageResult{ID: strconv.FormatInt(id, 10), Name: name, Version: version}
	if name != validated.Name {
		if err := tx.QueryRow(ctx, renameManagedCollectionSQL, id, validated.Name).Scan(&result.Name, &result.Version); err != nil {
			return CollectionManageResult{}, err
		}
		result.Changed = true
	}
	if err := tx.Commit(ctx); err != nil {
		return CollectionManageResult{}, err
	}
	return result, nil
}

func (s *PostgresBackend) DeleteCollection(ctx context.Context, rawID string, expectedVersion int64) (CollectionManageResult, error) {
	if s == nil || s.pool == nil {
		return CollectionManageResult{}, ErrUnavailable
	}
	if expectedVersion < 0 {
		return CollectionManageResult{}, fmt.Errorf("%w: expected_version must be non-negative", ErrInvalidCollections)
	}
	id, err := collectionIDValue(rawID)
	if err != nil {
		return CollectionManageResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CollectionManageResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var name string
	var version int64
	if err := tx.QueryRow(ctx, lockManagedCollectionSQL, id).Scan(&name, &version); errors.Is(err, pgx.ErrNoRows) {
		return CollectionManageResult{}, ErrNotFound
	} else if err != nil {
		return CollectionManageResult{}, err
	}
	if version != expectedVersion {
		return CollectionManageResult{}, ErrCollectionChanged
	}
	tag, err := tx.Exec(ctx, deleteManagedCollectionSQL, id)
	if err != nil {
		return CollectionManageResult{}, err
	}
	if tag.RowsAffected() != 1 {
		return CollectionManageResult{}, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return CollectionManageResult{}, err
	}
	return CollectionManageResult{ID: strconv.FormatInt(id, 10), Name: name, Version: version, Changed: true}, nil
}
