package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MetadataTarget identifies one post with its expected metadata version.
type MetadataTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// MetadataRequest applies supplied source and/or artist to one target. A nil
// field is omitted and leaves the value unchanged; a pointer to an empty
// string clears it.
type MetadataRequest struct {
	Posts  []MetadataTarget `json:"posts"`
	Source *string          `json:"source,omitempty"`
	Artist *string          `json:"artist,omitempty"`
}

// MetadataResult is one acknowledged metadata state.
type MetadataResult struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Source  string `json:"source"`
	Artist  string `json:"artist"`
	Changed bool   `json:"changed"`
}

// MetadataResponse is the envelope for POST /api/posts/metadata.
type MetadataResponse struct {
	Posts []MetadataResult `json:"posts"`
}

func validateMetadataField(label string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if !utf8.ValidString(*value) {
		return nil, fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.ContainsRune(*value, '\x00') {
		return nil, fmt.Errorf("%s must not contain NUL", label)
	}
	trimmed := strings.TrimSpace(*value)
	if utf8.RuneCountInString(trimmed) > 512 {
		return nil, fmt.Errorf("%s must be at most 512 characters", label)
	}
	return &trimmed, nil
}

// EditMetadata updates source and/or artist for exactly one post. It never
// retries, and a malformed or mismatched acknowledgement is uncertain.
func (c *Client) EditMetadata(ctx context.Context, request MetadataRequest) (MetadataResponse, error) {
	if len(request.Posts) != 1 {
		return MetadataResponse{}, fmt.Errorf("metadata edit requires exactly one post")
	}
	if request.Source == nil && request.Artist == nil {
		return MetadataResponse{}, fmt.Errorf("metadata edit requires at least one of source or artist")
	}
	target := request.Posts[0]
	n, err := strconv.ParseInt(target.ID, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != target.ID {
		return MetadataResponse{}, fmt.Errorf("metadata target requires a canonical positive post id")
	}
	if target.Version < 0 || target.Version > 2147483647 {
		return MetadataResponse{}, fmt.Errorf("metadata target version must be between 0 and 2147483647")
	}
	source, err := validateMetadataField("source", request.Source)
	if err != nil {
		return MetadataResponse{}, err
	}
	artist, err := validateMetadataField("artist", request.Artist)
	if err != nil {
		return MetadataResponse{}, err
	}
	request.Source, request.Artist = source, artist

	var receipt struct {
		Posts []struct {
			ID      *string `json:"id"`
			Version *int    `json:"version"`
			Source  *string `json:"source"`
			Artist  *string `json:"artist"`
			Changed *bool   `json:"changed"`
		} `json:"posts"`
	}
	if err := c.postJSON(ctx, "/api/posts/metadata", request, &receipt); err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) && apiError.Status >= 500 {
			return MetadataResponse{}, &RequestError{Err: err, Uncertain: true}
		}
		return MetadataResponse{}, err
	}
	invalid := func() (MetadataResponse, error) {
		return MetadataResponse{}, &RequestError{Err: fmt.Errorf("invalid metadata acknowledgement; check the post before retrying"), Uncertain: true}
	}
	if len(receipt.Posts) != 1 {
		return invalid()
	}
	item := receipt.Posts[0]
	if item.ID == nil || item.Version == nil || item.Source == nil || item.Artist == nil || item.Changed == nil {
		return invalid()
	}
	if *item.ID != target.ID || *item.Version < 0 || *item.Version > 2147483647 {
		return invalid()
	}
	wantVersion := target.Version
	if *item.Changed {
		if target.Version == 2147483647 {
			return invalid()
		}
		wantVersion++
	}
	if *item.Version != wantVersion {
		return invalid()
	}
	if request.Source != nil && *item.Source != *request.Source {
		return invalid()
	}
	if request.Artist != nil && *item.Artist != *request.Artist {
		return invalid()
	}
	return MetadataResponse{Posts: []MetadataResult{{ID: *item.ID, Version: *item.Version, Source: *item.Source, Artist: *item.Artist, Changed: *item.Changed}}}, nil
}
