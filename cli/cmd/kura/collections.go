package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

type collectionConfig struct {
	commonFlags
	page searchConfig
}

func runCollection(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, collectionHelp)
		return 0
	}
	subcommand := args[0]
	subargs := args[1:]
	if subcommand != "list" && subcommand != "posts" && subcommand != "create" && subcommand != "add" && subcommand != "remove" && subcommand != "reorder" {
		fmt.Fprintf(stderr, "error: unknown collection command %q\n", subcommand)
		fmt.Fprint(stderr, collectionHelp)
		return 1
	}

	cfg := collectionConfig{commonFlags: newCommonFlags(getenv)}
	if subcommand == "create" || subcommand == "add" || subcommand == "remove" || subcommand == "reorder" {
		return runCollectionMutation(ctx, subcommand, subargs, cfg, stdout, stderr, getenv)
	}
	collectionID, err := parseCollectionArgs(subargs, &cfg, subcommand == "posts")
	if err != nil {
		if err == errHelpRequested {
			if subcommand == "list" {
				fmt.Fprint(stdout, collectionListHelp)
			} else {
				fmt.Fprint(stdout, collectionPostsHelp)
			}
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		if subcommand == "list" {
			fmt.Fprint(stderr, collectionListHelp)
		} else {
			fmt.Fprint(stderr, collectionPostsHelp)
		}
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	c, ok := cfg.client(getenv, stderr)
	if !ok {
		return 1
	}
	if subcommand == "list" {
		result, err := c.ListCollections(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return writeCollections(result, cfg, stdout, stderr)
	}
	limit, cursor, err := paginationArgs(cfg.page)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return readPostPages(ctx, cfg.page, cursor, func(ctx context.Context, cursor string) (client.SearchResponse, error) {
		return c.CollectionPostsPage(ctx, collectionID, cursor, limit)
	}, stdout, stderr)
}

type collectionMutationResult struct {
	CollectionID string `json:"collection_id"`
	PostID       string `json:"post_id"`
	Removed      bool   `json:"removed"`
}

func runCollectionMutation(ctx context.Context, subcommand string, args []string, cfg collectionConfig, stdout, stderr io.Writer, getenv func(string) string) int {
	positional, err := parseCollectionMutationArgs(args, &cfg, subcommand)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, collectionMutationHelp(subcommand))
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, collectionMutationHelp(subcommand))
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	c, ok := cfg.client(getenv, stderr)
	if !ok {
		return 1
	}
	switch subcommand {
	case "create":
		result, err := c.CreateCollection(ctx, client.CreateCollectionRequest{Name: positional[0]})
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return writeCollectionMutation(result, cfg, stdout, stderr)
	case "add":
		result, err := c.AddCollectionPosts(ctx, positional[0], client.AddCollectionPostsRequest{PostIDs: positional[1:]})
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return writeCollectionMutation(result, cfg, stdout, stderr)
	case "remove":
		if err := c.RemoveCollectionPost(ctx, positional[0], positional[1]); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		result := collectionMutationResult{CollectionID: positional[0], PostID: positional[1], Removed: true}
		if cfg.jsonOut {
			return encodeJSON(stdout, result)
		}
		if cfg.jsonlOut {
			return encodeJSONL(stdout, result, stderr)
		}
		fmt.Fprintf(stdout, "%s\tremoved post %s\n", result.CollectionID, result.PostID)
		return 0
	case "reorder":
		result, err := c.ReorderCollection(ctx, positional[0], client.ReorderCollectionRequest{PostIDs: positional[1:]})
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return writeCollectionMutation(result, cfg, stdout, stderr)
	default:
		return 1
	}
}

func collectionMutationHelp(subcommand string) string {
	switch subcommand {
	case "create":
		return collectionCreateHelp
	case "add":
		return collectionAddHelp
	case "remove":
		return collectionRemoveHelp
	case "reorder":
		return collectionReorderHelp
	default:
		return collectionHelp
	}
}

func parseCollectionMutationArgs(args []string, cfg *collectionConfig, subcommand string) ([]string, error) {
	positional, err := parseFlagArgs(args, cfg.specs(nil))
	if err != nil {
		return nil, err
	}
	if subcommand == "create" {
		if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
			return nil, fmt.Errorf("collection create requires exactly one NAME")
		}
		return []string{strings.TrimSpace(positional[0])}, nil
	}
	minimum := 1
	want := "at least one COLLECTION_ID"
	switch subcommand {
	case "add":
		minimum = 2
		want = "COLLECTION_ID and at least one POST_ID"
	case "remove":
		minimum = 2
		want = "COLLECTION_ID and POST_ID"
	case "reorder":
		minimum = 1
		want = "COLLECTION_ID"
	}
	if len(positional) < minimum || (subcommand == "remove" && len(positional) != minimum) {
		return nil, fmt.Errorf("collection %s requires %s", subcommand, want)
	}
	for i, raw := range positional {
		if i == 0 || subcommand != "create" {
			if err := validateCollectionIDArg(raw, i == 0); err != nil {
				return nil, err
			}
		}
	}
	return positional, nil
}

func validateCollectionIDArg(raw string, collection bool) error {
	label := "post id"
	if collection {
		label = "collection id"
	}
	_, err := parsePositiveID(raw, label)
	return err
}

func parseCollectionArgs(args []string, cfg *collectionConfig, requireID bool) (string, error) {
	parsed := searchConfig{commonFlags: cfg.commonFlags}
	// Collection IDs are a single positional value, unlike a multi-word query.
	query, err := parseSearchArgs(args, &parsed)
	if err != nil {
		return "", err
	}
	if !requireID && (parsed.limitSet || parsed.cursorSet || parsed.allPages) {
		return "", fmt.Errorf("pagination flags are only available for collection posts")
	}
	cfg.commonFlags = parsed.commonFlags
	cfg.page = parsed
	if requireID {
		if len(strings.Fields(query)) != 1 {
			return "", fmt.Errorf("collection posts requires exactly one COLLECTION_ID")
		}
		if err := validateCollectionIDArg(query, true); err != nil {
			return "", err
		}
		return strings.TrimSpace(query), nil
	}
	if query != "" {
		return "", fmt.Errorf("collection list does not accept positional arguments")
	}
	return "", nil
}
