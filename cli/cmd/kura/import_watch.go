package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/folderimport"
)

const importHelp = `kura import - poll one folder and import stable files

Usage:
  kura import --dir DIRECTORY [flags]

The command is disabled unless you run it. Without --apply it prints a
preview and does not upload or write a checkpoint. --interval turns on
polling; otherwise it scans once. Source files are never deleted or moved.

Flags:
  --dir DIRECTORY     Existing directory to watch. A filesystem root is refused.
  --apply             Upload stable files. Requires --checkpoint.
  --checkpoint PATH   Receipt file outside the watched directory. It stores
                      the library target and account id, never a credential.
  --interval DURATION Poll again after each pass, at least 5s.
  --settle DURATION   Wait for a file size to stay unchanged (default 1s).
  --collection ID     Attach imported posts to this collection.
  --tags LIST         Comma-separated tags applied to every imported file.
  --artist TEXT       Shared artist metadata.
  --source TEXT       Shared source metadata.
  --api-url URL       Base API URL (env KURA_API_URL)
  --api-token VALUE   Deployment admin token. Prefer KURA_API_TOKEN.
`

type importConfig struct {
	commonFlags
	dir        string
	checkpoint string
	apply      bool
	interval   string
	settle     string
	collection string
	tags       string
	artist     string
	source     string
}

func runImport(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := importConfig{commonFlags: newCommonFlags(getenv)}
	if _, err := parseFlagArgs(args, cfg.specs(map[string]flagSpec{
		"--dir":        valueFlag(func(v string) { cfg.dir = strings.TrimSpace(v) }),
		"--checkpoint": valueFlag(func(v string) { cfg.checkpoint = strings.TrimSpace(v) }),
		"--apply":      boolFlag(func() { cfg.apply = true }),
		"--interval":   valueFlag(func(v string) { cfg.interval = strings.TrimSpace(v) }),
		"--settle":     valueFlag(func(v string) { cfg.settle = strings.TrimSpace(v) }),
		"--collection": valueFlag(func(v string) { cfg.collection = strings.TrimSpace(v) }),
		"--tags":       valueFlag(func(v string) { cfg.tags = v }),
		"--artist":     valueFlag(func(v string) { cfg.artist = strings.TrimSpace(v) }),
		"--source":     valueFlag(func(v string) { cfg.source = strings.TrimSpace(v) }),
	})); err != nil {
		if err == errHelpRequested {
			fmt.Fprint(stdout, importHelp)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.dir == "" {
		fmt.Fprintln(stderr, "error: --dir is required")
		return 1
	}
	settle := time.Second
	if cfg.settle != "" {
		parsed, err := time.ParseDuration(cfg.settle)
		if err != nil || parsed < 0 {
			fmt.Fprintln(stderr, "error: --settle must be a duration")
			return 1
		}
		settle = parsed
	}
	var interval time.Duration
	if cfg.interval != "" {
		parsed, err := time.ParseDuration(cfg.interval)
		if err != nil || parsed < 5*time.Second {
			fmt.Fprintln(stderr, "error: --interval must be at least 5s")
			return 1
		}
		interval = parsed
	}
	var tags []string
	if strings.TrimSpace(cfg.tags) != "" {
		for _, tag := range strings.Split(cfg.tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	var importer folderimport.Importer
	if cfg.apply {
		if !cfg.checkExclusive(stderr) {
			return 1
		}
		api, ok := cfg.client(ctx, getenv, stderr)
		if !ok {
			return 1
		}
		if api.HTTPClient != nil {
			api.HTTPClient.Timeout = 2 * time.Minute
		}
		importer = folderClient{client: api}
	}
	err := folderimport.Run(ctx, folderimport.Options{
		Root: cfg.dir, Checkpoint: cfg.checkpoint, Apply: cfg.apply,
		CollectionID: cfg.collection, Tags: tags, Artist: cfg.artist, Source: cfg.source,
		Settle: settle, Interval: interval, Importer: importer, Stdout: stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

type folderClient struct{ client *client.Client }

func (f folderClient) Identity(ctx context.Context) (string, string, error) {
	return f.client.ImportIdentity(ctx)
}

func (f folderClient) Duplicates(ctx context.Context, hashes []string) (map[string]string, error) {
	return f.client.ImportDuplicates(ctx, hashes)
}

func (f folderClient) Import(ctx context.Context, request folderimport.ImportRequest) (folderimport.ImportResult, error) {
	body, err := request.Open()
	if err != nil {
		return folderimport.ImportResult{}, err
	}
	defer body.Close()
	result, uncertain, err := f.client.ImportFolderFile(ctx, client.FolderImport{
		Filename: request.Rel, Body: body, ExpectedHash: request.ExpectedHash,
		CollectionID: request.CollectionID, Tags: request.Tags, Artist: request.Artist, Source: request.Source,
	})
	if uncertain {
		return folderimport.ImportResult{Uncertain: true}, err
	}
	if err != nil {
		return folderimport.ImportResult{}, err
	}
	return folderimport.ImportResult{PostID: result.PostID, Duplicate: result.Duplicate}, nil
}
