package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// PostSummary is the stable browser-facing representation from GET /api/posts.
type PostSummary struct {
	ID          string   `json:"id"`
	PreviewURL  string   `json:"preview_url"`
	OriginalURL string   `json:"original_url"`
	MediaType   string   `json:"media_type"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Tags        []string `json:"tags,omitempty"`
}

// SearchResponse is the envelope for GET /api/posts.
// next_cursor is optional and forwards pagination when the API adds it.
// collection_version is present only on collection page responses.
type SearchResponse struct {
	Posts             []PostSummary `json:"posts"`
	NextCursor        *string       `json:"next_cursor,omitempty"`
	CollectionVersion *int64        `json:"collection_version,omitempty"`
}

// Collection is the stable browser-facing representation from
// GET /api/collections. Version is the membership/order version and is
// zero for responses from servers that predate versioned pagination.
type Collection struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	PostIDs []string `json:"post_ids"`
	Version int64    `json:"version,omitempty"`
}

// CollectionsResponse is the envelope for GET /api/collections.
type CollectionsResponse struct {
	Collections []Collection `json:"collections"`
}

// CreateCollectionRequest is the payload for POST /api/collections.
type CreateCollectionRequest struct {
	Name string `json:"name"`
}

// AddCollectionPostsRequest is the payload for
// POST /api/collections/{id}/posts.
type AddCollectionPostsRequest struct {
	PostIDs []string `json:"post_ids"`
}

// ReorderCollectionRequest is the payload for
// POST /api/collections/{id}/order.
type ReorderCollectionRequest struct {
	PostIDs []string `json:"post_ids"`
}

// PostDetail is the stable read-only representation from GET /api/posts/{id}.
// It extends PostSummary with Inspector metadata and revision histories.
type PostDetail struct {
	PostSummary
	Source          string             `json:"source"`
	Artist          string             `json:"artist"`
	Hash            string             `json:"hash"`
	FileSize        int64              `json:"file_size"`
	CreatedAt       string             `json:"created_at"`
	TagVersion      int                `json:"tag_version"`
	Favorite        bool               `json:"favorite"`
	Score           int                `json:"score"`
	ReactionVersion int                `json:"reaction_version"`
	History         []Revision         `json:"history"`
	ReactionHistory []ReactionRevision `json:"reaction_history"`
}

// Revision is an immutable tag-edit entry.
type Revision struct {
	Version     int      `json:"version"`
	Kind        string   `json:"kind"`
	AddedTags   []string `json:"added_tags"`
	RemovedTags []string `json:"removed_tags"`
	TargetTags  []string `json:"target_tags,omitempty"`
	CreatedAt   string   `json:"created_at"`
}

// ReactionRevision is an immutable favorite/score entry.
type ReactionRevision struct {
	Version   int    `json:"version"`
	Favorite  bool   `json:"favorite"`
	Score     int    `json:"score"`
	CreatedAt string `json:"created_at"`
}

// TagTarget identifies a post with its expected tag version.
type TagTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// TagEditRequest is the payload for POST /api/posts/tags.
type TagEditRequest struct {
	Posts  []TagTarget `json:"posts"`
	Add    []string    `json:"add"`
	Remove []string    `json:"remove"`
}

// TagEditResult is one post result from a tag mutation.
type TagEditResult struct {
	ID      string   `json:"id"`
	Version int      `json:"version"`
	Tags    []string `json:"tags"`
	Changed bool     `json:"changed"`
}

// TagEditResponse is the envelope for tag mutations.
type TagEditResponse struct {
	Posts []TagEditResult `json:"posts"`
}

// TagRevertRequest is the payload for POST /api/posts/tags/revert.
type TagRevertRequest struct {
	Posts         []TagTarget `json:"posts"`
	TargetVersion int         `json:"target_version"`
}

// TagRevertResponse is the envelope for tag reverts.
type TagRevertResponse struct {
	Posts []TagEditResult `json:"posts"`
}

// ReactionTarget identifies a post with its expected reaction version.
type ReactionTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// ReactionRequest is the payload for POST /api/posts/reactions.
type ReactionRequest struct {
	Posts    []ReactionTarget `json:"posts"`
	Favorite *bool            `json:"favorite,omitempty"`
	Score    *int             `json:"score,omitempty"`
}

// ReactionResult is one post result from a reaction mutation.
type ReactionResult struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Favorite bool   `json:"favorite"`
	Score    int    `json:"score"`
	Changed  bool   `json:"changed"`
}

// ReactionResponse is the envelope for reaction mutations.
type ReactionResponse struct {
	Posts []ReactionResult `json:"posts"`
}

// Client talks to the Kura HTTP API. It uses only the public API surface
// (GET /api/posts, GET /health) and does not import server implementation.
type Client struct {
	BaseURL    string
	APIToken   string
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
		BaseURL:  baseURL,
		APIToken: strings.TrimSpace(os.Getenv("KURA_API_TOKEN")),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) authorize(req *http.Request) {
	if req.Method == http.MethodGet || req.Method == http.MethodHead || req.Method == http.MethodOptions {
		return
	}
	if token := strings.TrimSpace(c.APIToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
// the newest visible posts). It is a first-page wrapper around
// SearchPage with no cursor and no limit.
func (c *Client) Search(ctx context.Context, query string) (SearchResponse, error) {
	return c.SearchPage(ctx, query, "", 0)
}

// SearchPage executes GET /api/posts?q=&cursor=&limit= with cursor pagination.
// query is sent as q (empty allowed for browse). cursor is opaque and sent as
// cursor when non-empty. limit is page size 1..60; 0 means omit and let the
// server use its default. Values outside 1..60 return an error without
// contacting the server.
func (c *Client) SearchPage(ctx context.Context, query, cursor string, limit int) (SearchResponse, error) {
	if limit != 0 && (limit < 1 || limit > 60) {
		return SearchResponse{}, fmt.Errorf("limit must be between 1 and 60, got %d", limit)
	}
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
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit != 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return SearchResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	c.authorize(req)

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

// ListCollections executes GET /api/collections.
func (c *Client) ListCollections(ctx context.Context) (CollectionsResponse, error) {
	var result CollectionsResponse
	if err := c.getJSON(ctx, "/api/collections", &result); err != nil {
		return CollectionsResponse{}, err
	}
	if result.Collections == nil {
		result.Collections = []Collection{}
	}
	return result, nil
}

// CollectionPosts executes GET /api/collections/{id}/posts. The collection
// id is escaped as a path segment; the server remains responsible for
// validating whether it identifies an existing collection.
func (c *Client) CollectionPosts(ctx context.Context, collectionID string) (SearchResponse, error) {
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return SearchResponse{}, fmt.Errorf("collection id must not be empty")
	}
	var result SearchResponse
	path := "/api/collections/" + url.PathEscape(collectionID) + "/posts"
	if err := c.getJSON(ctx, path, &result); err != nil {
		return SearchResponse{}, err
	}
	if result.Posts == nil {
		result.Posts = []PostSummary{}
	}
	return result, nil
}

// ListCollectionPosts is an explicit alias for callers that prefer a list
// verb in method names.
func (c *Client) ListCollectionPosts(ctx context.Context, collectionID string) (SearchResponse, error) {
	return c.CollectionPosts(ctx, collectionID)
}

// CreateCollection executes POST /api/collections.
func (c *Client) CreateCollection(ctx context.Context, request CreateCollectionRequest) (Collection, error) {
	var result Collection
	if err := c.postJSON(ctx, "/api/collections", request, &result); err != nil {
		return Collection{}, err
	}
	if result.PostIDs == nil {
		result.PostIDs = []string{}
	}
	return result, nil
}

// AddCollectionPosts executes POST /api/collections/{id}/posts.
func (c *Client) AddCollectionPosts(ctx context.Context, collectionID string, request AddCollectionPostsRequest) (Collection, error) {
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return Collection{}, fmt.Errorf("collection id must not be empty")
	}
	var result Collection
	path := "/api/collections/" + url.PathEscape(collectionID) + "/posts"
	if err := c.postJSON(ctx, path, request, &result); err != nil {
		return Collection{}, err
	}
	if result.PostIDs == nil {
		result.PostIDs = []string{}
	}
	return result, nil
}

// RemoveCollectionPost executes DELETE /api/collections/{id}/posts/{postID}.
func (c *Client) RemoveCollectionPost(ctx context.Context, collectionID, postID string) error {
	collectionID = strings.TrimSpace(collectionID)
	postID = strings.TrimSpace(postID)
	if collectionID == "" {
		return fmt.Errorf("collection id must not be empty")
	}
	if postID == "" {
		return fmt.Errorf("post id must not be empty")
	}
	path := "/api/collections/" + url.PathEscape(collectionID) + "/posts/" + url.PathEscape(postID)
	return c.deleteNoContent(ctx, path)
}

// ReorderCollection executes POST /api/collections/{id}/order.
func (c *Client) ReorderCollection(ctx context.Context, collectionID string, request ReorderCollectionRequest) (Collection, error) {
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return Collection{}, fmt.Errorf("collection id must not be empty")
	}
	var result Collection
	path := "/api/collections/" + url.PathEscape(collectionID) + "/order"
	if err := c.postJSON(ctx, path, request, &result); err != nil {
		return Collection{}, err
	}
	if result.PostIDs == nil {
		result.PostIDs = []string{}
	}
	return result, nil
}

// GetPostDetail executes GET /api/posts/{id}.
func (c *Client) GetPostDetail(ctx context.Context, id string) (PostDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return PostDetail{}, fmt.Errorf("post id must not be empty")
	}
	var result PostDetail
	path := "/api/posts/" + url.PathEscape(id)
	if err := c.getJSON(ctx, path, &result); err != nil {
		return PostDetail{}, err
	}
	if result.History == nil {
		result.History = []Revision{}
	}
	if result.ReactionHistory == nil {
		result.ReactionHistory = []ReactionRevision{}
	}
	if result.Tags == nil {
		result.Tags = []string{}
	}
	return result, nil
}

// EditTags executes POST /api/posts/tags.
func (c *Client) EditTags(ctx context.Context, req TagEditRequest) (TagEditResponse, error) {
	var result TagEditResponse
	if err := c.postJSON(ctx, "/api/posts/tags", req, &result); err != nil {
		return TagEditResponse{}, err
	}
	if result.Posts == nil {
		result.Posts = []TagEditResult{}
	}
	return result, nil
}

// RevertTags executes POST /api/posts/tags/revert.
func (c *Client) RevertTags(ctx context.Context, req TagRevertRequest) (TagRevertResponse, error) {
	var result TagRevertResponse
	if err := c.postJSON(ctx, "/api/posts/tags/revert", req, &result); err != nil {
		return TagRevertResponse{}, err
	}
	if result.Posts == nil {
		result.Posts = []TagEditResult{}
	}
	return result, nil
}

// EditReactions executes POST /api/posts/reactions.
func (c *Client) EditReactions(ctx context.Context, req ReactionRequest) (ReactionResponse, error) {
	var result ReactionResponse
	if err := c.postJSON(ctx, "/api/posts/reactions", req, &result); err != nil {
		return ReactionResponse{}, err
	}
	if result.Posts == nil {
		result.Posts = []ReactionResult{}
	}
	return result, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("invalid api url %q: %w", base, err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	c.authorize(req)
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiMsg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &apiMsg) == nil && apiMsg.Error != "" {
			return &APIError{Status: resp.StatusCode, Message: apiMsg.Error, Body: strings.TrimSpace(string(body))}
		}
		return &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, request any, target any) error {
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("invalid api url %q: %w", base, err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = ""
	bodyBytes, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiMsg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &apiMsg) == nil && apiMsg.Error != "" {
			return &APIError{Status: resp.StatusCode, Message: apiMsg.Error, Body: strings.TrimSpace(string(respBody))}
		}
		return &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	if err := json.Unmarshal(respBody, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) deleteNoContent(ctx context.Context, path string) error {
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("invalid api url %q: %w", base, err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	c.authorize(req)
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiMsg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &apiMsg) == nil && apiMsg.Error != "" {
			return &APIError{Status: resp.StatusCode, Message: apiMsg.Error, Body: strings.TrimSpace(string(body))}
		}
		return &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return nil
}
