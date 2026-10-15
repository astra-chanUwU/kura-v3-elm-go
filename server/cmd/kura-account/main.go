// Command kura-account is the offline account bootstrap tool. It is the
// only way to create the initial login admin; there is no HTTP
// registration. The password is read from stdin and never accepted as an
// argument, so it cannot leak through argv or shell history.
//
// Usage (local only, against the same database the server uses):
//
//	DATABASE_URL=postgres://... kura-account bootstrap --handle owner
//	DATABASE_URL=postgres://... kura-account bootstrap --adopt-legacy local
//
// The password (12..72 bytes) is read from stdin, e.g. from a prompt or a
// local secret file. --adopt-legacy local|system gives that existing
// legacy user the password and admin role, retaining its actor key (and
// saved searches) as the initial owner. Bootstrap refuses once any
// enabled admin with a login exists.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kura-account:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "bootstrap" {
		return fmt.Errorf("usage: kura-account bootstrap [--handle NAME] [--adopt-legacy local|system]")
	}
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	handle := fs.String("handle", "", "login handle for the new admin (must match --adopt-legacy when adopting)")
	adopt := fs.String("adopt-legacy", "", "adopt existing legacy actor as initial owner: local or system")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments (the password is read from stdin, never from argv)")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	// Only the trailing newline from a prompt/pipe is stripped; spaces in
	// the password itself are preserved.
	password := strings.TrimRight(string(raw), "\r\n")
	if password == "" {
		return fmt.Errorf("password from stdin is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	service := auth.NewService(auth.NewPostgresStore(pool))
	user, err := service.BootstrapAdmin(ctx, *handle, password, *adopt)
	if err != nil {
		return err
	}
	fmt.Printf("created initial admin id=%d handle=%s actor=%s\n", user.ID, user.Handle, user.Actor().ID)
	return nil
}
