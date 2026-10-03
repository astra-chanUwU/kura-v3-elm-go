package posts

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidateTagRevert validates the bounded optimistic restore request.
func ValidateTagRevert(request TagRevertRequest) (TagRevertRequest, error) {
	if request.TargetVersion <= 0 {
		return TagRevertRequest{}, fmt.Errorf("%w: target_version must be positive", ErrInvalidTags)
	}
	if len(request.Posts) == 0 || len(request.Posts) > maxTagEditPosts {
		return TagRevertRequest{}, fmt.Errorf("%w: posts must contain 1 to 100 targets", ErrInvalidTags)
	}
	seen := make(map[string]struct{}, len(request.Posts))
	for index, target := range request.Posts {
		id := strings.TrimSpace(target.ID)
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 || id != strconv.FormatInt(parsed, 10) || target.ID != id {
			return TagRevertRequest{}, fmt.Errorf("%w: posts[%d].id must be a positive integer", ErrInvalidTags, index)
		}
		if target.Version < 0 {
			return TagRevertRequest{}, fmt.Errorf("%w: posts[%d].version must be non-negative", ErrInvalidTags, index)
		}
		if _, ok := seen[id]; ok {
			return TagRevertRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidTags, id)
		}
		seen[id] = struct{}{}
		target.ID = id
		request.Posts[index] = target
	}
	return request, nil
}
