package profiles

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const accountWriteLockKey = "kura-account-writes"

type Record struct {
	UserID      int64
	Handle      string
	Username    string
	DisplayName string
	Bio         string
	Website     string
	AvatarKey   string
	Version     int64
	Disabled    bool
}

type Store interface {
	GetByUsername(ctx context.Context, username string) (Record, error)
	GetByUserID(ctx context.Context, userID int64) (Record, error)
	Update(ctx context.Context, userID int64, input Input) (Record, error)
	SetAvatar(ctx context.Context, userID, expectedVersion int64, key string) (Record, string, error)
	Username(ctx context.Context, userID int64) (string, bool, error)
}

type VersionConflict struct {
	Current Record
}

func (e *VersionConflict) Error() string { return ErrConflict.Error() }
func (e *VersionConflict) Unwrap() error { return ErrConflict }

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const profileSelect = `SELECT id, handle, username, display_name, bio, website, avatar_key, profile_version, disabled FROM users`

func (s *PostgresStore) GetByUsername(ctx context.Context, username string) (Record, error) {
	if s == nil || s.pool == nil {
		return Record{}, ErrUnavailable
	}
	return s.get(ctx, profileSelect+` WHERE username = $1`, username)
}

func (s *PostgresStore) GetByUserID(ctx context.Context, userID int64) (Record, error) {
	if s == nil || s.pool == nil {
		return Record{}, ErrUnavailable
	}
	return s.get(ctx, profileSelect+` WHERE id = $1`, userID)
}

func (s *PostgresStore) get(ctx context.Context, query string, arg any) (Record, error) {
	row := s.pool.QueryRow(ctx, query, arg)
	rec, err := scanRecord(row, false)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && rec.Disabled) {
		return Record{}, ErrNotFound
	}
	return rec, err
}

func (s *PostgresStore) Username(ctx context.Context, userID int64) (string, bool, error) {
	rec, err := s.GetByUserID(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if rec.Username == "" {
		return "", false, nil
	}
	return rec.Username, true, nil
}

func (s *PostgresStore) Update(ctx context.Context, userID int64, input Input) (Record, error) {
	if s == nil || s.pool == nil {
		return Record{}, ErrUnavailable
	}
	normalized, err := NormalizeProfile(input)
	if err != nil {
		return Record{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, accountWriteLockKey); err != nil {
		return Record{}, err
	}
	rec, err := scanRecord(tx.QueryRow(ctx, `
UPDATE users
SET username = $1,
    display_name = NULLIF($2, ''),
    bio = NULLIF($3, ''),
    website = NULLIF($4, ''),
    profile_version = profile_version + 1,
    updated_at = now()
WHERE id = $5 AND profile_version = $6 AND NOT disabled
RETURNING id, handle, username, display_name, bio, website, avatar_key, profile_version, disabled
`, normalized.Username, normalized.DisplayName, normalized.Bio, normalized.Website, userID, normalized.ExpectedVersion), false)
	if errors.Is(err, pgx.ErrNoRows) {
		current, readErr := readConflict(ctx, tx, userID)
		if readErr != nil {
			return Record{}, readErr
		}
		return Record{}, &VersionConflict{Current: current}
	}
	if isUsernameUnique(err) {
		return Record{}, ErrUsernameTaken
	}
	if err != nil {
		return Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		if isUsernameUnique(err) {
			return Record{}, ErrUsernameTaken
		}
		return Record{}, err
	}
	return rec, nil
}

func (s *PostgresStore) SetAvatar(ctx context.Context, userID, expectedVersion int64, key string) (Record, string, error) {
	if s == nil || s.pool == nil {
		return Record{}, "", ErrUnavailable
	}
	if expectedVersion < 1 {
		return Record{}, "", ErrInvalidProfile
	}
	trimmed := strings.TrimSpace(key)
	if key != trimmed || strings.Contains(trimmed, "..") || strings.Contains(trimmed, "\\") || strings.HasPrefix(trimmed, "/") {
		return Record{}, "", ErrAvatar
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Record{}, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previous *string
	var rec Record
	var username, displayName, bio, website, avatarKey *string
	err = tx.QueryRow(ctx, `
UPDATE users AS u
SET avatar_key = NULLIF($1, ''),
    profile_version = u.profile_version + 1,
    updated_at = now()
FROM (
    SELECT id, avatar_key
    FROM users
    WHERE id = $2 AND profile_version = $3 AND NOT disabled
    FOR UPDATE
) AS prev
WHERE u.id = prev.id
RETURNING u.id, u.handle, u.username, u.display_name, u.bio, u.website, u.avatar_key, u.profile_version, u.disabled, prev.avatar_key
`, trimmed, userID, expectedVersion).Scan(
		&rec.UserID, &rec.Handle, &username, &displayName, &bio, &website, &avatarKey, &rec.Version, &rec.Disabled, &previous,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		current, readErr := readConflict(ctx, tx, userID)
		if readErr != nil {
			return Record{}, "", readErr
		}
		return Record{}, "", &VersionConflict{Current: current}
	}
	if err != nil {
		return Record{}, "", err
	}
	rec.Username = deref(username)
	rec.DisplayName = deref(displayName)
	rec.Bio = deref(bio)
	rec.Website = deref(website)
	rec.AvatarKey = deref(avatarKey)
	if err := tx.Commit(ctx); err != nil {
		return Record{}, "", err
	}
	return rec, deref(previous), nil
}

func readConflict(ctx context.Context, tx pgx.Tx, userID int64) (Record, error) {
	rec, err := scanRecord(tx.QueryRow(ctx, profileSelect+` WHERE id = $1`, userID), false)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && rec.Disabled) {
		return Record{}, ErrNotFound
	}
	return rec, err
}

func scanRecord(row pgx.Row, _ bool) (Record, error) {
	var rec Record
	var username, displayName, bio, website, avatarKey *string
	err := row.Scan(&rec.UserID, &rec.Handle, &username, &displayName, &bio, &website, &avatarKey, &rec.Version, &rec.Disabled)
	if err != nil {
		return Record{}, err
	}
	rec.Username = deref(username)
	rec.DisplayName = deref(displayName)
	rec.Bio = deref(bio)
	rec.Website = deref(website)
	rec.AvatarKey = deref(avatarKey)
	return rec, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isUsernameUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
