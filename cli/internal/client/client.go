package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PostSummary is the stable browser-facing representation from GET /api/posts.
type PostSummary struct {
	ID          string `json:"id"`
	PreviewURL  string `json:"preview_url"`
	OriginalURL string `json:"original_url"`
	MediaType   string `json:"media_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

// SearchResponse is the envelope for GET /api/posts.
// next_cursor is optional and forwards pagination when the API adds it.
type SearchResponse struct {
	Posts      []PostSummary `json:"posts"`
	NextCursor *string       `json:"next_cursor,omitempty"`
}

// Client talks to the Kura HTTP API. It uses only the public API surface
// (GET /api/posts, GET /health) and does not import server implementation.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New creates a client with a default 10s timeout. BaseURL is trimmed of
// trailing slashes.
func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// APIError is a readable error derived from the API JSON body or HTTP status.
type APIError struct {
	Status  int
	Message string
	Body    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("api error %d: %s", e.Status, e.Message)
	}
	if e.Body != "" {
		return fmt.Sprintf("api error %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("api error %d: %s", e.Status, http.StatusText(e.Status))
}

// Search executes GET /api/posts?q=query with URL encoding and a request
// timeout. An empty or whitespace-only query is sent as q= (server returns
// empty posts without DB contact).
func (c *Client) Search(ctx context.Context, query string) (SearchResponse, error) {
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := url.Parse(base)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("invalid api url %q: %w", base, err)
	}
	// Ensure we request /api/posts.
	u.Path = strings.TrimRight(u.Path, "/") + "/api/posts"
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return SearchResponse{}, err
	}
	req.Header.Set("Accept", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return SearchResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract {"error":"..."}.
		var apiMsg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &apiMsg) == nil && apiMsg.Error != "" {
			return SearchResponse{}, &APIError{Status: resp.StatusCode, Message: apiMsg.Error, Body: strings.TrimSpace(string(body))}
		}
		return SearchResponse{}, &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var result SearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return SearchResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if result.Posts == nil {
		result.Posts = []PostSummary{}
	}
	return result, nil
}
