package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

// checkedWriter records the first output failure, including short writes.
// Returning the failure on later writes prevents continued output after failure.
type checkedWriter struct {
	writer  io.Writer
	err     error
	written int
}

func (w *checkedWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(p)
	w.written += n
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func newAPIClient(ctx context.Context, base, flagToken string, getenv func(string) string) (*client.Client, error) {
	if err := client.ValidateBaseURL(base); err != nil {
		return nil, fmt.Errorf("invalid --api-url: %w", err)
	}
	c := client.New(base)
	// The supplied environment is authoritative, including absent values.
	// New defaults PrivateReads to false and never reads ambient environment,
	// so only the getenv passed here can opt into private reads.
	// Deployment-token precedence is flag, then KURA_API_TOKEN. Either one
	// selects the deployment admin identity and skips the account session.
	c.APIToken = ""
	c.PrivateReads = false
	c.SessionCookieName = ""
	c.SessionCookieValue = ""
	c.Origin = ""
	token := strings.TrimSpace(flagToken)
	if token == "" && getenv != nil {
		token = strings.TrimSpace(getenv("KURA_API_TOKEN"))
	}
	c.APIToken = token
	wantPrivate := false
	if getenv != nil {
		if raw := strings.TrimSpace(getenv("KURA_PRIVATE_READS")); raw != "" {
			enabled, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("KURA_PRIVATE_READS must be a boolean (true/false/1/0), got %q", raw)
			}
			wantPrivate = enabled
		}
		if value := strings.TrimSpace(getenv("KURA_HTTP_TIMEOUT")); value != "" {
			timeout, err := time.ParseDuration(value)
			if err != nil || timeout <= 0 {
				return nil, fmt.Errorf("KURA_HTTP_TIMEOUT must be a positive duration, such as 10s or 1m")
			}
			c.HTTPClient.Timeout = timeout
		}
		if origin := strings.TrimSpace(getenv("KURA_AUTH_TRUSTED_ORIGIN")); origin != "" {
			if !client.ValidOrigin(origin) {
				return nil, fmt.Errorf("KURA_AUTH_TRUSTED_ORIGIN must be an exact origin like https://library.example")
			}
			c.Origin = origin
		}
	}
	if c.APIToken != "" {
		if wantPrivate {
			c.PrivateReads = true
		}
		return c, nil
	}
	session, err := resolveAccountSession(ctx, c.BaseURL, getenv)
	if err != nil {
		return nil, err
	}
	if session.Cookie != "" {
		c.SessionCookieName = session.CookieName
		c.SessionCookieValue = session.Cookie
		return c, nil
	}
	if wantPrivate {
		return nil, fmt.Errorf("KURA_PRIVATE_READS=true requires KURA_API_TOKEN")
	}
	return c, nil
}

func resolveAccountSession(ctx context.Context, apiURL string, getenv func(string) string) (storedSession, error) {
	if memory, ok := memorySessionFrom(ctx); ok {
		return bindSession(memory, apiURL)
	}
	path := sessionFileFromEnv(getenv)
	if path == "" {
		return storedSession{}, nil
	}
	session, err := readStoredSession(path)
	if errors.Is(err, os.ErrNotExist) {
		return storedSession{}, nil
	}
	if err != nil {
		return storedSession{}, err
	}
	return bindSession(session, apiURL)
}

func bindSession(session storedSession, apiURL string) (storedSession, error) {
	if normalizeAPI(session.APIURL) != normalizeAPI(apiURL) {
		return storedSession{}, fmt.Errorf("account session is for %s", session.APIURL)
	}
	if session.expired(time.Now()) {
		return storedSession{}, fmt.Errorf("account session expired; run kura login")
	}
	return session, nil
}
