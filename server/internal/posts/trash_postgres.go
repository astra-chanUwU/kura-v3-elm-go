package posts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxTrashLockRetries = 3

const listTrashSQL = `
SELECT id::text, preview_url, original_url, media_type, width, height, tags,
       deleted_at, deleted_at::text AS deleted_at_text, moderation_state
FROM posts
WHERE deleted_at IS NOT NULL
  AND ($1::timestamptz IS NULL OR (deleted_at, id) < ($1::timestamptz, $2::bigint))
ORDER BY deleted_at DESC, id DESC
LIMIT $3
`

const trashCollectionIDsSQL = `
SELECT DISTINCT collection_id
FROM collection_posts
WHERE post_id = ANY($1::bigint[])
ORDER BY collection_id
`

const lockTrashCollectionSQL = `
SELECT id FROM collections WHERE id = $1::bigint FOR UPDATE
`

const lockTrashPostSQL = `
SELECT deleted_at IS NOT NULL, moderation_state
FROM posts
WHERE id = $1::bigint
FOR UPDATE
`

const trashPostUpdateSQL = `
UPDATE posts SET deleted_at = now()
WHERE id = $1::bigint AND deleted_at IS NULL
`

const restorePostUpdateSQL = `
UPDATE posts SET deleted_at = NULL
WHERE id = $1::bigint AND deleted_at IS NOT NULL
`

const insertTrashActionSQL = `
INSERT INTO post_trash_actions (post_id, action, actor, reason)
VALUES ($1::bigint, $2, $3, $4)
`

const bumpTrashCollectionVersionSQL = `
UPDATE collections SET version = version + 1 WHERE id = $1::bigint
`

const trashCollectionHasChangedPostSQL = `
SELECT post_id FROM collection_posts
WHERE collection_id = $1::bigint AND post_id = ANY($2::bigint[])
`

func (s *PostgresBackend) ListTrash(ctx context.Context, actor string, limit int, cursor *TrashCursor) (TrashPage, error) {
	if s == nil || s.pool == nil {
		return TrashPage{}, ErrUnavailable
	}
	if _, err := ValidateTrashActor(actor); err != nil {
		return TrashPage{}, err
	}
	if err := ValidateTrashLimit(limit); err != nil {
		return TrashPage{}, err
	}
	var deletedAt any
	var afterID any
	if cursor != nil {
		if cursor.ID <= 0 || cursor.DeletedAt.IsZero() {
			return TrashPage{}, ErrInvalidCursor
		}
		deletedAt, afterID = cursor.DeletedAt, cursor.ID
	}
	rows, err := s.pool.Query(ctx, listTrashSQL, deletedAt, afterID, limit+1)
	if err != nil {
		return TrashPage{}, err
	}
	defer rows.Close()
	page := TrashPage{Posts: make([]TrashPost, 0, limit)}
	var lastDeletedAt = cursorTime(cursor)
	var lastID int64
	for rows.Next() {
		var item TrashPost
		var deletedAtValue time.Time
		if err := rows.Scan(&item.ID, &item.PreviewURL, &item.OriginalURL, &item.MediaType, &item.Width, &item.Height, &item.Tags, &deletedAtValue, &item.DeletedAt, &item.ModerationState); err != nil {
			return TrashPage{}, err
		}
		if len(page.Posts) == limit {
			token, err := EncodeTrashCursor(lastDeletedAt, lastID)
			if err != nil {
				return TrashPage{}, err
			}
			page.NextCursor = &token
			break
		}
		page.Posts = append(page.Posts, item)
		lastDeletedAt, lastID = deletedAtValue.UTC(), parsePostID(item.ID)
	}
	if err := rows.Err(); err != nil {
		return TrashPage{}, err
	}
	// Apply the preview overlay to the embedded summaries after the shared
	// helper has resolved ready derivative URLs.
	summaries := pageSummaries(page.Posts)
	if err := s.overlayReadyPreviews(ctx, summaries); err != nil {
		return TrashPage{}, err
	}
	for i := range page.Posts {
		page.Posts[i].PreviewURL = summaries[i].PreviewURL
	}
	return page, nil
}

func (s *PostgresBackend) Trash(ctx context.Context, actor string, request TrashRequest) (TrashResponse, error) {
	return s.changeTrashState(ctx, actor, request, true)
}

func (s *PostgresBackend) Restore(ctx context.Context, actor string, request TrashRequest) (TrashResponse, error) {
	return s.changeTrashState(ctx, actor, request, false)
}

func (s *PostgresBackend) changeTrashState(ctx context.Context, actor string, request TrashRequest, trash bool) (TrashResponse, error) {
	if s == nil || s.pool == nil {
		return TrashResponse{}, ErrUnavailable
	}
	owner, err := ValidateTrashActor(actor)
	if err != nil {
		return TrashResponse{}, err
	}
	validated, err := ValidateTrashRequest(request)
	if err != nil {
		return TrashResponse{}, err
	}
	ids := make([]int64, len(validated.PostIDs))
	for i, id := range validated.PostIDs {
		ids[i], _ = strconv.ParseInt(id, 10, 64)
	}
	lockIDs := append([]int64(nil), ids...)
	sort.Slice(lockIDs, func(i, j int) bool { return lockIDs[i] < lockIDs[j] })
	for attempt := 0; attempt < maxTrashLockRetries; attempt++ {
		result, retry, err := s.changeTrashStateAttempt(ctx, owner, validated, ids, lockIDs, trash)
		if err != nil {
			return TrashResponse{}, err
		}
		if !retry {
			return result, nil
		}
	}
	return TrashResponse{}, fmt.Errorf("%w: collection membership changed during trash operation", ErrConflict)
}

func (s *PostgresBackend) changeTrashStateAttempt(ctx context.Context, actor string, request TrashRequest, inputIDs, sortedPostIDs []int64, trash bool) (TrashResponse, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TrashResponse{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	collectionIDs, err := trashCollectionIDs(ctx, tx, sortedPostIDs)
	if err != nil {
		return TrashResponse{}, false, err
	}
	if err := lockTrashCollections(ctx, tx, collectionIDs); errors.Is(err, pgx.ErrNoRows) {
		return TrashResponse{}, true, nil
	} else if err != nil {
		return TrashResponse{}, false, err
	}
	states := make(map[int64]bool, len(sortedPostIDs))
	moderationStates := make(map[int64]string, len(sortedPostIDs))
	for _, id := range sortedPostIDs {
		var deleted bool
		var moderationState string
		if err := tx.QueryRow(ctx, lockTrashPostSQL, id).Scan(&deleted, &moderationState); errors.Is(err, pgx.ErrNoRows) {
			return TrashResponse{}, false, ErrNotFound
		} else if err != nil {
			return TrashResponse{}, false, err
		}
		states[id] = deleted
		moderationStates[id] = moderationState
	}
	currentCollections, err := trashCollectionIDs(ctx, tx, sortedPostIDs)
	if err != nil {
		return TrashResponse{}, false, err
	}
	if !sameIDs(collectionIDs, currentCollections) {
		return TrashResponse{}, true, nil
	}
	visibilityChangedPosts := make(map[int64]struct{}, len(sortedPostIDs))
	for _, id := range sortedPostIDs {
		alreadyDeleted := states[id]
		if trash == alreadyDeleted {
			continue
		}
		update := restorePostUpdateSQL
		action := "restore"
		if trash {
			update, action = trashPostUpdateSQL, "trash"
		}
		if _, err := tx.Exec(ctx, update, id); err != nil {
			return TrashResponse{}, false, err
		}
		if _, err := tx.Exec(ctx, insertTrashActionSQL, id, action, actor, request.Reason); err != nil {
			return TrashResponse{}, false, err
		}
		if IsPubliclyVisibleModerationState(moderationStates[id]) {
			visibilityChangedPosts[id] = struct{}{}
		}
	}
	if len(visibilityChangedPosts) > 0 {
		for _, collectionID := range collectionIDs {
			members, err := tx.Query(ctx, trashCollectionHasChangedPostSQL, collectionID, sortedPostIDs)
			if err != nil {
				return TrashResponse{}, false, err
			}
			affected := false
			for members.Next() {
				var postID int64
				if err := members.Scan(&postID); err != nil {
					members.Close()
					return TrashResponse{}, false, err
				}
				if _, ok := visibilityChangedPosts[postID]; ok {
					affected = true
					break
				}
			}
			members.Close()
			if err := members.Err(); err != nil {
				return TrashResponse{}, false, err
			}
			if !affected {
				continue
			}
			if _, err := tx.Exec(ctx, bumpTrashCollectionVersionSQL, collectionID); err != nil {
				return TrashResponse{}, false, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return TrashResponse{}, false, err
	}
	response := TrashResponse{Posts: make([]TrashResult, 0, len(inputIDs))}
	for _, id := range inputIDs {
		response.Posts = append(response.Posts, TrashResult{ID: strconv.FormatInt(id, 10), Changed: states[id] != trash})
	}
	return response, false, nil
}

func trashCollectionIDs(ctx context.Context, tx pgx.Tx, postIDs []int64) ([]int64, error) {
	rows, err := tx.Query(ctx, trashCollectionIDsSQL, postIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, 8)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func lockTrashCollections(ctx context.Context, tx pgx.Tx, collectionIDs []int64) error {
	for _, id := range collectionIDs {
		var locked int64
		if err := tx.QueryRow(ctx, lockTrashCollectionSQL, id).Scan(&locked); err != nil {
			return err
		}
	}
	return nil
}

func sameIDs(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func cursorTime(cursor *TrashCursor) time.Time {
	if cursor == nil {
		return time.Time{}
	}
	return cursor.DeletedAt
}

func parsePostID(id string) int64 {
	parsed, _ := strconv.ParseInt(id, 10, 64)
	return parsed
}

func pageSummaries(posts []TrashPost) []PostSummary {
	summaries := make([]PostSummary, len(posts))
	for i := range posts {
		summaries[i] = posts[i].PostSummary
	}
	return summaries
}
