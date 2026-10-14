package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// TrashRequest is the body for POST /api/posts/trash and /api/posts/restore.
type TrashRequest struct {
	PostIDs []string `json:"post_ids"`
	Reason  *string  `json:"reason,omitempty"`
}

// TrashResult is one trash or restore acknowledgement.
type TrashResult struct {
	ID      string `json:"id"`
	Changed bool   `json:"changed"`
}

// TrashResponse is the envelope returned by trash and restore.
type TrashResponse struct {
	Posts []TrashResult `json:"posts"`
}

// TrashPost is a trashed post summary with recovery metadata.
type TrashPost struct {
	PostSummary
	DeletedAt       string `json:"deleted_at"`
	ModerationState string `json:"moderation_state"`
}

// TrashListResponse is one protected page from GET /api/posts/trash.
type TrashListResponse struct {
	Posts      []TrashPost `json:"posts"`
	NextCursor *string     `json:"next_cursor"`
}

func validateTrashIDs(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > 100 {
		return nil, fmt.Errorf("trash requires 1 to 100 post ids")
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, len(ids))
	for i, raw := range ids {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != raw {
			return nil, fmt.Errorf("post id %q must be a canonical positive integer", raw)
		}
		if seen[raw] {
			return nil, fmt.Errorf("duplicate post id %q", raw)
		}
		seen[raw] = true
		out[i] = raw
	}
	return out, nil
}

func (c *Client) changeTrash(ctx context.Context, path string, ids []string, reason *string) (TrashResponse, error) {
	validated, err := validateTrashIDs(ids)
	if err != nil {
		return TrashResponse{}, err
	}
	request := TrashRequest{PostIDs: validated}
	if reason != nil {
		if !utf8.ValidString(*reason) {
			return TrashResponse{}, fmt.Errorf("reason must be valid UTF-8")
		}
		if strings.ContainsRune(*reason, '\x00') {
			return TrashResponse{}, fmt.Errorf("reason must not contain NUL")
		}
		trimmed := strings.TrimSpace(*reason)
		if utf8.RuneCountInString(trimmed) > 512 {
			return TrashResponse{}, fmt.Errorf("reason must be at most 512 characters")
		}
		request.Reason = &trimmed
	}
	var receipt struct {
		Posts []struct {
			ID      *string `json:"id"`
			Changed *bool   `json:"changed"`
		} `json:"posts"`
	}
	if err := c.postJSON(ctx, path, request, &receipt); err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) && apiError.Status >= 500 {
			return TrashResponse{}, &RequestError{Err: err, Uncertain: true}
		}
		return TrashResponse{}, err
	}
	invalid := func() (TrashResponse, error) {
		return TrashResponse{}, &RequestError{Err: fmt.Errorf("invalid trash acknowledgement; check the posts before retrying"), Uncertain: true}
	}
	if len(receipt.Posts) != len(validated) {
		return invalid()
	}
	expected := make(map[string]bool, len(validated))
	for _, id := range validated {
		expected[id] = true
	}
	result := TrashResponse{Posts: make([]TrashResult, 0, len(receipt.Posts))}
	for _, item := range receipt.Posts {
		if item.ID == nil || item.Changed == nil || !expected[*item.ID] {
			return invalid()
		}
		delete(expected, *item.ID)
		result.Posts = append(result.Posts, TrashResult{ID: *item.ID, Changed: *item.Changed})
	}
	if len(expected) != 0 {
		return invalid()
	}
	return result, nil
}

// TrashPosts moves posts to trash via POST /api/posts/trash.
func (c *Client) TrashPosts(ctx context.Context, ids []string, reason *string) (TrashResponse, error) {
	return c.changeTrash(ctx, "/api/posts/trash", ids, reason)
}

// RestorePosts restores trashed posts via POST /api/posts/restore.
func (c *Client) RestorePosts(ctx context.Context, ids []string) (TrashResponse, error) {
	return c.changeTrash(ctx, "/api/posts/restore", ids, nil)
}

// ListTrash reads one protected trash page via GET /api/posts/trash.
func (c *Client) ListTrash(ctx context.Context, cursor string, limit int) (TrashListResponse, error) {
	if limit != 0 && (limit < 1 || limit > 60) {
		return TrashListResponse{}, fmt.Errorf("limit must be between 1 and 60, got %d", limit)
	}
	query := url.Values{}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if limit != 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var result TrashListResponse
	if err := c.requestWithProtectedRead(ctx, http.MethodGet, "/api/posts/trash", query, nil, &result, true); err != nil {
		return TrashListResponse{}, err
	}
	if result.Posts == nil {
		result.Posts = []TrashPost{}
	}
	return result, nil
}
