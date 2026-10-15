package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

// flagSpec describes one flag: whether it consumes a value and how to store it.
type flagSpec struct {
	takesValue bool
	set        func(string)
}

func boolFlag(set func()) flagSpec {
	return flagSpec{set: func(string) { set() }}
}

func valueFlag(set func(string)) flagSpec {
	return flagSpec{takesValue: true, set: set}
}

// parseFlagArgs consumes flags before the first positional argument and
// returns the positional remainder verbatim. A "--" ends flag parsing, and
// everything after the first positional is literal, preserving the CLI's
// documented flags-before-args contract without a command framework.
func parseFlagArgs(args []string, specs map[string]flagSpec) ([]string, error) {
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			return append([]string{}, args[i+1:]...), nil
		}
		if arg == "-h" || arg == "--help" {
			return nil, errHelpRequested
		}
		if !strings.HasPrefix(arg, "-") {
			return append([]string{}, args[i:]...), nil
		}
		name, value, hasValue := arg, "", false
		if eq := strings.Index(arg, "="); eq >= 0 {
			name, value, hasValue = arg[:eq], arg[eq+1:], true
		}
		if name == "--password" {
			return nil, fmt.Errorf("password cannot be passed as a flag; it is read from the terminal or stdin")
		}
		spec, ok := specs[name]
		if !ok || (hasValue && !spec.takesValue) {
			return nil, fmt.Errorf("unknown flag %q", arg)
		}
		if spec.takesValue {
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %s requires a value", name)
				}
				value = args[i+1]
				i++
			}
			spec.set(value)
		} else {
			spec.set("")
		}
		i++
	}
	return nil, nil
}

// commonFlags holds the flags every command shares.
type commonFlags struct {
	apiURL      string
	apiToken    string
	apiTokenSet bool
	jsonOut     bool
	jsonlOut    bool
}

func newCommonFlags(getenv func(string) string) commonFlags {
	apiURL := defaultAPIURL
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			apiURL = envURL
		}
	}
	return commonFlags{apiURL: apiURL}
}

func (c *commonFlags) specs(extra map[string]flagSpec) map[string]flagSpec {
	specs := map[string]flagSpec{
		"--json":    boolFlag(func() { c.jsonOut = true }),
		"--jsonl":   boolFlag(func() { c.jsonlOut = true }),
		"--api-url": valueFlag(func(v string) { c.apiURL = strings.TrimSpace(v) }),
		"--api-token": valueFlag(func(v string) {
			c.apiToken = strings.TrimSpace(v)
			c.apiTokenSet = true
		}),
	}
	for name, spec := range extra {
		specs[name] = spec
	}
	return specs
}

// checkExclusive reports the --json/--jsonl conflict, if any.
func (c *commonFlags) checkExclusive(stderr io.Writer) bool {
	if c.jsonOut && c.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return false
	}
	return true
}

func (c *commonFlags) client(ctx context.Context, getenv func(string) string, stderr io.Writer) (*client.Client, bool) {
	if c.apiTokenSet && c.apiToken == "" {
		fmt.Fprintln(stderr, "error: --api-token requires a non-empty value")
		return nil, false
	}
	flagToken := ""
	if c.apiTokenSet {
		flagToken = c.apiToken
	}
	cl, err := newAPIClient(ctx, c.apiURL, flagToken, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return nil, false
	}
	return cl, true
}

// parsePositiveID validates the canonical positive-integer ID form shared by
// post and collection positional arguments.
func parsePositiveID(raw, label string) (string, error) {
	id := strings.TrimSpace(raw)
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || id != strconv.FormatInt(n, 10) {
		return "", fmt.Errorf("%s must be a positive integer", label)
	}
	return id, nil
}

// parseVersionFlag parses a required-style version flag value. The returned
// error names the flag so callers can print it directly after "error: ".
func parseVersionFlag(flag, raw string, positive bool) (int, error) {
	value := strings.TrimSpace(raw)
	n, err := strconv.Atoi(value)
	kind := "non-negative"
	if positive {
		kind = "positive"
	}
	if err != nil || value != strconv.Itoa(n) || (positive && n <= 0) || (!positive && n < 0) {
		return 0, fmt.Errorf("invalid %s %q: must be a %s integer", flag, raw, kind)
	}
	return n, nil
}
