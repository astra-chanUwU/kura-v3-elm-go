package posts

import (
	"context"
	"errors"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresBackend) ListCollections(ctx context.Context) ([]Collection, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, listCollectionsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collections := make([]Collection, 0)
	for rows.Next() {
		var collection Collection
		if err := rows.Scan(&collection.ID, &collection.Name, &collection.Version, &collection.PostIDs); err != nil {
			return nil, err
		}
		if collection.PostIDs == nil {
			collection.PostIDs = []string{}
		}
		collections = append(collections, collection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return collections, nil
}

func (s *PostgresBackend) ListCollectionSummaries(ctx context.Context) ([]CollectionSummary, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, listCollectionSummariesSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make([]CollectionSummary, 0)
	for rows.Next() {
		var summary CollectionSummary
		if err := rows.Scan(&summary.ID, &summary.Name, &summary.Version, &summary.MemberCount, &summary.VisiblePostCount); err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summaries, nil
}

// SearchCollectionPosts returns at most limit visible members in collection
// order with the version observed by the same snapshot. The version and the
// rows come from one repeatable-read transaction, so a concurrent mutation
// cannot slip between the version check and the page. A cursor issued for
// an older version fails with ErrCollectionChanged; callers answer 409 and
// the client restarts from the first page.
func (s *PostgresBackend) SearchCollectionPosts(ctx context.Context, collectionID string, cursor *CollectionCursor, limit int) (CollectionPostsPage, error) {
	if s == nil || s.pool == nil {
		return CollectionPostsPage{}, ErrUnavailable
	}
	if limit < 1 || limit > 60 {
		return CollectionPostsPage{}, ErrInvalidQuery
	}
	id, err := collectionIDValue(collectionID)
	if err != nil {
		return CollectionPostsPage{}, err
	}
	if cursor != nil && cursor.CollectionID != id {
		return CollectionPostsPage{}, ErrInvalidCursor
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return CollectionPostsPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	if err := tx.QueryRow(ctx, collectionVersionSQL, id).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return CollectionPostsPage{}, ErrNotFound
	} else if err != nil {
		return CollectionPostsPage{}, err
	}
	if cursor != nil && cursor.Version != version {
		return CollectionPostsPage{}, ErrCollectionChanged
	}
	var cursorPosition, cursorPostID any
	if cursor != nil {
		cursorPosition, cursorPostID = cursor.Position, cursor.PostID
	}
	rows, err := tx.Query(ctx, searchCollectionPostsSQL, id, cursorPosition, cursorPostID, limit+1)
	if err != nil {
		return CollectionPostsPage{}, err
	}
	defer rows.Close()
	type positionedPost struct {
		position int64
		post     PostSummary
	}
	members := make([]positionedPost, 0, limit+1)
	for rows.Next() {
		var member positionedPost
		if err := rows.Scan(&member.position, &member.post.ID, &member.post.PreviewURL, &member.post.OriginalURL, &member.post.MediaType, &member.post.Width, &member.post.Height, &member.post.Tags); err != nil {
			return CollectionPostsPage{}, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return CollectionPostsPage{}, err
	}
	page := CollectionPostsPage{Posts: make([]PostSummary, 0, limit), CollectionVersion: version}
	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}
	for _, member := range members {
		page.Posts = append(page.Posts, member.post)
	}
	if err := overlayReadyPreviewsTx(ctx, tx, page.Posts); err != nil {
		return CollectionPostsPage{}, err
	}
	if hasMore {
		last := members[len(members)-1]
		var lastID int64
		if parsed, err := strconv.ParseInt(last.post.ID, 10, 64); err != nil || parsed <= 0 {
			return CollectionPostsPage{}, ErrInvalidQuery
		} else {
			lastID = parsed
		}
		token, err := EncodeCollectionCursor(id, version, last.position, lastID)
		if err != nil {
			return CollectionPostsPage{}, err
		}
		page.NextCursor = &token
	}
	if err := tx.Commit(ctx); err != nil {
		return CollectionPostsPage{}, err
	}
	return page, nil
}

func (s *PostgresBackend) CreateCollection(ctx context.Context, request CreateCollectionRequest) (Collection, error) {
	if s == nil || s.pool == nil {
		return Collection{}, ErrUnavailable
	}
	validated, err := ValidateCreateCollection(request)
	if err != nil {
		return Collection{}, err
	}
	var collection Collection
	err = s.pool.QueryRow(ctx, insertCollectionSQL, validated.Name).Scan(&collection.ID, &collection.Name, &collection.Version)
	if err != nil {
		return Collection{}, err
	}
	collection.PostIDs = []string{}
	return collection, nil
}

func (s *PostgresBackend) AddCollectionPosts(ctx context.Context, collectionID string, request AddCollectionPostsRequest) (Collection, error) {
	if s == nil || s.pool == nil {
		return Collection{}, ErrUnavailable
	}
	validated, err := ValidateAddCollectionPosts(request)
	if err != nil {
		return Collection{}, err
	}
	id, err := collectionIDValue(collectionID)
	if err != nil {
		return Collection{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Collection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int64
	if err := tx.QueryRow(ctx, lockCollectionSQL, id).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, ErrNotFound
	} else if err != nil {
		return Collection{}, err
	}
	postIDs := make([]int64, len(validated.PostIDs))
	for i, postID := range validated.PostIDs {
		postIDs[i], _ = strconv.ParseInt(postID, 10, 64)
	}
	// Match trash's collection-then-post lock order. The shared row locks
	// serialize the visibility check with deleted_at changes, so a concurrent
	// add cannot insert a stale membership after trash has committed.
	lockPostIDs := append([]int64(nil), postIDs...)
	sort.Slice(lockPostIDs, func(i, j int) bool { return lockPostIDs[i] < lockPostIDs[j] })
	for _, postID := range lockPostIDs {
		var locked int64
		err := tx.QueryRow(ctx, `
SELECT id FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL AND moderation_state = 'published'
FOR SHARE
`, postID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return Collection{}, err
		}
	}
	tag, err := tx.Exec(ctx, addCollectionPostSQL, id, postIDs)
	if err != nil {
		return Collection{}, err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, bumpCollectionVersionSQL, id); err != nil {
			return Collection{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Collection{}, err
	}
	return s.collectionByID(ctx, collectionID)
}

func (s *PostgresBackend) RemoveCollectionPost(ctx context.Context, collectionID string, postID string) error {
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	cid, err := collectionIDValue(collectionID)
	if err != nil {
		return err
	}
	pid, err := postIDValue(postID)
	if err != nil {
		return ErrInvalidCollections
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int64
	if err := tx.QueryRow(ctx, lockCollectionSQL, cid).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, deleteCollectionPostSQL, cid, pid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, bumpCollectionVersionSQL, cid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresBackend) ReorderCollection(ctx context.Context, collectionID string, request ReorderCollectionRequest) (Collection, error) {
	if s == nil || s.pool == nil {
		return Collection{}, ErrUnavailable
	}
	validated, err := ValidateReorderCollection(request)
	if err != nil {
		return Collection{}, err
	}
	cid, err := collectionIDValue(collectionID)
	if err != nil {
		return Collection{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Collection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int64
	if err := tx.QueryRow(ctx, lockCollectionSQL, cid).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, ErrNotFound
	} else if err != nil {
		return Collection{}, err
	}
	rows, err := tx.Query(ctx, collectionOrderedMembersSQL, cid)
	if err != nil {
		return Collection{}, err
	}
	existing := []string{}
	for rows.Next() {
		var pid string
		var position int64
		if err := rows.Scan(&pid, &position); err != nil {
			rows.Close()
			return Collection{}, err
		}
		existing = append(existing, pid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Collection{}, err
	}
	if len(existing) != len(validated.PostIDs) {
		return Collection{}, ErrInvalidCollections
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, id := range existing {
		existingSet[id] = struct{}{}
	}
	for _, id := range validated.PostIDs {
		if _, ok := existingSet[id]; !ok {
			return Collection{}, ErrInvalidCollections
		}
	}
	unchanged := true
	for idx, id := range validated.PostIDs {
		if existing[idx] != id {
			unchanged = false
			break
		}
	}
	if !unchanged {
		for idx, raw := range validated.PostIDs {
			pid, _ := strconv.ParseInt(raw, 10, 64)
			if _, err := tx.Exec(ctx, `UPDATE collection_posts SET position=$3 WHERE collection_id=$1::bigint AND post_id=$2::bigint`, cid, pid, int64(idx)); err != nil {
				return Collection{}, err
			}
		}
		if _, err := tx.Exec(ctx, bumpCollectionVersionSQL, cid); err != nil {
			return Collection{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Collection{}, err
	}
	return s.collectionByID(ctx, collectionID)
}

// MoveCollectionPost moves one member before the neighbor (or to the
// end) under the collection-row lock. It checks expected_version first,
// operates on every member including hidden and deleted posts, and bumps
// the version only when the order effectively changes.
func (s *PostgresBackend) MoveCollectionPost(ctx context.Context, collectionID string, request MoveCollectionPostRequest) (MoveCollectionPostResult, error) {
	if s == nil || s.pool == nil {
		return MoveCollectionPostResult{}, ErrUnavailable
	}
	validated, err := ValidateMoveCollectionPostRequest(request)
	if err != nil {
		return MoveCollectionPostResult{}, err
	}
	cid, err := collectionIDValue(collectionID)
	if err != nil {
		return MoveCollectionPostResult{}, err
	}
	pid, _ := strconv.ParseInt(validated.PostID, 10, 64)
	var before *int64
	if validated.BeforePostID != nil {
		parsed, _ := strconv.ParseInt(*validated.BeforePostID, 10, 64)
		before = &parsed
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MoveCollectionPostResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	if err := tx.QueryRow(ctx, lockCollectionSQL, cid).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return MoveCollectionPostResult{}, ErrNotFound
	} else if err != nil {
		return MoveCollectionPostResult{}, err
	}
	if version != validated.ExpectedVersion {
		return MoveCollectionPostResult{}, ErrCollectionChanged
	}
	rows, err := tx.Query(ctx, collectionAllMembersSQL, cid)
	if err != nil {
		return MoveCollectionPostResult{}, err
	}
	existing := []int64{}
	for rows.Next() {
		var member int64
		if err := rows.Scan(&member); err != nil {
			rows.Close()
			return MoveCollectionPostResult{}, err
		}
		existing = append(existing, member)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return MoveCollectionPostResult{}, err
	}
	next, changed, err := moveCollectionOrder(existing, pid, before)
	if err != nil {
		return MoveCollectionPostResult{}, err
	}
	result := MoveCollectionPostResult{ID: collectionID, CollectionID: collectionID, Version: version, Changed: changed}
	if changed {
		if _, err := tx.Exec(ctx, updateCollectionPositionsSQL, cid, next); err != nil {
			return MoveCollectionPostResult{}, err
		}
		if _, err := tx.Exec(ctx, bumpCollectionVersionSQL, cid); err != nil {
			return MoveCollectionPostResult{}, err
		}
		result.Version = version + 1
	}
	if err := tx.Commit(ctx); err != nil {
		return MoveCollectionPostResult{}, err
	}
	return result, nil
}

func (s *PostgresBackend) collectionByID(ctx context.Context, id string) (Collection, error) {
	collections, err := s.ListCollections(ctx)
	if err != nil {
		return Collection{}, err
	}
	for _, collection := range collections {
		if collection.ID == id {
			return collection, nil
		}
	}
	return Collection{}, ErrNotFound
}

func collectionIDValue(raw string) (int64, error) {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
		return 0, ErrInvalidCollections
	}
	return parsed, nil
}

func postIDValue(raw string) (int64, error) {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
		return 0, err
	}
	return parsed, nil
}
