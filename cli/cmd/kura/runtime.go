package main

import (
	"fmt"
	"io"
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

func newAPIClient(base string, getenv func(string) string) (*client.Client, error) {
	if err := client.ValidateBaseURL(base); err != nil {
		return nil, fmt.Errorf("invalid --api-url: %w", err)
	}
	c := client.New(base)
	// The supplied environment is authoritative, including absent values.
	// New defaults PrivateReads to false and never reads ambient environment,
	// so only the getenv passed here can opt into private reads.
	c.APIToken = ""
	c.PrivateReads = false
	if getenv != nil {
		c.APIToken = strings.TrimSpace(getenv("KURA_API_TOKEN"))
		if raw := strings.TrimSpace(getenv("KURA_PRIVATE_READS")); raw != "" {
			enabled, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("KURA_PRIVATE_READS must be a boolean (true/false/1/0), got %q", raw)
			}
			if enabled {
				if c.APIToken == "" {
					return nil, fmt.Errorf("KURA_PRIVATE_READS=true requires KURA_API_TOKEN")
				}
				c.PrivateReads = true
			}
		}
		if value := strings.TrimSpace(getenv("KURA_HTTP_TIMEOUT")); value != "" {
			timeout, err := time.ParseDuration(value)
			if err != nil || timeout <= 0 {
				return nil, fmt.Errorf("KURA_HTTP_TIMEOUT must be a positive duration, such as 10s or 1m")
			}
			c.HTTPClient.Timeout = timeout
		}
	}
	return c, nil
}
