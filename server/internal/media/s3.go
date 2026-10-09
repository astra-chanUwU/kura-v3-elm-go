// Package media contains the durable object-storage boundary used by posts.
package media

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// S3Config holds explicit S3-compatible configuration for a remote media store.
// All fields are plain strings so callers and tests can construct the config
// without provider SDK types.
type S3Config struct {
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	URLPrefix       string
	HTTPClient      *http.Client
}

// S3Store stores objects in an S3-compatible bucket using the HTTP API.
// It implements Store. Keys are provider-relative and validated with the same
// rules as LocalStore. Put is idempotent: an existing key is never overwritten.
type S3Store struct {
	endpoint        *url.URL
	bucket          string
	region          string
	accessKeyID     string
	secretAccessKey string
	urlPrefix       string
	httpClient      *http.Client
}

// S3ConfigFromEnv reads explicit S3 configuration from environment variables.
// Endpoint and bucket are required; region, credentials, and URL prefix are
// optional. Recognized variables are:
//
//	S3_ENDPOINT / AWS_S3_ENDPOINT
//	S3_BUCKET / AWS_S3_BUCKET
//	S3_REGION / AWS_REGION / AWS_DEFAULT_REGION
//	S3_ACCESS_KEY_ID / AWS_ACCESS_KEY_ID
//	S3_SECRET_ACCESS_KEY / AWS_SECRET_ACCESS_KEY
//	S3_URL_PREFIX / MEDIA_URL_PREFIX / S3_PUBLIC_URL_PREFIX
func S3ConfigFromEnv() (S3Config, bool) {
	endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("AWS_S3_ENDPOINT"))
	}
	bucket := strings.TrimSpace(os.Getenv("S3_BUCKET"))
	if bucket == "" {
		bucket = strings.TrimSpace(os.Getenv("AWS_S3_BUCKET"))
	}
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(bucket) == "" {
		return S3Config{}, false
	}
	region := strings.TrimSpace(os.Getenv("S3_REGION"))
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_REGION"))
		if region == "" {
			region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
		}
	}
	accessKey := strings.TrimSpace(os.Getenv("S3_ACCESS_KEY_ID"))
	if accessKey == "" {
		accessKey = strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	}
	secretKey := strings.TrimSpace(os.Getenv("S3_SECRET_ACCESS_KEY"))
	if secretKey == "" {
		secretKey = strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	}
	urlPrefix := strings.TrimSpace(os.Getenv("S3_URL_PREFIX"))
	if urlPrefix == "" {
		urlPrefix = strings.TrimSpace(os.Getenv("MEDIA_URL_PREFIX"))
		if urlPrefix == "" {
			urlPrefix = strings.TrimSpace(os.Getenv("S3_PUBLIC_URL_PREFIX"))
		}
	}
	return S3Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		Region:          region,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		URLPrefix:       urlPrefix,
	}, true
}

// NewStoreFromEnv returns an S3Store when S3_ENDPOINT and S3_BUCKET are set
// and valid, otherwise a LocalStore rooted at root.
func NewStoreFromEnv(root string) Store {
	if cfg, ok := S3ConfigFromEnv(); ok {
		if store, err := NewS3Store(cfg); err == nil {
			return store
		}
	}
	return NewLocalStore(root)
}

// NewS3Store creates an S3Store from explicit configuration.
func NewS3Store(cfg S3Config) (*S3Store, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	bucket := strings.TrimSpace(cfg.Bucket)
	if endpoint == "" {
		return nil, errors.New("s3 endpoint is required")
	}
	if bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if strings.Contains(bucket, "/") || strings.Contains(bucket, "\\") {
		return nil, errors.New("s3 bucket is invalid")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("s3 endpoint is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("s3 endpoint must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("s3 endpoint must include a host")
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	urlPrefix := strings.TrimRight(strings.TrimSpace(cfg.URLPrefix), "/")
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &S3Store{
		endpoint:        parsed,
		bucket:          bucket,
		region:          region,
		accessKeyID:     strings.TrimSpace(cfg.AccessKeyID),
		secretAccessKey: strings.TrimSpace(cfg.SecretAccessKey),
		urlPrefix:       urlPrefix,
		httpClient:      client,
	}, nil
}

func (s *S3Store) Put(ctx context.Context, key, contentType string, content io.Reader) (Object, error) {
	if err := validateKey(key); err != nil {
		return Object{}, err
	}
	if content == nil {
		return Object{}, errors.New("media content is required")
	}
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	exists, size, err := s.headObject(ctx, key)
	if err != nil {
		return Object{}, err
	}
	if exists {
		return s.object(key, contentType, size, false), nil
	}
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return Object{}, err
	}
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	if err := s.putObject(ctx, key, contentType, data); err != nil {
		// If the object was created concurrently, treat PUT conflict as idempotent.
		if isAlreadyExistsError(err) {
			exists, size, headErr := s.headObject(ctx, key)
			if headErr != nil {
				return Object{}, headErr
			}
			if exists {
				return s.object(key, contentType, size, false), nil
			}
		}
		return Object{}, err
	}
	return s.object(key, contentType, int64(len(data)), true), nil
}

// Open streams the stored bytes for key through the S3 HTTP API. The
// caller must close the body. A missing object is a plain not-found error;
// callers map it to job failure.
func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u := s.requestURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	emptyHash := hex.EncodeToString(sha256.New().Sum(nil))
	req.Header.Set("X-Amz-Content-Sha256", emptyHash)
	s.signRequest(req, emptyHash)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return resp.Body, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("media object not found: %s", key)
	}
	return nil, fmt.Errorf("s3 get %s: %d %s", key, resp.StatusCode, strings.TrimSpace(string(body)))
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.deleteObject(ctx, key)
}

func (s *S3Store) object(key, contentType string, size int64, created bool) Object {
	var prefix string
	if s.urlPrefix != "" {
		prefix = s.urlPrefix
	} else {
		base := strings.TrimRight(s.endpoint.String(), "/")
		prefix = base + "/" + s.bucket
	}
	u := prefix + "/" + key
	return Object{Key: key, URL: u, ContentType: contentType, Size: size, Created: created}
}

func (s *S3Store) requestURL(key string) string {
	base := strings.TrimRight(s.endpoint.String(), "/")
	// Preserve endpoint path if present, then bucket, then escaped key.
	endpointPath := strings.TrimRight(s.endpoint.Path, "/")
	if endpointPath != "" && endpointPath != "/" {
		base = strings.TrimRight(s.endpoint.Scheme+"://"+s.endpoint.Host+endpointPath, "/")
	}
	return base + "/" + s.bucket + "/" + escapeKey(key)
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (s *S3Store) headObject(ctx context.Context, key string) (bool, int64, error) {
	u := s.requestURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false, 0, err
	}
	emptyHash := hex.EncodeToString(sha256.New().Sum(nil))
	req.Header.Set("X-Amz-Content-Sha256", emptyHash)
	s.signRequest(req, emptyHash)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		size := int64(0)
		if v := resp.Header.Get("Content-Length"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				size = n
			}
		}
		return true, size, nil
	case http.StatusNotFound:
		return false, 0, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, 0, fmt.Errorf("s3 head %s: %d %s", key, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

func (s *S3Store) putObject(ctx context.Context, key, contentType string, data []byte) error {
	u := s.requestURL(key)
	payloadHash := sha256Hex(data)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	s.signRequest(req, payloadHash)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusAccepted {
		return nil
	}
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusPreconditionFailed {
		return fmt.Errorf("s3 put %s: %d already exists", key, resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("s3 put %s: %d %s", key, resp.StatusCode, strings.TrimSpace(string(body)))
}

func (s *S3Store) deleteObject(ctx context.Context, key string) error {
	u := s.requestURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	emptyHash := hex.EncodeToString(sha256.New().Sum(nil))
	req.Header.Set("X-Amz-Content-Sha256", emptyHash)
	s.signRequest(req, emptyHash)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("s3 delete %s: %d %s", key, resp.StatusCode, strings.TrimSpace(string(body)))
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists")
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (s *S3Store) signRequest(req *http.Request, payloadHash string) {
	if s.accessKeyID == "" || s.secretAccessKey == "" {
		return
	}
	amzDate := req.Header.Get("X-Amz-Date")
	var t time.Time
	if amzDate != "" {
		if parsed, err := time.Parse("20060102T150405Z", amzDate); err == nil {
			t = parsed
		} else {
			t = time.Now().UTC()
			amzDate = t.Format("20060102T150405Z")
			req.Header.Set("X-Amz-Date", amzDate)
		}
	} else {
		t = time.Now().UTC()
		amzDate = t.Format("20060102T150405Z")
		req.Header.Set("X-Amz-Date", amzDate)
	}
	dateStamp := amzDate[:8]
	region := s.region
	if region == "" {
		region = "us-east-1"
	}
	credentialScope := dateStamp + "/" + region + "/s3/aws4_request"
	hasContentType := strings.TrimSpace(req.Header.Get("Content-Type")) != ""
	var signedHeaders string
	if hasContentType {
		signedHeaders = "content-type;host;x-amz-content-sha256;x-amz-date"
	} else {
		signedHeaders = "host;x-amz-content-sha256;x-amz-date"
	}
	var canonicalHeaders strings.Builder
	if hasContentType {
		canonicalHeaders.WriteString("content-type:" + strings.TrimSpace(req.Header.Get("Content-Type")) + "\n")
	}
	canonicalHeaders.WriteString("host:" + req.URL.Host + "\n")
	canonicalHeaders.WriteString("x-amz-content-sha256:" + payloadHash + "\n")
	canonicalHeaders.WriteString("x-amz-date:" + amzDate + "\n")

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	// Canonical query string is sorted encoded query; for our usage it's empty.
	canonicalQueryString := ""
	if req.URL.RawQuery != "" {
		q, _ := url.ParseQuery(req.URL.RawQuery)
		canonicalQueryString = q.Encode()
	}
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	hashedCanonical := sha256.Sum256([]byte(canonicalRequest))
	hashedCanonicalHex := hex.EncodeToString(hashedCanonical[:])
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hashedCanonicalHex,
	}, "\n")
	kSecret := []byte("AWS4" + s.secretAccessKey)
	kDate := hmacSHA256(kSecret, dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", s.accessKeyID, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

// Ensure S3Store implements Store.
var _ Store = (*S3Store)(nil)
