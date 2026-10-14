package posts

import (
	"context"
	"fmt"
	"strconv"
)

type MetadataReadRequest struct {
	PostIDs []string `json:"post_ids"`
}

type MetadataSnapshot struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Source  string `json:"source"`
	Artist  string `json:"artist"`
}

type MetadataReadResponse struct {
	Posts []MetadataSnapshot `json:"posts"`
}

type MetadataReader interface {
	ReadMetadata(context.Context, MetadataReadRequest) (MetadataReadResponse, error)
}

func ValidateMetadataRead(request MetadataReadRequest) error {
	if len(request.PostIDs) == 0 || len(request.PostIDs) > maxMetadataPosts {
		return fmt.Errorf("%w: post_ids must contain 1 to 100 targets", ErrInvalidMetadata)
	}
	seen := make(map[string]bool, len(request.PostIDs))
	for _, id := range request.PostIDs {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || seen[id] {
			return fmt.Errorf("%w: post_ids must be distinct canonical positive integers", ErrInvalidMetadata)
		}
		seen[id] = true
	}
	return nil
}

// ReadMetadata obtains the small edit bases in one statement snapshot, avoiding
// a separate Inspector/history request for every selected image. Inaccessible
// targets reject the entire read, just as they reject an atomic edit.
func (s *PostgresBackend) ReadMetadata(ctx context.Context, request MetadataReadRequest) (MetadataReadResponse, error) {
	if err := ValidateMetadataRead(request); err != nil {
		return MetadataReadResponse{}, err
	}
	if s == nil || s.pool == nil {
		return MetadataReadResponse{}, ErrUnavailable
	}
	ids := make([]int64, len(request.PostIDs))
	for i, id := range request.PostIDs {
		ids[i], _ = strconv.ParseInt(id, 10, 64)
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, metadata_version, source, artist
FROM posts WHERE id=ANY($1::bigint[]) AND deleted_at IS NULL AND moderation_state='published'
ORDER BY posts.id`, ids)
	if err != nil {
		return MetadataReadResponse{}, err
	}
	defer rows.Close()
	found := make(map[string]MetadataSnapshot, len(ids))
	for rows.Next() {
		var snapshot MetadataSnapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.Version, &snapshot.Source, &snapshot.Artist); err != nil {
			return MetadataReadResponse{}, err
		}
		found[snapshot.ID] = snapshot
	}
	if err := rows.Err(); err != nil {
		return MetadataReadResponse{}, err
	}
	if len(found) != len(ids) {
		return MetadataReadResponse{}, ErrNotFound
	}
	result := MetadataReadResponse{Posts: make([]MetadataSnapshot, 0, len(ids))}
	for _, id := range request.PostIDs {
		result.Posts = append(result.Posts, found[id])
	}
	return result, nil
}
