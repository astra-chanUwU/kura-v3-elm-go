package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) CreateInvitation(ctx context.Context, actor Actor, role, tokenHash string, created, expires time.Time) (Invitation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAccountWrites(ctx, tx); err != nil {
		return Invitation{}, err
	}
	var issuerID any
	issuerActor := actor.ID
	if actor.UserID > 0 {
		var isAdmin bool
		err = tx.QueryRow(ctx, `SELECT NOT u.disabled AND EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
			WHERE ur.user_id = u.id AND r.name = 'admin') FROM users u WHERE u.id = $1 FOR UPDATE`, actor.UserID).Scan(&isAdmin)
		if err != nil || !isAdmin {
			return Invitation{}, ErrForbidden
		}
		issuerID = actor.UserID
	} else if !(actor.System || actor.ID == SystemActorID || actor.ID == LocalActorID) || !HasAdminCapability(actor) {
		return Invitation{}, ErrForbidden
	}
	var inv Invitation
	err = tx.QueryRow(ctx, `INSERT INTO account_invitations
		(token_hash, role, issuer_user_id, issuer_actor_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, role, created_at, expires_at, false, false`, tokenHash, role, issuerID, issuerActor, created, expires).
		Scan(&inv.ID, &inv.Role, &inv.CreatedAt, &inv.ExpiresAt, &inv.Revoked, &inv.Used)
	if err != nil {
		return Invitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, err
	}
	return inv, nil
}

func (s *PostgresStore) ListInvitations(ctx context.Context) ([]Invitation, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, role, created_at, expires_at,
		(revoked_at IS NOT NULL), (used_at IS NOT NULL)
		FROM account_invitations ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var inv Invitation
		if err := rows.Scan(&inv.ID, &inv.Role, &inv.CreatedAt, &inv.ExpiresAt, &inv.Revoked, &inv.Used); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *PostgresStore) RevokeInvitation(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAccountWrites(ctx, tx); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `UPDATE account_invitations SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrInvitationUnavailable
	}
	return tx.Commit(ctx)
}

func readInvitation(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tokenHash string, now time.Time, lock bool) (Invitation, int64, string, error) {
	query := `SELECT i.id, i.role, i.created_at, i.expires_at, i.revoked_at, i.used_at, i.issuer_user_id, i.issuer_actor_id
		FROM account_invitations i WHERE i.token_hash = $1`
	if lock {
		query += ` FOR UPDATE OF i`
	}
	var inv Invitation
	var revokedAt, usedAt *time.Time
	var issuerID *int64
	var issuerActor string
	err := q.QueryRow(ctx, query, tokenHash).Scan(&inv.ID, &inv.Role, &inv.CreatedAt, &inv.ExpiresAt, &revokedAt, &usedAt, &issuerID, &issuerActor)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, 0, "", ErrInvitationUnavailable
	}
	if err != nil {
		return Invitation{}, 0, "", err
	}
	inv.Revoked, inv.Used = revokedAt != nil, usedAt != nil
	if inv.Revoked || inv.Used || !inv.ExpiresAt.After(now) {
		return Invitation{}, 0, "", ErrInvitationUnavailable
	}
	if issuerID == nil {
		if issuerActor != SystemActorID && issuerActor != LocalActorID {
			return Invitation{}, 0, "", ErrInvitationUnavailable
		}
	} else {
		var activeAdmin bool
		if err := q.QueryRow(ctx, `SELECT NOT u.disabled AND EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
			WHERE ur.user_id = u.id AND r.name = 'admin') FROM users u WHERE u.id = $1`, *issuerID).Scan(&activeAdmin); err != nil || !activeAdmin {
			return Invitation{}, 0, "", ErrInvitationUnavailable
		}
	}
	issuerIDValue := int64(0)
	if issuerID != nil {
		issuerIDValue = *issuerID
	}
	return inv, issuerIDValue, issuerActor, nil
}

func (s *PostgresStore) PreviewInvitation(ctx context.Context, tokenHash string, now time.Time) (Invitation, error) {
	inv, _, _, err := readInvitation(ctx, s.pool, tokenHash, now, false)
	return inv, err
}

func (s *PostgresStore) RedeemInvitation(ctx context.Context, tokenHash, handle, passwordHash, sessionHash string, sessionExpires, now time.Time) (UserRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Keep the global lock before every row lock, matching all other account
	// writes and preventing both account-write deadlocks and issuer races.
	if err := lockAccountWrites(ctx, tx); err != nil {
		return UserRecord{}, err
	}
	inv, _, _, err := readInvitation(ctx, tx, tokenHash, now, true)
	if err != nil {
		return UserRecord{}, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE handle = $1)`, handle).Scan(&exists); err != nil {
		return UserRecord{}, err
	}
	if exists {
		return UserRecord{}, ErrDuplicateHandle
	}
	var userID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users (handle, password_hash) VALUES ($1, $2) RETURNING id`, handle, passwordHash).Scan(&userID); err != nil {
		if isUniqueViolation(err) {
			return UserRecord{}, ErrDuplicateHandle
		}
		return UserRecord{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = $2`, userID, inv.Role); err != nil {
		return UserRecord{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`, userID, sessionHash, sessionExpires); err != nil {
		return UserRecord{}, err
	}
	cmd, err := tx.Exec(ctx, `UPDATE account_invitations SET used_at = $2 WHERE token_hash = $1 AND used_at IS NULL`, tokenHash, now)
	if err != nil {
		return UserRecord{}, err
	}
	if cmd.RowsAffected() != 1 {
		return UserRecord{}, ErrInvitationUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return UserRecord{}, err
	}
	// All fields are established in this transaction. Do not perform a
	// fallible post-commit read after consuming the invitation and creating
	// the session: a read failure must not turn a successful redemption into
	// an HTTP error that invites a misleading retry.
	storedPasswordHash := passwordHash
	return UserRecord{ID: userID, Handle: handle, Role: inv.Role, PasswordHash: &storedPasswordHash}, nil
}

var _ InvitationStore = (*PostgresStore)(nil)
