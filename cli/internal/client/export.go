package client

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// maxExportZipBytes caps a downloaded export archive (256 MiB).
const maxExportZipBytes = 256 << 20

// ExportResult reports a published export archive without emitting its bytes.
type ExportResult struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

func validateExportIDs(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > 1000 {
		return nil, fmt.Errorf("export requires 1 to 1000 post ids")
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

// ExportPosts downloads GET /api/posts/export for the given ids to output.
// The destination must not already exist and is published atomically.
func (c *Client) ExportPosts(ctx context.Context, ids []string, metadata bool, output string) (ExportResult, error) {
	validated, err := validateExportIDs(ids)
	if err != nil {
		return ExportResult{}, err
	}
	query := url.Values{}
	query.Set("ids", strings.Join(validated, ","))
	query.Set("metadata", strconv.FormatBool(metadata))
	return c.downloadExport(ctx, "/api/posts/export", query, output)
}

// ExportCollection downloads GET /api/collections/{id}/export at an expected
// version to output. The destination must not already exist.
func (c *Client) ExportCollection(ctx context.Context, collectionID string, version int64, metadata bool, output string) (ExportResult, error) {
	n, err := strconv.ParseInt(collectionID, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != collectionID {
		return ExportResult{}, fmt.Errorf("collection id must be a canonical positive integer")
	}
	if version < 0 {
		return ExportResult{}, fmt.Errorf("expected version must be non-negative")
	}
	query := url.Values{}
	query.Set("expected_version", strconv.FormatInt(version, 10))
	query.Set("metadata", strconv.FormatBool(metadata))
	path := "/api/collections/" + url.PathEscape(collectionID) + "/export"
	return c.downloadExport(ctx, path, query, output)
}

func (c *Client) downloadExport(ctx context.Context, path string, query url.Values, output string) (result ExportResult, err error) {
	defer func() { err = c.scrub(err) }()
	if err = c.prepareAuth(); err != nil {
		return ExportResult{}, err
	}
	if strings.TrimSpace(output) == "" {
		return ExportResult{}, fmt.Errorf("output path is required")
	}
	base := c.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := parseBaseURL(base)
	if err != nil {
		return ExportResult{}, err
	}
	if _, err := os.Lstat(output); err == nil {
		return ExportResult{}, fmt.Errorf("output file already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return ExportResult{}, fmt.Errorf("check output path: %w", err)
	}
	dir := filepath.Dir(output)
	temp, err := os.CreateTemp(dir, ".kura-export-*.tmp")
	if err != nil {
		return ExportResult{}, fmt.Errorf("create temporary export file: %w", err)
	}
	tempName := temp.Name()
	published := false
	defer func() {
		_ = temp.Close()
		if !published {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return ExportResult{}, fmt.Errorf("create temporary export file: %w", err)
	}

	escapedPath := strings.TrimRight(u.EscapedPath(), "/") + path
	u.Path, err = url.PathUnescape(escapedPath)
	if err != nil {
		return ExportResult{}, fmt.Errorf("invalid request path: %w", err)
	}
	u.RawPath = escapedPath
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ExportResult{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/zip")
	c.authorizeWithProtected(req, false)
	if err = missingSessionOrigin(req); err != nil {
		return ExportResult{}, err
	}
	transport := c.httpTransport()
	resp, err := transport.Do(req)
	if err != nil {
		return ExportResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		var message struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if json.Unmarshal(data, &message) != nil || message.Error == "" {
			message.Error = http.StatusText(resp.StatusCode) + " (unexpected API response; check --api-url)"
		}
		message.Error = c.redact(message.Error)
		message.Code = c.redact(message.Code)
		return ExportResult{}, &APIError{Status: resp.StatusCode, Code: message.Code, Message: strings.Join(strings.Fields(message.Error), " ")}
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/zip" {
		return ExportResult{}, fmt.Errorf("expected application/zip export response; check --api-url")
	}
	if resp.ContentLength > maxExportZipBytes {
		return ExportResult{}, fmt.Errorf("export archive exceeds %d bytes", maxExportZipBytes)
	}
	written, err := io.Copy(temp, io.LimitReader(resp.Body, maxExportZipBytes+1))
	if err != nil {
		return ExportResult{}, fmt.Errorf("download export: %w", err)
	}
	if written > maxExportZipBytes {
		return ExportResult{}, fmt.Errorf("export archive exceeds %d bytes", maxExportZipBytes)
	}
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		return ExportResult{}, fmt.Errorf("truncated export archive")
	}
	if err := temp.Sync(); err != nil {
		return ExportResult{}, fmt.Errorf("write export: %w", err)
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return ExportResult{}, fmt.Errorf("verify export archive: %w", err)
	}
	if _, err := zip.NewReader(temp, written); err != nil {
		return ExportResult{}, fmt.Errorf("invalid export archive: %w", err)
	}
	if err := temp.Close(); err != nil {
		return ExportResult{}, fmt.Errorf("write export: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return ExportResult{}, fmt.Errorf("download export: %w", err)
	}
	if err := os.Link(tempName, output); err != nil {
		return ExportResult{}, fmt.Errorf("publish export: %w", err)
	}
	_ = os.Remove(tempName)
	published = true
	return ExportResult{Path: output, Bytes: written}, nil
}
