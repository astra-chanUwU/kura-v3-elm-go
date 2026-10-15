package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

func runPostFavorite(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return runReactionCommand(ctx, args, stdout, stderr, getenv, true, false, postFavoriteHelp)
}

func runPostScore(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return runReactionCommand(ctx, args, stdout, stderr, getenv, false, true, postScoreHelp)
}

func runPostReaction(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return runReactionCommand(ctx, args, stdout, stderr, getenv, false, false, postReactionHelp)
}

func runReactionCommand(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, requireFavorite, requireScore bool, help string) int {
	cfg := postReactionConfig{commonFlags: newCommonFlags(getenv)}
	postID, err := parsePostReactionArgs(args, &cfg, requireFavorite, requireScore)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, help)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, help)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	return executeReaction(ctx, cfg, postID, stdout, stderr, getenv, requireFavorite, requireScore)
}

func parsePostReactionArgs(args []string, cfg *postReactionConfig, requireFavorite, requireScore bool) (string, error) {
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--version":  valueFlag(func(v string) { cfg.versionSet = true; cfg.version = strings.TrimSpace(v) }),
		"--favorite": valueFlag(func(v string) { cfg.favoriteSet = true; cfg.favoriteVal = strings.TrimSpace(v) }),
		"--score":    valueFlag(func(v string) { cfg.scoreSet = true; cfg.scoreVal = strings.TrimSpace(v) }),
	}))
	if err != nil {
		return "", err
	}
	if len(positional) != 1 {
		if requireFavorite {
			return "", fmt.Errorf("post favorite requires exactly one POST_ID")
		}
		if requireScore {
			return "", fmt.Errorf("post score requires exactly one POST_ID")
		}
		return "", fmt.Errorf("post reaction requires exactly one POST_ID")
	}
	return parsePositiveID(positional[0], "post id")
}

func executeReaction(ctx context.Context, cfg postReactionConfig, postID string, stdout, stderr io.Writer, getenv func(string) string, requireFavorite, requireScore bool) int {
	if !cfg.versionSet {
		fmt.Fprintln(stderr, "error: flag --version is required")
		return 1
	}
	version, err := parseVersionFlag("--version", cfg.version, false)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if requireFavorite && !cfg.favoriteSet {
		fmt.Fprintln(stderr, "error: flag --favorite is required")
		return 1
	}
	if requireScore && !cfg.scoreSet {
		fmt.Fprintln(stderr, "error: flag --score is required")
		return 1
	}
	if !cfg.favoriteSet && !cfg.scoreSet {
		fmt.Fprintln(stderr, "error: at least one of --favorite or --score is required")
		return 1
	}
	var favPtr *bool
	if cfg.favoriteSet {
		val := strings.ToLower(strings.TrimSpace(cfg.favoriteVal))
		var fav bool
		switch val {
		case "true", "1", "t", "yes":
			fav = true
		case "false", "0", "f", "no":
			fav = false
		default:
			fmt.Fprintf(stderr, "error: invalid --favorite %q: must be true or false\n", cfg.favoriteVal)
			return 1
		}
		favPtr = &fav
	}
	var scorePtr *int
	if cfg.scoreSet {
		val := strings.TrimSpace(cfg.scoreVal)
		score, err := strconv.Atoi(val)
		if err != nil || score < 0 || score > 2147483647 || val != strconv.Itoa(score) {
			fmt.Fprintf(stderr, "error: invalid --score %q: must be an integer between 0 and 2147483647\n", cfg.scoreVal)
			return 1
		}
		scorePtr = &score
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	req := client.ReactionRequest{
		Posts:    []client.ReactionTarget{{ID: postID, Version: version}},
		Favorite: favPtr,
		Score:    scorePtr,
	}
	result, err := c.EditReactions(ctx, req)
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
		fmt.Fprintf(stdout, "%s\tversion:%d\tchanged:%v\tfavorite:%v\tscore:%d\n", r.ID, r.Version, r.Changed, r.Favorite, r.Score)
	}
	return 0
}
