// Package httpapi adapts Kura's domain contracts to HTTP using Chi and
// standard net/http handlers. router.go is the route and middleware map;
// feature files own request parsing and response mapping, while posts,
// auth, jobs, and media own the behavior behind those endpoints.
package httpapi

import (
	"net/http"
	"os"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

// NewRouter returns the HTTP surface using KURA_API_TOKEN. With no backend,
// health and capability discovery remain usable and domain endpoints return
// their existing unavailable responses.
func NewRouter(searchers ...posts.Searcher) http.Handler {
	return NewRouterWithToken(strings.TrimSpace(os.Getenv("KURA_API_TOKEN")), searchers...)
}

// NewRouterWithToken assembles the Chi router with an optional write token.
// CORS runs first so preflight does not require authentication. The capability
// middleware resolves actors for reads and writes and protects API mutations;
// keeping it at the root also guards unknown API write paths.
func NewRouterWithToken(apiToken string, searchers ...posts.Searcher) http.Handler {
	apiToken = strings.TrimSpace(apiToken)
	r := chi.NewRouter()
	r.Use(localDevCORS)
	r.Use(requireWriteCapability(apiToken))

	r.Get("/health", health)
	r.Handle("/media/*", gatedMedia(searchers...))

	r.Route("/api", func(api chi.Router) {
		// Deployment capabilities (capabilities.go).
		api.Get("/capabilities", capabilities(apiToken))

		// Post browsing (posts.go), edits (tags.go, reactions.go), and
		// moderation (moderation.go). Static paths precede the id routes.
		api.Get("/posts", searchPosts(searchers...))
		api.Get("/posts/moderation", listModerationQueue(searchers...))
		api.Post("/posts/moderation", moderatePosts(searchers...))
		api.Post("/posts/tags", editTags(searchers...))
		api.Post("/posts/tags/revert", revertTags(searchers...))
		api.Post("/posts/reactions", editReactions(searchers...))
		api.Get("/posts/{id}", postDetail(searchers...))
		api.Get("/posts/{id}/moderation", listPostModerationActions(searchers...))

		// Uploads and durable processing status (uploads.go, jobs.go).
		api.Post("/uploads", uploadPost(searchers...))
		api.Get("/jobs/{id}", getJobStatus(searchers...))

		// Saved searches (saved_searches.go).
		api.Get("/saved-searches", listSavedSearches(searchers...))
		api.Post("/saved-searches", createSavedSearch(searchers...))
		api.Patch("/saved-searches/{id}", updateSavedSearch(searchers...))
		api.Delete("/saved-searches/{id}", deleteSavedSearch(searchers...))

		// Ordered collections (collections.go).
		api.Get("/collections", listCollections(searchers...))
		api.Post("/collections", createCollection(searchers...))
		api.Get("/collections/{id}/posts", searchCollectionPosts(searchers...))
		api.Post("/collections/{id}/posts", addCollectionPosts(searchers...))
		api.Delete("/collections/{id}/posts/{postID}", removeCollectionPost(searchers...))
		api.Post("/collections/{id}/order", reorderCollection(searchers...))
	})
	return r
}
