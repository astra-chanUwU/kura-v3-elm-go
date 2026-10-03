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
		if err := rows.Scan(&collection.ID, &collection.Name, &collection.PostIDs); err != nil {
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
// order. Collection browsing deliberately has no cursor: collections are
// bounded and this endpoint is a single MediaGrid load.
func (s *PostgresSearcher) SearchCollectionPosts(ctx context.Context, collectionID string, limit int) (SearchPage, error) {
	if s == nil || s.pool == nil {
		return SearchPage{}, ErrUnavailable
	}
	if limit < 1 || limit > 60 {
		return SearchPage{}, ErrInvalidQuery
	}
	id, err := collectionIDValue(collectionID)
	if err != nil {
		return SearchPage{}, err
	}
	var exists int
	if err := s.pool.QueryRow(ctx, collectionExistsSQL, id).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return SearchPage{}, ErrNotFound
	} else if err != nil {
		return SearchPage{}, err
	}
	rows, err := s.pool.Query(ctx, searchCollectionPostsSQL, id, limit)
	if err != nil {
		return SearchPage{}, err
	}
	defer rows.Close()
	page := SearchPage{Posts: make([]PostSummary, 0, limit)}
	for rows.Next() {
		var post PostSummary
		if err := rows.Scan(&post.ID, &post.PreviewURL, &post.OriginalURL, &post.MediaType, &post.Width, &post.Height, &post.Tags); err != nil {
			return SearchPage{}, err
		}
		page.Posts = append(page.Posts, post)
	}
	if err := rows.Err(); err != nil {
		return SearchPage{}, err
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
	err = s.pool.QueryRow(ctx, insertCollectionSQL, validated.Name).Scan(&collection.ID, &collection.Name)
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
	var exists int
	if err := s.pool.QueryRow(ctx, collectionExistsSQL, id).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, ErrNotFound
	} else if err != nil {
		return Collection{}, err
	}
	postIDs := make([]int64, len(validated.PostIDs))
	for i, postID := range validated.PostIDs {
		postIDs[i], _ = strconv.ParseInt(postID, 10, 64)
	}
	if _, err := s.pool.Exec(ctx, addCollectionPostSQL, id, postIDs); err != nil {
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
	var exists int
	if err := s.pool.QueryRow(ctx, collectionExistsSQL, cid).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, deleteCollectionPostSQL, cid, pid)
	return err
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
	var exists int
	if err := tx.QueryRow(ctx, collectionExistsSQL, cid).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, ErrNotFound
	} else if err != nil {
		return Collection{}, err
	}
	rows, err := tx.Query(ctx, collectionPostIDsSQL, cid)
	if err != nil {
		return Collection{}, err
	}
	existing := []string{}
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
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
	for idx, raw := range validated.PostIDs {
		pid, _ := strconv.ParseInt(raw, 10, 64)
		if _, err := tx.Exec(ctx, `UPDATE collection_posts SET position=$3 WHERE collection_id=$1::bigint AND post_id=$2::bigint`, cid, pid, int64(idx)); err != nil {
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
