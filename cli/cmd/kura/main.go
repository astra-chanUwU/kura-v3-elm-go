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

Commands:
  search    Search posts via GET /api/posts

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
or empty the server returns an empty browse result without contacting the
database. Multi-word queries may be quoted or passed as separate words:

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
	case "post":
		// No detail endpoint exists in the current API (only GET /api/posts and
		// GET /health). Do not invent one; explain and exit non-zero.
		fmt.Fprintln(stderr, "error: post detail endpoint is not available in the current API")
		fmt.Fprintln(stderr, "only 'kura search' is supported; the server exposes GET /api/posts and GET /health")
		return 1
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", args[0])
		fmt.Fprint(stderr, rootHelp)
		return 1
	}
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
