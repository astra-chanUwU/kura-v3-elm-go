package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

type memorySessionKey struct{}

func withMemorySession(ctx context.Context, session storedSession) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, memorySessionKey{}, session)
}

func memorySessionFrom(ctx context.Context) (storedSession, bool) {
	if ctx == nil {
		return storedSession{}, false
	}
	session, ok := ctx.Value(memorySessionKey{}).(storedSession)
	return session, ok
}

const loginHelp = `kura login - sign in as a named account

Usage:
  kura login --handle NAME [--persist]
  kura login --handle NAME -- COMMAND [ARGS...]

Reads the password from the terminal without echo, or from one line of stdin
when stdin is not a terminal. The password is never accepted as a flag or
argument.

--persist stores the session cookie in a user-private credential file
(KURA_SESSION_FILE, or the default file under the user config directory).
Without --persist the session stays in this process only. Pass a command
after -- to run it with that in-memory session; the session is revoked when
the process exits unless --persist was set.

The saved session is the same role-limited account cookie the browser uses.
It is sent on later commands only when no deployment token is configured.

Precedence for existing commands:
  1. --api-token VALUE     deployment admin token (overrides the environment)
  2. KURA_API_TOKEN        deployment admin token
  3. in-memory login       this process only
  4. persisted login       credential file from --persist
  A deployment token is not combined with the account session.

Cookie writes send Origin from KURA_AUTH_TRUSTED_ORIGIN when that exact
origin is set, otherwise the origin of the API URL. The server accepts the
write only when that origin matches its trusted origin.

Flags:
  --handle NAME     Account handle (required)
  --persist         Save the session for later commands
  --api-url URL     Base API URL (env KURA_API_URL)
  --api-token VALUE Deployment admin token; overrides KURA_API_TOKEN
  --json            Output the login status as JSON (no session material)
  --jsonl           Output the login status as one JSON Lines object
  -h, --help        Show this help
`

const logoutHelp = `kura logout - revoke the logged-in account session

Usage:
  kura logout [flags]

Revokes the persisted account session and deletes the credential file.
A deployment token in --api-token or KURA_API_TOKEN is left unchanged.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL)
  --json            Output the logout status as JSON
  --jsonl           Output the logout status as one JSON Lines object
  -h, --help        Show this help
`

type loginConfig struct {
	commonFlags
	handle  string
	persist bool
}

func runLogin(ctx context.Context, args []string, stdout, stderr io.Writer, stdin io.Reader, getenv func(string) string) int {
	flagArgs, command, explicitCommand := splitLoginCommand(args)
	cfg := loginConfig{commonFlags: newCommonFlags(getenv)}
	positional, err := parseFlagArgs(flagArgs, cfg.specs(map[string]flagSpec{
		"--handle":  valueFlag(func(v string) { cfg.handle = strings.TrimSpace(v) }),
		"--persist": boolFlag(func() { cfg.persist = true }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, loginHelp)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, loginHelp)
		return 1
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "error: unexpected arguments; the password is read from the terminal or stdin, never from arguments")
		fmt.Fprint(stderr, loginHelp)
		return 1
	}
	if explicitCommand && len(command) == 0 {
		fmt.Fprintln(stderr, "error: missing command after --")
		fmt.Fprint(stderr, loginHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	if cfg.handle == "" {
		fmt.Fprintln(stderr, "error: flag --handle is required")
		fmt.Fprint(stderr, loginHelp)
		return 1
	}
	if cfg.apiTokenSet && cfg.apiToken == "" {
		fmt.Fprintln(stderr, "error: --api-token requires a non-empty value")
		return 1
	}
	password, err := readPassword(stdin, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	api, err := loginClient(cfg.apiURL, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	loggedIn, err := api.Login(ctx, cfg.handle, password)
	password = ""
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", redactLogin(err, loggedIn))
		return 1
	}
	session := storedSession{
		Version:    1,
		APIURL:     api.BaseURL,
		CookieName: loggedIn.CookieName,
		Cookie:     loggedIn.CookieValue,
		ExpiresAt:  loggedIn.ExpiresAt,
		Handle:     loggedIn.Handle,
		Role:       loggedIn.Role,
	}
	path := sessionFileFromEnv(getenv)
	if cfg.persist {
		if path == "" {
			_ = revokeQuiet(session, getenv)
			fmt.Fprintln(stderr, "error: no credential file configured")
			return 1
		}
		if old, err := readStoredSession(path); err == nil {
			_ = revokeQuiet(old, getenv)
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = revokeQuiet(session, getenv)
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		if err := writeStoredSession(path, session); err != nil {
			_ = revokeQuiet(session, getenv)
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	}
	code := 0
	if len(command) > 0 {
		code = dispatch(withMemorySession(ctx, session), command, stdout, stderr, stdin, getenv)
	} else if cfg.jsonOut {
		code = encodeJSON(stdout, loginStatus(session, cfg.persist))
	} else if cfg.jsonlOut {
		code = encodeJSONL(stdout, loginStatus(session, cfg.persist), stderr)
	} else {
		fmt.Fprintf(stdout, "authenticated as %s (%s)\n", session.Handle, session.Role)
		if cfg.persist {
			fmt.Fprintln(stdout, "account session saved")
		} else {
			fmt.Fprintln(stdout, "account session was not saved; pass --persist to reuse it")
		}
	}
	if !cfg.persist {
		if err := revokeQuiet(session, getenv); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}
	return code
}

func loginStatus(session storedSession, persisted bool) map[string]any {
	return map[string]any{
		"authenticated": true,
		"handle":        session.Handle,
		"role":          session.Role,
		"persisted":     persisted,
	}
}

func redactLogin(err error, session client.AccountLogin) error {
	if err == nil {
		return nil
	}
	text := redactText(err.Error(), []string{session.CookieValue, session.CookieName + "=" + session.CookieValue})
	return fmt.Errorf("%s", text)
}

func redactText(msg string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" && secret != "=" {
			msg = strings.ReplaceAll(msg, secret, "[redacted]")
		}
	}
	return msg
}

func splitLoginCommand(args []string) (flags, command []string, explicit bool) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], append([]string{}, args[i+1:]...), true
		}
	}
	return args, nil, false
}

func loginClient(apiURL string, getenv func(string) string) (*client.Client, error) {
	return newAPIClient(context.Background(), apiURL, "", func(key string) string {
		switch key {
		case "KURA_API_TOKEN", "KURA_PRIVATE_READS", sessionFileEnv:
			return ""
		default:
			if getenv == nil {
				return ""
			}
			return getenv(key)
		}
	})
}

func revokeQuiet(session storedSession, getenv func(string) string) error {
	api, err := clientForSession(session, getenv)
	if err != nil {
		return err
	}
	timeout := 15 * time.Second
	if api.HTTPClient != nil && api.HTTPClient.Timeout > 0 && api.HTTPClient.Timeout < timeout {
		timeout = api.HTTPClient.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return api.Logout(ctx)
}

func clientForSession(session storedSession, getenv func(string) string) (*client.Client, error) {
	api, err := newAPIClient(context.Background(), session.APIURL, "", func(key string) string {
		switch key {
		case "KURA_API_TOKEN", "KURA_PRIVATE_READS", sessionFileEnv:
			return ""
		default:
			if getenv == nil {
				return ""
			}
			return getenv(key)
		}
	})
	if err != nil {
		return nil, err
	}
	api.APIToken = ""
	api.PrivateReads = false
	api.SessionCookieName = session.CookieName
	api.SessionCookieValue = session.Cookie
	return api, nil
}

func runLogout(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := loginConfig{commonFlags: newCommonFlags(getenv)}
	positional, err := parseFlagArgs(args, cfg.specs(nil))
	if err == errHelpRequested {
		fmt.Fprint(stdout, logoutHelp)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, logoutHelp)
		return 1
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "error: logout does not accept arguments")
		fmt.Fprint(stderr, logoutHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	var sessions []storedSession
	if memory, ok := memorySessionFrom(ctx); ok {
		sessions = append(sessions, memory)
	}
	path := sessionFileFromEnv(getenv)
	removeFile := false
	if path != "" {
		stored, err := readStoredSession(path)
		switch {
		case err == nil:
			removeFile = true
			if !sameSession(memoryFrom(sessions), stored) {
				sessions = append(sessions, stored)
			}
		case errors.Is(err, os.ErrNotExist):
		default:
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	}
	if len(sessions) == 0 {
		if cfg.jsonOut {
			return encodeJSON(stdout, map[string]any{"logged_out": false})
		}
		if cfg.jsonlOut {
			return encodeJSONL(stdout, map[string]any{"logged_out": false}, stderr)
		}
		fmt.Fprintln(stdout, "no account session")
		return 0
	}
	var revokeErr error
	for _, session := range sessions {
		if err := revokeQuiet(session, getenv); err != nil {
			revokeErr = err
		}
	}
	if revokeErr != nil {
		fmt.Fprintf(stderr, "error: %v\n", revokeErr)
		return 1
	}
	if removeFile {
		if err := removeStoredSession(path); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, map[string]any{"logged_out": true})
	}
	if cfg.jsonlOut {
		return encodeJSONL(stdout, map[string]any{"logged_out": true}, stderr)
	}
	fmt.Fprintln(stdout, "logged out")
	return 0
}

func memoryFrom(sessions []storedSession) storedSession {
	if len(sessions) == 0 {
		return storedSession{}
	}
	return sessions[0]
}

func sameSession(a, b storedSession) bool {
	return a.CookieName != "" && a.CookieName == b.CookieName && a.Cookie == b.Cookie && normalizeAPI(a.APIURL) == normalizeAPI(b.APIURL)
}

func normalizeAPI(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
