package posts

import "errors"

// ErrUnavailable indicates that post search has not been configured for the
// running process. The HTTP layer maps it to a service-unavailable response.
var ErrUnavailable = errors.New("post search unavailable")
