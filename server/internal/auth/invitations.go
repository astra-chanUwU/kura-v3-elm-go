package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const InvitationTTL = 7 * 24 * time.Hour

var ErrInvitationUnavailable = errors.New("invitation unavailable")

// Invitation is safe for listing and preview. It never includes a token or
// token hash; the raw token is returned only by CreateInvitation.
type Invitation struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	Used      bool      `json:"used"`
}

// InvitationStore is optional so existing AuthService implementations and
// test fakes remain source-compatible until they need invitation behavior.
type InvitationStore interface {
	CreateInvitation(context.Context, Actor, string, string, time.Time, time.Time) (Invitation, error)
	ListInvitations(context.Context) ([]Invitation, error)
	RevokeInvitation(context.Context, int64) error
	PreviewInvitation(context.Context, string, time.Time) (Invitation, error)
	RedeemInvitation(context.Context, string, string, string, string, time.Time, time.Time) (UserRecord, error)
}

func (s *Service) invitationStore() (InvitationStore, error) {
	if s == nil || s.Store == nil {
		return nil, ErrInvitationUnavailable
	}
	store, ok := s.Store.(InvitationStore)
	if !ok {
		return nil, ErrInvitationUnavailable
	}
	return store, nil
}

func newInvitationToken() (string, string, error) {
	var rawBytes [32]byte
	if _, err := rand.Read(rawBytes[:]); err != nil {
		return "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(rawBytes[:])
	return raw, HashToken(raw), nil
}

func (s *Service) CreateInvitation(ctx context.Context, actor Actor, role string) (Invitation, string, error) {
	if !HasAdminCapability(actor) {
		return Invitation{}, "", ErrForbidden
	}
	if !ValidRole(role) {
		return Invitation{}, "", ErrInvalidRole
	}
	store, err := s.invitationStore()
	if err != nil {
		return Invitation{}, "", err
	}
	raw, hash, err := newInvitationToken()
	if err != nil {
		return Invitation{}, "", err
	}
	now := s.now()
	inv, err := store.CreateInvitation(ctx, actor, role, hash, now, now.Add(InvitationTTL))
	if err != nil {
		return Invitation{}, "", err
	}
	return inv, raw, nil
}

func (s *Service) ListInvitations(ctx context.Context, actor Actor) ([]Invitation, error) {
	if !HasAdminCapability(actor) {
		return nil, ErrForbidden
	}
	store, err := s.invitationStore()
	if err != nil {
		return nil, err
	}
	return store.ListInvitations(ctx)
}

func (s *Service) RevokeInvitation(ctx context.Context, actor Actor, id int64) error {
	if !HasAdminCapability(actor) {
		return ErrForbidden
	}
	if id <= 0 {
		return ErrInvalidUserID
	}
	store, err := s.invitationStore()
	if err != nil {
		return err
	}
	return store.RevokeInvitation(ctx, id)
}

func (s *Service) PreviewInvitation(ctx context.Context, token string) (Invitation, error) {
	store, err := s.invitationStore()
	if err != nil {
		return Invitation{}, err
	}
	if strings.TrimSpace(token) == "" {
		return Invitation{}, ErrInvitationUnavailable
	}
	return store.PreviewInvitation(ctx, HashToken(token), s.now())
}

// RegisterWithInvitation atomically redeems a one-use invitation and creates
// the account, assigned role, and initial session.
func (s *Service) RegisterWithInvitation(ctx context.Context, token, handle, password string) (LoginResult, error) {
	if err := ValidateHandle(handle); err != nil {
		return LoginResult{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return LoginResult{}, err
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return LoginResult{}, err
	}
	rawSession, sessionHash, err := GenerateSessionToken()
	if err != nil {
		return LoginResult{}, err
	}
	store, err := s.invitationStore()
	if err != nil {
		return LoginResult{}, err
	}
	if strings.TrimSpace(token) == "" {
		return LoginResult{}, ErrInvitationUnavailable
	}
	now := s.now()
	expires := now.Add(SessionTTL)
	userRec, err := store.RedeemInvitation(ctx, HashToken(token), handle, passwordHash, sessionHash, expires, now)
	if err != nil {
		return LoginResult{}, err
	}
	user := userRec.Public()
	return LoginResult{User: user, Actor: user.Actor(), RawToken: rawSession, ExpiresAt: expires}, nil
}
