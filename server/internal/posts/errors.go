package posts

import "errors"

// ErrUnavailable indicates that post search has not been configured for the
// running process. The HTTP layer maps it to a service-unavailable response.
var ErrUnavailable = errors.New("post search unavailable")

// ErrInvalidQuery indicates that PostgreSQL could not parse a full-text
// search expression supplied by the caller.
var ErrInvalidQuery = errors.New("invalid post search query")

// ErrNotFound indicates that a post does not exist or has been deleted.
var ErrNotFound = errors.New("post not found")

// ErrConflict indicates that a tag edit was based on an older tag version.
var ErrConflict = errors.New("post tag version conflict")

// ErrInvalidTags indicates malformed tag-edit input.
var ErrInvalidTags = errors.New("invalid tags")

// ErrRevisionNotFound indicates that a requested immutable tag state is not
// available (old revisions may predate complete target-state storage).
var ErrRevisionNotFound = errors.New("tag revision not found")

// ErrReactionConflict indicates that a favorite/score edit was based on an
// older reaction version.
var ErrReactionConflict = errors.New("post reaction version conflict")

// ErrInvalidReactions indicates malformed favorite/score mutation input.
var ErrInvalidReactions = errors.New("invalid reactions")

// ErrRatingConflict indicates that a rating edit used an older independent
// rating version.
var ErrRatingConflict = errors.New("post rating version conflict")

// ErrInvalidRating indicates malformed content-rating mutation input.
var ErrInvalidRating = errors.New("invalid rating edit")

var ErrInvalidCollections = errors.New("invalid collections")

// ErrCollectionChanged indicates that a versioned collection cursor was
// issued for an older membership/order version. The HTTP layer maps it to
// a 409 response carrying the collection_changed code.
var ErrCollectionChanged = errors.New("collection changed")

// ErrInvalidSavedSearch indicates malformed owner-scoped saved-search input.
var ErrInvalidSavedSearch = errors.New("invalid saved search")

// ErrInvalidUpload indicates an unsupported, malformed, or oversized upload.
var ErrInvalidUpload = errors.New("invalid upload")

// ErrInvalidModeration indicates a malformed moderation command.
var ErrInvalidModeration = errors.New("invalid moderation")

var ErrInvalidTagTaxonomy = errors.New("invalid tag taxonomy request")
var ErrTagTaxonomyChanged = errors.New("tag taxonomy changed")
var ErrTagRetired = errors.New("tag is retired")
var ErrTagTargetExists = errors.New("target tag already exists")
var ErrTagOperationTooLarge = errors.New("tag operation exceeds 1000 affected posts")
