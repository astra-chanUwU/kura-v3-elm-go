package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

func paginationArgs(cfg searchConfig) (int, string, error) {
	limit := 0
	if cfg.limitSet {
		n, err := strconv.Atoi(strings.TrimSpace(cfg.limitVal))
		if err != nil || n < 1 || n > 60 {
			return 0, "", fmt.Errorf("--limit must be an integer between 1 and 60")
		}
		limit = n
	}
	if cfg.cursorSet && strings.TrimSpace(cfg.cursorVal) == "" {
		return 0, "", fmt.Errorf("flag --cursor requires a non-empty value")
	}
	return limit, cfg.cursorVal, nil
}

// readPostPages streams human/JSONL pages. Single-page JSON is emitted after
// its one request; --all --json is rejected because it would buffer every
// page in memory. It never retries or restarts a changed collection.
func readPostPages(ctx context.Context, cfg searchConfig, cursor string, fetch func(context.Context, string) (client.SearchResponse, error), stdout, stderr io.Writer) int {
	if cfg.allPages && cfg.jsonOut {
		fmt.Fprintln(stderr, "error: --all --json would buffer every page in memory; use --all --jsonl to stream output instead")
		return 1
	}
	seenCursors := map[string]bool{cursor: true}
	seenPosts := map[string]bool{}
	var version *int64
	combined := client.SearchResponse{Posts: []client.PostSummary{}}
	for page := 0; page < 1000; page++ {
		if err := ctx.Err(); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		result, err := fetch(ctx, cursor)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		if cfg.allPages {
			if page == 0 {
				version = result.CollectionVersion
			} else if (version == nil) != (result.CollectionVersion == nil) || (version != nil && *version != *result.CollectionVersion) {
				fmt.Fprintln(stderr, "error: collection version changed while paging; run the command again")
				return 1
			}
			for _, post := range result.Posts {
				if post.ID == "" || seenPosts[post.ID] {
					fmt.Fprintln(stderr, "error: missing or duplicate post ID while paging; run the command again")
					return 1
				}
				seenPosts[post.ID] = true
			}
		}
		if cfg.jsonOut {
			combined.Posts = append(combined.Posts, result.Posts...)
			combined.NextCursor, combined.CollectionVersion = result.NextCursor, result.CollectionVersion
		} else {
			output := result
			if cfg.allPages {
				output.NextCursor = nil
			}
			if code := writePosts(output, collectionConfig{jsonlOut: cfg.jsonlOut}, stdout, stderr); code != 0 {
				return code
			}
		}
		if !cfg.allPages || result.NextCursor == nil {
			if cfg.jsonOut {
				return encodeJSON(stdout, combined)
			}
			return 0
		}
		cursor = *result.NextCursor
		if strings.TrimSpace(cursor) == "" || seenCursors[cursor] {
			fmt.Fprintln(stderr, "error: empty or repeated pagination cursor; stopped to avoid a loop")
			return 1
		}
		seenCursors[cursor] = true
	}
	fmt.Fprintf(stderr, "error: stopped after 1,000 pages; continue with --cursor %q\n", cursor)
	return 1
}
