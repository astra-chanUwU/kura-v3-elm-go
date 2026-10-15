package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

type searchConfig struct {
	commonFlags
	allPages  bool
	limitSet  bool
	limitVal  string
	cursorSet bool
	cursorVal string
}

func runSearch(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := searchConfig{commonFlags: newCommonFlags(getenv)}

	query, err := parseSearchArgs(args, &cfg)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, searchHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, searchHelp)
		return 1
	}

	if !cfg.checkExclusive(stderr) {
		return 1
	}

	limit, cursor, err := paginationArgs(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}

	return readPostPages(ctx, cfg, cursor, func(ctx context.Context, cursor string) (client.SearchResponse, error) {
		return c.SearchPage(ctx, query, cursor, limit)
	}, stdout, stderr)
}

var errHelpRequested = fmt.Errorf("help requested")

func parseSearchArgs(args []string, cfg *searchConfig) (string, error) {
	remaining, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--all": boolFlag(func() { cfg.allPages = true }),
		"--limit": valueFlag(func(v string) {
			cfg.limitSet = true
			cfg.limitVal = v
		}),
		"--cursor": valueFlag(func(v string) {
			cfg.cursorSet = true
			cfg.cursorVal = v
		}),
	}))
	if err != nil {
		return "", err
	}
	// Remaining args are query parts, joined with a space.
	return strings.Join(remaining, " "), nil
}
