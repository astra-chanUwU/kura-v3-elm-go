package posts

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresSearcher) ListCollections(ctx context.Context) ([]Collection, error) {
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

// SearchCollectionPosts returns at most limit visible members in collection
// order with the version observed by the same snapshot. The version and the
// rows come from one repeatable-read transaction, so a concurrent mutation
// cannot slip between the version check and the page. A cursor issued for
// an older version fails with ErrCollectionChanged; callers answer 409 and
// the client restarts from the first page.
func (s *PostgresSearcher) SearchCollectionPosts(ctx context.Context, collectionID string, cursor *CollectionCursor, limit int) (CollectionPostsPage, error) {
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

func (s *PostgresSearcher) CreateCollection(ctx context.Context, request CreateCollectionRequest) (Collection, error) {
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

func (s *PostgresSearcher) AddCollectionPosts(ctx context.Context, collectionID string, request AddCollectionPostsRequest) (Collection, error) {
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

func (s *PostgresSearcher) RemoveCollectionPost(ctx context.Context, collectionID string, postID string) error {
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

func (s *PostgresSearcher) ReorderCollection(ctx context.Context, collectionID string, request ReorderCollectionRequest) (Collection, error) {
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

func (s *PostgresSearcher) collectionByID(ctx context.Context, id string) (Collection, error) {
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
