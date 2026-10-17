package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresStoreAccounts is opt-in: it only runs against an explicitly
// supplied, already-migrated disposable database (never a dev library).
// No database is created here.
func TestPostgresStoreAccounts(t *testing.T) {
	url := os.Getenv("KURA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewService(NewPostgresStore(pool))
	handle := fmt.Sprintf("testacct%d", time.Now().UnixNano()%100000000)
	sys := Actor{ID: SystemActorID, System: true}
	// Bootstrap-free path: use the system legacy actor as admin creator.
	user, err := svc.CreateUser(ctx, sys, handle, testPassword, "editor")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE handle = $1`, handle) }()
	login, err := svc.Login(ctx, handle, testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, _, ok := svc.ResolveSession(ctx, login.RawToken); !ok {
		t.Fatal("session did not resolve")
	}
	disabled := true
	if _, err := svc.UpdateUser(ctx, sys, user.ID, UpdateUserPatch{Disabled: &disabled}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, ok := svc.ResolveSession(ctx, login.RawToken); ok {
		t.Fatal("disabled user's session resolved")
	}
}

func TestPostgresSelfServiceAndOfflineReset(t *testing.T) {
	url := os.Getenv("KURA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewService(NewPostgresStore(pool))
	sys := Actor{ID: SystemActorID, System: true}
	handle := fmt.Sprintf("pwreset%d", time.Now().UnixNano()%100000000)
	user, err := svc.CreateUser(ctx, sys, handle, testPassword, "viewer")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID) }()
	login, err := svc.Login(ctx, handle, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	const changed = "changed-password-1"
	if err := svc.ChangePassword(ctx, login.Actor, testPassword, changed); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := svc.ResolveSession(ctx, login.RawToken); ok {
		t.Fatal("session survived password change")
	}
	if _, err := svc.Login(ctx, handle, testPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password err=%v", err)
	}
	current, err := svc.Login(ctx, handle, changed)
	if err != nil || current.User.Role != "viewer" {
		t.Fatalf("new login=%+v err=%v", current, err)
	}
	reset, err := svc.ResetPassword(ctx, handle, "offline-reset-password")
	if err != nil || reset.ID != user.ID || reset.Role != "viewer" || reset.Disabled {
		t.Fatalf("reset=%+v err=%v", reset, err)
	}
	if _, _, ok := svc.ResolveSession(ctx, current.RawToken); ok {
		t.Fatal("session survived offline reset")
	}
	if _, err := svc.Login(ctx, handle, "offline-reset-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetPassword(ctx, "missing-"+handle, testPassword); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing reset err=%v", err)
	}
}

func TestPostgresInvitations(t *testing.T) {
	url := os.Getenv("KURA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewService(NewPostgresStore(pool))
	sys := Actor{ID: SystemActorID, System: true}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	adminHandle := "inviteadmin" + suffix
	defer pool.Exec(ctx, `DELETE FROM users WHERE handle LIKE '%' || $1`, suffix)

	if _, _, err := svc.CreateInvitation(ctx, Actor{ID: "user:4", UserID: 4, Role: RoleViewer}, "viewer"); err != ErrForbidden {
		t.Fatalf("viewer could create invitation: %v", err)
	}
	duplicateUser, err := svc.CreateUser(ctx, sys, "duplicate"+suffix, testPassword, "editor")
	if err != nil {
		t.Fatal(err)
	}

	inv, token, err := svc.CreateInvitation(ctx, sys, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || inv.Role != "viewer" || !inv.ExpiresAt.Equal(inv.CreatedAt.Add(InvitationTTL)) {
		t.Fatalf("unexpected invitation: %#v token length %d", inv, len(token))
	}
	var storedHash string
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM account_invitations WHERE id = $1`, inv.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != HashToken(token) || storedHash == token {
		t.Fatal("invitation token was not stored exclusively as its hash")
	}
	if _, err := svc.RegisterWithInvitation(ctx, token, duplicateUser.Handle, testPassword); err != ErrDuplicateHandle {
		t.Fatalf("duplicate registration error = %v", err)
	}
	if _, err := svc.PreviewInvitation(ctx, token); err != nil {
		t.Fatalf("duplicate consumed invitation: %v", err)
	}

	type result struct {
		login LoginResult
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			login, err := svc.RegisterWithInvitation(ctx, token, fmt.Sprintf("redeem%s%d", suffix, i), testPassword)
			results <- result{login, err}
		}(i)
	}
	wg.Wait()
	close(results)
	var success *LoginResult
	failed := 0
	for got := range results {
		if got.err == nil {
			copy := got.login
			success = &copy
		} else if got.err == ErrInvitationUnavailable {
			failed++
		} else {
			t.Fatalf("unexpected concurrent redemption error: %v", got.err)
		}
	}
	if success == nil || failed != 1 {
		t.Fatalf("concurrent redemption success=%v failed=%d", success != nil, failed)
	}
	if success.User.Role != "viewer" {
		t.Fatalf("assigned role = %q", success.User.Role)
	}
	if _, _, ok := svc.ResolveSession(ctx, success.RawToken); !ok {
		t.Fatal("registration session did not resolve")
	}
	if _, err := svc.PreviewInvitation(ctx, token); err != ErrInvitationUnavailable {
		t.Fatalf("replay preview error = %v", err)
	}

	revoked, revokeToken, err := svc.CreateInvitation(ctx, sys, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeInvitation(ctx, sys, revoked.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewInvitation(ctx, revokeToken); err != ErrInvitationUnavailable {
		t.Fatalf("revoked preview error = %v", err)
	}

	expired, expiredToken, err := svc.CreateInvitation(ctx, sys, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE account_invitations SET created_at = now() - interval '2 seconds', expires_at = now() - interval '1 second' WHERE id = $1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewInvitation(ctx, expiredToken); err != ErrInvitationUnavailable {
		t.Fatalf("expired preview error = %v", err)
	}

	survivor, err := svc.CreateUser(ctx, sys, "survivor"+suffix, testPassword, "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, survivor.ID)
	issuer, err := svc.CreateUser(ctx, sys, adminHandle, testPassword, "admin")
	if err != nil {
		t.Fatal(err)
	}
	issuerInv, issuerToken, err := svc.CreateInvitation(ctx, issuer.Actor(), "editor")
	if err != nil {
		t.Fatal(err)
	}
	demote := "editor"
	if _, err := svc.UpdateUser(ctx, sys, issuer.ID, UpdateUserPatch{Role: &demote}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewInvitation(ctx, issuerToken); err != ErrInvitationUnavailable {
		t.Fatalf("demoted issuer preview error = %v (invitation %d)", err, issuerInv.ID)
	}

	disabledIssuer, err := svc.CreateUser(ctx, sys, "disabledissuer"+suffix, testPassword, "admin")
	if err != nil {
		t.Fatal(err)
	}
	disabledInv, disabledToken, err := svc.CreateInvitation(ctx, disabledIssuer.Actor(), "editor")
	if err != nil {
		t.Fatal(err)
	}
	disabled := true
	if _, err := svc.UpdateUser(ctx, sys, disabledIssuer.ID, UpdateUserPatch{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewInvitation(ctx, disabledToken); err != ErrInvitationUnavailable {
		t.Fatalf("disabled issuer preview error = %v (invitation %d)", err, disabledInv.ID)
	}

	local := Actor{ID: LocalActorID}
	localInv, localToken, err := svc.CreateInvitation(ctx, local, "editor")
	if err != nil {
		t.Fatalf("local legacy actor could not create invitation: %v", err)
	}
	localLogin, err := svc.RegisterWithInvitation(ctx, localToken, "localredeem"+suffix, testPassword)
	if err != nil {
		t.Fatalf("local-issued invitation could not be redeemed: %v", err)
	}
	if localLogin.User.Role != "editor" {
		t.Fatalf("local-issued invitation role = %q, want editor", localLogin.User.Role)
	}
	if _, err := svc.PreviewInvitation(ctx, localToken); err != ErrInvitationUnavailable {
		t.Fatalf("local-issued replay preview error = %v (invitation %d)", err, localInv.ID)
	}
}
