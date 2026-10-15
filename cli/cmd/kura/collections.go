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
	if subcommand == "export" {
		return runCollectionExport(ctx, subargs, stdout, stderr, getenv)
	}
	if subcommand != "list" && subcommand != "summaries" && subcommand != "posts" && subcommand != "create" && subcommand != "add" && subcommand != "remove" && subcommand != "reorder" && subcommand != "move" && subcommand != "rename" && subcommand != "delete" {
		fmt.Fprintf(stderr, "error: unknown collection command %q\n", subcommand)
		fmt.Fprint(stderr, collectionHelp)
		return 1
	}

	cfg := collectionConfig{commonFlags: newCommonFlags(getenv)}
	if subcommand == "move" || subcommand == "rename" || subcommand == "delete" {
		return runCollectionManage(ctx, subcommand, subargs, cfg, stdout, stderr, getenv)
	}
	if subcommand == "create" || subcommand == "add" || subcommand == "remove" || subcommand == "reorder" {
		return runCollectionMutation(ctx, subcommand, subargs, cfg, stdout, stderr, getenv)
	}
	if subcommand == "summaries" {
		positional, err := parseFlagArgs(subargs, cfg.specs(nil))
		if err == errHelpRequested {
			fmt.Fprint(stdout, collectionSummariesHelp)
			return 0
		}
		if err != nil || len(positional) != 0 {
			if err == nil {
				err = fmt.Errorf("collection summaries does not accept positional arguments")
			}
			fmt.Fprintf(stderr, "error: %v\n", err)
			fmt.Fprint(stderr, collectionSummariesHelp)
			return 1
		}
		if !cfg.checkExclusive(stderr) {
			return 1
		}
		c, ok := cfg.client(ctx, getenv, stderr)
		if !ok {
			return 1
		}
		result, err := c.ListCollectionSummaries(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return writeCollectionSummaries(result, cfg, stdout, stderr)
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
	c, ok := cfg.client(ctx, getenv, stderr)
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

func writeCollectionSummaries(result client.CollectionSummariesResponse, cfg collectionConfig, stdout, stderr io.Writer) int {
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		for _, summary := range result.Collections {
			if code := encodeJSONL(stdout, summary, stderr); code != 0 {
				return code
			}
		}
		return 0
	}
	for _, summary := range result.Collections {
		fmt.Fprintf(stdout, "%s\t%s\t%d/%d\tv%d\n", summary.ID, summary.Name, summary.VisiblePostCount, summary.MemberCount, summary.Version)
	}
	return 0
}

func runCollectionManage(ctx context.Context, command string, args []string, cfg collectionConfig, stdout, stderr io.Writer, getenv func(string) string) int {
	var versionText, before string
	var end, yes, beforeSet bool
	commandFlags := map[string]flagSpec{
		"--expected-version": valueFlag(func(v string) { versionText = v }),
	}
	if command == "move" {
		commandFlags["--before"] = valueFlag(func(v string) { before = v; beforeSet = true })
		commandFlags["--end"] = boolFlag(func() { end = true })
	}
	if command == "delete" {
		commandFlags["--yes"] = boolFlag(func() { yes = true })
	}
	flags := cfg.specs(commandFlags)
	positional, err := parseFlagArgs(args, flags)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, collectionManagementHelp(command))
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, collectionManagementHelp(command))
		return 1
	}
	version, err := parseVersionFlag("--expected-version", versionText, false)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	want := map[string]int{"move": 2, "rename": 2, "delete": 1}[command]
	if len(positional) != want {
		fmt.Fprintf(stderr, "error: collection %s requires %d positional arguments\n", command, want)
		return 1
	}
	if _, err := parsePositiveID(positional[0], "collection id"); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if command == "move" {
		if beforeSet == end {
			fmt.Fprintln(stderr, "error: collection move requires exactly one of --before POST_ID or --end")
			return 1
		}
		if err := validateCollectionIDArg(positional[1], false); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		if beforeSet {
			if err := validateCollectionIDArg(before, false); err != nil {
				fmt.Fprintf(stderr, "error: %v\n", err)
				return 1
			}
		}
	} else if command == "rename" {
		if strings.TrimSpace(positional[1]) == "" {
			fmt.Fprintln(stderr, "error: collection rename requires a non-empty NAME")
			return 1
		}
	} else if !yes {
		fmt.Fprintln(stderr, "error: collection delete requires --yes")
		return 1
	}
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	var result any
	switch command {
	case "move":
		request := client.MoveCollectionPostRequest{PostID: positional[1], ExpectedVersion: int64(version)}
		if before != "" {
			request.BeforePostID = &before
		}
		result, err = c.MoveCollectionPost(ctx, positional[0], request)
	case "rename":
		result, err = c.RenameCollection(ctx, positional[0], client.CollectionManageRequest{Name: positional[1], ExpectedVersion: int64(version)})
	case "delete":
		result, err = c.DeleteCollection(ctx, positional[0], int64(version))
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		return encodeJSONL(stdout, result, stderr)
	}
	switch value := result.(type) {
	case client.CollectionManageResult:
		fmt.Fprintf(stdout, "%s\t%s\tv%d\tchanged=%t\n", value.ID, value.Name, value.Version, value.Changed)
	case client.MoveCollectionPostResult:
		fmt.Fprintf(stdout, "%s\t%s\tv%d\tchanged=%t\n", value.CollectionID, positional[1], value.Version, value.Changed)
	}
	return 0
}

func collectionManagementHelp(command string) string {
	switch command {
	case "move":
		return collectionMoveHelp
	case "rename":
		return collectionRenameHelp
	case "delete":
		return collectionDeleteHelp
	}
	return collectionHelp
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
	c, ok := cfg.client(ctx, getenv, stderr)
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
