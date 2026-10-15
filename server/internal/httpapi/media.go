package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

func mediaRoot() string {
	if configured := strings.TrimSpace(os.Getenv("MEDIA_ROOT")); configured != "" {
		return configured
	}
	return filepath.Join("..", "web", "static", "media")
}

// gatedMedia serves /media/* files while enforcing the centralized
// visibility rule: a path referenced by post media is served only when a
// non-deleted referencing post is published. Unreferenced paths keep the
// previous public behavior; without a checker the handler also serves,
// preserving routers whose searcher predates moderation.
//
// Private mode with a configured store serves referenced, visible media by
// opening that store with the key recorded for the stored URL. It does not
// open a browser-supplied key, fetch an external URL, or follow a redirect.
func gatedMedia(checker posts.MediaVisibilityChecker, store media.Store, private bool) http.Handler {
	fileServer := http.StripPrefix("/media/", http.FileServer(http.Dir(mediaRoot())))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checker == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if private && store != nil {
			if servePrivateStoredMedia(w, r, checker, store) {
				return
			}
		}
		subpath := chi.URLParam(r, "*")
		if subpath == "" {
			subpath = strings.TrimPrefix(r.URL.Path, "/media/")
		}
		subpath = strings.TrimSpace(subpath)
		if subpath == "" {
			writeJSONError(w, http.StatusNotFound, "media not found")
			return
		}
		visibility, err := checker.LookupMediaVisibility(r.Context(), "/media/"+subpath)
		if err != nil {
			if errors.Is(err, posts.ErrUnavailable) {
				writeJSONError(w, http.StatusServiceUnavailable, "media unavailable")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "media lookup failed")
			return
		}
		if visibility.Referenced && !visibility.Visible {
			writeJSONError(w, http.StatusNotFound, "media not found")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// servePrivateStoredMedia handles private /media requests when a store is
// configured. It returns false only for a canonical unreferenced path, which
// keeps the local file-server behavior for files that are not post media.
func servePrivateStoredMedia(w http.ResponseWriter, r *http.Request, checker posts.MediaVisibilityChecker, store media.Store) bool {
	reference, ok := canonicalMediaReference(r)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "media not found")
		return true
	}
	visibility, err := checker.LookupMediaVisibility(r.Context(), reference)
	if err != nil {
		if errors.Is(err, posts.ErrUnavailable) {
			writeJSONError(w, http.StatusServiceUnavailable, "media unavailable")
			return true
		}
		writeJSONError(w, http.StatusInternalServerError, "media lookup failed")
		return true
	}
	if visibility.Referenced && !visibility.Visible {
		writeJSONError(w, http.StatusNotFound, "media not found")
		return true
	}
	if !visibility.Visible {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	resolver, ok := checker.(posts.StoredMediaKeyResolver)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "media not found")
		return true
	}
	key, mediaType, err := resolver.ResolveStoredMediaKey(r.Context(), reference)
	if err != nil {
		if errors.Is(err, posts.ErrUnavailable) {
			writeJSONError(w, http.StatusServiceUnavailable, "media unavailable")
			return true
		}
		if errors.Is(err, posts.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "media not found")
			return true
		}
		writeJSONError(w, http.StatusInternalServerError, "media lookup failed")
		return true
	}
	mapped, err := media.KeyFromStoredMediaURL(reference)
	if err != nil || mapped != key {
		writeJSONError(w, http.StatusNotFound, "media not found")
		return true
	}
	ranges, ok := store.(media.RangeReader)
	if !ok {
		writeJSONError(w, http.StatusServiceUnavailable, "media range reads unavailable")
		return true
	}
	size, err := ranges.Stat(r.Context(), mapped)
	if err != nil || size < 0 {
		writeJSONError(w, http.StatusNotFound, "media not found")
		return true
	}
	readContext, cancel := context.WithCancel(r.Context())
	reader := &storedMediaReader{ctx: readContext, cancel: cancel, store: ranges, key: mapped, size: size}
	defer reader.Close()
	// Validate the object GET before committing response headers. Seeking
	// later closes this stream and resumes at the requested offset; no bytes
	// are buffered or discarded while seeking. HEAD only stats.
	if r.Method == http.MethodGet && size > 0 {
		reader.body, err = ranges.OpenRange(readContext, mapped, 0, size)
		if err != nil || reader.body == nil {
			writeJSONError(w, http.StatusNotFound, "media not found")
			return true
		}
	}
	w.Header().Set("Content-Type", safeMediaType(mediaType))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Add("Vary", "Authorization")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(privateMediaResponseWriter{w}, r, "", time.Time{}, reader)
	return true
}

type privateMediaResponseWriter struct{ http.ResponseWriter }

func (w privateMediaResponseWriter) WriteHeader(status int) {
	// ServeContent removes Cache-Control on errors (including 416). Keep
	// the private library's policy on those responses as well.
	w.Header().Set("Cache-Control", "private, no-store")
	w.ResponseWriter.WriteHeader(status)
}

// storedMediaReader adapts streaming store ranges to ServeContent without
// loading the object into memory. Size seeks and HEAD do not open a body.
type storedMediaReader struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	store        media.RangeReader
	key          string
	size         int64
	position     int64
	bodyPosition int64
	body         io.ReadCloser
	closed       bool
}

func (s *storedMediaReader) Close() error {
	// A multipart ServeContent writer may still be blocked in a store read
	// when the client disconnects. Cancel it before waiting for its lock.
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.closeBody()
}

func (s *storedMediaReader) closeBody() error {
	if s.body != nil {
		err := s.body.Close()
		s.body = nil
		return err
	}
	return nil
}

func (s *storedMediaReader) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	base := int64(0)
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = s.position
	case io.SeekEnd:
		base = s.size
	default:
		return 0, errors.New("invalid media seek")
	}
	if offset < -base || offset > s.size-base {
		return 0, errors.New("invalid media seek")
	}
	s.position = base + offset
	return s.position, nil
}

func (s *storedMediaReader) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if s.position >= s.size {
		return 0, io.EOF
	}
	if s.body == nil || s.bodyPosition != s.position {
		_ = s.closeBody()
		body, err := s.store.OpenRange(s.ctx, s.key, s.position, s.size-s.position)
		if err != nil {
			return 0, err
		}
		s.body = body
		s.bodyPosition = s.position
	}
	n, err := s.body.Read(p)
	s.position += int64(n)
	s.bodyPosition += int64(n)
	return n, err
}

func canonicalMediaReference(r *http.Request) (string, bool) {
	if r == nil || r.URL == nil {
		return "", false
	}
	if strings.Contains(r.URL.RawPath, "%") {
		return "", false
	}
	subpath := chi.URLParam(r, "*")
	if subpath == "" {
		subpath = strings.TrimPrefix(r.URL.Path, "/media/")
	}
	subpath = strings.TrimSpace(subpath)
	reference := "/media/" + subpath
	if r.URL.Path != reference {
		return "", false
	}
	key, err := media.KeyFromStoredMediaURL(reference)
	if err != nil {
		return "", false
	}
	if param := chi.URLParam(r, "*"); param != "" && param != key {
		return "", false
	}
	return reference, true
}

func safeMediaType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n;") || strings.Contains(raw, " ") {
		return "application/octet-stream"
	}
	return raw
}
