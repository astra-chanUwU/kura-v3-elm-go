package posts

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	maxSavedSearchNameLength  = 120
	maxSavedSearchQueryLength = 256
)

// SavedSearch is an owner-scoped, server-persisted KuraQL query.
type SavedSearch struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateSavedSearchRequest struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

// UpdateSavedSearchRequest uses pointers so PATCH can distinguish an omitted
// field from an intentional empty value.
type UpdateSavedSearchRequest struct {
	Name  *string `json:"name"`
	Query *string `json:"query"`
}

type SavedSearchStore interface {
	ListSavedSearches(context.Context, string) ([]SavedSearch, error)
	CreateSavedSearch(context.Context, string, CreateSavedSearchRequest) (SavedSearch, error)
	UpdateSavedSearch(context.Context, string, string, UpdateSavedSearchRequest) (SavedSearch, error)
	DeleteSavedSearch(context.Context, string, string) error
}

func ValidateCreateSavedSearch(request CreateSavedSearchRequest) (CreateSavedSearchRequest, error) {
	name, query, err := validateSavedSearchValues(request.Name, request.Query)
	if err != nil {
		return CreateSavedSearchRequest{}, err
	}
	return CreateSavedSearchRequest{Name: name, Query: query}, nil
}

func ValidateUpdateSavedSearch(request UpdateSavedSearchRequest) (UpdateSavedSearchRequest, error) {
	if request.Name == nil && request.Query == nil {
		return UpdateSavedSearchRequest{}, fmt.Errorf("%w: at least one field is required", ErrInvalidSavedSearch)
	}
	result := request
	if request.Name != nil {
		name := strings.TrimSpace(*request.Name)
		if name == "" || len([]rune(name)) > maxSavedSearchNameLength {
			return UpdateSavedSearchRequest{}, fmt.Errorf("%w: name must contain 1 to %d characters", ErrInvalidSavedSearch, maxSavedSearchNameLength)
		}
		result.Name = &name
	}
	if request.Query != nil {
		query := strings.TrimSpace(*request.Query)
		if len([]rune(query)) > maxSavedSearchQueryLength {
			return UpdateSavedSearchRequest{}, fmt.Errorf("%w: query must contain at most %d characters", ErrInvalidSavedSearch, maxSavedSearchQueryLength)
		}
		if _, err := ParseQuery(query); err != nil {
			return UpdateSavedSearchRequest{}, fmt.Errorf("%w: invalid KuraQL query", ErrInvalidSavedSearch)
		}
		result.Query = &query
	}
	return result, nil
}

func validateSavedSearchValues(rawName, rawQuery string) (string, string, error) {
	name := strings.TrimSpace(rawName)
	query := strings.TrimSpace(rawQuery)
	if name == "" || len([]rune(name)) > maxSavedSearchNameLength {
		return "", "", fmt.Errorf("%w: name must contain 1 to %d characters", ErrInvalidSavedSearch, maxSavedSearchNameLength)
	}
	if len([]rune(query)) > maxSavedSearchQueryLength {
		return "", "", fmt.Errorf("%w: query must contain at most %d characters", ErrInvalidSavedSearch, maxSavedSearchQueryLength)
	}
	if _, err := ParseQuery(query); err != nil {
		return "", "", fmt.Errorf("%w: invalid KuraQL query", ErrInvalidSavedSearch)
	}
	return name, query, nil
}

func (s *PostgresSearcher) ListSavedSearches(ctx context.Context, owner string) ([]SavedSearch, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(owner) == "" {
		return nil, ErrInvalidSavedSearch
	}
	rows, err := s.pool.Query(ctx, listSavedSearchesSQL, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]SavedSearch, 0)
	for rows.Next() {
		var saved SavedSearch
		if err := rows.Scan(&saved.ID, &saved.Name, &saved.Query, &saved.CreatedAt, &saved.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, saved)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PostgresSearcher) CreateSavedSearch(ctx context.Context, owner string, request CreateSavedSearchRequest) (SavedSearch, error) {
	if s == nil || s.pool == nil {
		return SavedSearch{}, ErrUnavailable
	}
	if strings.TrimSpace(owner) == "" {
		return SavedSearch{}, ErrInvalidSavedSearch
	}
	validated, err := ValidateCreateSavedSearch(request)
	if err != nil {
		return SavedSearch{}, err
	}
	var result SavedSearch
	err = s.pool.QueryRow(ctx, insertSavedSearchSQL, owner, validated.Name, validated.Query).Scan(&result.ID, &result.Name, &result.Query, &result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		return SavedSearch{}, err
	}
	return result, nil
}

func (s *PostgresSearcher) UpdateSavedSearch(ctx context.Context, owner, id string, request UpdateSavedSearchRequest) (SavedSearch, error) {
	if s == nil || s.pool == nil {
		return SavedSearch{}, ErrUnavailable
	}
	if strings.TrimSpace(owner) == "" {
		return SavedSearch{}, ErrInvalidSavedSearch
	}
	parsedID, err := savedSearchIDValue(id)
	if err != nil {
		return SavedSearch{}, err
	}
	validated, err := ValidateUpdateSavedSearch(request)
	if err != nil {
		return SavedSearch{}, err
	}
	var name, query any
	if validated.Name != nil {
		name = *validated.Name
	}
	if validated.Query != nil {
		query = *validated.Query
	}
	var result SavedSearch
	err = s.pool.QueryRow(ctx, updateSavedSearchSQL, owner, parsedID, name, query).Scan(&result.ID, &result.Name, &result.Query, &result.CreatedAt, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SavedSearch{}, ErrNotFound
	}
	if err != nil {
		return SavedSearch{}, err
	}
	return result, nil
}

func (s *PostgresSearcher) DeleteSavedSearch(ctx context.Context, owner, id string) error {
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(owner) == "" {
		return ErrInvalidSavedSearch
	}
	parsedID, err := savedSearchIDValue(id)
	if err != nil {
		return err
	}
	command, err := s.pool.Exec(ctx, deleteSavedSearchSQL, owner, parsedID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func savedSearchIDValue(raw string) (int64, error) {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
		return 0, ErrInvalidSavedSearch
	}
	return parsed, nil
}
