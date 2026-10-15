package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
)

// FolderImport is one file sent to the existing browser import API.
type FolderImport struct {
	Filename     string
	Body         io.Reader
	ExpectedHash string
	CollectionID string
	Tags         []string
	Artist       string
	Source       string
}

// FolderImportResult is the durable import receipt.
type FolderImportResult struct {
	PostID    string
	Duplicate bool
}

// ImportIdentity reads the account and library target without returning credentials.
func (c *Client) ImportIdentity(ctx context.Context) (string, string, error) {
	library, err := originFromBase(c.BaseURL)
	if err != nil {
		return "", "", err
	}
	status, _, body, err := c.doRaw(ctx, http.MethodGet, "/api/auth/session", nil)
	if err != nil {
		return "", "", fmt.Errorf("library identity is unavailable")
	}
	if status != http.StatusOK {
		return "", "", fmt.Errorf("library identity is unavailable")
	}
	var decoded struct {
		PrivateMode   bool `json:"private_mode"`
		Authenticated bool `json:"authenticated"`
		User          *struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if json.Unmarshal(body, &decoded) != nil {
		return "", "", fmt.Errorf("library identity is unavailable")
	}
	if decoded.User != nil && decoded.User.ID > 0 {
		return strconv.FormatInt(decoded.User.ID, 10), library, nil
	}
	if decoded.Authenticated {
		return "deployment", library, nil
	}
	if decoded.PrivateMode {
		return "", "", fmt.Errorf("sign in before importing into a private library")
	}
	return "local", library, nil
}

// ImportDuplicates looks up already stored hashes. The map key is the canonical hash.
func (c *Client) ImportDuplicates(ctx context.Context, hashes []string) (map[string]string, error) {
	if len(hashes) == 0 || len(hashes) > 100 {
		return nil, fmt.Errorf("duplicate lookup is unavailable")
	}
	status, body, err := c.doReader(ctx, http.MethodGet, "/api/imports/duplicates", url.Values{"hashes": {strings.Join(hashes, ",")}}, nil, "")
	if err != nil {
		return nil, fmt.Errorf("duplicate lookup is unavailable")
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("duplicate lookup is unavailable")
	}
	var decoded struct {
		Matches []struct {
			Hash   string `json:"hash"`
			PostID string `json:"post_id"`
		} `json:"matches"`
	}
	if json.Unmarshal(body, &decoded) != nil {
		return nil, fmt.Errorf("duplicate lookup is unavailable")
	}
	out := map[string]string{}
	for _, match := range decoded.Matches {
		if match.Hash != "" && match.PostID != "" {
			out[match.Hash] = match.PostID
		}
	}
	return out, nil
}

// ImportFolderFile posts one stable file. A transport failure after the
// request is sent is uncertain and must be reconciled before another upload.
func (c *Client) ImportFolderFile(ctx context.Context, item FolderImport) (result FolderImportResult, uncertain bool, err error) {
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(item.Filename)))
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		return FolderImportResult{}, false, fmt.Errorf("import could not be prepared")
	}
	if _, err := io.Copy(part, item.Body); err != nil {
		return FolderImportResult{}, false, fmt.Errorf("import could not be prepared")
	}
	fields := map[string]string{
		"expected_hash":    item.ExpectedHash,
		"duplicate_policy": "skip",
		"collection_id":    item.CollectionID,
		"artist":           item.Artist,
		"source":           item.Source,
	}
	for key, value := range fields {
		if strings.TrimSpace(value) == "" && key != "expected_hash" && key != "duplicate_policy" {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return FolderImportResult{}, false, fmt.Errorf("import could not be prepared")
		}
	}
	for _, tag := range item.Tags {
		if err := writer.WriteField("tags", tag); err != nil {
			return FolderImportResult{}, false, fmt.Errorf("import could not be prepared")
		}
	}
	if err := writer.Close(); err != nil {
		return FolderImportResult{}, false, fmt.Errorf("import could not be prepared")
	}
	status, body, err := c.doReader(ctx, http.MethodPost, "/api/imports", nil, &payload, writer.FormDataContentType())
	if err != nil {
		return FolderImportResult{}, true, fmt.Errorf("import result is uncertain")
	}
	if status == http.StatusConflict {
		return FolderImportResult{Duplicate: true}, false, nil
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return FolderImportResult{}, false, fmt.Errorf("import was not accepted")
	}
	var decoded struct {
		Post struct {
			ID string `json:"id"`
		} `json:"post"`
		Duplicate bool `json:"duplicate"`
	}
	if json.Unmarshal(body, &decoded) != nil || (decoded.Post.ID == "" && !decoded.Duplicate) {
		return FolderImportResult{}, true, fmt.Errorf("import result is uncertain")
	}
	return FolderImportResult{PostID: decoded.Post.ID, Duplicate: decoded.Duplicate}, false, nil
}

func escapeQuotes(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func (c *Client) doReader(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (status int, data []byte, err error) {
	defer func() { err = c.scrub(err) }()
	if err = c.prepareAuth(); err != nil {
		return 0, nil, err
	}
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := parseBaseURL(base)
	if err != nil {
		return 0, nil, err
	}
	escapedPath := strings.TrimRight(u.EscapedPath(), "/") + path
	u.Path, err = url.PathUnescape(escapedPath)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid request path")
	}
	u.RawPath = escapedPath
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return 0, nil, fmt.Errorf("create request")
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.authorizeWithProtected(req, true)
	if err = missingSessionOrigin(req); err != nil {
		return 0, nil, err
	}
	transport := c.httpTransport()
	resp, err := transport.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed")
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response")
	}
	if len(data) > maxResponseBytes {
		return resp.StatusCode, nil, fmt.Errorf("API response is too large")
	}
	return resp.StatusCode, data, nil
}
