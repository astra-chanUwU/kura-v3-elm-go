package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

func parseDistinctPostIDs(raw []string, max int) ([]string, error) {
	if len(raw) < 1 || len(raw) > max {
		return nil, fmt.Errorf("requires 1 to %d post ids", max)
	}
	seen := make(map[string]bool, len(raw))
	ids := make([]string, len(raw))
	for i, value := range raw {
		id, err := parsePositiveID(value, "post id")
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate post id %q", id)
		}
		seen[id] = true
		ids[i] = id
	}
	return ids, nil
}

func validateMetadataValue(label, value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%s must not contain NUL", label)
	}
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > 512 {
		return "", fmt.Errorf("%s must be at most 512 characters", label)
	}
	return trimmed, nil
}

func runPostMetadata(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := newCommonFlags(getenv)
	var versionText, sourceText, artistText string
	var versionSet, sourceSet, artistSet bool
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--version": valueFlag(func(v string) { versionSet = true; versionText = v }),
		"--source":  valueFlag(func(v string) { sourceSet = true; sourceText = v }),
		"--artist":  valueFlag(func(v string) { artistSet = true; artistText = v }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, postMetadataHelp)
		return 0
	}
	if err == nil && len(positional) != 1 {
		err = fmt.Errorf("post metadata requires exactly one POST_ID")
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postMetadataHelp)
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
	if err != nil || version > 2147483647 {
		fmt.Fprintln(stderr, "error: --version must be an integer between 0 and 2147483647")
		return 1
	}
	if !sourceSet && !artistSet {
		fmt.Fprintln(stderr, "error: at least one of --source or --artist is required")
		return 1
	}
	id, err := parsePositiveID(positional[0], "post id")
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	var sourcePtr, artistPtr *string
	if sourceSet {
		value, err := validateMetadataValue("--source", sourceText)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		sourcePtr = &value
	}
	if artistSet {
		value, err := validateMetadataValue("--artist", artistText)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		artistPtr = &value
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	result, err := c.EditMetadata(ctx, client.MetadataRequest{
		Posts:  []client.MetadataTarget{{ID: id, Version: version}},
		Source: sourcePtr,
		Artist: artistPtr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		return encodeJSONLList(stdout, result.Posts, stderr)
	}
	for _, post := range result.Posts {
		fmt.Fprintf(stdout, "%s\tversion:%d\tchanged:%v\tsource:%s\tartist:%s\n", post.ID, post.Version, post.Changed, post.Source, post.Artist)
	}
	return 0
}

func runPostTrash(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return runTrashChange(ctx, args, stdout, stderr, getenv, true)
}

func runPostRestore(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return runTrashChange(ctx, args, stdout, stderr, getenv, false)
}

func runTrashChange(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, trash bool) int {
	cfg := newCommonFlags(getenv)
	help := postRestoreHelp
	extra := map[string]flagSpec{}
	var reasonText string
	var reasonSet bool
	if trash {
		help = postTrashHelp
		extra["--reason"] = valueFlag(func(v string) { reasonSet = true; reasonText = v })
	}
	positional, err := parseFlagArgs(args, cfg.specs(extra))
	if err == errHelpRequested {
		fmt.Fprint(stdout, help)
		return 0
	}
	name := "restore"
	if trash {
		name = "trash"
	}
	if err == nil && (len(positional) < 1 || len(positional) > 100) {
		err = fmt.Errorf("post %s requires 1 to 100 post ids", name)
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, help)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	ids, err := parseDistinctPostIDs(positional, 100)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	var reasonPtr *string
	if trash && reasonSet {
		value, err := validateMetadataValue("--reason", reasonText)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		reasonPtr = &value
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	var result client.TrashResponse
	if trash {
		result, err = c.TrashPosts(ctx, ids, reasonPtr)
	} else {
		result, err = c.RestorePosts(ctx, ids)
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		return encodeJSONLList(stdout, result.Posts, stderr)
	}
	for _, post := range result.Posts {
		fmt.Fprintf(stdout, "%s\tchanged:%v\n", post.ID, post.Changed)
	}
	return 0
}

func runPostTrashList(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := newCommonFlags(getenv)
	var limitText, cursorText string
	var limitSet, cursorSet bool
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--limit":  valueFlag(func(v string) { limitSet = true; limitText = v }),
		"--cursor": valueFlag(func(v string) { cursorSet = true; cursorText = v }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, postTrashListHelp)
		return 0
	}
	if err == nil && len(positional) != 0 {
		err = fmt.Errorf("post trash-list does not accept positional arguments")
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postTrashListHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	limit := 0
	if limitSet {
		n, err := strconv.Atoi(strings.TrimSpace(limitText))
		if err != nil || n < 1 || n > 60 || strings.TrimSpace(limitText) != strconv.Itoa(n) {
			fmt.Fprintln(stderr, "error: --limit must be an integer between 1 and 60")
			return 1
		}
		limit = n
	}
	if cursorSet && strings.TrimSpace(cursorText) == "" {
		fmt.Fprintln(stderr, "error: flag --cursor requires a non-empty value")
		return 1
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	result, err := c.ListTrash(ctx, cursorText, limit)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		if code := encodeJSONLList(stdout, result.Posts, stderr); code != 0 {
			return code
		}
	} else {
		for _, post := range result.Posts {
			fmt.Fprintf(stdout, "%s\t%s\t%dx%d\t%s\t%s\t%s\t%s\n", post.ID, post.MediaType, post.Width, post.Height, post.PreviewURL, post.OriginalURL, post.DeletedAt, post.ModerationState)
		}
	}
	if result.NextCursor != nil {
		fmt.Fprintf(stderr, "next_cursor: %s\n", *result.NextCursor)
	}
	return 0
}
