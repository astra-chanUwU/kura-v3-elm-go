package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

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

// encodeJSONLList streams one JSON object per item, stopping at the first
// encode failure.
func encodeJSONLList[T any](stdout io.Writer, items []T, stderr io.Writer) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			fmt.Fprintf(stderr, "error: encode jsonl: %v\n", err)
			return 1
		}
	}
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

func writeCollections(result client.CollectionsResponse, cfg collectionConfig, stdout, stderr io.Writer) int {
	if cfg.jsonOut {
		return encodeJSON(stdout, result)
	}
	if cfg.jsonlOut {
		return encodeJSONLList(stdout, result.Collections, stderr)
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
		if code := encodeJSONLList(stdout, result.Posts, stderr); code != 0 {
			return code
		}
	} else {
		for _, post := range result.Posts {
			if _, err := fmt.Fprintf(stdout, "%s\t%s\t%dx%d\t%s\t%s\n", post.ID, post.MediaType, post.Width, post.Height, post.PreviewURL, post.OriginalURL); err != nil {
				fmt.Fprintf(stderr, "error: write output: %v\n", err)
				return 1
			}
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
			if rev.TargetTags != nil {
				fmt.Fprintf(stdout, " target:[%s]", strings.Join(*rev.TargetTags, ","))
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
