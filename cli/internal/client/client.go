package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client talks to the public Kura HTTP API without importing server internals.
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
	var result SearchResponse
	if err := c.request(ctx, http.MethodGet, "/api/posts", pageQuery(query, cursor, limit), nil, &result); err != nil {
		return SearchResponse{}, err
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

func (c *Client) ListCollectionSummaries(ctx context.Context) (CollectionSummariesResponse, error) {
	var result CollectionSummariesResponse
	if err := c.getJSON(ctx, "/api/collections/summaries", &result); err != nil {
		return CollectionSummariesResponse{}, err
	}
	if result.Collections == nil {
		result.Collections = []CollectionSummary{}
	}
	return result, nil
}

func (c *Client) MoveCollectionPost(ctx context.Context, collectionID string, request MoveCollectionPostRequest) (MoveCollectionPostResult, error) {
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return MoveCollectionPostResult{}, fmt.Errorf("collection id must not be empty")
	}
	var result MoveCollectionPostResult
	path := "/api/collections/" + url.PathEscape(collectionID) + "/move"
	if err := c.postJSON(ctx, path, request, &result); err != nil {
		return MoveCollectionPostResult{}, err
	}
	if result.ID != collectionID {
		return MoveCollectionPostResult{}, &RequestError{Err: fmt.Errorf("move response identifies another collection"), Uncertain: true}
	}
	return result, nil
}

func (c *Client) RenameCollection(ctx context.Context, collectionID string, request CollectionManageRequest) (CollectionManageResult, error) {
	return c.collectionManage(ctx, http.MethodPatch, collectionID, request)
}

func (c *Client) DeleteCollection(ctx context.Context, collectionID string, expectedVersion int64) (CollectionManageResult, error) {
	return c.collectionManage(ctx, http.MethodDelete, collectionID, CollectionManageRequest{ExpectedVersion: expectedVersion})
}

func (c *Client) collectionManage(ctx context.Context, method, collectionID string, request CollectionManageRequest) (CollectionManageResult, error) {
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return CollectionManageResult{}, fmt.Errorf("collection id must not be empty")
	}
	var result CollectionManageResult
	path := "/api/collections/" + url.PathEscape(collectionID)
	if err := c.request(ctx, method, path, nil, request, &result); err != nil {
		return CollectionManageResult{}, err
	}
	if result.ID != collectionID {
		return CollectionManageResult{}, &RequestError{Err: fmt.Errorf("management response identifies another collection"), Uncertain: true}
	}
	return result, nil
}

// CollectionPosts executes GET /api/collections/{id}/posts. The collection
// id is escaped as a path segment; the server remains responsible for
// validating whether it identifies an existing collection.
func (c *Client) CollectionPosts(ctx context.Context, collectionID string) (SearchResponse, error) {
	return c.CollectionPostsPage(ctx, collectionID, "", 0)
}

// CollectionPostsPage reads one versioned collection page. Cursor values are opaque.
func (c *Client) CollectionPostsPage(ctx context.Context, collectionID, cursor string, limit int) (SearchResponse, error) {
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return SearchResponse{}, fmt.Errorf("collection id must not be empty")
	}
	if limit != 0 && (limit < 1 || limit > 60) {
		return SearchResponse{}, fmt.Errorf("limit must be between 1 and 60, got %d", limit)
	}
	var result SearchResponse
	query := pageQuery("", cursor, limit)
	query.Del("q")
	path := "/api/collections/" + url.PathEscape(collectionID) + "/posts"
	if err := c.request(ctx, http.MethodGet, path, query, nil, &result); err != nil {
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
	return c.request(ctx, http.MethodGet, path, nil, nil, target)
}

func (c *Client) postJSON(ctx context.Context, path string, request any, target any) error {
	return c.request(ctx, http.MethodPost, path, nil, request, target)
}

func (c *Client) deleteNoContent(ctx context.Context, path string) error {
	return c.request(ctx, http.MethodDelete, path, nil, nil, nil)
}
