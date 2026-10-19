package profiles

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

type Service struct {
	Store Store
	Media media.Store
}

func NewService(store Store, objects media.Store) *Service {
	return &Service{Store: store, Media: objects}
}

func (s *Service) View(ctx context.Context, username string) (Profile, error) {
	name, err := NormalizeUsername(username)
	if err != nil {
		return Profile{}, ErrNotFound
	}
	rec, err := s.store().GetByUsername(ctx, name)
	if err != nil {
		return Profile{}, err
	}
	return profileFrom(rec), nil
}

func (s *Service) Own(ctx context.Context, userID int64) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrNotFound
	}
	rec, err := s.store().GetByUserID(ctx, userID)
	if err != nil {
		return Profile{}, err
	}
	return profileFrom(rec), nil
}

func (s *Service) Update(ctx context.Context, userID int64, input Input) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrForbidden
	}
	if _, err := NormalizeProfile(input); err != nil {
		return Profile{}, err
	}
	rec, err := s.store().Update(ctx, userID, input)
	if err != nil {
		return Profile{}, err
	}
	return profileFrom(rec), nil
}

func (s *Service) ReplaceAvatar(ctx context.Context, userID, expectedVersion int64, data []byte) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrForbidden
	}
	normalized, err := NormalizeAvatar(data)
	if err != nil {
		return Profile{}, err
	}
	if s == nil || s.Media == nil {
		return Profile{}, ErrUnavailable
	}
	key, err := newAvatarKey(userID)
	if err != nil {
		return Profile{}, err
	}
	if _, err := s.Media.Put(ctx, key, "image/jpeg", bytes.NewReader(normalized)); err != nil {
		return Profile{}, err
	}
	rec, previous, err := s.store().SetAvatar(ctx, userID, expectedVersion, key)
	if err != nil {
		_ = s.Media.Delete(ctx, key)
		return Profile{}, err
	}
	if previous != "" && previous != key {
		_ = s.Media.Delete(ctx, previous)
	}
	return profileFrom(rec), nil
}

func (s *Service) ClearAvatar(ctx context.Context, userID, expectedVersion int64) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrForbidden
	}
	rec, previous, err := s.store().SetAvatar(ctx, userID, expectedVersion, "")
	if err != nil {
		return Profile{}, err
	}
	if previous != "" && s != nil && s.Media != nil {
		_ = s.Media.Delete(ctx, previous)
	}
	return profileFrom(rec), nil
}

func (s *Service) AvatarBytes(ctx context.Context, username string) ([]byte, error) {
	profile, err := s.View(ctx, username)
	if err != nil {
		return nil, err
	}
	if profile.AvatarKey == "" || s == nil || s.Media == nil {
		return nil, ErrNotFound
	}
	body, err := s.Media.Open(ctx, profile.AvatarKey)
	if err != nil {
		return nil, ErrNotFound
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxAvatarBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxAvatarBytes {
		return nil, ErrNotFound
	}
	return data, nil
}

func (s *Service) store() Store {
	if s == nil || s.Store == nil {
		return unavailableStore{}
	}
	return s.Store
}

type unavailableStore struct{}

func (unavailableStore) GetByUsername(context.Context, string) (Record, error) {
	return Record{}, ErrUnavailable
}
func (unavailableStore) GetByUserID(context.Context, int64) (Record, error) {
	return Record{}, ErrUnavailable
}
func (unavailableStore) Update(context.Context, int64, Input) (Record, error) {
	return Record{}, ErrUnavailable
}
func (unavailableStore) SetAvatar(context.Context, int64, int64, string) (Record, string, error) {
	return Record{}, "", ErrUnavailable
}
func (unavailableStore) Username(context.Context, int64) (string, bool, error) {
	return "", false, ErrUnavailable
}

func profileFrom(rec Record) Profile {
	return Profile{
		UserID: rec.UserID, Username: rec.Username, DisplayName: rec.DisplayName,
		Bio: rec.Bio, Website: rec.Website, Version: rec.Version, AvatarKey: rec.AvatarKey,
	}
}

func newAvatarKey(userID int64) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("avatars/%d/%s.jpg", userID, hex.EncodeToString(buf[:])), nil
}

func ConflictProfile(err error) (Profile, bool) {
	var conflict *VersionConflict
	if errors.As(err, &conflict) && conflict != nil {
		return profileFrom(conflict.Current), true
	}
	return Profile{}, false
}
