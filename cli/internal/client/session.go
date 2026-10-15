package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const sessionCookieName = "kura_session"

// AccountLogin is a successful account login. The cookie value is session
// material; callers must not print it.
type AccountLogin struct {
	Handle      string
	Role        string
	CookieName  string
	CookieValue string
	ExpiresAt   time.Time
}

// ValidSessionCookie reports whether name and value are the account session
// cookie this client is willing to store or send.
func ValidSessionCookie(name, value string) bool {
	if name != sessionCookieName || value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func validAccountRole(role string) bool {
	switch role {
	case "viewer", "editor", "admin":
		return true
	default:
		return false
	}
}

func originFromBase(base string) (string, error) {
	u, err := parseBaseURL(base)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid api url: missing host")
	}
	return u.Scheme + "://" + u.Host, nil
}

// ValidOrigin reports whether raw is an exact http(s) origin with no path,
// query, fragment, or credentials.
func ValidOrigin(raw string) bool {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return false
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" || raw != u.Scheme+"://"+u.Host {
		return false
	}
	return true
}

func (c *Client) prepareAuth() error {
	if c == nil {
		return fmt.Errorf("missing API client")
	}
	if strings.TrimSpace(c.APIToken) != "" {
		return nil
	}
	name := strings.TrimSpace(c.SessionCookieName)
	value := strings.TrimSpace(c.SessionCookieValue)
	if name == "" && value == "" {
		return nil
	}
	if !ValidSessionCookie(name, value) {
		return fmt.Errorf("account session is invalid")
	}
	if origin := strings.TrimSpace(c.Origin); origin != "" && !ValidOrigin(origin) {
		return fmt.Errorf("account session origin is invalid")
	}
	return nil
}

func missingSessionOrigin(req *http.Request) error {
	if req == nil || req.Header.Get("Cookie") == "" {
		return nil
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	if req.Header.Get("Origin") == "" {
		return fmt.Errorf("account session requires a request origin")
	}
	return nil
}

func (c *Client) httpTransport() http.Client {
	transport := http.Client{Timeout: 10 * time.Second}
	if c != nil && c.HTTPClient != nil {
		transport = *c.HTTPClient
	}
	if transport.Timeout <= 0 {
		transport.Timeout = 10 * time.Second
	}
	// Redirects can turn writes into reads or resend credentials. Surface them.
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return transport
}

func (c *Client) doRaw(ctx context.Context, method, path string, payload any) (status int, header http.Header, body []byte, err error) {
	defer func() { err = c.scrub(err) }()
	if err = c.prepareAuth(); err != nil {
		return 0, nil, nil, err
	}
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := parseBaseURL(base)
	if err != nil {
		return 0, nil, nil, err
	}
	escapedPath := strings.TrimRight(u.EscapedPath(), "/") + path
	u.Path, err = url.PathUnescape(escapedPath)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("invalid request path: %w", err)
	}
	u.RawPath = escapedPath
	var reader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorizeWithProtected(req, true)
	if err = missingSessionOrigin(req); err != nil {
		return 0, nil, nil, err
	}
	transport := c.httpTransport()
	resp, err := transport.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return resp.StatusCode, resp.Header.Clone(), nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, resp.Header.Clone(), data, nil
}

func apiMessage(status int, body []byte) string {
	var message struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &message) != nil || strings.TrimSpace(message.Error) == "" {
		return http.StatusText(status) + " (unexpected API response; check --api-url)"
	}
	return strings.Join(strings.Fields(message.Error), " ")
}

// Login exchanges a handle and password for the account session cookie.
// The password is never written to the client and is redacted from errors.
func (c *Client) Login(ctx context.Context, handle, password string) (AccountLogin, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" || strings.ContainsAny(handle, " \r\n\t") {
		return AccountLogin{}, fmt.Errorf("handle is required")
	}
	if password == "" {
		return AccountLogin{}, fmt.Errorf("password is empty")
	}
	if len(password) > 1024 {
		return AccountLogin{}, fmt.Errorf("password is too long")
	}
	fail := func(err error) (AccountLogin, error) {
		if err == nil {
			return AccountLogin{}, nil
		}
		secrets := append(c.secretValues(), password)
		return AccountLogin{}, errors.New(redactText(err.Error(), secrets))
	}
	status, header, body, err := c.doRaw(ctx, http.MethodPost, "/api/auth/login", map[string]string{
		"handle":   handle,
		"password": password,
	})
	if err != nil {
		return fail(err)
	}
	secrets := append(c.secretValues(), password)
	if status != http.StatusOK {
		return fail(fmt.Errorf("api error %d: %s", status, redactText(apiMessage(status, body), secrets)))
	}
	resp := &http.Response{Header: header, StatusCode: status}
	var cookie *http.Cookie
	for _, item := range resp.Cookies() {
		if item.Name == sessionCookieName {
			cookie = item
			break
		}
	}
	if cookie == nil || !ValidSessionCookie(cookie.Name, cookie.Value) {
		return fail(fmt.Errorf("login did not return a session cookie"))
	}
	secrets = append(secrets, cookie.Value, cookie.Name+"="+cookie.Value)
	if strings.Contains(string(body), cookie.Value) {
		return fail(fmt.Errorf("login response included session material in the body"))
	}
	if cookie.Expires.IsZero() || !cookie.Expires.After(time.Now()) {
		return fail(fmt.Errorf("login session is missing a future expiry"))
	}
	var decoded struct {
		Authenticated bool `json:"authenticated"`
		User          *struct {
			Handle string `json:"handle"`
			Role   string `json:"role"`
		} `json:"user"`
	}
	if json.Unmarshal(body, &decoded) != nil || !decoded.Authenticated || decoded.User == nil {
		return fail(fmt.Errorf("login response did not include an account"))
	}
	decoded.User.Handle = strings.TrimSpace(decoded.User.Handle)
	decoded.User.Role = strings.TrimSpace(decoded.User.Role)
	if decoded.User.Handle == "" || !validAccountRole(decoded.User.Role) {
		return fail(fmt.Errorf("login response included an unusable account"))
	}
	return AccountLogin{
		Handle:      decoded.User.Handle,
		Role:        decoded.User.Role,
		CookieName:  cookie.Name,
		CookieValue: cookie.Value,
		ExpiresAt:   cookie.Expires.UTC(),
	}, nil
}

// Logout revokes the account session cookie. A 401 means it was already gone.
func (c *Client) Logout(ctx context.Context) error {
	if strings.TrimSpace(c.APIToken) != "" {
		c.APIToken = ""
	}
	if !ValidSessionCookie(strings.TrimSpace(c.SessionCookieName), strings.TrimSpace(c.SessionCookieValue)) {
		return fmt.Errorf("no account session")
	}
	status, _, body, err := c.doRaw(ctx, http.MethodPost, "/api/auth/logout", nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusUnauthorized {
		return nil
	}
	return c.scrub(fmt.Errorf("logout failed: %s", apiMessage(status, body)))
}
