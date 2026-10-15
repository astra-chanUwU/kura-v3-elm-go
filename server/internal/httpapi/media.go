package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	body, err := store.Open(r.Context(), mapped)
	if err != nil || body == nil {
		writeJSONError(w, http.StatusNotFound, "media not found")
		return true
	}
	defer body.Close()
	w.Header().Set("Content-Type", safeMediaType(mediaType))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Add("Vary", "Authorization")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		_, _ = io.Copy(io.Discard, body)
		return true
	}
	_, _ = io.Copy(w, body)
	return true
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
