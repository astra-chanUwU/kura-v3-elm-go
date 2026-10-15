package auth

import (
	"context"
	"fmt"
	"os"
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
