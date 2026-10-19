package profiles

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

type memStore struct {
	rows map[int64]Record
}

func (m *memStore) GetByUsername(_ context.Context, username string) (Record, error) {
	for _, rec := range m.rows {
		if rec.Username == username && !rec.Disabled {
			return rec, nil
		}
	}
	return Record{}, ErrNotFound
}

func (m *memStore) GetByUserID(_ context.Context, userID int64) (Record, error) {
	rec, ok := m.rows[userID]
	if !ok || rec.Disabled {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

func (m *memStore) Update(_ context.Context, userID int64, input Input) (Record, error) {
	rec, ok := m.rows[userID]
	if !ok || rec.Disabled {
		return Record{}, ErrNotFound
	}
	normalized, err := NormalizeProfile(input)
	if err != nil {
		return Record{}, err
	}
	if rec.Version != normalized.ExpectedVersion {
		return Record{}, &VersionConflict{Current: rec}
	}
	for id, other := range m.rows {
		if id != userID && other.Username == normalized.Username {
			return Record{}, ErrUsernameTaken
		}
	}
	handle := rec.Handle
	rec.Username = normalized.Username
	rec.DisplayName = normalized.DisplayName
	rec.Bio = normalized.Bio
	rec.Website = normalized.Website
	rec.Version++
	rec.Handle = handle
	m.rows[userID] = rec
	return rec, nil
}

func (m *memStore) SetAvatar(_ context.Context, userID, expected int64, key string) (Record, string, error) {
	rec, ok := m.rows[userID]
	if !ok || rec.Disabled {
		return Record{}, "", ErrNotFound
	}
	if rec.Version != expected {
		return Record{}, "", &VersionConflict{Current: rec}
	}
	previous := rec.AvatarKey
	rec.AvatarKey = key
	rec.Version++
	m.rows[userID] = rec
	return rec, previous, nil
}

func (m *memStore) Username(_ context.Context, userID int64) (string, bool, error) {
	rec, ok := m.rows[userID]
	if !ok || rec.Disabled || rec.Username == "" {
		return "", false, nil
	}
	return rec.Username, true, nil
}

type memMedia struct {
	objects map[string][]byte
	deleted []string
	failSet bool
}

func (m *memMedia) Put(_ context.Context, key, contentType string, content io.Reader) (media.Object, error) {
	if _, ok := m.objects[key]; ok {
		return media.Object{}, errors.New("key exists")
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return media.Object{}, err
	}
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = data
	return media.Object{Key: key, ContentType: contentType, Size: int64(len(data)), Created: true}, nil
}

func (m *memMedia) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memMedia) Delete(_ context.Context, key string) error {
	m.deleted = append(m.deleted, key)
	delete(m.objects, key)
	return nil
}

func TestUpdateKeepsHandleAndRejectsConflicts(t *testing.T) {
	store := &memStore{rows: map[int64]Record{
		1: {UserID: 1, Handle: "login-a", Username: "ada", Version: 3},
		2: {UserID: 2, Handle: "login-b", Username: "bea", Version: 1},
	}}
	svc := NewService(store, &memMedia{objects: map[string][]byte{}})
	updated, err := svc.Update(context.Background(), 1, Input{Username: "Ada_New", DisplayName: "Ada", ExpectedVersion: 3})
	if err != nil || updated.Username != "ada_new" || store.rows[1].Handle != "login-a" || store.rows[2].Username != "bea" {
		t.Fatalf("update=%#v store=%#v err=%v", updated, store.rows, err)
	}
	if _, err := svc.Update(context.Background(), 2, Input{Username: "ada_new", ExpectedVersion: 1}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("taken err=%v", err)
	}
	if store.rows[2].Username != "bea" || store.rows[2].Version != 1 {
		t.Fatalf("rejected update changed account B: %#v", store.rows[2])
	}
	_, staleErr := svc.Update(context.Background(), 1, Input{Username: "other", ExpectedVersion: 3})
	if !errors.Is(staleErr, ErrConflict) {
		t.Fatalf("stale err=%v", staleErr)
	}
	current, ok := ConflictProfile(staleErr)
	if !ok || current.Username != "ada_new" || store.rows[1].Username != "ada_new" {
		t.Fatalf("conflict current=%#v ok=%v row=%#v", current, ok, store.rows[1])
	}
	if _, err := svc.Update(context.Background(), 0, Input{Username: "ada", ExpectedVersion: 1}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("anonymous edit err=%v", err)
	}
}

func TestAvatarReplaceKeepsPreviousOnFailure(t *testing.T) {
	pngBytes := mustPNG(t)
	objects := &memMedia{objects: map[string][]byte{"avatars/1/old.jpg": []byte("old-jpeg")}}
	store := &memStore{rows: map[int64]Record{
		1: {UserID: 1, Handle: "login-a", Username: "ada", Version: 2, AvatarKey: "avatars/1/old.jpg"},
	}}
	svc := NewService(store, objects)
	if _, err := svc.ReplaceAvatar(context.Background(), 1, 9, pngBytes); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale avatar err=%v", err)
	}
	if _, ok := objects.objects["avatars/1/old.jpg"]; !ok || store.rows[1].AvatarKey != "avatars/1/old.jpg" {
		t.Fatalf("stale upload destroyed previous avatar objects=%v row=%#v deleted=%v", objects.objects, store.rows[1], objects.deleted)
	}
	for _, key := range objects.deleted {
		if key == "avatars/1/old.jpg" {
			t.Fatal("previous avatar was deleted")
		}
	}
	if _, err := svc.ReplaceAvatar(context.Background(), 1, 2, []byte("<svg></svg>")); !errors.Is(err, ErrAvatar) {
		t.Fatalf("svg err=%v", err)
	}
	if len(objects.objects) != 1 || store.rows[1].AvatarKey != "avatars/1/old.jpg" {
		t.Fatalf("rejected upload changed storage objects=%d key=%s", len(objects.objects), store.rows[1].AvatarKey)
	}
	updated, err := svc.ReplaceAvatar(context.Background(), 1, 2, pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AvatarKey == "" || updated.AvatarKey == "avatars/1/old.jpg" || store.rows[1].Handle != "login-a" {
		t.Fatalf("replace=%#v", updated)
	}
	if _, ok := objects.objects[updated.AvatarKey]; !ok {
		t.Fatal("new avatar missing")
	}
	if _, ok := objects.objects["avatars/1/old.jpg"]; ok {
		t.Fatal("previous avatar still stored after replacement")
	}
	cleared, err := svc.ClearAvatar(context.Background(), 1, updated.Version)
	if err != nil || cleared.AvatarKey != "" {
		t.Fatalf("clear=%#v err=%v", cleared, err)
	}
}

func mustPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 5))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
