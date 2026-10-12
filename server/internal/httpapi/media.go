package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
func gatedMedia(checker posts.MediaVisibilityChecker) http.Handler {
	fileServer := http.StripPrefix("/media/", http.FileServer(http.Dir(mediaRoot())))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checker == nil {
			fileServer.ServeHTTP(w, r)
			return
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
