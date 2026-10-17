// Command kura-account is the offline account tool. bootstrap creates the
// initial login admin and refuses once one exists. reset-password replaces
// the password of an account that already has a login. Neither command
// accepts the password as an argument, so it cannot leak through argv or
// shell history.
//
// Usage (local only, against the same database the server uses):
//
//	DATABASE_URL=postgres://... kura-account bootstrap --handle owner
//	DATABASE_URL=postgres://... kura-account bootstrap --adopt-legacy local
//	DATABASE_URL=postgres://... kura-account reset-password --handle owner
//
// The password (12..72 bytes) is read from stdin, e.g. from a prompt or a
// local secret file. --adopt-legacy local|system gives that existing
// legacy user the password and admin role, retaining its actor key (and
// saved searches) as the initial owner. Bootstrap refuses once any
// enabled admin with a login exists. reset-password does not create
// accounts, grant admin, or change role or disabled state.
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
	return execute(args, os.Stdin, os.Getenv("DATABASE_URL"))
}

func execute(args []string, stdin io.Reader, databaseURL string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "bootstrap":
		return bootstrap(args[1:], stdin, databaseURL)
	case "reset-password":
		return resetPassword(args[1:], stdin, databaseURL)
	default:
		return usageError()
	}
}

func usageError() error {
	return fmt.Errorf("usage: kura-account bootstrap [--handle NAME] [--adopt-legacy local|system] | kura-account reset-password --handle NAME")
}

func bootstrap(args []string, stdin io.Reader, databaseURL string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	handle := fs.String("handle", "", "login handle for the new admin (must match --adopt-legacy when adopting)")
	adopt := fs.String("adopt-legacy", "", "adopt existing legacy actor as initial owner: local or system")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments (the password is read from stdin, never from argv)")
	}
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	password, err := readPassword(stdin)
	if err != nil {
		return err
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

func resetPassword(args []string, stdin io.Reader, databaseURL string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	handle := fs.String("handle", "", "login handle of the existing account")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments (the password is read from stdin, never from argv)")
	}
	if strings.TrimSpace(*handle) == "" {
		return fmt.Errorf("handle is required")
	}
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	password, err := readPassword(stdin)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	service := auth.NewService(auth.NewPostgresStore(pool))
	user, err := service.ResetPassword(ctx, *handle, password)
	if err != nil {
		return err
	}
	fmt.Printf("reset password id=%d handle=%s role=%s\n", user.ID, user.Handle, user.Role)
	return nil
}

func readPassword(r io.Reader) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	// Only the trailing newline from a prompt/pipe is stripped; spaces in
	// the password itself are preserved.
	password := strings.TrimRight(string(raw), "\r\n")
	if password == "" {
		return "", fmt.Errorf("password from stdin is empty")
	}
	return password, nil
}
