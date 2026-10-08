package posts

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// ModeratePosts applies one capability-checked moderation action to every
// target in a single transaction. The actor is the identity-contract owner
// key recorded on each immutable audit row. Deleted posts are
// indistinguishable from missing posts; targets already in the requested
// state return unchanged without recording an audit row.
func (s *PostgresSearcher) ModeratePosts(ctx context.Context, actor string, request ModerationRequest) (ModerationResponse, error) {
	if s == nil || s.pool == nil {
		return ModerationResponse{}, ErrUnavailable
	}
	owner, err := ValidateModerationActor(actor)
	if err != nil {
		return ModerationResponse{}, err
	}
	validated, err := ValidateModerationRequest(request)
	if err != nil {
		return ModerationResponse{}, err
	}
	newState, ok := ModerationStateForAction(validated.Action)
	if !ok {
		return ModerationResponse{}, ErrInvalidModeration
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ModerationResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	response := ModerationResponse{Posts: make([]ModerationResult, 0, len(validated.Posts))}
	for _, target := range validated.Posts {
		var previous string
		if err := tx.QueryRow(ctx, lockPostForModerationSQL, target.ID).Scan(&previous); errors.Is(err, pgx.ErrNoRows) {
			return ModerationResponse{}, ErrNotFound
		} else if err != nil {
			return ModerationResponse{}, err
		}
		result := ModerationResult{ID: target.ID, Action: validated.Action, PreviousState: previous, NewState: previous}
		if previous == newState {
			response.Posts = append(response.Posts, result)
			continue
		}
		if _, err := tx.Exec(ctx, updatePostModerationSQL, newState, target.ID); err != nil {
			return ModerationResponse{}, err
		}
		if _, err := tx.Exec(ctx, insertModerationActionSQL, target.ID, validated.Action, previous, newState, owner, validated.Reason); err != nil {
			return ModerationResponse{}, err
		}
		result.NewState = newState
		result.Changed = true
		response.Posts = append(response.Posts, result)
	}
	if err := tx.Commit(ctx); err != nil {
		return ModerationResponse{}, err
	}
	return response, nil
}

// ListModerationActions returns the newest audit entries for one non-deleted
// post. It is served only to capability-checked readers by the HTTP layer.
func (s *PostgresSearcher) ListModerationActions(ctx context.Context, postID string) ([]ModerationAction, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	id, err := moderatedPostID(postID)
	if err != nil {
		return nil, err
	}
	var exists int
	if err := s.pool.QueryRow(ctx, moderatedPostExistsSQL, id).Scan(&exists); err != nil {
		return nil, moderationExistenceError(err)
	}
	rows, err := s.pool.Query(ctx, moderationActionsByPostSQL, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	actions := make([]ModerationAction, 0, 50)
	for rows.Next() {
		var action ModerationAction
		if err := rows.Scan(&action.ID, &action.PostID, &action.Action, &action.PreviousState, &action.NewState, &action.Actor, &action.Reason, &action.CreatedAt); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return actions, nil
}

// ListModerationQueue returns at most limit non-deleted posts in the
// requested moderation state, newest first. It is served only to
// capability-checked readers by the HTTP layer.
func (s *PostgresSearcher) ListModerationQueue(ctx context.Context, state string, limit int) (ModerationQueuePage, error) {
	if s == nil || s.pool == nil {
		return ModerationQueuePage{}, ErrUnavailable
	}
	validState, err := ValidateModerationQueue(state, limit)
	if err != nil {
		return ModerationQueuePage{}, err
	}
	rows, err := s.pool.Query(ctx, moderationQueueSQL, validState, limit)
	if err != nil {
		return ModerationQueuePage{}, err
	}
	defer rows.Close()
	page := ModerationQueuePage{Posts: make([]ModerationQueueItem, 0, limit)}
	for rows.Next() {
		var item ModerationQueueItem
		if err := rows.Scan(&item.ID, &item.PreviewURL, &item.OriginalURL, &item.MediaType, &item.Width, &item.Height, &item.Tags, &item.ModerationState); err != nil {
			return ModerationQueuePage{}, err
		}
		page.Posts = append(page.Posts, item)
	}
	if err := rows.Err(); err != nil {
		return ModerationQueuePage{}, err
	}
	return page, nil
}

// LookupMediaVisibility maps a /media/* URL path to post visibility so the
// HTTP layer can deny direct access to hidden, rejected, pending, or deleted
// media. Paths no post references are not post media and stay servable;
// otherwise the path is visible only when a non-deleted referencing post is
// published.
func (s *PostgresSearcher) LookupMediaVisibility(ctx context.Context, mediaPath string) (MediaVisibility, error) {
	if s == nil || s.pool == nil {
		return MediaVisibility{}, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, mediaPostStatesSQL, mediaPath)
	if err != nil {
		return MediaVisibility{}, err
	}
	defer rows.Close()
	visibility := MediaVisibility{}
	for rows.Next() {
		var state string
		var live bool
		if err := rows.Scan(&state, &live); err != nil {
			return MediaVisibility{}, err
		}
		visibility.Referenced = true
		if live && IsPubliclyVisibleModerationState(state) {
			visibility.Visible = true
		}
	}
	if err := rows.Err(); err != nil {
		return MediaVisibility{}, err
	}
	return visibility, nil
}

func moderationExistenceError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func moderatedPostID(raw string) (int64, error) {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
		return 0, ErrInvalidModeration
	}
	return parsed, nil
}
