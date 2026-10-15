package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// fakeStore is an in-memory Store with the same protection semantics as
// PostgresStore (self/last-admin checks and session revocation on update).
type fakeStore struct {
	users    map[int64]*UserRecord
	sessions map[string]SessionRecord
	nextID   int64
}

func newFakeStore() *fakeStore {
	f := &fakeStore{users: map[int64]*UserRecord{}, sessions: map[string]SessionRecord{}, nextID: 1}
	f.add("local", "", "")
	f.add("system", "", "admin")
	return f
}

func (f *fakeStore) add(handle, hash, role string) UserRecord {
	rec := UserRecord{ID: f.nextID, Handle: handle, Role: role}
	f.nextID++
	if hash != "" {
		h := hash
		rec.PasswordHash = &h
	}
	f.users[rec.ID] = &rec
	return rec
}

func (f *fakeStore) find(handle string) (UserRecord, error) {
	for _, u := range f.users {
		if u.Handle == handle {
			return *u, nil
		}
	}
	return UserRecord{}, errStoreNotFound
}

func (f *fakeStore) GetUserByHandle(_ context.Context, h string) (UserRecord, error) {
	return f.find(h)
}
func (f *fakeStore) GetUserByID(_ context.Context, id int64) (UserRecord, error) {
	if u, ok := f.users[id]; ok {
		return *u, nil
	}
	return UserRecord{}, errStoreNotFound
}
func (f *fakeStore) CreateSession(_ context.Context, uid int64, hash string, exp time.Time) error {
	f.sessions[hash] = SessionRecord{UserID: uid, ExpiresAt: exp}
	return nil
}
func (f *fakeStore) GetSession(_ context.Context, hash string) (SessionRecord, error) {
	if s, ok := f.sessions[hash]; ok {
		return s, nil
	}
	return SessionRecord{}, errStoreNotFound
}
func (f *fakeStore) RevokeSession(_ context.Context, hash string) error {
	if s, ok := f.sessions[hash]; ok {
		now := time.Now()
		s.RevokedAt = &now
		f.sessions[hash] = s
	}
	return nil
}
func (f *fakeStore) RevokeAllSessions(_ context.Context, uid int64) error {
	for k, s := range f.sessions {
		if s.UserID == uid && s.RevokedAt == nil {
			now := time.Now()
			s.RevokedAt = &now
			f.sessions[k] = s
		}
	}
	return nil
}
func (f *fakeStore) ListUsers(_ context.Context) ([]UserRecord, error) {
	out := []UserRecord{}
	for _, u := range f.users {
		out = append(out, *u)
	}
	return out, nil
}
func (f *fakeStore) CreateUser(_ context.Context, handle, hash, role string) (UserRecord, error) {
	if _, err := f.find(handle); err == nil {
		return UserRecord{}, ErrDuplicateHandle
	}
	return f.add(handle, hash, role), nil
}
func (f *fakeStore) enabledAdmins() int {
	n := 0
	for _, u := range f.users {
		if u.Role == "admin" && !u.Disabled && u.PasswordHash != nil && *u.PasswordHash != "" {
			n++
		}
	}
	return n
}
func (f *fakeStore) UpdateUser(ctx context.Context, actorID, id int64, role *string, disabled *bool, hash *string) (UserRecord, error) {
	u, ok := f.users[id]
	if !ok {
		return UserRecord{}, ErrUserNotFound
	}
	if isReservedHandle(u.Handle) {
		return UserRecord{}, ErrReservedHandle
	}
	demoting := role != nil && u.Role == "admin" && *role != "admin"
	disabling := disabled != nil && *disabled && !u.Disabled
	if (demoting || disabling) && actorID != 0 && actorID == id {
		return UserRecord{}, ErrSelfAction
	}
	if (demoting || disabling) && u.Role == "admin" && !u.Disabled && f.enabledAdmins() <= 1 {
		return UserRecord{}, ErrLastAdmin
	}
	if hash != nil {
		u.PasswordHash = hash
	}
	if disabled != nil {
		u.Disabled = *disabled
	}
	if role != nil {
		u.Role = *role
	}
	_ = f.RevokeAllSessions(ctx, id)
	return *u, nil
}
func (f *fakeStore) BootstrapAdmin(_ context.Context, handle, hash, adopt string) (UserRecord, error) {
	for _, u := range f.users {
		if u.Role == "admin" && !u.Disabled && u.PasswordHash != nil && *u.PasswordHash != "" {
			return UserRecord{}, ErrBootstrapExists
		}
	}
	if adopt != "" {
		for _, u := range f.users {
			if u.Handle == adopt {
				u.PasswordHash = &hash
				u.Disabled = false
				u.Role = "admin"
				return *u, nil
			}
		}
		return UserRecord{}, errStoreNotFound
	}
	if _, err := f.find(handle); err == nil {
		return UserRecord{}, ErrDuplicateHandle
	}
	return f.add(handle, hash, "admin"), nil
}

var _ Store = (*fakeStore)(nil)

const testPassword = "correct-horse-12"

func TestPasswordBoundsAndHashing(t *testing.T) {
	for _, pw := range []string{"short", strings.Repeat("a", 11), strings.Repeat("a", 73), "invalid\xffutf8-password"} {
		if err := ValidatePassword(pw); !errors.Is(err, ErrInvalidPassword) {
			t.Fatalf("ValidatePassword(%q len=%d) = %v", pw, len(pw), err)
		}
	}
	for _, pw := range []string{strings.Repeat("a", 12), strings.Repeat("a", 72)} {
		if err := ValidatePassword(pw); err != nil {
			t.Fatalf("ValidatePassword(len=%d) = %v", len(pw), err)
		}
	}
	hash, err := HashPassword(testPassword)
	if err != nil || hash == testPassword || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("hash=%q err=%v", hash, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(testPassword)); err != nil {
		t.Fatalf("bcrypt verify: %v", err)
	}
	cost, _ := bcrypt.Cost([]byte(hash))
	if cost != BcryptCost {
		t.Fatalf("cost=%d want %d", cost, BcryptCost)
	}
}

func TestLoginSessionLifecycleExpiryRevokeAndDisable(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := &Service{Store: store, Now: func() time.Time { return now }}
	owner, err := svc.BootstrapAdmin(ctx, "owner", testPassword, "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if owner.Actor().ID != "user:3" && owner.Handle != "owner" {
		t.Fatalf("owner=%+v actor=%q", owner, owner.Actor().ID)
	}
	login, err := svc.Login(ctx, "owner", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	// Only the hash is stored, never the raw token.
	if _, ok := store.sessions[login.RawToken]; ok {
		t.Fatal("raw token stored as session key")
	}
	if _, ok := store.sessions[HashToken(login.RawToken)]; !ok {
		t.Fatal("token hash not stored")
	}
	if !login.ExpiresAt.Equal(now.Add(SessionTTL)) {
		t.Fatalf("expires=%v want fixed 24h", login.ExpiresAt)
	}
	actor, user, ok := svc.ResolveSession(ctx, login.RawToken)
	if !ok || actor.Role != RoleAdmin || user.Handle != "owner" || !HasAdminCapability(actor) {
		t.Fatalf("resolve actor=%+v user=%+v ok=%v", actor, user, ok)
	}
	// Wrong password and unknown handle are indistinguishable.
	if _, err := svc.Login(ctx, "owner", "wrong-password-1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong pw err=%v", err)
	}
	if _, err := svc.Login(ctx, "ghost", testPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("ghost err=%v", err)
	}
	// Expiry: past 24h the same token no longer resolves.
	now = now.Add(SessionTTL + time.Minute)
	if _, _, ok := svc.ResolveSession(ctx, login.RawToken); ok {
		t.Fatal("expired session resolved")
	}
	now = now.Add(-SessionTTL)
	// Logout revokes.
	login2, _ := svc.Login(ctx, "owner", testPassword)
	if err := svc.Logout(ctx, login2.RawToken); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := svc.ResolveSession(ctx, login2.RawToken); ok {
		t.Fatal("revoked session resolved")
	}
	// Password change revokes all sessions and rotates the credential.
	login3, _ := svc.Login(ctx, "owner", testPassword)
	newPw := "new-password-456"
	if _, err := svc.UpdateUser(ctx, login3.Actor, owner.ID, UpdateUserPatch{Password: &newPw}); err != nil {
		t.Fatalf("update pw: %v", err)
	}
	if _, _, ok := svc.ResolveSession(ctx, login3.RawToken); ok {
		t.Fatal("session survived password change")
	}
	if _, err := svc.Login(ctx, "owner", testPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old pw err=%v", err)
	}
	// Disable revokes and blocks login (need a second admin to disable owner).
	admin2, err := svc.CreateUser(ctx, login3.Actor, "admin2", testPassword, "admin")
	if err != nil {
		// login3 actor is owner admin (stale session irrelevant for service authz).
		t.Fatalf("create admin2: %v", err)
	}
	_ = admin2
	disabled := true
	ownerLogin, err := svc.Login(ctx, "owner", newPw)
	if err != nil {
		t.Fatalf("relogin: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, Actor{ID: "system", System: true}, owner.ID, UpdateUserPatch{Disabled: &disabled}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, ok := svc.ResolveSession(ctx, ownerLogin.RawToken); ok {
		t.Fatal("session of disabled user resolved")
	}
	if _, err := svc.Login(ctx, "owner", newPw); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled login err=%v", err)
	}
	// Role is resolved fresh: re-enable as viewer and the actor changes.
	enabled := false
	viewer := "viewer"
	sysActor := Actor{ID: "system", System: true}
	if _, err := svc.UpdateUser(ctx, sysActor, owner.ID, UpdateUserPatch{Disabled: &enabled, Role: &viewer}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	vl, err := svc.Login(ctx, "owner", newPw)
	if err != nil {
		t.Fatalf("viewer login: %v", err)
	}
	if HasWriteCapability(vl.Actor) || !HasReadCapability(vl.Actor) || HasAdminCapability(vl.Actor) {
		t.Fatalf("viewer caps actor=%+v", vl.Actor)
	}
}

func TestBootstrapAdoptLegacyRetainsActorAndSecondBootstrapFails(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStore())
	// Reserved handles cannot be created or logged into before bootstrap.
	if _, err := svc.CreateUser(ctx, Actor{ID: "system", System: true}, "local", testPassword, "admin"); !errors.Is(err, ErrReservedHandle) {
		t.Fatalf("create local err=%v", err)
	}
	if _, err := svc.Login(ctx, "local", testPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("legacy login err=%v", err)
	}
	owner, err := svc.BootstrapAdmin(ctx, "", testPassword, "local")
	if err != nil {
		t.Fatalf("adopt bootstrap: %v", err)
	}
	if owner.Actor().ID != LocalActorID {
		t.Fatalf("adopted actor=%q want local (saved searches retained)", owner.Actor().ID)
	}
	login, err := svc.Login(ctx, "local", testPassword)
	if err != nil || login.Actor.ID != LocalActorID {
		t.Fatalf("adopted login=%+v err=%v", login, err)
	}
	if _, err := svc.BootstrapAdmin(ctx, "other", testPassword, ""); !errors.Is(err, ErrBootstrapExists) {
		t.Fatalf("second bootstrap err=%v", err)
	}
	if _, err := svc.BootstrapAdmin(ctx, "x", testPassword, "bogus"); err == nil {
		t.Fatal("invalid adopt-legacy accepted")
	}
}

func TestLastAdminAndSelfProtection(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStore())
	owner, err := svc.BootstrapAdmin(ctx, "owner", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	ownerActor := owner.Actor() // admin, UserID set
	disabled := true
	viewer := "viewer"
	// Self-disable and self-demotion are refused.
	if _, err := svc.UpdateUser(ctx, ownerActor, owner.ID, UpdateUserPatch{Disabled: &disabled}); !errors.Is(err, ErrSelfAction) {
		t.Fatalf("self-disable err=%v", err)
	}
	if _, err := svc.UpdateUser(ctx, ownerActor, owner.ID, UpdateUserPatch{Role: &viewer}); !errors.Is(err, ErrSelfAction) {
		t.Fatalf("self-demote err=%v", err)
	}
	// Last admin cannot be disabled/demoted even by the legacy system actor.
	sys := Actor{ID: "system", System: true}
	if _, err := svc.UpdateUser(ctx, sys, owner.ID, UpdateUserPatch{Disabled: &disabled}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last-admin disable err=%v", err)
	}
	if _, err := svc.UpdateUser(ctx, sys, owner.ID, UpdateUserPatch{Role: &viewer}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last-admin demote err=%v", err)
	}
	// With a second admin, demoting that admin succeeds.
	second, err := svc.CreateUser(ctx, ownerActor, "second", testPassword, "admin")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateUser(ctx, ownerActor, second.ID, UpdateUserPatch{Role: &viewer})
	if err != nil || updated.Role != "viewer" {
		t.Fatalf("demote second=%+v err=%v", updated, err)
	}
	// Non-admins cannot create users; invalid ids are rejected.
	if _, err := svc.CreateUser(ctx, updated.Actor(), "nope", testPassword, "viewer"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer create err=%v", err)
	}
	if _, err := svc.UpdateUser(ctx, ownerActor, 0, UpdateUserPatch{Role: &viewer}); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("id 0 err=%v", err)
	}
	for raw, ok := range map[string]bool{"1": true, "42": true, "0": false, "01": false, "+1": false, "-1": false, " 1": false, "x": false} {
		id, err := ParseCanonicalUserID(raw)
		if ok && (err != nil || id <= 0) {
			t.Fatalf("ParseCanonicalUserID(%q)=%d,%v", raw, id, err)
		}
		if !ok && err == nil {
			t.Fatalf("ParseCanonicalUserID(%q) accepted", raw)
		}
	}
}

func TestRoleCapabilityMatrixAndLegacyCompatibility(t *testing.T) {
	cases := []struct {
		actor                   Actor
		read, write, mod, admin bool
	}{
		{Actor{}, false, false, false, false},
		{Actor{ID: "local"}, true, true, true, true},
		{Actor{ID: "system", System: true}, true, true, true, true},
		{Actor{ID: "owner-1"}, true, true, false, false}, // pre-role literal keeps write
		{Actor{ID: "user:1", UserID: 1, Role: RoleViewer}, true, false, false, false},
		{Actor{ID: "user:2", UserID: 2, Role: RoleEditor}, true, true, false, false},
		{Actor{ID: "user:3", UserID: 3, Role: RoleAdmin}, true, true, true, true},
	}
	for _, c := range cases {
		if HasReadCapability(c.actor) != c.read || HasWriteCapability(c.actor) != c.write ||
			HasModerateCapability(c.actor) != c.mod || HasAdminCapability(c.actor) != c.admin {
			t.Fatalf("caps(%+v) read=%v write=%v mod=%v admin=%v", c.actor,
				HasReadCapability(c.actor), HasWriteCapability(c.actor), HasModerateCapability(c.actor), HasAdminCapability(c.actor))
		}
	}
	if ActorIDForUser(9, "someone") != "user:9" || ActorIDForUser(1, "local") != "local" {
		t.Fatal("actor id mapping wrong")
	}
}
