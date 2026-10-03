package posts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const maxReactionPosts = 100

// ValidateReactionRequest normalizes and validates a bounded reaction shape.
// It is shared by HTTP and storage callers so malformed input never opens a
// database transaction.
func ValidateReactionRequest(request ReactionRequest) (ReactionRequest, error) {
	if len(request.Posts) == 0 || len(request.Posts) > maxReactionPosts {
		return ReactionRequest{}, fmt.Errorf("%w: posts must contain 1 to 100 targets", ErrInvalidReactions)
	}
	if request.Favorite == nil && request.Score == nil {
		return ReactionRequest{}, fmt.Errorf("%w: favorite or score is required", ErrInvalidReactions)
	}
	if request.Score != nil && (*request.Score < 0 || *request.Score > math.MaxInt32) {
		return ReactionRequest{}, fmt.Errorf("%w: score must be a non-negative integer", ErrInvalidReactions)
	}
	seen := make(map[string]struct{}, len(request.Posts))
	for index, target := range request.Posts {
		id := target.ID
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 || id != strconv.FormatInt(parsed, 10) {
			return ReactionRequest{}, fmt.Errorf("%w: posts[%d].id must be a positive integer", ErrInvalidReactions, index)
		}
		if target.Version < 0 {
			return ReactionRequest{}, fmt.Errorf("%w: posts[%d].version must be non-negative", ErrInvalidReactions, index)
		}
		if _, exists := seen[id]; exists {
			return ReactionRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidReactions, id)
		}
		seen[id] = struct{}{}
	}
	return request, nil
}

// applyReaction updates one row while its transaction lock is held.
func (s *PostgresSearcher) applyReaction(ctx context.Context, tx pgx.Tx, target ReactionTarget, favorite *bool, score *int) (ReactionResult, error) {
	var currentFavorite bool
	var currentScore, currentVersion int
	err := tx.QueryRow(ctx, lockPostForReactionSQL, target.ID).Scan(&currentFavorite, &currentScore, &currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReactionResult{}, ErrNotFound
	}
	if err != nil {
		return ReactionResult{}, err
	}
	if currentVersion != target.Version {
		return ReactionResult{}, ErrReactionConflict
	}

	nextFavorite, nextScore := currentFavorite, currentScore
	if favorite != nil {
		nextFavorite = *favorite
	}
	if score != nil {
		nextScore = *score
	}
	result := ReactionResult{
		ID: target.ID, Version: currentVersion, Favorite: nextFavorite, Score: nextScore,
	}
	if nextFavorite == currentFavorite && nextScore == currentScore {
		return result, nil
	}

	nextVersion := currentVersion + 1
	if _, err := tx.Exec(ctx, updatePostReactionSQL, nextFavorite, nextScore, nextVersion, target.ID); err != nil {
		return ReactionResult{}, err
	}
	if _, err := tx.Exec(ctx, insertPostReactionRevisionSQL, target.ID, nextVersion, nextFavorite, nextScore); err != nil {
		return ReactionResult{}, err
	}
	result.Version = nextVersion
	result.Changed = true
	return result, nil
}
