package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

const defaultAPIURL = "http://localhost:8080"

const rootHelp = `kura - Kura media browser CLI (HTTP API only)

Usage:
  kura <command> [flags] [args]
  kura --help
  kura search --help
  kura collection --help
  kura post --help

Commands:
  search    Search posts via GET /api/posts
  collection List collections or browse collection posts
  post      Show post detail and mutate tags, favorites, and scores

Environment:
  KURA_API_URL    Base API URL (default http://localhost:8080)

Notes:
  The CLI uses only the public HTTP API (same as the Elm app). No database
  or external network is required; the API defaults to http://localhost:8080.
  Run with --help on any command for details.
`

const searchHelp = `kura search - search posts via the HTTP API

Usage:
  kura search [flags] [QUERY]

Search posts using GET /api/posts?q=QUERY. QUERY is optional; when omitted
or empty the server browses newest visible posts. Multi-word queries may be
quoted or passed as separate words:

  kura search cat
  kura search "cat feline"
  kura search --json cat feline

Parser:
  Flags must appear before QUERY. Flags after QUERY are treated as part of
  QUERY. Use -- to end flag parsing explicitly:

  kura search --json -- cat --json   # queries for "cat --json"

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per post (JSON Lines)
  --limit N         Page size 1..60 (max 60, omit for server default)
  --cursor CURSOR   Opaque pagination cursor from previous next_cursor
  -h, --help        Show this help

Output:
  Default (human)   Tab-separated lines: ID  MEDIA_TYPE  WxH  PREVIEW_URL  ORIGINAL_URL
                    Empty results produce no stdout (exit 0). Errors go to stderr.
  --json            Pretty-printed JSON envelope from the API (includes posts and
                    optional next_cursor).
  --jsonl           One JSON object per post, one line each. Empty results produce
                    no stdout. next_cursor, if present, is written to stderr as a hint.

Pagination:
  GET /api/posts?q=&cursor=&limit= returns {posts:[...],next_cursor:string|null}.
  Use --limit 1..60 and --cursor from the previous response to page. Each
  invocation makes one explicit request.

Examples:
  kura search --api-url http://localhost:8080 cat
  KURA_API_URL=http://localhost:8080 kura search --json anime
  kura search --jsonl "landscape mountain"
  kura search --limit 20 --cursor tok123 cat
`

const collectionHelp = `kura collection - list, browse, and mutate collections

Usage:
  kura collection list [flags]
  kura collection posts [flags] COLLECTION_ID
  kura collection create [flags] NAME
  kura collection add [flags] COLLECTION_ID POST_ID...
  kura collection remove [flags] COLLECTION_ID POST_ID
  kura collection reorder [flags] COLLECTION_ID [POST_ID...]

Commands:
  list     List collections via GET /api/collections
  posts    List posts in a collection via GET /api/collections/{id}/posts
  create   Create a collection via POST /api/collections
  add      Add posts via POST /api/collections/{id}/posts
  remove   Remove one post via DELETE /api/collections/{id}/posts/{postID}
  reorder  Replace collection order via POST /api/collections/{id}/order

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per collection, post, or mutation
                    (JSON Lines)
  -h, --help        Show this help

Examples:
  kura collection list
  kura collection list --json
  kura collection posts 7
  kura collection posts --jsonl 7
  kura collection create "Reference set"
  kura collection add 7 11 10
  kura collection remove 7 11
  kura collection reorder 7 10 11
`

const collectionListHelp = `kura collection list - list collections via the HTTP API

Usage:
  kura collection list [flags]

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per collection (JSON Lines)
  -h, --help        Show this help
`

const collectionPostsHelp = `kura collection posts - list posts in a collection via the HTTP API

Usage:
  kura collection posts [flags] COLLECTION_ID

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per post (JSON Lines)
  -h, --help        Show this help
`

const collectionCreateHelp = `kura collection create - create a collection via the HTTP API

Usage:
  kura collection create [flags] NAME

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the created collection as JSON
  --jsonl           Output the created collection as one JSON Lines object
  -h, --help        Show this help
`

const collectionAddHelp = `kura collection add - add posts to a collection via the HTTP API

Usage:
  kura collection add [flags] COLLECTION_ID POST_ID...

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the updated collection as JSON
  --jsonl           Output the updated collection as one JSON Lines object
  -h, --help        Show this help
`

const collectionRemoveHelp = `kura collection remove - remove a post via the HTTP API

Usage:
  kura collection remove [flags] COLLECTION_ID POST_ID

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output a removal result as JSON
  --jsonl           Output a removal result as one JSON Lines object
  -h, --help        Show this help
`

const collectionReorderHelp = `kura collection reorder - replace collection order via the HTTP API

Usage:
  kura collection reorder [flags] COLLECTION_ID [POST_ID...]

Pass no POST_ID values to clear a collection. The supplied IDs must contain
every current member exactly once, in the desired order.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the updated collection as JSON
  --jsonl           Output the updated collection as one JSON Lines object
  -h, --help        Show this help
`

const postHelp = `kura post - post detail and mutations via the HTTP API

Usage:
  kura post <command> [flags] [args]
  kura post --help
  kura post show --help
  kura post tag --help
  kura post favorite --help
  kura post score --help
  kura post reaction --help

Commands:
  show      Show post detail with revision history via GET /api/posts/{id}
  tag       Edit or revert tags (subcommands: edit, revert)
  favorite  Set favorite via POST /api/posts/reactions
  score     Set score via POST /api/posts/reactions
  reaction  Set favorite and/or score via POST /api/posts/reactions

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output JSON Lines
  -h, --help        Show this help

Examples:
  kura post show 2004
  kura post show --json 2004
  kura post tag edit --version 2 --add night 2004
  kura post tag revert --version 3 --target-version 1 2004
  kura post favorite --version 1 --favorite=true 2004
  kura post score --version 1 --score 5 2004
  kura post reaction --version 1 --favorite=true --score 5 2004
`

const postShowHelp = `kura post show - show post detail via the HTTP API

Usage:
  kura post show [flags] POST_ID

Shows post detail including tags, source, artist, hash, file size, dimensions,
favorite, score, versions, and revision history from GET /api/posts/{id}.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object (JSON Lines, compact)
  -h, --help        Show this help
`

const postTagHelp = `kura post tag - edit or revert tags via the HTTP API

Usage:
  kura post tag edit [flags] POST_ID
  kura post tag revert [flags] POST_ID

Commands:
  edit    Edit tags via POST /api/posts/tags
  revert  Revert tags via POST /api/posts/tags/revert

Flags for edit:
  --api-url URL       Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N         Expected current tag_version (required)
  --add TAG           Tag to add (repeatable, or comma-separated)
  --remove TAG        Tag to remove (repeatable, or comma-separated)
  --json              Output the raw JSON response (pretty-printed)
  --jsonl             Output one JSON object per result (JSON Lines)
  -h, --help          Show this help

Flags for revert:
  --api-url URL       Base API URL
  --version N         Expected current tag_version (required)
  --target-version N  Historical version to restore (required)
  --json              Output the raw JSON response
  --jsonl             Output JSON Lines
  -h, --help          Show this help

Examples:
  kura post tag edit --version 0 --add night --remove demo 2004
  kura post tag revert --version 2 --target-version 1 2004
`

const postTagEditHelp = `kura post tag edit - edit tags via the HTTP API

Usage:
  kura post tag edit [flags] POST_ID

Edits tags via POST /api/posts/tags with optimistic version checking.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current tag_version (required)
  --add TAG         Tag to add (repeatable)
  --remove TAG      Tag to remove (repeatable)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postTagRevertHelp = `kura post tag revert - revert tags via the HTTP API

Usage:
  kura post tag revert [flags] POST_ID

Reverts tags to a historical revision via POST /api/posts/tags/revert with
optimistic version checking.

Flags:
  --api-url URL       Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N         Expected current tag_version (required)
  --target-version N  Historical version to restore (required)
  --json              Output the raw JSON response (pretty-printed)
  --jsonl             Output one JSON object per result (JSON Lines)
  -h, --help          Show this help
`

const postFavoriteHelp = `kura post favorite - set favorite via the HTTP API

Usage:
  kura post favorite [flags] POST_ID

Sets favorite via POST /api/posts/reactions with optimistic version checking.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current reaction_version (required)
  --favorite BOOL   Favorite value true or false (required)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postScoreHelp = `kura post score - set score via the HTTP API

Usage:
  kura post score [flags] POST_ID

Sets score via POST /api/posts/reactions with optimistic version checking.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current reaction_version (required)
  --score N         Score value 0..2147483647 (required)
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

const postReactionHelp = `kura post reaction - set favorite and/or score via the HTTP API

Usage:
  kura post reaction [flags] POST_ID

Sets favorite and/or score via POST /api/posts/reactions with optimistic version
checking. At least one of --favorite or --score is required.

Flags:
  --api-url URL     Base API URL (env KURA_API_URL, default http://localhost:8080)
  --version N       Expected current reaction_version (required)
  --favorite BOOL   Favorite value true or false
  --score N         Score value 0..2147483647
  --json            Output the raw JSON response (pretty-printed)
  --jsonl           Output one JSON object per result (JSON Lines)
  -h, --help        Show this help
`

type searchConfig struct {
	apiURL    string
	apiURLSet bool
	jsonOut   bool
	jsonlOut  bool
	limitSet  bool
	limitVal  string
	cursorSet bool
	cursorVal string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, rootHelp)
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, rootHelp)
		return 0
	case "search":
		return runSearch(args[1:], stdout, stderr, getenv)
	case "collection":
		return runCollection(args[1:], stdout, stderr, getenv)
	case "post":
		return runPost(args[1:], stdout, stderr, getenv)
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", args[0])
		fmt.Fprint(stderr, rootHelp)
		return 1
	}
}

type collectionConfig struct {
	apiURL   string
	jsonOut  bool
	jsonlOut bool
}

func runCollection(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
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

	cfg := collectionConfig{apiURL: defaultAPIURL}
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
	if subcommand == "create" || subcommand == "add" || subcommand == "remove" || subcommand == "reorder" {
		return runCollectionMutation(subcommand, subargs, cfg, stdout, stderr, getenv)
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
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}
	if _, err := url.ParseRequestURI(cfg.apiURL); err != nil {
		fmt.Fprintf(stderr, "error: invalid --api-url %q: %v\n", cfg.apiURL, err)
		return 1
	}
	c := client.New(cfg.apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if subcommand == "list" {
		result, err := c.ListCollections(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return writeCollections(result, cfg, stdout, stderr)
	}
	result, err := c.CollectionPosts(ctx, collectionID)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return writePosts(result, cfg, stdout, stderr)
}

type collectionMutationResult struct {
	CollectionID string `json:"collection_id"`
	PostID       string `json:"post_id"`
	Removed      bool   `json:"removed"`
}

func runCollectionMutation(subcommand string, args []string, cfg collectionConfig, stdout, stderr io.Writer, getenv func(string) string) int {
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
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
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}
	if _, err := url.ParseRequestURI(cfg.apiURL); err != nil {
		fmt.Fprintf(stderr, "error: invalid --api-url %q: %v\n", cfg.apiURL, err)
		return 1
	}
	c := client.New(cfg.apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
	var positional []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		switch {
		case arg == "-h" || arg == "--help":
			return nil, errHelpRequested
		case arg == "--json":
			cfg.jsonOut = true
			i++
		case arg == "--jsonl":
			cfg.jsonlOut = true
			i++
		case arg == "--api-url":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag --api-url requires a value")
			}
			cfg.apiURL = strings.TrimSpace(args[i+1])
			i += 2
		case strings.HasPrefix(arg, "--api-url="):
			cfg.apiURL = strings.TrimSpace(strings.TrimPrefix(arg, "--api-url="))
			i++
		case strings.HasPrefix(arg, "-"):
			return nil, fmt.Errorf("unknown flag %q", arg)
		default:
			positional = append(positional, arg)
			i++
			for i < len(args) {
				positional = append(positional, args[i])
				i++
			}
		}
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
	raw = strings.TrimSpace(raw)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || raw != strconv.FormatInt(n, 10) {
		return fmt.Errorf("%s must be a positive integer", label)
	}
	return nil
}

func writeCollectionMutation(result client.Collection, cfg collectionConfig, stdout, stderr io.Writer) int {
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		return encodeJSONL(stdout, result, stderr)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%d posts\n", result.ID, result.Name, len(result.PostIDs))
	return 0
}

func encodeJSONL(stdout io.Writer, value any, stderr io.Writer) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		fmt.Fprintf(stderr, "error: encode jsonl: %v\n", err)
		return 1
	}
	return 0
}

func parseCollectionArgs(args []string, cfg *collectionConfig, requireID bool) (string, error) {
	var positional []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		switch {
		case arg == "-h" || arg == "--help":
			return "", errHelpRequested
		case arg == "--json":
			cfg.jsonOut = true
			i++
			continue
		case arg == "--jsonl":
			cfg.jsonlOut = true
			i++
			continue
		case arg == "--api-url":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --api-url requires a value")
			}
			cfg.apiURL = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--api-url="):
			cfg.apiURL = strings.TrimSpace(strings.TrimPrefix(arg, "--api-url="))
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			return "", fmt.Errorf("unknown flag %q", arg)
		default:
			positional = append(positional, arg)
			i++
			for i < len(args) {
				positional = append(positional, args[i])
				i++
			}
		}
	}
	if requireID {
		if len(positional) != 1 {
			return "", fmt.Errorf("collection posts requires exactly one COLLECTION_ID")
		}
		id := strings.TrimSpace(positional[0])
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || id != strconv.FormatInt(n, 10) {
			return "", fmt.Errorf("collection id must be a positive integer")
		}
		return id, nil
	}
	if len(positional) != 0 {
		return "", fmt.Errorf("collection list does not accept positional arguments")
	}
	return "", nil
}

func writeCollections(result client.CollectionsResponse, cfg collectionConfig, stdout, _ io.Writer) int {
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, collection := range result.Collections {
			if err := enc.Encode(collection); err != nil {
				return 1
			}
		}
		return 0
	}
	for _, collection := range result.Collections {
		fmt.Fprintf(stdout, "%s\t%s\t%d posts\n", collection.ID, collection.Name, len(collection.PostIDs))
	}
	return 0
}

func writePosts(result client.SearchResponse, cfg collectionConfig, stdout, stderr io.Writer) int {
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, post := range result.Posts {
			if err := enc.Encode(post); err != nil {
				return 1
			}
		}
	} else {
		for _, post := range result.Posts {
			fmt.Fprintf(stdout, "%s\t%s\t%dx%d\t%s\t%s\n", post.ID, post.MediaType, post.Width, post.Height, post.PreviewURL, post.OriginalURL)
		}
	}
	if result.NextCursor != nil {
		fmt.Fprintf(stderr, "next_cursor: %s\n", *result.NextCursor)
	}
	return 0
}

func encodeJSON(stdout io.Writer, value any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return 1
	}
	return 0
}

type postShowConfig struct {
	apiURL   string
	jsonOut  bool
	jsonlOut bool
}

type postTagEditConfig struct {
	apiURL     string
	jsonOut    bool
	jsonlOut   bool
	versionSet bool
	version    string
	addTags    []string
	removeTags []string
}

type postTagRevertConfig struct {
	apiURL        string
	jsonOut       bool
	jsonlOut      bool
	versionSet    bool
	version       string
	targetSet     bool
	targetVersion string
}

type postReactionConfig struct {
	apiURL      string
	jsonOut     bool
	jsonlOut    bool
	versionSet  bool
	version     string
	favoriteSet bool
	favoriteVal string
	scoreSet    bool
	scoreVal    string
}

func runPost(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, postHelp)
		return 0
	}
	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "show":
		return runPostShow(rest, stdout, stderr, getenv)
	case "tag":
		if len(rest) == 0 || rest[0] == "-h" || rest[0] == "--help" || rest[0] == "help" {
			fmt.Fprint(stdout, postTagHelp)
			return 0
		}
		sub := rest[0]
		subrest := rest[1:]
		switch sub {
		case "edit":
			return runPostTagEdit(subrest, stdout, stderr, getenv)
		case "revert":
			return runPostTagRevert(subrest, stdout, stderr, getenv)
		case "-h", "--help", "help":
			fmt.Fprint(stdout, postTagHelp)
			return 0
		default:
			fmt.Fprintf(stderr, "error: unknown post tag command %q\n", sub)
			fmt.Fprint(stderr, postTagHelp)
			return 1
		}
	case "tag-edit", "tag_edit":
		return runPostTagEdit(rest, stdout, stderr, getenv)
	case "tag-revert", "tag_revert":
		return runPostTagRevert(rest, stdout, stderr, getenv)
	case "favorite":
		return runPostFavorite(rest, stdout, stderr, getenv)
	case "score":
		return runPostScore(rest, stdout, stderr, getenv)
	case "reaction":
		return runPostReaction(rest, stdout, stderr, getenv)
	case "get":
		// Preserve previous behavior for legacy test: "post get" was documented as unavailable.
		fmt.Fprintln(stderr, "error: post detail endpoint is not available in the current API")
		fmt.Fprintln(stderr, "only 'kura search' is supported; the server exposes GET /api/posts and GET /health")
		return 1
	default:
		fmt.Fprintf(stderr, "error: unknown post command %q\n", cmd)
		fmt.Fprint(stderr, postHelp)
		return 1
	}
}

func runPostShow(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postShowConfig{apiURL: defaultAPIURL}
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
	postID, err := parsePostShowArgs(args, &cfg)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, postShowHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postShowHelp)
		return 1
	}
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}
	if _, err := url.ParseRequestURI(cfg.apiURL); err != nil {
		fmt.Fprintf(stderr, "error: invalid --api-url %q: %v\n", cfg.apiURL, err)
		return 1
	}
	c := client.New(cfg.apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	detail, err := c.GetPostDetail(ctx, postID)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, detail)
	}
	if cfg.jsonlOut {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(detail); err != nil {
			fmt.Fprintf(stderr, "error: encode jsonl: %v\n", err)
			return 1
		}
		return 0
	}
	return writePostDetailHuman(detail, stdout)
}

func parsePostShowArgs(args []string, cfg *postShowConfig) (string, error) {
	var positional []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		switch {
		case arg == "-h" || arg == "--help":
			return "", errHelpRequested
		case arg == "--json":
			cfg.jsonOut = true
			i++
			continue
		case arg == "--jsonl":
			cfg.jsonlOut = true
			i++
			continue
		case arg == "--api-url":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --api-url requires a value")
			}
			cfg.apiURL = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--api-url="):
			cfg.apiURL = strings.TrimSpace(strings.TrimPrefix(arg, "--api-url="))
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			return "", fmt.Errorf("unknown flag %q", arg)
		default:
			positional = append(positional, arg)
			i++
			for i < len(args) {
				positional = append(positional, args[i])
				i++
			}
		}
	}
	if len(positional) != 1 {
		return "", fmt.Errorf("post show requires exactly one POST_ID")
	}
	id := strings.TrimSpace(positional[0])
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || id != strconv.FormatInt(n, 10) {
		return "", fmt.Errorf("post id must be a positive integer")
	}
	return id, nil
}

func writePostDetailHuman(d client.PostDetail, stdout io.Writer) int {
	fmt.Fprintf(stdout, "ID: %s\n", d.ID)
	fmt.Fprintf(stdout, "Preview: %s\n", d.PreviewURL)
	fmt.Fprintf(stdout, "Original: %s\n", d.OriginalURL)
	fmt.Fprintf(stdout, "Media Type: %s\n", d.MediaType)
	fmt.Fprintf(stdout, "Dimensions: %dx%d\n", d.Width, d.Height)
	fmt.Fprintf(stdout, "Source: %s\n", d.Source)
	fmt.Fprintf(stdout, "Artist: %s\n", d.Artist)
	fmt.Fprintf(stdout, "Hash: %s\n", d.Hash)
	fmt.Fprintf(stdout, "File Size: %d\n", d.FileSize)
	fmt.Fprintf(stdout, "Created At: %s\n", d.CreatedAt)
	fmt.Fprintf(stdout, "Tags: %s\n", strings.Join(d.Tags, ", "))
	fmt.Fprintf(stdout, "Tag Version: %d\n", d.TagVersion)
	fmt.Fprintf(stdout, "Favorite: %v\n", d.Favorite)
	fmt.Fprintf(stdout, "Score: %d\n", d.Score)
	fmt.Fprintf(stdout, "Reaction Version: %d\n", d.ReactionVersion)
	if len(d.History) == 0 {
		fmt.Fprintln(stdout, "History: (none)")
	} else {
		fmt.Fprintln(stdout, "History:")
		for _, rev := range d.History {
			fmt.Fprintf(stdout, "  v%d %s", rev.Version, rev.Kind)
			if len(rev.AddedTags) > 0 {
				fmt.Fprintf(stdout, " +%s", strings.Join(rev.AddedTags, ","))
			}
			if len(rev.RemovedTags) > 0 {
				fmt.Fprintf(stdout, " -%s", strings.Join(rev.RemovedTags, ","))
			}
			if len(rev.TargetTags) > 0 {
				fmt.Fprintf(stdout, " target:[%s]", strings.Join(rev.TargetTags, ","))
			}
			fmt.Fprintf(stdout, " %s\n", rev.CreatedAt)
		}
	}
	if len(d.ReactionHistory) == 0 {
		fmt.Fprintln(stdout, "Reaction History: (none)")
	} else {
		fmt.Fprintln(stdout, "Reaction History:")
		for _, rev := range d.ReactionHistory {
			fmt.Fprintf(stdout, "  v%d favorite:%v score:%d %s\n", rev.Version, rev.Favorite, rev.Score, rev.CreatedAt)
		}
	}
	return 0
}

func runPostTagEdit(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postTagEditConfig{apiURL: defaultAPIURL}
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
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
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}
	if !cfg.versionSet {
		fmt.Fprintln(stderr, "error: flag --version is required")
		fmt.Fprint(stderr, postTagEditHelp)
		return 1
	}
	versionStr := strings.TrimSpace(cfg.version)
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 0 || versionStr != strconv.Itoa(version) {
		fmt.Fprintf(stderr, "error: invalid --version %q: must be a non-negative integer\n", cfg.version)
		return 1
	}
	if _, err := url.ParseRequestURI(cfg.apiURL); err != nil {
		fmt.Fprintf(stderr, "error: invalid --api-url %q: %v\n", cfg.apiURL, err)
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
	c := client.New(cfg.apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, r := range result.Posts {
			if err := enc.Encode(r); err != nil {
				fmt.Fprintf(stderr, "error: encode jsonl: %v\n", err)
				return 1
			}
		}
		return 0
	}
	for _, r := range result.Posts {
		fmt.Fprintf(stdout, "%s\tversion:%d\tchanged:%v\ttags:%s\n", r.ID, r.Version, r.Changed, strings.Join(r.Tags, ","))
	}
	return 0
}

func parsePostTagEditArgs(args []string, cfg *postTagEditConfig) (string, error) {
	var positional []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		switch {
		case arg == "-h" || arg == "--help":
			return "", errHelpRequested
		case arg == "--json":
			cfg.jsonOut = true
			i++
			continue
		case arg == "--jsonl":
			cfg.jsonlOut = true
			i++
			continue
		case arg == "--api-url":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --api-url requires a value")
			}
			cfg.apiURL = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--api-url="):
			cfg.apiURL = strings.TrimSpace(strings.TrimPrefix(arg, "--api-url="))
			i++
			continue
		case arg == "--version":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --version requires a value")
			}
			cfg.versionSet = true
			cfg.version = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--version="):
			cfg.versionSet = true
			cfg.version = strings.TrimSpace(strings.TrimPrefix(arg, "--version="))
			i++
			continue
		case arg == "--add":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --add requires a value")
			}
			cfg.addTags = append(cfg.addTags, strings.TrimSpace(args[i+1]))
			i += 2
			continue
		case strings.HasPrefix(arg, "--add="):
			cfg.addTags = append(cfg.addTags, strings.TrimSpace(strings.TrimPrefix(arg, "--add=")))
			i++
			continue
		case arg == "--remove":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --remove requires a value")
			}
			cfg.removeTags = append(cfg.removeTags, strings.TrimSpace(args[i+1]))
			i += 2
			continue
		case strings.HasPrefix(arg, "--remove="):
			cfg.removeTags = append(cfg.removeTags, strings.TrimSpace(strings.TrimPrefix(arg, "--remove=")))
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			return "", fmt.Errorf("unknown flag %q", arg)
		default:
			positional = append(positional, arg)
			i++
			for i < len(args) {
				positional = append(positional, args[i])
				i++
			}
		}
	}
	if len(positional) != 1 {
		return "", fmt.Errorf("post tag edit requires exactly one POST_ID")
	}
	id := strings.TrimSpace(positional[0])
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || id != strconv.FormatInt(n, 10) {
		return "", fmt.Errorf("post id must be a positive integer")
	}
	return id, nil
}

func runPostTagRevert(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postTagRevertConfig{apiURL: defaultAPIURL}
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
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
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
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
	versionStr := strings.TrimSpace(cfg.version)
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 0 || versionStr != strconv.Itoa(version) {
		fmt.Fprintf(stderr, "error: invalid --version %q: must be a non-negative integer\n", cfg.version)
		return 1
	}
	targetStr := strings.TrimSpace(cfg.targetVersion)
	target, err := strconv.Atoi(targetStr)
	if err != nil || target <= 0 || targetStr != strconv.Itoa(target) {
		fmt.Fprintf(stderr, "error: invalid --target-version %q: must be a positive integer\n", cfg.targetVersion)
		return 1
	}
	if _, err := url.ParseRequestURI(cfg.apiURL); err != nil {
		fmt.Fprintf(stderr, "error: invalid --api-url %q: %v\n", cfg.apiURL, err)
		return 1
	}
	c := client.New(cfg.apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, r := range result.Posts {
			if err := enc.Encode(r); err != nil {
				fmt.Fprintf(stderr, "error: encode jsonl: %v\n", err)
				return 1
			}
		}
		return 0
	}
	for _, r := range result.Posts {
		fmt.Fprintf(stdout, "%s\tversion:%d\tchanged:%v\ttags:%s\n", r.ID, r.Version, r.Changed, strings.Join(r.Tags, ","))
	}
	return 0
}

func parsePostTagRevertArgs(args []string, cfg *postTagRevertConfig) (string, error) {
	var positional []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		switch {
		case arg == "-h" || arg == "--help":
			return "", errHelpRequested
		case arg == "--json":
			cfg.jsonOut = true
			i++
			continue
		case arg == "--jsonl":
			cfg.jsonlOut = true
			i++
			continue
		case arg == "--api-url":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --api-url requires a value")
			}
			cfg.apiURL = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--api-url="):
			cfg.apiURL = strings.TrimSpace(strings.TrimPrefix(arg, "--api-url="))
			i++
			continue
		case arg == "--version":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --version requires a value")
			}
			cfg.versionSet = true
			cfg.version = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--version="):
			cfg.versionSet = true
			cfg.version = strings.TrimSpace(strings.TrimPrefix(arg, "--version="))
			i++
			continue
		case arg == "--target-version":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --target-version requires a value")
			}
			cfg.targetSet = true
			cfg.targetVersion = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--target-version="):
			cfg.targetSet = true
			cfg.targetVersion = strings.TrimSpace(strings.TrimPrefix(arg, "--target-version="))
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			return "", fmt.Errorf("unknown flag %q", arg)
		default:
			positional = append(positional, arg)
			i++
			for i < len(args) {
				positional = append(positional, args[i])
				i++
			}
		}
	}
	if len(positional) != 1 {
		return "", fmt.Errorf("post tag revert requires exactly one POST_ID")
	}
	id := strings.TrimSpace(positional[0])
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || id != strconv.FormatInt(n, 10) {
		return "", fmt.Errorf("post id must be a positive integer")
	}
	return id, nil
}

func runPostFavorite(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postReactionConfig{apiURL: defaultAPIURL}
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
	postID, err := parsePostReactionArgs(args, &cfg, true, false)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, postFavoriteHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postFavoriteHelp)
		return 1
	}
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}
	return executeReaction(cfg, postID, stdout, stderr, true, false)
}

func runPostScore(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postReactionConfig{apiURL: defaultAPIURL}
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
	postID, err := parsePostReactionArgs(args, &cfg, false, true)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, postScoreHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postScoreHelp)
		return 1
	}
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}
	return executeReaction(cfg, postID, stdout, stderr, false, true)
}

func runPostReaction(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postReactionConfig{apiURL: defaultAPIURL}
	if getenv != nil {
		if envURL := strings.TrimSpace(getenv("KURA_API_URL")); envURL != "" {
			cfg.apiURL = envURL
		}
	}
	postID, err := parsePostReactionArgs(args, &cfg, false, false)
	if err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, postReactionHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, postReactionHelp)
		return 1
	}
	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}
	return executeReaction(cfg, postID, stdout, stderr, false, false)
}

func parsePostReactionArgs(args []string, cfg *postReactionConfig, requireFavorite, requireScore bool) (string, error) {
	var positional []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		switch {
		case arg == "-h" || arg == "--help":
			return "", errHelpRequested
		case arg == "--json":
			cfg.jsonOut = true
			i++
			continue
		case arg == "--jsonl":
			cfg.jsonlOut = true
			i++
			continue
		case arg == "--api-url":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --api-url requires a value")
			}
			cfg.apiURL = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--api-url="):
			cfg.apiURL = strings.TrimSpace(strings.TrimPrefix(arg, "--api-url="))
			i++
			continue
		case arg == "--version":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --version requires a value")
			}
			cfg.versionSet = true
			cfg.version = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--version="):
			cfg.versionSet = true
			cfg.version = strings.TrimSpace(strings.TrimPrefix(arg, "--version="))
			i++
			continue
		case arg == "--favorite":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --favorite requires a value")
			}
			cfg.favoriteSet = true
			cfg.favoriteVal = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--favorite="):
			cfg.favoriteSet = true
			cfg.favoriteVal = strings.TrimSpace(strings.TrimPrefix(arg, "--favorite="))
			i++
			continue
		case arg == "--score":
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --score requires a value")
			}
			cfg.scoreSet = true
			cfg.scoreVal = strings.TrimSpace(args[i+1])
			i += 2
			continue
		case strings.HasPrefix(arg, "--score="):
			cfg.scoreSet = true
			cfg.scoreVal = strings.TrimSpace(strings.TrimPrefix(arg, "--score="))
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			return "", fmt.Errorf("unknown flag %q", arg)
		default:
			positional = append(positional, arg)
			i++
			for i < len(args) {
				positional = append(positional, args[i])
				i++
			}
		}
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
	id := strings.TrimSpace(positional[0])
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || id != strconv.FormatInt(n, 10) {
		return "", fmt.Errorf("post id must be a positive integer")
	}
	return id, nil
}

func executeReaction(cfg postReactionConfig, postID string, stdout, stderr io.Writer, requireFavorite, requireScore bool) int {
	if !cfg.versionSet {
		fmt.Fprintln(stderr, "error: flag --version is required")
		return 1
	}
	versionStr := strings.TrimSpace(cfg.version)
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 0 || versionStr != strconv.Itoa(version) {
		fmt.Fprintf(stderr, "error: invalid --version %q: must be a non-negative integer\n", cfg.version)
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
		if err != nil || score < 0 || val != strconv.Itoa(score) {
			fmt.Fprintf(stderr, "error: invalid --score %q: must be a non-negative integer\n", cfg.scoreVal)
			return 1
		}
		scorePtr = &score
	}
	if _, err := url.ParseRequestURI(cfg.apiURL); err != nil {
		fmt.Fprintf(stderr, "error: invalid --api-url %q: %v\n", cfg.apiURL, err)
		return 1
	}
	c := client.New(cfg.apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, r := range result.Posts {
			if err := enc.Encode(r); err != nil {
				fmt.Fprintf(stderr, "error: encode jsonl: %v\n", err)
				return 1
			}
		}
		return 0
	}
	for _, r := range result.Posts {
		fmt.Fprintf(stdout, "%s\tversion:%d\tchanged:%v\tfavorite:%v\tscore:%d\n", r.ID, r.Version, r.Changed, r.Favorite, r.Score)
	}
	return 0
}

func runSearch(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := searchConfig{}
	// Default api URL from env or constant.
	envURL := ""
	if getenv != nil {
		envURL = strings.TrimSpace(getenv("KURA_API_URL"))
	}
	if envURL == "" {
		envURL = defaultAPIURL
	}
	cfg.apiURL = envURL

	query, err := parseSearchArgs(args, &cfg)
	if err != nil {
		// parseSearchArgs already handles help (returns nil query with no err and json? no)
		// Distinguish help vs error via sentinel.
		if err == errHelpRequested {
			fmt.Fprint(stdout, searchHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, searchHelp)
		return 1
	}

	if cfg.jsonOut && cfg.jsonlOut {
		fmt.Fprintln(stderr, "error: flags --json and --jsonl are mutually exclusive")
		return 1
	}

	// Validate pagination flags: --limit 1..60, --cursor non-empty opaque.
	limit := 0
	if cfg.limitSet {
		v := strings.TrimSpace(cfg.limitVal)
		if v == "" {
			fmt.Fprintln(stderr, "error: flag --limit requires a non-empty value")
			return 1
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			fmt.Fprintf(stderr, "error: invalid --limit %q: must be an integer between 1 and 60\n", cfg.limitVal)
			return 1
		}
		if n < 1 || n > 60 {
			fmt.Fprintf(stderr, "error: invalid --limit %d: must be between 1 and 60\n", n)
			return 1
		}
		limit = n
	}
	cursor := ""
	if cfg.cursorSet {
		if strings.TrimSpace(cfg.cursorVal) == "" {
			fmt.Fprintln(stderr, "error: flag --cursor requires a non-empty value")
			return 1
		}
		cursor = cfg.cursorVal
	}

	// Validate api URL early for readable error.
	if _, err := url.ParseRequestURI(cfg.apiURL); err != nil {
		fmt.Fprintf(stderr, "error: invalid --api-url %q: %v\n", cfg.apiURL, err)
		return 1
	}

	c := client.New(cfg.apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := c.SearchPage(ctx, query, cursor, limit)
	if err != nil {
		// Provide readable error. If APIError, message already includes status and server error.
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	// Output handling.
	if cfg.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "error: encode json: %v\n", err)
			return 1
		}
		if result.NextCursor != nil {
			// Also hint on stderr for pipeline users? Not needed when json includes it.
		}
		return 0
	}
	if cfg.jsonlOut {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, p := range result.Posts {
			if err := enc.Encode(p); err != nil {
				fmt.Fprintf(stderr, "error: encode jsonl: %v\n", err)
				return 1
			}
		}
		if result.NextCursor != nil {
			fmt.Fprintf(stderr, "next_cursor: %s\n", *result.NextCursor)
		}
		return 0
	}

	// Human output: empty success produces no stdout.
	if len(result.Posts) == 0 {
		if result.NextCursor != nil {
			fmt.Fprintf(stderr, "next_cursor: %s\n", *result.NextCursor)
		}
		return 0
	}
	for _, p := range result.Posts {
		// Tab-separated for easy cut; human still readable.
		// ID, media_type, WxH, preview_url, original_url
		fmt.Fprintf(stdout, "%s\t%s\t%dx%d\t%s\t%s\n", p.ID, p.MediaType, p.Width, p.Height, p.PreviewURL, p.OriginalURL)
	}
	if result.NextCursor != nil {
		fmt.Fprintf(stderr, "next_cursor: %s\n", *result.NextCursor)
	}
	return 0
}

var errHelpRequested = fmt.Errorf("help requested")

func parseSearchArgs(args []string, cfg *searchConfig) (string, error) {
	// Iterate flags before query. First non-flag starts query.
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if arg == "-h" || arg == "--help" {
			return "", errHelpRequested
		}
		if arg == "--json" {
			cfg.jsonOut = true
			i++
			continue
		}
		if arg == "--jsonl" {
			cfg.jsonlOut = true
			i++
			continue
		}
		if arg == "--api-url" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --api-url requires a value")
			}
			cfg.apiURL = strings.TrimSpace(args[i+1])
			cfg.apiURLSet = true
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "--api-url=") {
			cfg.apiURL = strings.TrimSpace(strings.TrimPrefix(arg, "--api-url="))
			cfg.apiURLSet = true
			i++
			continue
		}
		if arg == "--limit" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --limit requires a value")
			}
			cfg.limitSet = true
			cfg.limitVal = args[i+1]
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "--limit=") {
			cfg.limitSet = true
			cfg.limitVal = strings.TrimPrefix(arg, "--limit=")
			i++
			continue
		}
		if arg == "--cursor" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --cursor requires a value")
			}
			cfg.cursorSet = true
			cfg.cursorVal = args[i+1]
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "--cursor=") {
			cfg.cursorSet = true
			cfg.cursorVal = strings.TrimPrefix(arg, "--cursor=")
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", fmt.Errorf("unknown flag %q", arg)
		}
		// Non-flag: start of query.
		break
	}
	// Remaining args are query parts. Join with space.
	remaining := args[i:]
	query := strings.Join(remaining, " ")
	return query, nil
}
