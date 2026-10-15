package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxResponseBytes = 16 << 20

// ValidateBaseURL rejects ambiguous destinations before a request is sent.
func ValidateBaseURL(base string) error {
	_, err := parseBaseURL(base)
	return err
}

func parseBaseURL(base string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid api url: use an absolute http:// or https:// URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(base, "#") {
		return nil, fmt.Errorf("invalid api url: credentials, query strings, and fragments are not allowed")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid api url: port must be between 1 and 65535")
		}
	}
	return u, nil
}

func pageQuery(query, cursor string, limit int) url.Values {
	q := url.Values{"q": {query}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit != 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

// APIError preserves the HTTP status and machine-readable API code.
type APIError struct {
	Status  int
	Code    string
	Message string
	Body    string
}

func (e *APIError) Error() string {
	message := e.Message
	if message == "" {
		message = e.Body
	}
	if message == "" {
		message = http.StatusText(e.Status)
	}
	return fmt.Sprintf("api error %d: %s", e.Status, message)
}

// RequestError marks writes whose outcome cannot be inferred from the response.
// Callers must reconcile the resource before retrying such a write.
type RequestError struct {
	Err       error
	Uncertain bool
}

func (e *RequestError) Error() string {
	if e.Uncertain {
		return e.Err.Error() + "; write outcome is uncertain; check the resource before retrying"
	}
	return e.Err.Error()
}

func (e *RequestError) Unwrap() error { return e.Err }

func (c *Client) request(ctx context.Context, method, path string, query url.Values, payload, target any) error {
	return c.requestWithProtectedRead(ctx, method, path, query, payload, target, false)
}

// requestWithProtectedRead includes the bearer token on GET requests when the
// endpoint requires authentication. Existing public reads keep their behavior.
func (c *Client) requestWithProtectedRead(ctx context.Context, method, path string, query url.Values, payload, target any, protectedRead bool) (err error) {
	defer func() { err = c.scrub(err) }()
	if err = c.prepareAuth(); err != nil {
		return err
	}
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := parseBaseURL(base)
	if err != nil {
		return err
	}
	// Both base prefixes and escaped IDs must be escaped exactly once.
	escapedPath := strings.TrimRight(u.EscapedPath(), "/") + path
	u.Path, err = url.PathUnescape(escapedPath)
	if err != nil {
		return fmt.Errorf("invalid request path: %w", err)
	}
	u.RawPath = escapedPath
	u.RawQuery = query.Encode()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorizeWithProtected(req, protectedRead)
	if err = missingSessionOrigin(req); err != nil {
		return err
	}
	// Redirects can turn writes into reads or resend credentials. httpTransport refuses them.
	transport := c.httpTransport()
	write := method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
	responseError := func(err error) error { return &RequestError{Err: err, Uncertain: write} }
	resp, err := transport.Do(req)
	if err != nil {
		return responseError(fmt.Errorf("request failed: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
		if err != nil {
			return responseError(fmt.Errorf("read response: %w", err))
		}
		var message struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if json.Unmarshal(data, &message) != nil || message.Error == "" {
			message.Error = http.StatusText(resp.StatusCode) + " (unexpected API response; check --api-url)"
		}
		message.Error = c.redact(message.Error)
		message.Code = c.redact(message.Code)
		return &APIError{Status: resp.StatusCode, Code: message.Code, Message: strings.Join(strings.Fields(message.Error), " ")}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return responseError(fmt.Errorf("read response: %w", err))
	}
	if len(data) > maxResponseBytes {
		return responseError(fmt.Errorf("API response exceeds %d bytes", maxResponseBytes))
	}
	if target == nil {
		if resp.StatusCode != http.StatusNoContent {
			return responseError(fmt.Errorf("expected HTTP 204 from Kura; check --api-url"))
		}
		return nil
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return responseError(fmt.Errorf("expected a JSON object from Kura; check --api-url"))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return responseError(fmt.Errorf("decode response: %w", err))
	}
	// A different JSON service must not silently look like an empty library.
	field := "posts"
	required := []string(nil)
	switch target.(type) {
	case *CollectionsResponse:
		field = "collections"
	case *CollectionSummariesResponse:
		field = "collections"
	case *Collection, *PostDetail:
		field = "id"
	case *CollectionManageResult:
		field = "id"
		required = []string{"id", "name", "version", "changed"}
	case *MoveCollectionPostResult:
		field = "id"
		required = []string{"id", "collection_id", "version", "changed"}
	case *TagManageListResponse:
		field = "tags"
		required = []string{"version", "tags"}
	case *TagManageResult:
		field = "version"
		required = []string{"version", "mode", "source_tag", "target_tag", "affected_post_count", "visible_post_count", "target_post_count"}
	case *TagManageApplyResult:
		field = "id"
		required = []string{"id", "changed", "version", "mode", "source_tag", "target_tag", "affected_post_count", "visible_post_count"}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return responseError(fmt.Errorf("decode response: %w", err))
	}
	for _, name := range required {
		if raw, exists := fields[name]; !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return responseError(fmt.Errorf("API response is missing %q; check --api-url", name))
		}
	}
	switch result := target.(type) {
	case *MoveCollectionPostResult:
		if result.CollectionID != result.ID || result.Version < 0 {
			return responseError(fmt.Errorf("invalid collection move acknowledgement; check --api-url"))
		}
	case *CollectionManageResult:
		if strings.TrimSpace(result.Name) == "" || result.Version < 0 {
			return responseError(fmt.Errorf("invalid collection management acknowledgement; check --api-url"))
		}
	case *CollectionSummariesResponse:
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal(fields["collections"], &entries); err != nil {
			return responseError(fmt.Errorf("invalid collection summaries; check --api-url"))
		}
		seen := map[string]bool{}
		for i, entry := range entries {
			for _, name := range []string{"id", "name", "version", "member_count", "visible_post_count"} {
				if raw, exists := entry[name]; !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return responseError(fmt.Errorf("collection summary is missing %q; check --api-url", name))
				}
			}
			item := result.Collections[i]
			n, err := strconv.ParseInt(item.ID, 10, 64)
			if err != nil || n < 1 || strconv.FormatInt(n, 10) != item.ID || seen[item.ID] || strings.TrimSpace(item.Name) == "" || item.Version < 0 || item.MemberCount < 0 || item.VisiblePostCount < 0 || item.VisiblePostCount > item.MemberCount {
				return responseError(fmt.Errorf("invalid collection summary; check --api-url"))
			}
			seen[item.ID] = true
		}
	case *TagManageListResponse:
		var entries []map[string]json.RawMessage
		if result.Version < 0 || json.Unmarshal(fields["tags"], &entries) != nil {
			return responseError(fmt.Errorf("invalid tag list; check --api-url"))
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			for _, name := range []string{"tag", "post_count", "visible_post_count"} {
				if raw, exists := entry[name]; !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return responseError(fmt.Errorf("tag list is missing %q; check --api-url", name))
				}
			}
			var item TagSummary
			encoded, _ := json.Marshal(entry)
			if json.Unmarshal(encoded, &item) != nil || strings.TrimSpace(item.Tag) == "" || seen[item.Tag] || item.PostCount < 0 || item.VisiblePostCount < 0 || item.VisiblePostCount > item.PostCount {
				return responseError(fmt.Errorf("invalid tag list entry; check --api-url"))
			}
			seen[item.Tag] = true
		}
		if result.NextCursor != nil && strings.TrimSpace(*result.NextCursor) == "" {
			return responseError(fmt.Errorf("invalid tag list cursor; check --api-url"))
		}
	case *TagManageResult:
		if result.Version < 0 || result.AffectedPostCount < 1 || result.AffectedPostCount > 1000 || result.VisiblePostCount < 0 || result.VisiblePostCount > result.AffectedPostCount || result.TargetPostCount < 0 || result.Mode != "rename" && result.Mode != "merge" || strings.TrimSpace(result.SourceTag) == "" || strings.TrimSpace(result.TargetTag) == "" || result.SourceTag == result.TargetTag || result.Mode == "rename" && result.TargetPostCount != 0 || result.Mode == "merge" && result.TargetPostCount == 0 {
			return responseError(fmt.Errorf("invalid tag management acknowledgement; check --api-url"))
		}
	case *TagManageApplyResult:
		id, err := strconv.ParseInt(result.ID, 10, 64)
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != result.ID || result.Version < 0 || !result.Changed || result.AffectedPostCount < 1 || result.AffectedPostCount > 1000 || result.VisiblePostCount < 0 || result.VisiblePostCount > result.AffectedPostCount || result.Mode != "rename" && result.Mode != "merge" || strings.TrimSpace(result.SourceTag) == "" || strings.TrimSpace(result.TargetTag) == "" {
			return responseError(fmt.Errorf("invalid tag management receipt; check --api-url"))
		}
	}
	raw, ok := fields[field]
	if !ok {
		return responseError(fmt.Errorf("API response is missing %q; check --api-url", field))
	}
	// Null array fields are left to the callers, which normalize them to
	// empty slices for compatibility with older servers. An empty id is
	// different: it cannot identify the returned resource.
	if field == "id" {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil || strings.TrimSpace(id) == "" {
			return responseError(fmt.Errorf("API response field %q is empty; check --api-url", field))
		}
	}
	return nil
}
