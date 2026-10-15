package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

func writeExportResult(result client.ExportResult, cfg commonFlags, stdout, stderr io.Writer) int {
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		return encodeJSONL(stdout, result, stderr)
	}
	fmt.Fprintf(stdout, "%s\t%d bytes\n", result.Path, result.Bytes)
	return 0
}

func parseExportIDs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("--ids requires 1 to 1000 post ids")
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 1000 {
		return nil, fmt.Errorf("--ids requires 1 to 1000 post ids")
	}
	seen := make(map[string]bool, len(parts))
	ids := make([]string, len(parts))
	for i, part := range parts {
		id, err := parsePositiveID(part, "post id")
		if err != nil {
			return nil, err
		}
		// Reject surrounding whitespace in the comma list: each element must
		// already be canonical.
		if part != id {
			return nil, fmt.Errorf("post id %q must be a canonical positive integer", part)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate post id %q", id)
		}
		seen[id] = true
		ids[i] = id
	}
	return ids, nil
}

func runExport(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := newCommonFlags(getenv)
	var idsText, output string
	var metadata bool
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--ids":      valueFlag(func(v string) { idsText = v }),
		"--output":   valueFlag(func(v string) { output = v }),
		"--metadata": boolFlag(func() { metadata = true }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, exportHelp)
		return 0
	}
	if err == nil && len(positional) != 0 {
		err = fmt.Errorf("export does not accept positional arguments")
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, exportHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	ids, err := parseExportIDs(idsText)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if strings.TrimSpace(output) == "" {
		fmt.Fprintln(stderr, "error: flag --output is required")
		return 1
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	result, err := c.ExportPosts(ctx, ids, metadata, output)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return writeExportResult(result, cfg, stdout, stderr)
}

func runCollectionExport(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := newCommonFlags(getenv)
	var versionText, output string
	var versionSet, metadata bool
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--version":  valueFlag(func(v string) { versionSet = true; versionText = v }),
		"--output":   valueFlag(func(v string) { output = v }),
		"--metadata": boolFlag(func() { metadata = true }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, collectionExportHelp)
		return 0
	}
	if err == nil && len(positional) != 1 {
		err = fmt.Errorf("collection export requires exactly one COLLECTION_ID")
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, collectionExportHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	if !versionSet {
		fmt.Fprintln(stderr, "error: flag --version is required")
		return 1
	}
	version, err := parseVersionFlag("--version", versionText, false)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	id, err := parsePositiveID(positional[0], "collection id")
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if strings.TrimSpace(output) == "" {
		fmt.Fprintln(stderr, "error: flag --output is required")
		return 1
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	result, err := c.ExportCollection(ctx, id, int64(version), metadata, output)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return writeExportResult(result, cfg, stdout, stderr)
}
