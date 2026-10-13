package httpapi

import (
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

// Dependencies lists the independently supplied backend capabilities used by
// each HTTP surface. This lets partial backends serve only the routes they
// implement without requiring a Searcher implementation.
type Dependencies struct {
	Posts               posts.Searcher
	PostDetails         posts.PostDetailer
	Uploads             posts.UploadCreator
	Tags                posts.TagMutator
	TagRevisions        posts.TagReverter
	Reactions           posts.ReactionMutator
	SavedSearches       posts.SavedSearchStore
	Collections         posts.CollectionMutator
	CollectionSummaries posts.CollectionSummaryReader
	CollectionPosts     posts.CollectionPostSearcher
	CollectionPostMover posts.CollectionPostMover
	Moderation          posts.ModerationMutator
	ModerationRead      posts.ModerationReader
	Jobs                jobs.Reader
	MediaVisibility     posts.MediaVisibilityChecker
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
	deps.Tags, _ = backend.(posts.TagMutator)
	deps.TagRevisions, _ = backend.(posts.TagReverter)
	deps.Reactions, _ = backend.(posts.ReactionMutator)
	deps.SavedSearches, _ = backend.(posts.SavedSearchStore)
	deps.Collections, _ = backend.(posts.CollectionMutator)
	deps.CollectionSummaries, _ = backend.(posts.CollectionSummaryReader)
	deps.CollectionPosts, _ = backend.(posts.CollectionPostSearcher)
	deps.CollectionPostMover, _ = backend.(posts.CollectionPostMover)
	deps.Moderation, _ = backend.(posts.ModerationMutator)
	deps.ModerationRead, _ = backend.(posts.ModerationReader)
	deps.Jobs, _ = backend.(jobs.Reader)
	deps.MediaVisibility, _ = backend.(posts.MediaVisibilityChecker)
	return deps
}
