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
