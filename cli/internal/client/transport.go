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
	"time"
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
	c.authorize(req)
	transport := http.Client{Timeout: 10 * time.Second}
	if c.HTTPClient != nil {
		transport = *c.HTTPClient
	}
	if transport.Timeout <= 0 {
		transport.Timeout = 10 * time.Second
	}
	// Redirects can turn writes into reads or resend credentials. Surface them.
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
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
		if token := strings.TrimSpace(c.APIToken); token != "" {
			message.Error = strings.ReplaceAll(message.Error, token, "[redacted]")
		}
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
	switch target.(type) {
	case *CollectionsResponse:
		field = "collections"
	case *Collection, *PostDetail:
		field = "id"
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return responseError(fmt.Errorf("decode response: %w", err))
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
