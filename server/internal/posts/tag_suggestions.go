package posts

import (
	"context"
	"strings"
	"unicode/utf8"
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
	// Literal prefix comparison prevents wildcard characters from expanding a
	// suggestion query. Only tags on publicly visible posts contribute counts.
	rows, err := s.pool.Query(ctx, `SELECT t.tag, COUNT(DISTINCT p.id)
FROM posts p CROSS JOIN LATERAL unnest(p.tags) t(tag)
WHERE p.deleted_at IS NULL AND p.moderation_state = 'published'
AND t.tag <> '' AND left(lower(t.tag), length($1)) = lower($1)
GROUP BY t.tag ORDER BY COUNT(DISTINCT p.id) DESC, t.tag LIMIT $2`, prefix, limit)
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
	return result, rows.Err()
}
