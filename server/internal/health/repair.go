package health

import (
	"context"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

// JobRepair asks the existing derivative queue to build thumb-320 again.
// The original object is not opened, rewritten, or deleted here.
type JobRepair struct {
	Store *jobs.Store
}

func (r JobRepair) RepairThumbnail(ctx context.Context, postID, originalKey string) (string, string, error) {
	if r.Store == nil {
		return "", "", ErrUnavailable
	}
	request, err := media.EnqueueDerivative(postID, originalKey, media.DefaultDerivativeVariant)
	if err != nil {
		return "", "", ErrUnavailable
	}
	job, err := r.Store.EnqueueOrResetFailed(ctx, request)
	if err != nil {
		return "", "", ErrUnavailable
	}
	return job.ID, string(job.Status), nil
}
