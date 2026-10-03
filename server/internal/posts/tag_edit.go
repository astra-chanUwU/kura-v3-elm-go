package posts

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	maxTagEditPosts = 100
	maxTagEditTags  = 100
	maxTagLength    = 128
)

// ValidateTagEdit normalizes tags and validates the bounded mutation shape.
// It is shared by HTTP and storage-facing callers so invalid requests never
// open a transaction.
func ValidateTagEdit(request TagEditRequest) (TagEditRequest, error) {
	if len(request.Posts) == 0 || len(request.Posts) > maxTagEditPosts {
		return TagEditRequest{}, fmt.Errorf("%w: posts must contain 1 to 100 targets", ErrInvalidTags)
	}
	seenPosts := make(map[string]struct{}, len(request.Posts))
	for index, target := range request.Posts {
		id, err := strconv.ParseInt(strings.TrimSpace(target.ID), 10, 64)
		if err != nil || id <= 0 || strings.TrimSpace(target.ID) != strconv.FormatInt(id, 10) {
			return TagEditRequest{}, fmt.Errorf("%w: posts[%d].id must be a positive integer", ErrInvalidTags, index)
		}
		if target.Version < 0 {
			return TagEditRequest{}, fmt.Errorf("%w: posts[%d].version must be non-negative", ErrInvalidTags, index)
		}
		if _, exists := seenPosts[target.ID]; exists {
			return TagEditRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidTags, target.ID)
		}
		seenPosts[target.ID] = struct{}{}
	}

	add, err := normalizeTags(request.Add)
	if err != nil {
		return TagEditRequest{}, err
	}
	remove, err := normalizeTags(request.Remove)
	if err != nil {
		return TagEditRequest{}, err
	}
	if len(add)+len(remove) > maxTagEditTags {
		return TagEditRequest{}, fmt.Errorf("%w: at most 100 tags may be changed", ErrInvalidTags)
	}
	removeSet := make(map[string]struct{}, len(remove))
	for _, tag := range remove {
		removeSet[tag] = struct{}{}
	}
	for _, tag := range add {
		if _, overlap := removeSet[tag]; overlap {
			return TagEditRequest{}, fmt.Errorf("%w: tag %q appears in add and remove", ErrInvalidTags, tag)
		}
	}
	request.Add, request.Remove = add, remove
	return request, nil
}

func normalizeTags(tags []string) ([]string, error) {
	normalized := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" || utf8.RuneCountInString(tag) > maxTagLength {
			return nil, fmt.Errorf("%w: tags must be non-empty and at most 128 characters", ErrInvalidTags)
		}
		if _, duplicate := seen[tag]; duplicate {
			return nil, fmt.Errorf("%w: duplicate tag %q", ErrInvalidTags, tag)
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	return normalized, nil
}

// applyTagEdit updates one row while its transaction lock is held.
func (s *PostgresSearcher) applyTagEdit(ctx context.Context, tx pgx.Tx, target TagTarget, add, remove []string) (TagEditResult, error) {
	var currentTags []string
	var searchText string
	var currentVersion int
	err := tx.QueryRow(ctx, lockPostForTagEditSQL, target.ID).Scan(&currentTags, &searchText, &currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return TagEditResult{}, ErrNotFound
	}
	if err != nil {
		return TagEditResult{}, err
	}
	if currentVersion != target.Version {
		return TagEditResult{}, ErrConflict
	}

	removeSet := make(map[string]struct{}, len(remove))
	for _, tag := range remove {
		removeSet[tag] = struct{}{}
	}
	present := make(map[string]struct{}, len(currentTags))
	nextTags := make([]string, 0, len(currentTags)+len(add))
	removed := make([]string, 0, len(remove))
	for _, tag := range currentTags {
		if _, shouldRemove := removeSet[tag]; shouldRemove {
			removed = append(removed, tag)
			continue
		}
		nextTags = append(nextTags, tag)
		present[tag] = struct{}{}
	}
	added := make([]string, 0, len(add))
	for _, tag := range add {
		if _, exists := present[tag]; exists {
			continue
		}
		nextTags = append(nextTags, tag)
		present[tag] = struct{}{}
		added = append(added, tag)
	}

	result := TagEditResult{ID: target.ID, Version: currentVersion, Tags: nextTags}
	if len(added) == 0 && len(removed) == 0 {
		return result, nil
	}

	nextVersion := currentVersion + 1
	updatedSearch := updateSearchText(searchText, removed, added)
	if _, err := tx.Exec(ctx, updatePostTagsSQL, nextTags, updatedSearch, nextVersion, target.ID); err != nil {
		return TagEditResult{}, err
	}
	if _, err := tx.Exec(ctx, insertPostRevisionSQL, target.ID, nextVersion, added, removed); err != nil {
		return TagEditResult{}, err
	}
	result.Version = nextVersion
	result.Changed = true
	return result, nil
}

// updateSearchText preserves existing terms while replacing tag terms. Tags
// are removed as contiguous whitespace-delimited phrases, then newly added
// tags are appended so the generated tsvector immediately reflects edits.
func updateSearchText(searchText string, removed, added []string) string {
	words := strings.Fields(searchText)
	for _, tag := range removed {
		tagWords := strings.Fields(tag)
		if len(tagWords) == 0 {
			continue
		}
		filtered := words[:0]
		for index := 0; index < len(words); {
			matches := index+len(tagWords) <= len(words)
			if matches {
				for offset, tagWord := range tagWords {
					if !strings.EqualFold(words[index+offset], tagWord) {
						matches = false
						break
					}
				}
			}
			if matches {
				index += len(tagWords)
				continue
			}
			filtered = append(filtered, words[index])
			index++
		}
		words = filtered
	}
	if len(added) > 0 {
		words = append(words, added...)
	}
	return strings.Join(words, " ")
}
