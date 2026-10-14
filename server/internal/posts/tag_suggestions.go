package posts

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type TagSuggestion struct {
	Tag   string `json:"tag"`
	Count int64  `json:"count"`
}

type TagSuggester interface {
	SuggestTags(context.Context, string, int) ([]TagSuggestion, error)
}

func ValidateTagSuggestionQuery(prefix string, limit int) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if !utf8.ValidString(prefix) || utf8.RuneCountInString(prefix) > maxTagLength || strings.ContainsRune(prefix, '\x00') || limit < 1 || limit > 50 {
		return "", ErrInvalidQuery
	}
	return prefix, nil
}

func (s *PostgresBackend) SuggestTags(ctx context.Context, prefix string, limit int) ([]TagSuggestion, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	prefix, err := ValidateTagSuggestionQuery(prefix, limit)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	redirects, err := loadRedirects(ctx, tx)
	if err != nil {
		return nil, err
	}
	effectivePrefix := prefix
	if canonical, ok := redirects[prefix]; ok {
		effectivePrefix = canonical
	}
	// Literal prefix comparison prevents wildcard characters from expanding a
	// suggestion query. Only canonical tags on publicly visible posts count.
	rows, err := tx.Query(ctx, `SELECT t.tag, COUNT(DISTINCT p.id)
FROM posts p CROSS JOIN LATERAL unnest(p.tags) t(tag)
WHERE p.deleted_at IS NULL AND p.moderation_state = 'published'
AND t.tag <> '' AND left(lower(t.tag), length($1)) = lower($1)
AND NOT EXISTS (SELECT 1 FROM tag_redirects r WHERE r.retired_tag = t.tag)
GROUP BY t.tag ORDER BY COUNT(DISTINCT p.id) DESC, t.tag LIMIT $2`, effectivePrefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]TagSuggestion, 0, limit)
	for rows.Next() {
		var item TagSuggestion
		if err := rows.Scan(&item.Tag, &item.Count); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
