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

func mediaVisibilityChecker(searchers ...posts.Searcher) (posts.MediaVisibilityChecker, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	checker, ok := searchers[0].(posts.MediaVisibilityChecker)
	return checker, ok
}

// moderationActor resolves the capability owner for moderation endpoints
// through the identity contract. The second result reports whether the actor
// holds the write capability that guards moderation; every resolved actor
// holds it once authenticated, while unauthenticated requests never do.

// gatedMedia serves /media/* files while enforcing the centralized
// visibility rule: a path referenced by post media is served only when a
// non-deleted referencing post is published. Unreferenced paths keep the
// previous public behavior; without a checker the handler also serves,
// preserving routers whose searcher predates moderation.
func gatedMedia(searchers ...posts.Searcher) http.Handler {
	fileServer := http.StripPrefix("/media/", http.FileServer(http.Dir(mediaRoot())))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checker, ok := mediaVisibilityChecker(searchers...)
		if !ok {
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
