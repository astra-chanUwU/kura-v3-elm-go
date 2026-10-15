package main

import (
	"context"
	"fmt"
	"io"
)

type postShowConfig struct {
	commonFlags
}

type postTagEditConfig struct {
	commonFlags
	versionSet bool
	version    string
	addTags    []string
	removeTags []string
}

type postTagRevertConfig struct {
	commonFlags
	versionSet    bool
	version       string
	targetSet     bool
	targetVersion string
}

type postReactionConfig struct {
	commonFlags
	versionSet  bool
	version     string
	favoriteSet bool
	favoriteVal string
	scoreSet    bool
	scoreVal    string
}

func runPost(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, postHelp)
		return 0
	}
	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "show", "get":
		return runPostShow(ctx, rest, stdout, stderr, getenv)
	case "tag":
		if len(rest) == 0 || rest[0] == "-h" || rest[0] == "--help" || rest[0] == "help" {
			fmt.Fprint(stdout, postTagHelp)
			return 0
		}
		sub := rest[0]
		subrest := rest[1:]
		switch sub {
		case "edit":
			return runPostTagEdit(ctx, subrest, stdout, stderr, getenv)
		case "revert":
			return runPostTagRevert(ctx, subrest, stdout, stderr, getenv)
		case "-h", "--help", "help":
			fmt.Fprint(stdout, postTagHelp)
			return 0
		default:
			fmt.Fprintf(stderr, "error: unknown post tag command %q\n", sub)
			fmt.Fprint(stderr, postTagHelp)
			return 1
		}
	case "tag-edit", "tag_edit":
		return runPostTagEdit(ctx, rest, stdout, stderr, getenv)
	case "tag-revert", "tag_revert":
		return runPostTagRevert(ctx, rest, stdout, stderr, getenv)
	case "metadata":
		return runPostMetadata(ctx, rest, stdout, stderr, getenv)
	case "trash":
		return runPostTrash(ctx, rest, stdout, stderr, getenv)
	case "restore":
		return runPostRestore(ctx, rest, stdout, stderr, getenv)
	case "trash-list":
		return runPostTrashList(ctx, rest, stdout, stderr, getenv)
	case "favorite":
		return runPostFavorite(ctx, rest, stdout, stderr, getenv)
	case "rating":
		return runPostRating(ctx, rest, stdout, stderr, getenv)
	case "score":
		return runPostScore(ctx, rest, stdout, stderr, getenv)
	case "reaction":
		return runPostReaction(ctx, rest, stdout, stderr, getenv)
	default:
		fmt.Fprintf(stderr, "error: unknown post command %q\n", cmd)
		fmt.Fprint(stderr, postHelp)
		return 1
	}
}

func runPostShow(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg := postShowConfig{commonFlags: newCommonFlags(getenv)}
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
	if !cfg.checkExclusive(stderr) {
		return 1
	}
	c, ok := cfg.client(ctx, getenv, stderr)
	if !ok {
		return 1
	}
	detail, err := c.GetPostDetail(ctx, postID)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if cfg.jsonOut {
		return encodeJSON(stdout, detail)
	}
	if cfg.jsonlOut {
		return encodeJSONL(stdout, detail, stderr)
	}
	return writePostDetailHuman(detail, stdout)
}

func parsePostShowArgs(args []string, cfg *postShowConfig) (string, error) {
	positional, err := parseFlagArgs(args, cfg.specs(nil))
	if err != nil {
		return "", err
	}
	if len(positional) != 1 {
		return "", fmt.Errorf("post show requires exactly one POST_ID")
	}
	return parsePositiveID(positional[0], "post id")
}
