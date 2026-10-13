package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

func runPostTagEdit(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postTagEditConfig{commonFlags: newCommonFlags(getenv)}
	postID, err := parsePostTagEditArgs(args, &cfg)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, postTagEditHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postTagEditHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	if !cfg.versionSet {
		fmt.Fprintln(stderr, "error: flag --version is required")
		fmt.Fprint(stderr, postTagEditHelp)
		return 1
	}
	version, err := parseVersionFlag("--version", cfg.version, false)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	add := cfg.addTags
	if add == nil {
		add = []string{}
	}
	remove := cfg.removeTags
	if remove == nil {
		remove = []string{}
	}
	c, ok := cfg.client(getenv, stderr)
	if !ok {
		return 1
	}
	req := client.TagEditRequest{
		Posts:  []client.TagTarget{{ID: postID, Version: version}},
		Add:    add,
		Remove: remove,
	}
	result, err := c.EditTags(ctx, req)
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
	for _, r := range result.Posts {
		fmt.Fprintf(stdout, "%s\tversion:%d\tchanged:%v\ttags:%s\n", r.ID, r.Version, r.Changed, strings.Join(r.Tags, ","))
	}
	return 0
}

func parsePostTagEditArgs(args []string, cfg *postTagEditConfig) (string, error) {
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--version": valueFlag(func(v string) { cfg.versionSet = true; cfg.version = strings.TrimSpace(v) }),
		"--add":     valueFlag(func(v string) { cfg.addTags = append(cfg.addTags, strings.TrimSpace(v)) }),
		"--remove":  valueFlag(func(v string) { cfg.removeTags = append(cfg.removeTags, strings.TrimSpace(v)) }),
	}))
	if err != nil {
		return "", err
	}
	if len(positional) != 1 {
		return "", fmt.Errorf("post tag edit requires exactly one POST_ID")
	}
	return parsePositiveID(positional[0], "post id")
}

func runPostTagRevert(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postTagRevertConfig{commonFlags: newCommonFlags(getenv)}
	postID, err := parsePostTagRevertArgs(args, &cfg)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, postTagRevertHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postTagRevertHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	if !cfg.versionSet {
		fmt.Fprintln(stderr, "error: flag --version is required")
		fmt.Fprint(stderr, postTagRevertHelp)
		return 1
	}
	if !cfg.targetSet {
		fmt.Fprintln(stderr, "error: flag --target-version is required")
		fmt.Fprint(stderr, postTagRevertHelp)
		return 1
	}
	version, err := parseVersionFlag("--version", cfg.version, false)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	target, err := parseVersionFlag("--target-version", cfg.targetVersion, true)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	c, ok := cfg.client(getenv, stderr)
	if !ok {
		return 1
	}
	req := client.TagRevertRequest{
		Posts:         []client.TagTarget{{ID: postID, Version: version}},
		TargetVersion: target,
	}
	result, err := c.RevertTags(ctx, req)
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
	for _, r := range result.Posts {
		fmt.Fprintf(stdout, "%s\tversion:%d\tchanged:%v\ttags:%s\n", r.ID, r.Version, r.Changed, strings.Join(r.Tags, ","))
	}
	return 0
}

func parsePostTagRevertArgs(args []string, cfg *postTagRevertConfig) (string, error) {
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--version":        valueFlag(func(v string) { cfg.versionSet = true; cfg.version = strings.TrimSpace(v) }),
		"--target-version": valueFlag(func(v string) { cfg.targetSet = true; cfg.targetVersion = strings.TrimSpace(v) }),
	}))
	if err != nil {
		return "", err
	}
	if len(positional) != 1 {
		return "", fmt.Errorf("post tag revert requires exactly one POST_ID")
	}
	return parsePositiveID(positional[0], "post id")
}
