package posts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

const maxRatingPosts = 100

// UnmarshalJSON distinguishes a missing/null version from version zero and
// keeps target objects strict even when the outer decoder allows other fields.
func (target *RatingEdit) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return fmt.Errorf("%w: invalid target", ErrInvalidRating)
	}
	for key := range fields {
		if key != "id" && key != "version" {
			return fmt.Errorf("%w: unknown target field %q", ErrInvalidRating, key)
		}
	}
	if raw, ok := fields["id"]; !ok || json.Unmarshal(raw, &target.ID) != nil {
		return fmt.Errorf("%w: target id is required", ErrInvalidRating)
	}
	raw, ok := fields["version"]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%w: target version is required", ErrInvalidRating)
	}
	var version int
	if err := json.Unmarshal(raw, &version); err != nil || version < 0 {
		return fmt.Errorf("%w: target version must be a non-negative integer", ErrInvalidRating)
	}
	target.Version = &version
	return nil
}

// ValidateRatingRequest enforces the bounded canonical request contract.
func ValidateRatingRequest(request RatingRequest) (RatingRequest, error) {
	if len(request.Posts) == 0 || len(request.Posts) > maxRatingPosts {
		return RatingRequest{}, fmt.Errorf("%w: posts must contain 1 to 100 targets", ErrInvalidRating)
	}
	if !request.Rating.Valid() {
		return RatingRequest{}, fmt.Errorf("%w: rating must be unrated, safe, questionable, or explicit", ErrInvalidRating)
	}
	seen := make(map[string]struct{}, len(request.Posts))
	for index, target := range request.Posts {
		parsed, err := strconv.ParseInt(target.ID, 10, 64)
		if err != nil || parsed <= 0 || target.ID != strconv.FormatInt(parsed, 10) {
			return RatingRequest{}, fmt.Errorf("%w: posts[%d].id must be a canonical positive integer", ErrInvalidRating, index)
		}
		if target.Version == nil || *target.Version < 0 {
			return RatingRequest{}, fmt.Errorf("%w: posts[%d].version must be a non-negative integer", ErrInvalidRating, index)
		}
		if _, exists := seen[target.ID]; exists {
			return RatingRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidRating, target.ID)
		}
		seen[target.ID] = struct{}{}
	}
	return request, nil
}

const lockPostsForRatingSQL = `
SELECT id::text, rating, rating_version
FROM posts
WHERE id = ANY($1::bigint[])
  AND deleted_at IS NULL
  AND moderation_state = 'published'
ORDER BY posts.id
FOR UPDATE
`

const updatePostRatingSQL = `
UPDATE posts
SET rating = $1, rating_version = $2
WHERE id = $3::bigint AND deleted_at IS NULL AND moderation_state = 'published'
`

const insertPostRatingRevisionSQL = `
INSERT INTO post_rating_revisions (post_id, version, previous_rating, rating)
VALUES ($1::bigint, $2, $3, $4)
`

type currentRating struct {
	id      string
	rating  Rating
	version int
}

// UpdateRatings locks all target post rows in numeric order, verifies every
// version before writing, then commits all effective edits and revisions as
// one unit. The posts-row locks serialize with tags, metadata, and reactions.
func (s *PostgresBackend) UpdateRatings(ctx context.Context, request RatingRequest) (RatingResult, error) {
	if s == nil || s.pool == nil {
		return RatingResult{}, ErrUnavailable
	}
	validated, err := ValidateRatingRequest(request)
	if err != nil {
		return RatingResult{}, err
	}
	ids := make([]int64, 0, len(validated.Posts))
	for _, target := range validated.Posts {
		id, _ := strconv.ParseInt(target.ID, 10, 64) // validated above
		ids = append(ids, id)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RatingResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, lockPostsForRatingSQL, ids)
	if err != nil {
		return RatingResult{}, err
	}
	current := make(map[string]currentRating, len(ids))
	for rows.Next() {
		var row currentRating
		if err := rows.Scan(&row.id, &row.rating, &row.version); err != nil {
			rows.Close()
			return RatingResult{}, err
		}
		current[row.id] = row
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return RatingResult{}, rowsErr
	}
	if len(current) != len(validated.Posts) {
		return RatingResult{}, ErrNotFound
	}
	for _, target := range validated.Posts {
		if current[target.ID].version != *target.Version {
			return RatingResult{}, ErrRatingConflict
		}
	}

	result := RatingResult{Posts: make([]RatingOutcome, 0, len(validated.Posts))}
	for _, target := range validated.Posts {
		row := current[target.ID]
		item := RatingOutcome{ID: target.ID, Version: row.version, Rating: row.rating}
		if row.rating == validated.Rating {
			result.Posts = append(result.Posts, item)
			continue
		}
		if row.version == math.MaxInt32 {
			return RatingResult{}, ErrInvalidRating
		}
		nextVersion := row.version + 1
		if _, err := tx.Exec(ctx, updatePostRatingSQL, validated.Rating, nextVersion, target.ID); err != nil {
			return RatingResult{}, err
		}
		if _, err := tx.Exec(ctx, insertPostRatingRevisionSQL, target.ID, nextVersion, row.rating, validated.Rating); err != nil {
			return RatingResult{}, err
		}
		item.Version, item.Rating, item.Changed = nextVersion, validated.Rating, true
		result.Posts = append(result.Posts, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return RatingResult{}, err
	}
	return result, nil
}

var _ RatingMutator = (*PostgresBackend)(nil)
