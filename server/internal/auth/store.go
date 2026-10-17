package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRecord is the stored account row. PasswordHash is nil for legacy
// users without a login; it must never be serialized to clients.
type UserRecord struct {
	ID           int64
	Handle       string
	Role         string
	Disabled     bool
	PasswordHash *string
}

// Public projects the record to the client-safe User shape.
func (r UserRecord) Public() User {
	return User{ID: r.ID, Handle: r.Handle, Role: r.Role, Disabled: r.Disabled}
}

// SessionRecord is one stored session row (hash only, never the raw token).
type SessionRecord struct {
	UserID    int64
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Store is the persistence contract behind Service. PostgresStore is the
// production implementation over the backend's shared pool; tests use an
// in-memory fake with the same semantics.
type Store interface {
	GetUserByHandle(ctx context.Context, handle string) (UserRecord, error)
	GetUserByID(ctx context.Context, id int64) (UserRecord, error)
	CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	GetSession(ctx context.Context, tokenHash string) (SessionRecord, error)
	RevokeSession(ctx context.Context, tokenHash string) error
	RevokeAllSessions(ctx context.Context, userID int64) error
	ListUsers(ctx context.Context) ([]UserRecord, error)
	CreateUser(ctx context.Context, handle, passwordHash, role string) (UserRecord, error)
	// UpdateUser applies the patch atomically, enforcing self-action and
	// last-admin protection inside the transaction. actorID is the acting
	// admin's user id (0 for legacy/deployment actors, which are never the
	// target). Any applied change revokes the target's sessions.
	UpdateUser(ctx context.Context, actorID, id int64, role *string, disabled *bool, passwordHash *string) (UserRecord, error)
	// BootstrapAdmin sets the initial login admin under the shared
	// account-write advisory lock, refusing when an enabled admin with
	// a password already exists.
	BootstrapAdmin(ctx context.Context, handle, passwordHash, adoptLegacy string) (UserRecord, error)
	// ReplacePassword sets the password hash of an existing login account
	// and revokes that account's sessions in the same transaction. It does
	// not create accounts, change roles, or change the disabled flag.
	// Accounts without a login password are refused.
	ReplacePassword(ctx context.Context, id int64, passwordHash string) error
}

var errStoreNotFound = errors.New("not found")

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// PostgresStore implements Store over the shared pgx pool. Construct it
// with NewPostgresStore(pool) using the posts backend's pool so the whole
// server keeps one pool per process.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore builds the account store over an existing pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const userSelect = `
SELECT u.id, u.handle, u.disabled, u.password_hash,
       COALESCE((SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id
                 WHERE ur.user_id = u.id
                 ORDER BY CASE r.name WHEN 'admin' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END
                 LIMIT 1), 'viewer')
FROM users u`

func scanUser(row pgx.Row) (UserRecord, error) {
	var rec UserRecord
	if err := row.Scan(&rec.ID, &rec.Handle, &rec.Disabled, &rec.PasswordHash, &rec.Role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserRecord{}, errStoreNotFound
		}
		return UserRecord{}, err
	}
	return rec, nil
}

func (s *PostgresStore) GetUserByHandle(ctx context.Context, handle string) (UserRecord, error) {
	if s == nil || s.pool == nil {
		return UserRecord{}, errStoreNotFound
	}
	return scanUser(s.pool.QueryRow(ctx, userSelect+" WHERE u.handle = $1", handle))
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id int64) (UserRecord, error) {
	if s == nil || s.pool == nil || id <= 0 {
		return UserRecord{}, errStoreNotFound
	}
	return scanUser(s.pool.QueryRow(ctx, userSelect+" WHERE u.id = $1", id))
}

func (s *PostgresStore) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt)
	return err
}

func (s *PostgresStore) GetSession(ctx context.Context, tokenHash string) (SessionRecord, error) {
	var rec SessionRecord
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, expires_at, revoked_at FROM sessions WHERE token_hash = $1`, tokenHash).
		Scan(&rec.UserID, &rec.ExpiresAt, &rec.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, errStoreNotFound
	}
	return rec, err
}

func (s *PostgresStore) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	return err
}

func (s *PostgresStore) RevokeAllSessions(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]UserRecord, error) {
	rows, err := s.pool.Query(ctx, userSelect+" ORDER BY u.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserRecord{}
	for rows.Next() {
		rec, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// accountWriteLockKey is the single advisory-lock key serializing every
// account write (create, update, bootstrap). Account writes are rare, so
// one lock is cheap; it makes the last-admin count check and the write
// atomic against every other account change, and a single lock taken
// before any row lock keeps lock ordering consistent (no deadlock cycle).
const accountWriteLockKey = "kura-account-writes"

func lockAccountWrites(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, accountWriteLockKey)
	return err
}

func (s *PostgresStore) CreateUser(ctx context.Context, handle, passwordHash, role string) (UserRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAccountWrites(ctx, tx); err != nil {
		return UserRecord{}, err
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO users (handle, password_hash) VALUES ($1, $2) RETURNING id`, handle, passwordHash).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return UserRecord{}, ErrDuplicateHandle
		}
		return UserRecord{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = $2`, id, role); err != nil {
		return UserRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			return UserRecord{}, ErrDuplicateHandle
		}
		return UserRecord{}, err
	}
	return s.GetUserByID(ctx, id)
}

// guardAdminLoss enforces, inside tx, the self-action and last-admin
// rules for disabling or demoting target. Callers must already hold the
// account-write advisory lock (see lockAccountWrites) so the count below
// cannot race another demote/disable: under READ COMMITTED the count
// statement runs after the lock is granted and therefore sees every
// previously committed account change.
func guardAdminLoss(ctx context.Context, tx pgx.Tx, actorID int64, target UserRecord, newRole *string, disabled *bool) error {
	demoting := newRole != nil && target.Role == string(RoleAdmin) && *newRole != string(RoleAdmin)
	disabling := disabled != nil && *disabled && !target.Disabled
	if !demoting && !disabling {
		return nil
	}
	if actorID != 0 && actorID == target.ID {
		return ErrSelfAction
	}
	if target.Role != string(RoleAdmin) || target.Disabled {
		return nil
	}
	var admins int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r ON r.id = ur.role_id AND r.name = 'admin'
		WHERE NOT u.disabled AND u.password_hash IS NOT NULL`).Scan(&admins); err != nil {
		return err
	}
	if admins <= 1 {
		return ErrLastAdmin
	}
	return nil
}

func (s *PostgresStore) UpdateUser(ctx context.Context, actorID, id int64, role *string, disabled *bool, passwordHash *string) (UserRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize the whole patch (role, disable, and password alike)
	// against every other account write BEFORE taking the target row
	// lock; this fixed ordering is what prevents the two-demote race.
	if err := lockAccountWrites(ctx, tx); err != nil {
		return UserRecord{}, err
	}
	target, err := scanUser(tx.QueryRow(ctx, userSelect+" WHERE u.id = $1 FOR UPDATE OF u", id))
	if err != nil {
		if errors.Is(err, errStoreNotFound) {
			return UserRecord{}, ErrUserNotFound
		}
		return UserRecord{}, err
	}
	// Reserved legacy rows stay immutable until bootstrap adoption gives
	// them a login password; an adopted local/system admin is a real
	// account and must stay manageable (password reset, demotion, or
	// disable, still subject to the self/last-admin guards below).
	if isReservedHandle(target.Handle) && (target.PasswordHash == nil || *target.PasswordHash == "") {
		return UserRecord{}, ErrReservedHandle
	}
	if err := guardAdminLoss(ctx, tx, actorID, target, role, disabled); err != nil {
		return UserRecord{}, err
	}
	if passwordHash != nil {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, *passwordHash); err != nil {
			return UserRecord{}, err
		}
	}
	if disabled != nil {
		if _, err := tx.Exec(ctx, `UPDATE users SET disabled = $2, updated_at = now() WHERE id = $1`, id, *disabled); err != nil {
			return UserRecord{}, err
		}
	}
	if role != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, id); err != nil {
			return UserRecord{}, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = $2`, id, *role); err != nil {
			return UserRecord{}, err
		}
	}
	// Every applied change revokes the target's live sessions: a password
	// change, disable, or role change must not leave old cookies valid.
	if _, err := tx.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return UserRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserRecord{}, err
	}
	return s.GetUserByID(ctx, id)
}

func (s *PostgresStore) BootstrapAdmin(ctx context.Context, handle, passwordHash, adoptLegacy string) (UserRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize bootstrap against every other account write under the
	// shared lock; the existence check and the write must be atomic so
	// two CLIs cannot both create the initial admin, and bootstrap
	// cannot race an in-flight demote/disable.
	if err := lockAccountWrites(ctx, tx); err != nil {
		return UserRecord{}, err
	}
	var existing int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r ON r.id = ur.role_id AND r.name = 'admin'
		WHERE u.password_hash IS NOT NULL AND NOT u.disabled`).Scan(&existing); err != nil {
		return UserRecord{}, err
	}
	if existing > 0 {
		return UserRecord{}, ErrBootstrapExists
	}
	var id int64
	if adoptLegacy != "" {
		err = tx.QueryRow(ctx,
			`UPDATE users SET password_hash = $2, disabled = false, updated_at = now()
			 WHERE handle = $1 RETURNING id`, adoptLegacy, passwordHash).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return UserRecord{}, fmt.Errorf("legacy user %q not found; run migration 008 first", adoptLegacy)
		}
		if err != nil {
			return UserRecord{}, err
		}
	} else {
		err = tx.QueryRow(ctx,
			`INSERT INTO users (handle, password_hash) VALUES ($1, $2) RETURNING id`, handle, passwordHash).Scan(&id)
		if err != nil {
			if isUniqueViolation(err) {
				return UserRecord{}, ErrDuplicateHandle
			}
			return UserRecord{}, err
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = 'admin'
		 ON CONFLICT DO NOTHING`, id); err != nil {
		return UserRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserRecord{}, err
	}
	return s.GetUserByID(ctx, id)
}

// ReplacePassword updates one existing login password and revokes that
// account's sessions atomically. Role and disabled state are untouched,
// and a row without a password hash is not given a login here.
func (s *PostgresStore) ReplacePassword(ctx context.Context, id int64, passwordHash string) error {
	if s == nil || s.pool == nil || id <= 0 {
		return ErrUserNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAccountWrites(ctx, tx); err != nil {
		return err
	}
	target, err := scanUser(tx.QueryRow(ctx, userSelect+" WHERE u.id = $1 FOR UPDATE OF u", id))
	if err != nil {
		if errors.Is(err, errStoreNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if target.PasswordHash == nil || *target.PasswordHash == "" {
		if isReservedHandle(target.Handle) {
			return ErrReservedHandle
		}
		return ErrNoLogin
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, passwordHash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Compile-time check that PostgresStore satisfies Store.
var _ Store = (*PostgresStore)(nil)

// NormalizeStoreError maps store-internal not-found to ErrUserNotFound for
// callers that distinguish it. Login deliberately does not use this.
func NormalizeStoreError(err error) error {
	if errors.Is(err, errStoreNotFound) {
		return ErrUserNotFound
	}
	return err
}
