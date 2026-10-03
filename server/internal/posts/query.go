package posts

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Query is the bounded, server-owned representation of a KuraQL search.
// Text is passed to PostgreSQL's websearch_to_tsquery; field filters are
// compiled into fixed SQL predicate slots by the PostgreSQL adapter.
type Query struct {
	Text     string
	Favorite *bool
	Score    *NumericFilter
	Width    *NumericFilter
	Height   *NumericFilter
}

// NumericFilter is a validated comparison against an integer post field.
type NumericFilter struct {
	Operator string
	Value    int
}

// ParseQuery parses the bounded KuraQL subset accepted by post search.
// Ordinary terms (including unary '-' terms) remain in Text and are delegated
// to PostgreSQL websearch syntax. Field expressions are exact, single-token
// expressions and are rejected when unknown or malformed.
func ParseQuery(raw string) (Query, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Query{}, nil
	}

	tokens := queryTokens(raw)
	query := Query{}
	textTerms := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		field, value, hasField := splitFieldExpression(token)
		if !hasField {
			textTerms = append(textTerms, token)
			continue
		}
		if strings.HasPrefix(field, "-") {
			return Query{}, invalidQuery("unary field expressions are not supported")
		}
		if err := addFieldFilter(&query, field, value); err != nil {
			return Query{}, err
		}
	}
	query.Text = strings.Join(textTerms, " ")
	return query, nil
}

func addFieldFilter(query *Query, field, value string) error {
	field = strings.ToLower(field)
	switch field {
	case "favorite":
		if query.Favorite != nil || (value != "true" && value != "false") {
			return invalidQuery("favorite expects true or false")
		}
		favorite := value == "true"
		query.Favorite = &favorite
		return nil
	case "score", "width", "height":
		filter, err := parseNumericFilter(value)
		if err != nil {
			return err
		}
		switch field {
		case "score":
			if query.Score != nil {
				return invalidQuery("duplicate score filter")
			}
			query.Score = &filter
		case "width":
			if query.Width != nil {
				return invalidQuery("duplicate width filter")
			}
			query.Width = &filter
		case "height":
			if query.Height != nil {
				return invalidQuery("duplicate height filter")
			}
			query.Height = &filter
		}
		return nil
	default:
		return invalidQuery("unknown field %q", field)
	}
}

func parseNumericFilter(raw string) (NumericFilter, error) {
	operator := ""
	for _, candidate := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(raw, candidate) {
			operator = candidate
			raw = strings.TrimPrefix(raw, candidate)
			break
		}
	}
	if operator == "" || raw == "" {
		return NumericFilter{}, invalidQuery("numeric filter expects >, >=, <, <=, or = followed by an integer")
	}
	for _, r := range raw {
		if !unicode.IsDigit(r) {
			return NumericFilter{}, invalidQuery("numeric filter expects an integer")
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return NumericFilter{}, invalidQuery("numeric filter integer is out of range")
	}
	return NumericFilter{Operator: operator, Value: value}, nil
}

// queryTokens splits on whitespace outside double quotes, preserving quotes so
// PostgreSQL websearch keeps phrase semantics. A quoted token is never treated
// as a field expression.
func queryTokens(raw string) []string {
	var tokens []string
	start := -1
	quoted := false
	for index, r := range raw {
		if r == '"' {
			quoted = !quoted
		}
		if unicode.IsSpace(r) && !quoted {
			if start >= 0 {
				tokens = append(tokens, raw[start:index])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = index
		}
	}
	if start >= 0 {
		tokens = append(tokens, raw[start:])
	}
	return tokens
}

func splitFieldExpression(token string) (field, value string, ok bool) {
	quoted := false
	for index, r := range token {
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == ':' && !quoted {
			if index == 0 || index == len(token)-1 {
				return "", "", true
			}
			return token[:index], token[index+1:], true
		}
	}
	return "", "", false
}

func invalidQuery(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuery, fmt.Sprintf(format, args...))
}
