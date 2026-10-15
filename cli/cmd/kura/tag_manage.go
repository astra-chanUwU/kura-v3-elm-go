package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

type tagManageConfig struct {
	commonFlags
	prefix     string
	limit      string
	cursor     string
	previewSet bool
	version    string
	versionSet bool
}

func runTag(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, tagHelp)
		return 0
	}
	subcommand := args[0]
	subargs := args[1:]
	if subcommand != "list" && subcommand != "rename" && subcommand != "merge" {
		fmt.Fprintf(stderr, "error: unknown tag command %q\n", subcommand)
		fmt.Fprint(stderr, tagHelp)
		return 1
	}
	cfg := tagManageConfig{commonFlags: newCommonFlags(getenv), limit: "60"}
	if subcommand == "list" {
		return runTagList(ctx, subargs, cfg, stdout, stderr, getenv)
	}
	return runTagMutation(ctx, subcommand, subargs, cfg, stdout, stderr, getenv)
}

func runTagList(ctx context.Context, args []string, cfg tagManageConfig, stdout, stderr io.Writer, getenv func(string) string) int {
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--prefix": valueFlag(func(v string) { cfg.prefix = v }),
		"--limit":  valueFlag(func(v string) { cfg.limit = v }),
		"--cursor": valueFlag(func(v string) { cfg.cursor = v }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, tagListHelp)
		return 0
	}
	if err == nil && len(positional) != 0 {
		err = fmt.Errorf("tag list does not accept positional arguments")
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, tagListHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	limit, err := strconv.Atoi(strings.TrimSpace(cfg.limit))
	if err != nil || limit < 1 || limit > 60 {
		fmt.Fprintln(stderr, "error: --limit must be an integer between 1 and 60")
		return 1
	}
	if strings.TrimSpace(cfg.cursor) == "" && cfg.cursor != "" {
		fmt.Fprintln(stderr, "error: flag --cursor requires a non-empty value")
		return 1
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	result, err := c.ListTagsPage(ctx, cfg.prefix, cfg.cursor, limit)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		if code := encodeJSONLList(stdout, result.Tags, stderr); code != 0 {
			return code
		}
		if result.NextCursor != nil {
			fmt.Fprintf(stderr, "next_cursor: %s\n", *result.NextCursor)
		}
		return 0
	}
	for _, tag := range result.Tags {
		fmt.Fprintf(stdout, "%s\tposts:%d\tvisible:%d\n", tag.Tag, tag.PostCount, tag.VisiblePostCount)
	}
	if result.NextCursor != nil {
		fmt.Fprintf(stderr, "next_cursor: %s\n", *result.NextCursor)
	}
	return 0
}

func runTagMutation(ctx context.Context, mode string, args []string, cfg tagManageConfig, stdout, stderr io.Writer, getenv func(string) string) int {
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--preview": boolFlag(func() { cfg.previewSet = true }),
		"--version": valueFlag(func(v string) { cfg.versionSet = true; cfg.version = strings.TrimSpace(v) }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, tagMutationHelp(mode))
		return 0
	}
	if err == nil && len(positional) != 2 {
		err = fmt.Errorf("tag %s requires exactly SOURCE and TARGET", mode)
	}
	if err == nil && (strings.TrimSpace(positional[0]) == "" || strings.TrimSpace(positional[1]) == "") {
		err = fmt.Errorf("source and target tags must be non-empty")
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, tagMutationHelp(mode))
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	if cfg.previewSet && cfg.versionSet {
		fmt.Fprintln(stderr, "error: --preview and --version cannot be used together")
		return 1
	}
	var version int64
	if cfg.versionSet {
		version, err = parseTagVersion(cfg.version)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	if !cfg.versionSet {
		result, err := c.PreviewTagManage(ctx, mode, positional[0], positional[1])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return writeTagManageResult(stdout, stderr, cfg, "preview", result, nil)
	}
	result, err := c.ApplyTagManage(ctx, mode, positional[0], positional[1], version)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return writeTagManageResult(stdout, stderr, cfg, "apply", client.TagManageResult{}, &result)
}

func writeTagManageResult(stdout, stderr io.Writer, cfg tagManageConfig, kind string, result client.TagManageResult, applied *client.TagManageApplyResult) int {
	if applied != nil {
		if cfg.jsonOut {
			return encodeJSON(stdout, applied)
		}
		if cfg.jsonlOut {
			return encodeJSONL(stdout, applied, stderr)
		}
		fmt.Fprintf(stdout, "%s\t%s -> %s\tversion:%d\tchanged:%v\taffected:%d\tvisible:%d\n", kind, applied.SourceTag, applied.TargetTag, applied.Version, applied.Changed, applied.AffectedPostCount, applied.VisiblePostCount)
		return 0
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		return encodeJSONL(stdout, result, stderr)
	}
	fmt.Fprintf(stdout, "%s\t%s -> %s\tversion:%d\taffected:%d\tvisible:%d\ttarget:%d\n", kind, result.SourceTag, result.TargetTag, result.Version, result.AffectedPostCount, result.VisiblePostCount, result.TargetPostCount)
	return 0
}

func parseTagVersion(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || value != strconv.FormatInt(n, 10) || n < 0 {
		return 0, fmt.Errorf("invalid --version %q: must be a non-negative int64 integer", raw)
	}
	return n, nil
}
