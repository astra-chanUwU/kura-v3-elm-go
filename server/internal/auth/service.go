package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// SessionTTL is the fixed session lifetime. There is no sliding renewal.
const SessionTTL = 24 * time.Hour

// BcryptCost is the password hashing cost.
const BcryptCost = 12

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidPassword    = errors.New("password must be between 12 and 72 bytes")
	ErrNoLogin            = errors.New("account has no login password")
	ErrInvalidHandle      = errors.New("handle must be 1-64 characters of letters, digits, '.', '_' or '-'")
	ErrInvalidRole        = errors.New("role must be viewer, editor or admin")
	ErrReservedHandle     = errors.New("handle is reserved")
	ErrDuplicateHandle    = errors.New("handle already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidUserID      = errors.New("user id must be a positive integer")
	ErrLastAdmin          = errors.New("cannot disable or demote the last admin")
	ErrSelfAction         = errors.New("cannot disable or demote your own account")
	ErrBootstrapExists    = errors.New("an admin account with a login already exists")
	ErrForbidden          = errors.New("admin capability required")
	ErrNoChange           = errors.New("no changes supplied")
)

// User is the public account shape. It never carries a password hash.
type User struct {
	ID       int64  `json:"id"`
	Handle   string `json:"handle"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

// Actor returns the request actor for this account user.
func (u User) Actor() Actor {
	return Actor{ID: ActorIDForUser(u.ID, u.Handle), UserID: u.ID, Role: Role(u.Role)}
}

// ValidatePassword enforces the bounded password rule: 12..72 UTF-8 bytes.
// The upper bound is bcrypt's own input limit; longer input is rejected
// clearly instead of being silently truncated by bcrypt.
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return fmt.Errorf("%w: password must be valid UTF-8", ErrInvalidPassword)
	}
	if n := len([]byte(password)); n < 12 || n > 72 {
		return ErrInvalidPassword
	}
	return nil
}

// ValidateHandle enforces the account handle shape and rejects the
// reserved legacy handles for normal account creation.
func ValidateHandle(handle string) error {
	h := strings.TrimSpace(handle)
	if h != handle || len(h) < 1 || len(h) > 64 {
		return ErrInvalidHandle
	}
	for _, r := range h {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			return ErrInvalidHandle
		}
	}
	if h == LocalActorID || h == SystemActorID {
		return ErrReservedHandle
	}
	return nil
}

func isReservedHandle(handle string) bool {
	return handle == LocalActorID || handle == SystemActorID
}

// HashPassword hashes a validated password. Callers must validate first.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func checkPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash lets login spend bcrypt time for unknown handles so response
// timing does not enumerate accounts.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("kura-dummy-password-for-timing"), BcryptCost)

// GenerateSessionToken returns a new opaque session token and its stored
// hash. Only the hash is ever persisted; the raw token goes to the cookie.
func GenerateSessionToken() (raw string, hash string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b[:])
	return raw, HashToken(raw), nil
}

// UpdateUserPatch describes an admin account update. Nil fields are
// untouched; the HTTP layer rejects explicit JSON nulls before this.
type UpdateUserPatch struct {
	Role     *string
	Disabled *bool
	Password *string
}

// Service owns account, credential, and session behavior over a Store.
// It is safe for concurrent use when the Store is.
type Service struct {
	Store Store
	// Now supplies the current time; nil means time.Now. Tests override it
	// to exercise session expiry deterministically.
	Now func() time.Time
}

// NewService builds a Service over store (see NewPostgresStore).
func NewService(store Store) *Service { return &Service{Store: store} }

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// LoginResult is a successful login: the account, its actor, the raw
// session token (cookie only, never JSON), and the fixed expiry.
type LoginResult struct {
	User      User
	Actor     Actor
	RawToken  string
	ExpiresAt time.Time
}

// Login verifies handle+password and creates a 24h session. Every
// failure mode (unknown handle, wrong password, disabled account, legacy
// handle without a bootstrapped login, missing password) returns the
// same ErrInvalidCredentials so callers cannot enumerate accounts.
func (s *Service) Login(ctx context.Context, handle, password string) (LoginResult, error) {
	if s == nil || s.Store == nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	handle = strings.TrimSpace(handle)
	if handle == "" || password == "" {
		return LoginResult{}, ErrInvalidCredentials
	}
	rec, err := s.Store.GetUserByHandle(ctx, handle)
	if err != nil {
		// Spend bcrypt time anyway to blunt timing enumeration.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return LoginResult{}, ErrInvalidCredentials
	}
	// Reserved legacy handles may only log in after explicit bootstrap
	// adoption, which is the only path that sets their password hash.
	if rec.PasswordHash == nil || *rec.PasswordHash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return LoginResult{}, ErrInvalidCredentials
	}
	if !checkPassword(*rec.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if rec.Disabled {
		return LoginResult{}, ErrInvalidCredentials
	}
	if !ValidRole(rec.Role) {
		return LoginResult{}, ErrInvalidCredentials
	}
	raw, hash, err := GenerateSessionToken()
	if err != nil {
		return LoginResult{}, err
	}
	expires := s.now().Add(SessionTTL)
	if err := s.Store.CreateSession(ctx, rec.ID, hash, expires); err != nil {
		return LoginResult{}, err
	}
	user := rec.Public()
	return LoginResult{User: user, Actor: user.Actor(), RawToken: raw, ExpiresAt: expires}, nil
}

// ResolveSession resolves a raw cookie token fresh on every call: the
// user's current role and disabled flag are read from the store, and
// expired or revoked sessions never resolve.
func (s *Service) ResolveSession(ctx context.Context, rawToken string) (Actor, User, bool) {
	if s == nil || s.Store == nil || strings.TrimSpace(rawToken) == "" {
		return Actor{}, User{}, false
	}
	sess, err := s.Store.GetSession(ctx, HashToken(rawToken))
	if err != nil {
		return Actor{}, User{}, false
	}
	if sess.RevokedAt != nil || !sess.ExpiresAt.After(s.now()) {
		return Actor{}, User{}, false
	}
	rec, err := s.Store.GetUserByID(ctx, sess.UserID)
	if err != nil || rec.Disabled || !ValidRole(rec.Role) {
		return Actor{}, User{}, false
	}
	user := rec.Public()
	return user.Actor(), user, true
}

// Logout revokes the session behind rawToken. Unknown tokens are a no-op.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if s == nil || s.Store == nil || strings.TrimSpace(rawToken) == "" {
		return nil
	}
	return s.Store.RevokeSession(ctx, HashToken(rawToken))
}

// ListUsers returns every account for the admin user list.
func (s *Service) ListUsers(ctx context.Context, actor Actor) ([]User, error) {
	if !HasAdminCapability(actor) {
		return nil, ErrForbidden
	}
	recs, err := s.Store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(recs))
	for _, r := range recs {
		users = append(users, r.Public())
	}
	if users == nil {
		users = []User{}
	}
	return users, nil
}

// CreateUser creates an account with an explicit viewer/editor/admin
// role. Only admins (or legacy actors) may call it; there is no public
// registration path.
func (s *Service) CreateUser(ctx context.Context, actor Actor, handle, password, role string) (User, error) {
	if !HasAdminCapability(actor) {
		return User{}, ErrForbidden
	}
	handle = strings.TrimSpace(handle)
	if err := ValidateHandle(handle); err != nil {
		return User{}, err
	}
	if !ValidRole(role) {
		return User{}, ErrInvalidRole
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	rec, err := s.Store.CreateUser(ctx, handle, hash, role)
	if err != nil {
		return User{}, err
	}
	return rec.Public(), nil
}

// UpdateUser applies an admin patch. Self-disable/self-demotion and
// disabling or demoting the last enabled admin are refused; the store
// enforces the same rules transactionally so concurrent patches cannot
// race past the service checks. A password change, disable, or role
// change revokes all of the target's sessions (in the store).
func (s *Service) UpdateUser(ctx context.Context, actor Actor, id int64, patch UpdateUserPatch) (User, error) {
	if !HasAdminCapability(actor) {
		return User{}, ErrForbidden
	}
	if id <= 0 {
		return User{}, ErrInvalidUserID
	}
	if patch.Role == nil && patch.Disabled == nil && patch.Password == nil {
		return User{}, ErrNoChange
	}
	if patch.Role != nil && !ValidRole(*patch.Role) {
		return User{}, ErrInvalidRole
	}
	var hash *string
	if patch.Password != nil {
		if err := ValidatePassword(*patch.Password); err != nil {
			return User{}, err
		}
		h, err := HashPassword(*patch.Password)
		if err != nil {
			return User{}, err
		}
		hash = &h
	}
	rec, err := s.Store.UpdateUser(ctx, actor.UserID, id, patch.Role, patch.Disabled, hash)
	if err != nil {
		return User{}, err
	}
	return rec.Public(), nil
}

// ChangePassword replaces the signed-in account's password after verifying
// the current one. Only actor.UserID is updated; there is no target id.
// Success revokes that account's sessions. Callers must sign in again.
func (s *Service) ChangePassword(ctx context.Context, actor Actor, currentPassword, newPassword string) error {
	if s == nil || s.Store == nil || actor.UserID <= 0 {
		return ErrForbidden
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	rec, err := s.Store.GetUserByID(ctx, actor.UserID)
	if err != nil || rec.ID != actor.UserID || rec.Disabled || rec.PasswordHash == nil || *rec.PasswordHash == "" {
		return ErrInvalidCredentials
	}
	if !checkPassword(*rec.PasswordHash, currentPassword) {
		return ErrInvalidCredentials
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.Store.ReplacePassword(ctx, rec.ID, hash)
}

// ResetPassword replaces the password of an existing login account. It is
// the offline recovery path: it does not create users, grant admin, adopt
// a legacy row, or change role or disabled state. Sessions for that
// account are revoked with the password write.
func (s *Service) ResetPassword(ctx context.Context, handle, password string) (User, error) {
	if s == nil || s.Store == nil {
		return User{}, ErrUserNotFound
	}
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return User{}, ErrInvalidHandle
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	rec, err := s.Store.GetUserByHandle(ctx, handle)
	if err != nil {
		if errors.Is(err, errStoreNotFound) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	if rec.PasswordHash == nil || *rec.PasswordHash == "" {
		if isReservedHandle(rec.Handle) {
			return User{}, ErrReservedHandle
		}
		return User{}, ErrNoLogin
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	if err := s.Store.ReplacePassword(ctx, rec.ID, hash); err != nil {
		return User{}, err
	}
	updated, err := s.Store.GetUserByID(ctx, rec.ID)
	if err != nil {
		return rec.Public(), nil
	}
	return updated.Public(), nil
}

// BootstrapAdmin creates the initial login admin, offline only. It fails
// if any enabled admin with a password already exists. With adoptLegacy
// "local" or "system" the existing legacy user row gains the password
// and admin role, so its actor key (and saved searches) are retained;
// otherwise a new account with handle is created as admin. The handle is
// the login handle: when adopting, it must be empty or equal the legacy
// handle.
func (s *Service) BootstrapAdmin(ctx context.Context, handle, password, adoptLegacy string) (User, error) {
	if s == nil || s.Store == nil {
		return User{}, ErrBootstrapExists
	}
	adoptLegacy = strings.TrimSpace(adoptLegacy)
	if adoptLegacy != "" && !isReservedHandle(adoptLegacy) {
		return User{}, fmt.Errorf("adopt-legacy must be %q or %q", LocalActorID, SystemActorID)
	}
	handle = strings.TrimSpace(handle)
	if adoptLegacy != "" {
		if handle != "" && handle != adoptLegacy {
			return User{}, fmt.Errorf("handle must match adopt-legacy %q", adoptLegacy)
		}
		handle = adoptLegacy
	} else {
		if err := ValidateHandle(handle); err != nil {
			return User{}, err
		}
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	rec, err := s.Store.BootstrapAdmin(ctx, handle, hash, adoptLegacy)
	if err != nil {
		return User{}, err
	}
	return rec.Public(), nil
}

// ParseCanonicalUserID parses a canonical positive user id: decimal, no
// sign, no leading zeros, no surrounding space.
func ParseCanonicalUserID(raw string) (int64, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return 0, ErrInvalidUserID
	}
	var id int64
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, ErrInvalidUserID
		}
	}
	if _, err := fmt.Sscan(raw, &id); err != nil || id <= 0 {
		return 0, ErrInvalidUserID
	}
	if int64String(id) != raw {
		return 0, ErrInvalidUserID
	}
	return id, nil
}
