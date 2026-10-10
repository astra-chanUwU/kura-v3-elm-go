package posts

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

// PostgresSearcher executes the browser-facing post summary query against
// PostgreSQL. It keeps the pool behind the Searcher interface used by HTTP.
//
// The searcher owns one shared PostgreSQL pool: the durable job store and
// the variant recorder wrap that same pool, so uploads, job status reads,
// and derivative processing never open a second pool per process.
type PostgresSearcher struct {
	pool     *pgxpool.Pool
	store    media.Store
	jobStore *jobs.Store
	variants *media.PostgresVariantStore
}

// NewPostgresSearcher creates a searcher for databaseURL. The pool connects on
// demand, so a server can still start and serve /health while PostgreSQL is
// unavailable. When S3_ENDPOINT and S3_BUCKET are set, the searcher uses an
// S3-compatible store; otherwise it falls back to a local filesystem store.
func NewPostgresSearcher(ctx context.Context, databaseURL string) (*PostgresSearcher, error) {
	return NewPostgresSearcherWithStore(ctx, databaseURL, media.NewStoreFromEnv(uploadMediaRoot()))
}

// NewPostgresSearcherWithStore creates a searcher with an explicit media
// provider. The default constructor keeps local filesystem uploads working.
func NewPostgresSearcherWithStore(ctx context.Context, databaseURL string, store media.Store) (*PostgresSearcher, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &PostgresSearcher{
		pool:     pool,
		store:    store,
		jobStore: jobs.NewStore(pool),
		variants: media.NewPostgresVariantStore(pool),
	}, nil
}

// JobsStore exposes the durable job store sharing the searcher's pool. The
// server wires it into the in-process derivative worker and nowhere else,
// keeping one pool per process.
func (s *PostgresSearcher) JobsStore() *jobs.Store {
	if s == nil {
		return nil
	}
	return s.jobStore
}

// NewDerivativeProcessor builds the derivative handler over the searcher's
// media provider and variant recorder. Uploads fall back to the
// environment media store when no explicit provider was configured, and the
// processor mirrors that fallback.
func (s *PostgresSearcher) NewDerivativeProcessor() *media.DerivativeProcessor {
	if s == nil {
		return nil
	}
	store := s.store
	if store == nil {
		store = media.NewStoreFromEnv(uploadMediaRoot())
	}
	return &media.DerivativeProcessor{
		Originals:   store,
		Derivatives: store,
		Variants:    s.variants,
	}
}

// RecoverExpiredJobs requeues running derivative jobs whose lease lapsed
// without renewal, e.g. after a crash. The server runs it once at startup
// before the worker claims, so interrupted thumbnails resume.
func (s *PostgresSearcher) RecoverExpiredJobs(ctx context.Context) (int64, error) {
	if s == nil || s.jobStore == nil {
		return 0, ErrUnavailable
	}
	return s.jobStore.RecoverExpiredLeases(ctx)
}

// GetJob serves one durable job row for GET /api/jobs/{id} polling,
// implementing jobs.Reader so the HTTP layer needs no second store.
func (s *PostgresSearcher) GetJob(ctx context.Context, id string) (jobs.Job, error) {
	if s == nil || s.jobStore == nil {
		return jobs.Job{}, jobs.ErrUnavailable
	}
	return s.jobStore.GetJob(ctx, id)
}

// previewQuerier is satisfied by both the pool and an open transaction so
// thumbnail overlays can run inside a caller's snapshot instead of
// borrowing a second pooled connection.
type previewQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// overlayReadyPreviews replaces the preview URL with the ready thumb-320
// variant URL for every post that has one. Posts still processing keep
// serving their original as the preview; original URLs never change.
func (s *PostgresSearcher) overlayReadyPreviews(ctx context.Context, summaries []PostSummary) error {
	if s == nil || s.pool == nil || len(summaries) == 0 {
		return nil
	}
	return overlayReadyPreviewsWith(ctx, s.pool, summaries)
}

// overlayReadyPreviewsTx is the transaction-scoped overlay for readers
// that already hold a connection, such as the versioned collection page
// read. The variant lookup observes the same snapshot as the page, and
// the read never waits on a second pooled connection.
func overlayReadyPreviewsTx(ctx context.Context, tx pgx.Tx, summaries []PostSummary) error {
	if len(summaries) == 0 {
		return nil
	}
	return overlayReadyPreviewsWith(ctx, tx, summaries)
}

func overlayReadyPreviewsWith(ctx context.Context, q previewQuerier, summaries []PostSummary) error {
	ids := make([]int64, 0, len(summaries))
	for _, post := range summaries {
		if id, err := strconv.ParseInt(post.ID, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, readyVariantURLsForPostsSQL, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	ready := make(map[string]string, len(ids))
	for rows.Next() {
		var id, url string
		if err := rows.Scan(&id, &url); err != nil {
			return err
		}
		ready[id] = url
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range summaries {
		if url, ok := ready[summaries[index].ID]; ok && url != "" {
			summaries[index].PreviewURL = url
		}
	}
	return nil
}

// enrichPostDetail overlays the ready thumbnail preview and attaches the
// discoverable derivative job metadata. Missing rows leave the detail
// untouched: the original stays the preview and no job fields are set.
func (s *PostgresSearcher) enrichPostDetail(ctx context.Context, detail *PostDetail) error {
	if s == nil || s.pool == nil || detail == nil {
		return nil
	}
	var preview string
	if err := s.pool.QueryRow(ctx, readyVariantURLSQL, detail.ID).Scan(&preview); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			preview = ""
		} else {
			return err
		}
	}
	if preview != "" {
		detail.PreviewURL = preview
	}
	var jobID, status string
	if err := s.pool.QueryRow(ctx, derivativeJobForPostSQL, derivativeJobKey(detail.ID)).Scan(&jobID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	detail.DerivativeJobID = jobID
	detail.DerivativeStatus = status
	return nil
}

// derivativeJobKey mirrors media.EnqueueDerivative's idempotency key for the
// default variant so reads find the job the upload transaction enqueued.
func derivativeJobKey(postID string) string {
	return "derivative:" + postID + ":" + media.DefaultDerivativeVariant
}

// Close releases PostgreSQL connections owned by the searcher.
func (s *PostgresSearcher) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// SearchPosts returns summaries in the stable API shape and uses keyset
// pagination. Newest ordering uses id DESC; score ordering uses score DESC
// then id DESC with a (score,id) cursor that cannot duplicate or skip rows.
func (s *PostgresSearcher) SearchPosts(ctx context.Context, query string, cursor *Cursor, limit int) (SearchPage, error) {
	if s == nil || s.pool == nil {
		return SearchPage{}, ErrUnavailable
	}
	if limit < 1 || limit > 60 {
		return SearchPage{}, ErrInvalidQuery
	}
	compiled, err := ParseQuery(query)
	if err != nil {
		return SearchPage{}, err
	}
	if cursor != nil && cursor.Order != compiled.Order {
		return SearchPage{}, ErrInvalidCursor
	}
	var favorite any
	if compiled.Favorite != nil {
		favorite = *compiled.Favorite
	}
	var scoreOperator, scoreValue any
	if compiled.Score != nil {
		scoreOperator, scoreValue = compiled.Score.Operator, compiled.Score.Value
	}
	var widthOperator, widthValue any
	if compiled.Width != nil {
		widthOperator, widthValue = compiled.Width.Operator, compiled.Width.Value
	}
	var heightOperator, heightValue any
	if compiled.Height != nil {
		heightOperator, heightValue = compiled.Height.Operator, compiled.Height.Value
	}
	var fileSizeOperator, fileSizeValue any
	if compiled.FileSize != nil {
		fileSizeOperator, fileSizeValue = compiled.FileSize.Operator, compiled.FileSize.Value
	}
	var idOperator, idValue any
	if compiled.ID != nil {
		idOperator, idValue = compiled.ID.Operator, compiled.ID.Value
	}
	var mediaType, source, artist any
	if compiled.MediaType != nil {
		mediaType = compiled.MediaType.Value
	}
	if compiled.Source != nil {
		source = compiled.Source.Value
	}
	if compiled.Artist != nil {
		artist = compiled.Artist.Value
	}
	var includedTags any
	var excludedTags any
	var included, excluded []string
	for _, tag := range compiled.Tags {
		if tag.Excluded {
			excluded = append(excluded, tag.Value)
		} else {
			included = append(included, tag.Value)
		}
	}
	if len(included) > 0 {
		includedTags = included
	}
	if len(excluded) > 0 {
		excludedTags = excluded
	}

	var rows pgx.Rows
	if compiled.Order == SortScore {
		var cursorScore any
		var cursorID any
		if cursor != nil {
			cursorScore = cursor.Score
			cursorID = cursor.ID
		}
		rows, err = s.pool.Query(ctx, SearchPostsScoreSQL, compiled.Text, favorite, scoreOperator, scoreValue, widthOperator, widthValue, heightOperator, heightValue, fileSizeOperator, fileSizeValue, idOperator, idValue, mediaType, source, artist, includedTags, excludedTags, cursorScore, cursorID, limit+1)
		if err != nil {
			return SearchPage{}, err
		}
		defer rows.Close()
		posts := make([]PostSummary, 0, limit+1)
		scores := make([]int, 0, limit+1)
		for rows.Next() {
			var post PostSummary
			var score int
			if err := rows.Scan(&post.ID, &post.PreviewURL, &post.OriginalURL, &post.MediaType, &post.Width, &post.Height, &post.Tags, &score); err != nil {
				return SearchPage{}, err
			}
			posts = append(posts, post)
			scores = append(scores, score)
		}
		if err := rows.Err(); err != nil {
			return SearchPage{}, err
		}
		if err := s.overlayReadyPreviews(ctx, posts); err != nil {
			return SearchPage{}, err
		}
		if len(posts) <= limit {
			return SearchPage{Posts: posts}, nil
		}
		posts = posts[:limit]
		scores = scores[:limit]
		lastID := posts[len(posts)-1].ID
		var id int64
		if _, err := fmt.Sscan(lastID, &id); err != nil || id <= 0 {
			return SearchPage{}, ErrInvalidQuery
		}
		lastScore := scores[len(scores)-1]
		token, err := EncodeSortCursor(query, SortScore, lastScore, id)
		if err != nil {
			return SearchPage{}, err
		}
		return SearchPage{Posts: posts, NextCursor: &token}, nil
	}

	var cursorID any
	if cursor != nil {
		cursorID = cursor.ID
	}
	rows, err = s.pool.Query(ctx, SearchPostsSQL, compiled.Text, favorite, scoreOperator, scoreValue, widthOperator, widthValue, heightOperator, heightValue, fileSizeOperator, fileSizeValue, idOperator, idValue, mediaType, source, artist, includedTags, excludedTags, cursorID, limit+1)
	if err != nil {
		return SearchPage{}, err
	}
	defer rows.Close()
	posts := make([]PostSummary, 0, limit+1)
	scores := make([]int, 0, limit+1)
	for rows.Next() {
		var post PostSummary
		var score int
		if err := rows.Scan(&post.ID, &post.PreviewURL, &post.OriginalURL, &post.MediaType, &post.Width, &post.Height, &post.Tags, &score); err != nil {
			return SearchPage{}, err
		}
		posts = append(posts, post)
		scores = append(scores, score)
	}
	if err := rows.Err(); err != nil {
		return SearchPage{}, err
	}
	if err := s.overlayReadyPreviews(ctx, posts); err != nil {
		return SearchPage{}, err
	}
	if len(posts) <= limit {
		return SearchPage{Posts: posts}, nil
	}
	posts = posts[:limit]
	lastID := posts[len(posts)-1].ID
	var id int64
	if _, err := fmt.Sscan(lastID, &id); err != nil || id <= 0 {
		return SearchPage{}, ErrInvalidQuery
	}
	token, err := EncodeSortCursor(query, SortNewest, 0, id)
	if err != nil {
		return SearchPage{}, err
	}
	return SearchPage{Posts: posts, NextCursor: &token}, nil
}

// GetPostDetail returns visible post metadata for the Inspector. Deleted rows
// are intentionally indistinguishable from missing rows.
func (s *PostgresSearcher) GetPostDetail(ctx context.Context, id string) (PostDetail, error) {
	if s == nil || s.pool == nil {
		return PostDetail{}, ErrUnavailable
	}
	var detail PostDetail
	err := s.pool.QueryRow(ctx, GetPostDetailSQL, id).Scan(
		&detail.ID,
		&detail.PreviewURL,
		&detail.OriginalURL,
		&detail.MediaType,
		&detail.Width,
		&detail.Height,
		&detail.Source,
		&detail.Artist,
		&detail.Hash,
		&detail.FileSize,
		&detail.CreatedAt,
		&detail.Tags,
		&detail.TagVersion,
		&detail.Favorite,
		&detail.Score,
		&detail.ReactionVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostDetail{}, ErrNotFound
	}
	if err != nil {
		return PostDetail{}, err
	}
	detail.History = make([]Revision, 0, 50)
	rows, err := s.pool.Query(ctx, GetPostRevisionsSQL, id)
	if err != nil {
		return PostDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var revision Revision
		var hasTarget bool
		var targetTags []string
		if err := rows.Scan(&revision.Version, &revision.Kind, &revision.AddedTags, &revision.RemovedTags, &hasTarget, &targetTags, &revision.CreatedAt); err != nil {
			return PostDetail{}, err
		}
		if hasTarget {
			tags := targetTags
			if tags == nil {
				empty := []string{}
				tags = empty
			}
			revision.TargetTags = &tags
			revision.Revertible = true
		} else {
			revision.TargetTags = nil
			revision.Revertible = false
		}
		detail.History = append(detail.History, revision)
	}
	if err := rows.Err(); err != nil {
		return PostDetail{}, err
	}
	detail.ReactionHistory = make([]ReactionRevision, 0, 50)
	reactionRows, err := s.pool.Query(ctx, GetPostReactionRevisionsSQL, id)
	if err != nil {
		return PostDetail{}, err
	}
	defer reactionRows.Close()
	for reactionRows.Next() {
		var revision ReactionRevision
		if err := reactionRows.Scan(&revision.Version, &revision.Favorite, &revision.Score, &revision.CreatedAt); err != nil {
			return PostDetail{}, err
		}
		detail.ReactionHistory = append(detail.ReactionHistory, revision)
	}
	if err := reactionRows.Err(); err != nil {
		return PostDetail{}, err
	}
	if err := s.enrichPostDetail(ctx, &detail); err != nil {
		return PostDetail{}, err
	}
	return detail, nil
}

// EditTags atomically applies a bounded optimistic tag edit to every target.
// Row locks keep the version check and revision insert in one transaction.
func (s *PostgresSearcher) EditTags(ctx context.Context, request TagEditRequest) (TagEditResponse, error) {
	if s == nil || s.pool == nil {
		return TagEditResponse{}, ErrUnavailable
	}
	validated, err := ValidateTagEdit(request)
	if err != nil {
		return TagEditResponse{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TagEditResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	response := TagEditResponse{Posts: make([]TagEditResult, 0, len(validated.Posts))}
	for _, target := range validated.Posts {
		result, err := s.applyTagEdit(ctx, tx, target, validated.Add, validated.Remove)
		if err != nil {
			return TagEditResponse{}, err
		}
		response.Posts = append(response.Posts, result)
	}
	if err := tx.Commit(ctx); err != nil {
		return TagEditResponse{}, err
	}
	return response, nil
}

// RevertTags restores a previously recorded tag state for every target in one
// transaction. Each row is locked before its expected current version and
// target revision are checked, so a stale batch cannot partially apply.
func (s *PostgresSearcher) RevertTags(ctx context.Context, request TagRevertRequest) (TagRevertResponse, error) {
	if s == nil || s.pool == nil {
		return TagRevertResponse{}, ErrUnavailable
	}
	validated, err := ValidateTagRevert(request)
	if err != nil {
		return TagRevertResponse{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TagRevertResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	response := TagRevertResponse{Posts: make([]TagEditResult, 0, len(validated.Posts))}
	for _, target := range validated.Posts {
		result, err := s.applyTagRevert(ctx, tx, target, validated.TargetVersion)
		if err != nil {
			return TagRevertResponse{}, err
		}
		response.Posts = append(response.Posts, result)
	}
	if err := tx.Commit(ctx); err != nil {
		return TagRevertResponse{}, err
	}
	return response, nil
}

// EditReactions atomically applies a bounded optimistic favorite/score edit
// to every target. Row locks keep the version check and immutable revision
// insert in one transaction.
func (s *PostgresSearcher) EditReactions(ctx context.Context, request ReactionRequest) (ReactionResponse, error) {
	if s == nil || s.pool == nil {
		return ReactionResponse{}, ErrUnavailable
	}
	validated, err := ValidateReactionRequest(request)
	if err != nil {
		return ReactionResponse{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ReactionResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	response := ReactionResponse{Posts: make([]ReactionResult, 0, len(validated.Posts))}
	for _, target := range validated.Posts {
		result, err := s.applyReaction(ctx, tx, target, validated.Favorite, validated.Score)
		if err != nil {
			return ReactionResponse{}, err
		}
		response.Posts = append(response.Posts, result)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReactionResponse{}, err
	}
	return response, nil
}
