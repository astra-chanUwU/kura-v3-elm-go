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
	return NewRouterWithDependencies(strings.TrimSpace(os.Getenv("KURA_API_TOKEN")), legacyDependencies(searchers...))
}

// NewRouterWithToken assembles the Chi router with an optional write token.
// CORS runs first so preflight does not require authentication. The capability
// middleware resolves actors for reads and writes and protects API mutations;
// keeping it at the root also guards unknown API write paths.
func NewRouterWithToken(apiToken string, searchers ...posts.Searcher) http.Handler {
	return NewRouterWithDependencies(apiToken, legacyDependencies(searchers...))
}

// NewRouterWithDependencies assembles the Chi router using explicit backend
// capabilities. CORS runs first so preflight does not require authentication.
func NewRouterWithDependencies(apiToken string, deps Dependencies) http.Handler {
	apiToken = strings.TrimSpace(apiToken)
	r := chi.NewRouter()
	r.Use(localDevCORS)
	r.Use(requireWriteCapability(apiToken))

	r.Get("/health", health)
	r.Handle("/media/*", gatedMedia(deps.MediaVisibility))

	r.Route("/api", func(api chi.Router) {
		// Deployment capabilities (capabilities.go).
		api.Get("/capabilities", capabilities(apiToken))

		// Post browsing (posts.go), edits (tags.go, reactions.go), and
		// moderation (moderation.go). Static paths precede the id routes.
		api.Get("/posts", searchPosts(deps.Posts))
		api.Get("/tags/suggestions", suggestTags(deps.TagSuggestions))
		api.Get("/tags/manage", listManageTags(deps.TagTaxonomy))
		api.Post("/tags/manage/preview", previewTagOperation(deps.TagTaxonomy))
		api.Post("/tags/manage/apply", applyTagOperation(deps.TagTaxonomy))
		api.Get("/posts/export", exportLibrary(deps.Exports, false))
		api.Get("/posts/trash", listTrash(deps.Trash))
		api.Post("/posts/trash", trashPosts(deps.Trash))
		api.Post("/posts/restore", restorePosts(deps.Trash))
		api.Get("/posts/moderation", listModerationQueue(deps.ModerationRead))
		api.Post("/posts/moderation", moderatePosts(deps.Moderation))
		api.Post("/posts/tags", editTags(deps.Tags))
		api.Post("/posts/tags/revert", revertTags(deps.TagRevisions))
		api.Post("/posts/reactions", editReactions(deps.Reactions))
		api.Post("/posts/metadata", EditMetadata(deps.Metadata))
		api.Post("/posts/ratings", EditRatings(deps.Ratings))
		api.Get("/posts/{id}", postDetail(deps.PostDetails))
		api.Get("/posts/{id}/moderation", listPostModerationActions(deps.ModerationRead))

		// Uploads and durable processing status (uploads.go, jobs.go).
		api.Post("/uploads", uploadPost(deps.Uploads))
		api.Get("/imports/duplicates", browserImportDuplicates(deps.BrowserImports))
		api.Post("/imports", browserImport(deps.BrowserImports))
		api.Get("/jobs/{id}", getJobStatus(deps.Jobs))

		// Saved searches (saved_searches.go).
		api.Get("/saved-searches", listSavedSearches(deps.SavedSearches))
		api.Post("/saved-searches", createSavedSearch(deps.SavedSearches))
		api.Patch("/saved-searches/{id}", updateSavedSearch(deps.SavedSearches))
		api.Delete("/saved-searches/{id}", deleteSavedSearch(deps.SavedSearches))

		// Ordered collections (collections.go).
		api.Get("/collections", listCollections(deps.Collections))
		api.Get("/collections/summaries", listCollectionSummaries(deps.CollectionSummaries))
		api.Post("/collections", createCollection(deps.Collections))
		api.Get("/collections/{id}/posts", searchCollectionPosts(deps.CollectionPosts))
		api.Post("/collections/{id}/posts", addCollectionPosts(deps.Collections))
		api.Delete("/collections/{id}/posts/{postID}", removeCollectionPost(deps.Collections))
		api.Post("/collections/{id}/order", reorderCollection(deps.Collections))
		api.Post("/collections/{id}/move", moveCollectionPost(deps.CollectionPostMover))
		api.Patch("/collections/{id}", RenameCollection(deps.CollectionManager))
		api.Delete("/collections/{id}", DeleteCollection(deps.CollectionManager))
		api.Get("/collections/{id}/export", exportLibrary(deps.Exports, true))
	})
	return r
}
