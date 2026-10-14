package main

import (
	"context"
	"fmt"
	"io"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

func runPostRating(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := newCommonFlags(getenv)
	versionValue := ""
	versionSet := false
	positional, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--version": valueFlag(func(value string) { versionValue = value; versionSet = true }),
	}))
	if err == errHelpRequested {
		fmt.Fprint(stdout, postRatingHelp)
		return 0
	}
	if err == nil && len(positional) != 2 {
		err = fmt.Errorf("post rating requires POST_ID and RATING")
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postRatingHelp)
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	id, err := parsePositiveID(positional[0], "post id")
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	rating := positional[1]
	if !client.ValidRating(rating) {
		fmt.Fprintln(stderr, "error: rating must be unrated, safe, questionable, or explicit")
		return 1
	}
	if !versionSet {
		fmt.Fprintln(stderr, "error: --version is required (use the observed rating_version)")
		return 1
	}
	version, err := parseVersionFlag("--version", versionValue, false)
	if err != nil || version > 2147483647 {
		fmt.Fprintln(stderr, "error: --version must be an integer between 0 and 2147483647")
		return 1
	}
	c, ok := cfg.client(getenv, stderr)
	if !ok {
		return 1
	}
	result, err := c.EditRatings(ctx, client.RatingRequest{Posts: []client.RatingTarget{{ID: id, Version: version}}, Rating: rating})
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
		if _, err := fmt.Fprintf(stdout, "%s\trating:%s\tversion:%d\tchanged:%v\n", post.ID, post.Rating, post.Version, post.Changed); err != nil {
			fmt.Fprintf(stderr, "error: write output: %v\n", err)
			return 1
		}
	}
	return 0
}

const postRatingHelp = `kura post rating - classify a post with optimistic version checking

Usage:
  kura post rating --version N [flags] POST_ID RATING

RATING: unrated, safe, questionable, or explicit.

Flags:
  --version N      Required observed rating_version (zero is valid)
  --api-url URL    Base API URL (env KURA_API_URL)
  --json | --jsonl Output the compact result
  -h, --help       Show this help

Example:
  kura post show --json 10
  kura post rating --version 0 10 safe

Ratings describe content; they do not change its visibility.
`
