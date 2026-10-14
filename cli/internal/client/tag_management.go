package client

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ListTagsPage reads one authenticated taxonomy page. Cursor values are opaque.
func (c *Client) ListTagsPage(ctx context.Context, prefix, cursor string, limit int) (TagManageListResponse, error) {
	if limit < 1 || limit > 60 {
		return TagManageListResponse{}, fmt.Errorf("limit must be between 1 and 60, got %d", limit)
	}
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	var result TagManageListResponse
	if err := c.requestWithProtectedRead(ctx, http.MethodGet, "/api/tags/manage", query, nil, &result, true); err != nil {
		return TagManageListResponse{}, err
	}
	if result.Tags == nil {
		result.Tags = []TagSummary{}
	}
	return result, nil
}

// PreviewTagManage previews one global taxonomy rename or merge.
func (c *Client) PreviewTagManage(ctx context.Context, mode, source, target string) (TagManageResult, error) {
	request, err := newTagManageRequest(mode, source, target, nil)
	if err != nil {
		return TagManageResult{}, err
	}
	var result TagManageResult
	if err := c.postJSON(ctx, "/api/tags/manage/preview", request, &result); err != nil {
		return TagManageResult{}, err
	}
	if !matchesTagManage(result, request) {
		return TagManageResult{}, &RequestError{Err: fmt.Errorf("tag preview identifies a different operation"), Uncertain: true}
	}
	return result, nil
}

// ApplyTagManage applies a previewed taxonomy operation with its observed
// version. The shared transport validates the required compact acknowledgement.
func (c *Client) ApplyTagManage(ctx context.Context, mode, source, target string, version int64) (TagManageApplyResult, error) {
	request, err := newTagManageRequest(mode, source, target, &version)
	if err != nil {
		return TagManageApplyResult{}, err
	}
	if version == math.MaxInt64 {
		return TagManageApplyResult{}, fmt.Errorf("expected version cannot be incremented")
	}
	var result TagManageApplyResult
	if err := c.postJSON(ctx, "/api/tags/manage/apply", request, &result); err != nil {
		return TagManageApplyResult{}, err
	}
	if result.Mode != mode || result.SourceTag != request.SourceTag || result.TargetTag != request.TargetTag {
		return TagManageApplyResult{}, &RequestError{Err: fmt.Errorf("tag management acknowledgement identifies a different operation"), Uncertain: true}
	}
	if result.Version != version+1 {
		return TagManageApplyResult{}, &RequestError{Err: fmt.Errorf("tag management acknowledgement has an unexpected taxonomy version"), Uncertain: true}
	}
	return result, nil
}

func newTagManageRequest(mode, source, target string, version *int64) (TagManageRequest, error) {
	if mode != "rename" && mode != "merge" {
		return TagManageRequest{}, fmt.Errorf("tag management mode must be rename or merge")
	}
	source, err := canonicalManageTag(source)
	if err != nil {
		return TagManageRequest{}, fmt.Errorf("source tag: %w", err)
	}
	target, err = canonicalManageTag(target)
	if err != nil {
		return TagManageRequest{}, fmt.Errorf("target tag: %w", err)
	}
	if source == target {
		return TagManageRequest{}, fmt.Errorf("source and target tags must be different")
	}
	if version != nil && *version < 0 {
		return TagManageRequest{}, fmt.Errorf("expected version must be non-negative")
	}
	return TagManageRequest{Mode: mode, SourceTag: source, TargetTag: target, ExpectedVersion: version}, nil
}

func canonicalManageTag(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("must be valid UTF-8")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("must not be empty")
	}
	if strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("must not contain NUL")
	}
	if utf8.RuneCountInString(value) > 128 {
		return "", fmt.Errorf("must be at most 128 Unicode characters")
	}
	return value, nil
}

func matchesTagManage(result TagManageResult, request TagManageRequest) bool {
	return result.Mode == request.Mode && result.SourceTag == request.SourceTag && result.TargetTag == request.TargetTag
}
