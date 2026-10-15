package httpapi

import (
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
)

// Dependencies lists the independently supplied backend capabilities used by
// each HTTP surface. This lets partial backends serve only the routes they
// implement without requiring a Searcher implementation.
type Dependencies struct {
	Posts               posts.Searcher
	PostDetails         posts.PostDetailer
	Uploads             posts.UploadCreator
	BrowserImports      posts.BrowserImporter
	Metadata            posts.MetadataMutator
	MetadataRead        posts.MetadataReader
	Ratings             posts.RatingMutator
	Tags                posts.TagMutator
	TagRevisions        posts.TagReverter
	Reactions           posts.ReactionMutator
	SavedSearches       posts.SavedSearchStore
	Collections         posts.CollectionMutator
	CollectionSummaries posts.CollectionSummaryReader
	CollectionPosts     posts.CollectionPostSearcher
	CollectionPostMover posts.CollectionPostMover
	CollectionManager   posts.CollectionManager
	Trash               posts.PostTrashStore
	TagSuggestions      posts.TagSuggester
	TagTaxonomy         posts.TagTaxonomyManager
	Exports             posts.LibraryExporter
	Moderation          posts.ModerationMutator
	ModerationRead      posts.ModerationReader
	ModerationPreview   posts.ModerationPreviewReader
	Jobs                jobs.Reader
	MediaVisibility     posts.MediaVisibilityChecker
	// MediaStore is the configured object store. Private mode opens it
	// only after resolving a stored /media URL to a writer-owned key.
	MediaStore      media.Store
	Duplicates      posts.DuplicateReader
	Similarity      posts.SimilarityReader
	SimilarityIndex posts.SimilarityIndexer
	// Accounts is optional: nil preserves legacy token/open behavior with
	// no session endpoints. When set, PrivateMode is honored by the router.
	Accounts *AccountsConfig
	Recovery *recovery.Service
}

// legacyDependencies preserves the historical Searcher-based constructors.
// Capability discovery is intentionally confined to this compatibility path.
func legacyDependencies(searchers ...posts.Searcher) Dependencies {
	if len(searchers) == 0 || searchers[0] == nil {
		return Dependencies{}
	}
	backend := searchers[0]
	deps := Dependencies{Posts: backend}
	deps.PostDetails, _ = backend.(posts.PostDetailer)
	deps.Uploads, _ = backend.(posts.UploadCreator)
	deps.BrowserImports, _ = backend.(posts.BrowserImporter)
	deps.Metadata, _ = backend.(posts.MetadataMutator)
	deps.MetadataRead, _ = backend.(posts.MetadataReader)
	deps.Ratings, _ = backend.(posts.RatingMutator)
	deps.Tags, _ = backend.(posts.TagMutator)
	deps.TagRevisions, _ = backend.(posts.TagReverter)
	deps.Reactions, _ = backend.(posts.ReactionMutator)
	deps.SavedSearches, _ = backend.(posts.SavedSearchStore)
	deps.Collections, _ = backend.(posts.CollectionMutator)
	deps.CollectionSummaries, _ = backend.(posts.CollectionSummaryReader)
	deps.CollectionPosts, _ = backend.(posts.CollectionPostSearcher)
	deps.CollectionPostMover, _ = backend.(posts.CollectionPostMover)
	deps.CollectionManager, _ = backend.(posts.CollectionManager)
	deps.Trash, _ = backend.(posts.PostTrashStore)
	deps.TagSuggestions, _ = backend.(posts.TagSuggester)
	deps.TagTaxonomy, _ = backend.(posts.TagTaxonomyManager)
	deps.Exports, _ = backend.(posts.LibraryExporter)
	deps.Moderation, _ = backend.(posts.ModerationMutator)
	deps.ModerationRead, _ = backend.(posts.ModerationReader)
	deps.ModerationPreview, _ = backend.(posts.ModerationPreviewReader)
	deps.Jobs, _ = backend.(jobs.Reader)
	deps.MediaVisibility, _ = backend.(posts.MediaVisibilityChecker)
	deps.Duplicates, _ = backend.(posts.DuplicateReader)
	deps.Similarity, _ = backend.(posts.SimilarityReader)
	deps.SimilarityIndex, _ = backend.(posts.SimilarityIndexer)
	return deps
}
