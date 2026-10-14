package posts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const maxTagManageAffected = 1000
const tagManageCursorVersion = 1

type TagManageEntry struct {
	Tag              string `json:"tag"`
	PostCount        int64  `json:"post_count"`
	VisiblePostCount int64  `json:"visible_post_count"`
}

type TagManagePage struct {
	Version    int64            `json:"version"`
	Tags       []TagManageEntry `json:"tags"`
	NextCursor *string          `json:"next_cursor"`
}

type TagManageRequest struct {
	Mode            string `json:"mode"`
	SourceTag       string `json:"source_tag"`
	TargetTag       string `json:"target_tag"`
	ExpectedVersion *int64 `json:"expected_version,omitempty"`
}

type TagManagePreview struct {
	Version           int64  `json:"version"`
	Mode              string `json:"mode"`
	SourceTag         string `json:"source_tag"`
	TargetTag         string `json:"target_tag"`
	AffectedPostCount int64  `json:"affected_post_count"`
	VisiblePostCount  int64  `json:"visible_post_count"`
	TargetPostCount   int64  `json:"target_post_count"`
}

type TagManageResult struct {
	ID                string `json:"id"`
	Version           int64  `json:"version"`
	Mode              string `json:"mode"`
	SourceTag         string `json:"source_tag"`
	TargetTag         string `json:"target_tag"`
	AffectedPostCount int64  `json:"affected_post_count"`
	VisiblePostCount  int64  `json:"visible_post_count"`
	Changed           bool   `json:"changed"`
}

type TagTaxonomyManager interface {
	ListManageTags(context.Context, string, int, *TagManageCursor) (TagManagePage, error)
	PreviewTagOperation(context.Context, TagManageRequest) (TagManagePreview, error)
	ApplyTagOperation(context.Context, string, TagManageRequest) (TagManageResult, error)
}

type TagManageCursor struct {
	Prefix  string
	Version int64
	LastTag string
}

type tagManageCursorPayload struct {
	V       int    `json:"v"`
	Prefix  string `json:"p"`
	Version int64  `json:"tv"`
	Last    string `json:"l"`
}

func EncodeTagManageCursor(prefix string, version int64, last string) (string, error) {
	if !validTagManageCursor(TagManageCursor{Prefix: prefix, Version: version, LastTag: last}) {
		return "", ErrInvalidCursor
	}
	b, err := json.Marshal(tagManageCursorPayload{V: tagManageCursorVersion, Prefix: prefix, Version: version, Last: last})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func DecodeTagManageCursor(token, prefix string) (TagManageCursor, error) {
	if token == "" || len(token) > maxCursorLength {
		return TagManageCursor{}, ErrInvalidCursor
	}
	dec, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(dec) == 0 {
		return TagManageCursor{}, ErrInvalidCursor
	}
	var p tagManageCursorPayload
	d := json.NewDecoder(bytes.NewReader(dec))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return TagManageCursor{}, ErrInvalidCursor
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return TagManageCursor{}, ErrInvalidCursor
	}
	if p.V != tagManageCursorVersion || p.Prefix != prefix || !validTagManageCursor(TagManageCursor{Prefix: p.Prefix, Version: p.Version, LastTag: p.Last}) {
		return TagManageCursor{}, ErrInvalidCursor
	}
	return TagManageCursor{Prefix: p.Prefix, Version: p.Version, LastTag: p.Last}, nil
}

func validTagManageCursor(cursor TagManageCursor) bool {
	if cursor.Version < 0 {
		return false
	}
	if _, err := ValidateTagManagePrefix(cursor.Prefix, 60); err != nil {
		return false
	}
	last, err := validateSingleTag(cursor.LastTag)
	return err == nil && last == cursor.LastTag
}

func validateSingleTag(raw string) (string, error) {
	tag := strings.TrimSpace(raw)
	if tag == "" || !utf8.ValidString(tag) || strings.ContainsRune(tag, '\x00') || utf8.RuneCountInString(tag) > maxTagLength {
		return "", fmt.Errorf("%w: tags must be valid UTF-8, contain no NUL, and be 1 to 128 characters", ErrInvalidTagTaxonomy)
	}
	return tag, nil
}

func validateTagValueForTags(raw string) (string, error) {
	tag, err := validateSingleTag(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidTags, strings.TrimPrefix(err.Error(), ErrInvalidTagTaxonomy.Error()+": "))
	}
	return tag, nil
}

func ValidateTagManageRequest(req TagManageRequest, requireVersion bool) (TagManageRequest, error) {
	if req.Mode != "rename" && req.Mode != "merge" {
		return TagManageRequest{}, fmt.Errorf("%w: mode must be rename or merge", ErrInvalidTagTaxonomy)
	}
	src, err := validateSingleTag(req.SourceTag)
	if err != nil {
		return TagManageRequest{}, err
	}
	tgt, err := validateSingleTag(req.TargetTag)
	if err != nil {
		return TagManageRequest{}, err
	}
	if src == tgt {
		return TagManageRequest{}, fmt.Errorf("%w: source_tag and target_tag must differ", ErrInvalidTagTaxonomy)
	}
	if requireVersion {
		if req.ExpectedVersion == nil {
			return TagManageRequest{}, fmt.Errorf("%w: expected_version is required", ErrInvalidTagTaxonomy)
		}
		if *req.ExpectedVersion < 0 {
			return TagManageRequest{}, fmt.Errorf("%w: expected_version must be non-negative", ErrInvalidTagTaxonomy)
		}
	} else if req.ExpectedVersion != nil && *req.ExpectedVersion < 0 {
		return TagManageRequest{}, fmt.Errorf("%w: expected_version must be non-negative", ErrInvalidTagTaxonomy)
	}
	req.SourceTag, req.TargetTag = src, tgt
	return req, nil
}

func ValidateTagManagePrefix(prefix string, limit int) (string, error) {
	if limit < 1 || limit > 60 {
		return "", fmt.Errorf("%w: limit must be 1 to 60", ErrInvalidTagTaxonomy)
	}
	if !utf8.ValidString(prefix) || strings.ContainsRune(prefix, '\x00') || utf8.RuneCountInString(prefix) > maxTagLength {
		return "", fmt.Errorf("%w: prefix must be valid UTF-8 with no NUL and at most 128 characters", ErrInvalidTagTaxonomy)
	}
	return prefix, nil
}

// taxonomy helpers

func lockTaxonomyTx(ctx context.Context, tx pgx.Tx) (int64, error) {
	var v int64
	err := tx.QueryRow(ctx, `SELECT version FROM tag_taxonomy_state WHERE id=1 FOR UPDATE`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnavailable
	}
	return v, err
}

func readTaxonomyTx(ctx context.Context, tx pgx.Tx) (int64, error) {
	var v int64
	err := tx.QueryRow(ctx, `SELECT version FROM tag_taxonomy_state WHERE id=1`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnavailable
	}
	return v, err
}

func bumpTaxonomyTx(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE tag_taxonomy_state SET version = version + 1 WHERE id=1`)
	return err
}

func setTaxonomyVersionTx(ctx context.Context, tx pgx.Tx, v int64) error {
	_, err := tx.Exec(ctx, `UPDATE tag_taxonomy_state SET version=$1 WHERE id=1`, v)
	return err
}

func loadRedirects(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (map[string]string, error) {
	rows, err := q.Query(ctx, `SELECT retired_tag, canonical_tag FROM tag_redirects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var r, c string
		if err := rows.Scan(&r, &c); err != nil {
			return nil, err
		}
		m[r] = c
	}
	return m, rows.Err()
}

func resolveCanonical(tag string, redirects map[string]string) string {
	seen := map[string]bool{tag: true}
	cur := tag
	for i := 0; i < 128; i++ {
		next, ok := redirects[cur]
		if !ok {
			return cur
		}
		if seen[next] {
			return next
		}
		seen[next] = true
		cur = next
	}
	return cur
}

func canonicalizeTags(tags []string, redirects map[string]string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		c := resolveCanonical(t, redirects)
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

func orderedEqualTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func checkRetired(source, target string, redirects map[string]string) error {
	if canonical, ok := redirects[source]; ok {
		return fmt.Errorf("%w: source_tag %q is retired to %q", ErrTagRetired, source, canonical)
	}
	if canonical, ok := redirects[target]; ok {
		return fmt.Errorf("%w: target_tag %q is retired to %q", ErrTagRetired, target, canonical)
	}
	return nil
}

func (s *PostgresBackend) ListManageTags(ctx context.Context, prefix string, limit int, cursor *TagManageCursor) (TagManagePage, error) {
	if s == nil || s.pool == nil {
		return TagManagePage{}, ErrUnavailable
	}
	prefix, err := ValidateTagManagePrefix(prefix, limit)
	if err != nil {
		return TagManagePage{}, err
	}
	if cursor != nil && (cursor.Prefix != prefix || !validTagManageCursor(*cursor)) {
		return TagManagePage{}, ErrInvalidCursor
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return TagManagePage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	version, err := readTaxonomyTx(ctx, tx)
	if err != nil {
		return TagManagePage{}, err
	}
	if cursor != nil && cursor.Version != version {
		return TagManagePage{}, ErrTagTaxonomyChanged
	}
	var last any
	if cursor != nil {
		last = cursor.LastTag
	}
	rows, err := tx.Query(ctx, `
SELECT t.tag, COUNT(DISTINCT p.id), COUNT(DISTINCT p.id) FILTER (WHERE p.deleted_at IS NULL AND p.moderation_state='published')
FROM posts p CROSS JOIN LATERAL unnest(p.tags) AS t(tag)
WHERE t.tag <> '' AND ($1='' OR left(lower(t.tag), length($1)) = lower($1))
  AND ($2::text IS NULL OR (t.tag COLLATE "C") > ($2::text COLLATE "C"))
GROUP BY t.tag ORDER BY t.tag COLLATE "C" ASC LIMIT $3`, prefix, last, limit+1)
	if err != nil {
		return TagManagePage{}, err
	}
	defer rows.Close()
	tags := make([]TagManageEntry, 0, limit+1)
	for rows.Next() {
		var e TagManageEntry
		if err := rows.Scan(&e.Tag, &e.PostCount, &e.VisiblePostCount); err != nil {
			return TagManagePage{}, err
		}
		tags = append(tags, e)
	}
	if err := rows.Err(); err != nil {
		return TagManagePage{}, err
	}
	page := TagManagePage{Version: version, Tags: tags}
	if len(tags) > limit {
		page.Tags = tags[:limit]
		tok, err := EncodeTagManageCursor(prefix, version, page.Tags[len(page.Tags)-1].Tag)
		if err != nil {
			return TagManagePage{}, err
		}
		page.NextCursor = &tok
	}
	if page.Tags == nil {
		page.Tags = []TagManageEntry{}
	}
	if err := tx.Commit(ctx); err != nil {
		return TagManagePage{}, err
	}
	return page, nil
}

func (s *PostgresBackend) previewInTx(ctx context.Context, tx pgx.Tx, req TagManageRequest) (TagManagePreview, int64, error) {
	version, err := readTaxonomyTx(ctx, tx)
	if err != nil {
		return TagManagePreview{}, 0, err
	}
	redirects, err := loadRedirects(ctx, tx)
	if err != nil {
		return TagManagePreview{}, 0, err
	}
	if err := checkRetired(req.SourceTag, req.TargetTag, redirects); err != nil {
		return TagManagePreview{}, 0, err
	}
	var affected, visible, targetCount int64
	if err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT id), COUNT(DISTINCT id) FILTER (WHERE deleted_at IS NULL AND moderation_state='published') FROM posts WHERE $1 = ANY(tags)`, req.SourceTag).Scan(&affected, &visible); err != nil {
		return TagManagePreview{}, 0, err
	}
	if affected == 0 {
		return TagManagePreview{}, 0, fmt.Errorf("%w: source tag not found", ErrNotFound)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT id) FROM posts WHERE $1 = ANY(tags)`, req.TargetTag).Scan(&targetCount); err != nil {
		return TagManagePreview{}, 0, err
	}
	if req.Mode == "rename" && targetCount > 0 {
		return TagManagePreview{}, 0, fmt.Errorf("%w: target tag %q already exists", ErrTagTargetExists, req.TargetTag)
	}
	if req.Mode == "merge" && targetCount == 0 {
		return TagManagePreview{}, 0, fmt.Errorf("%w: target tag not found", ErrNotFound)
	}
	if affected > maxTagManageAffected {
		return TagManagePreview{}, 0, fmt.Errorf("%w: %d posts exceed limit of %d", ErrTagOperationTooLarge, affected, maxTagManageAffected)
	}
	return TagManagePreview{Version: version, Mode: req.Mode, SourceTag: req.SourceTag, TargetTag: req.TargetTag, AffectedPostCount: affected, VisiblePostCount: visible, TargetPostCount: targetCount}, version, nil
}

func (s *PostgresBackend) PreviewTagOperation(ctx context.Context, req TagManageRequest) (TagManagePreview, error) {
	if s == nil || s.pool == nil {
		return TagManagePreview{}, ErrUnavailable
	}
	validated, err := ValidateTagManageRequest(req, false)
	if err != nil {
		return TagManagePreview{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return TagManagePreview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	preview, _, err := s.previewInTx(ctx, tx, validated)
	if err != nil {
		return TagManagePreview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TagManagePreview{}, err
	}
	return preview, nil
}

type lockedSourcePost struct {
	id         int64
	tags       []string
	source     string
	artist     string
	tagVersion int
	deleted    bool
	visible    bool
}

func (s *PostgresBackend) ApplyTagOperation(ctx context.Context, actor string, req TagManageRequest) (TagManageResult, error) {
	if s == nil || s.pool == nil {
		return TagManageResult{}, ErrUnavailable
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return TagManageResult{}, fmt.Errorf("%w: actor is required", ErrInvalidTagTaxonomy)
	}
	validated, err := ValidateTagManageRequest(req, true)
	if err != nil {
		return TagManageResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TagManageResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	version, err := lockTaxonomyTx(ctx, tx)
	if err != nil {
		return TagManageResult{}, err
	}
	// Check expected version BEFORE memberships.
	if *validated.ExpectedVersion != version {
		return TagManageResult{}, ErrTagTaxonomyChanged
	}
	redirects, err := loadRedirects(ctx, tx)
	if err != nil {
		return TagManageResult{}, err
	}
	if err := checkRetired(validated.SourceTag, validated.TargetTag, redirects); err != nil {
		return TagManageResult{}, err
	}
	rows, err := tx.Query(ctx, `SELECT id, tags, source, artist, tag_version, (deleted_at IS NOT NULL), (deleted_at IS NULL AND moderation_state='published') FROM posts WHERE $1 = ANY(tags) ORDER BY posts.id FOR UPDATE LIMIT 1001`, validated.SourceTag)
	if err != nil {
		return TagManageResult{}, err
	}
	locked := make([]lockedSourcePost, 0, 16)
	for rows.Next() {
		var p lockedSourcePost
		if err := rows.Scan(&p.id, &p.tags, &p.source, &p.artist, &p.tagVersion, &p.deleted, &p.visible); err != nil {
			rows.Close()
			return TagManageResult{}, err
		}
		locked = append(locked, p)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return TagManageResult{}, rowsErr
	}
	if len(locked) == 0 {
		return TagManageResult{}, fmt.Errorf("%w: source tag not found", ErrNotFound)
	}
	if len(locked) > maxTagManageAffected {
		return TagManageResult{}, fmt.Errorf("%w: %d posts exceed limit of %d", ErrTagOperationTooLarge, len(locked), maxTagManageAffected)
	}
	var targetCount int64
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM posts WHERE $1 = ANY(tags)`, validated.TargetTag).Scan(&targetCount); err != nil {
		return TagManageResult{}, err
	}
	if validated.Mode == "rename" && targetCount > 0 {
		return TagManageResult{}, fmt.Errorf("%w: target tag %q already exists", ErrTagTargetExists, validated.TargetTag)
	}
	if validated.Mode == "merge" && targetCount == 0 {
		return TagManageResult{}, fmt.Errorf("%w: target tag not found", ErrNotFound)
	}
	var visible int64
	for _, p := range locked {
		if p.visible {
			visible++
		}
	}
	newVersion := version + 1
	var opID int64
	if err := tx.QueryRow(ctx, `INSERT INTO tag_operations (mode, source_tag, target_tag, version, affected_post_count, visible_post_count, actor) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, validated.Mode, validated.SourceTag, validated.TargetTag, newVersion, int64(len(locked)), visible, actor).Scan(&opID); err != nil {
		return TagManageResult{}, err
	}
	for _, p := range locked {
		next := replaceAndDedup(p.tags, validated.SourceTag, validated.TargetTag)
		added, removed := tagDelta(p.tags, next)
		if added == nil {
			added = []string{}
		}
		if removed == nil {
			removed = []string{}
		}
		if next == nil {
			next = []string{}
		}
		nextVer := p.tagVersion + 1
		search := canonicalSearchText(next, p.source, p.artist)
		if _, err := tx.Exec(ctx, `UPDATE posts SET tags=$1, search_text=$2, tag_version=$3 WHERE id=$4`, next, search, nextVer, p.id); err != nil {
			return TagManageResult{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO post_tag_revisions (post_id, version, kind, added_tags, removed_tags, target_tags, tag_operation_id) VALUES ($1,$2,$3,$4,$5,$6,$7)`, p.id, nextVer, validated.Mode, added, removed, next, opID); err != nil {
			return TagManageResult{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE tag_redirects SET canonical_tag=$1 WHERE canonical_tag=$2`, validated.TargetTag, validated.SourceTag); err != nil {
		return TagManageResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tag_redirects (retired_tag, canonical_tag) VALUES ($1,$2)`, validated.SourceTag, validated.TargetTag); err != nil {
		return TagManageResult{}, err
	}
	if err := setTaxonomyVersionTx(ctx, tx, newVersion); err != nil {
		return TagManageResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TagManageResult{}, err
	}
	return TagManageResult{ID: strconv.FormatInt(opID, 10), Version: newVersion, Mode: validated.Mode, SourceTag: validated.SourceTag, TargetTag: validated.TargetTag, AffectedPostCount: int64(len(locked)), VisiblePostCount: visible, Changed: true}, nil
}

func replaceAndDedup(tags []string, source, target string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		v := t
		if v == source {
			v = target
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
