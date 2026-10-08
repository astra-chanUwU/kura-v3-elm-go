package posts

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type SortOrder string

const (
	SortNewest SortOrder = "newest"
	SortScore  SortOrder = "score"
)

type TagPredicate struct {
	Value    string
	Excluded bool
}

type StringFilter struct {
	Operator string
	Value    string
}

// QueryAST is the parsed KuraQL representation. Query retains compatibility
// fields while AST gives planners a typed boundary instead of reparsing text.
type QueryAST struct {
	Terms     []string
	Excluded  []string
	Tags      []TagPredicate
	Favorite  *bool
	Score     *NumericFilter
	Width     *NumericFilter
	Height    *NumericFilter
	FileSize  *NumericFilter
	ID        *NumericFilter
	MediaType *StringFilter
	Source    *StringFilter
	Artist    *StringFilter
	Order     SortOrder
}

// Query is the bounded, server-owned representation of a KuraQL search.
type Query struct {
	Text      string
	Favorite  *bool
	Score     *NumericFilter
	Width     *NumericFilter
	Height    *NumericFilter
	FileSize  *NumericFilter
	ID        *NumericFilter
	MediaType *StringFilter
	Source    *StringFilter
	Artist    *StringFilter
	Tags      []TagPredicate
	Order     SortOrder
	AST       *QueryAST
}

type NumericFilter struct {
	Operator string
	Value    int
}

// ParseQuery parses ordinary web-search terms, tag:NAME and -tag:NAME,
// favorite:true|false, numeric comparisons, exact text fields, and
// order:score/newest. Numeric expressions accept score:>=3 and score>=3.
func ParseQuery(raw string) (Query, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		ast := QueryAST{Order: SortNewest}
		return queryFromAST(ast, ""), nil
	}
	tokens, balanced := queryTokens(raw)
	if !balanced {
		return Query{}, invalidQuery("unclosed quoted expression")
	}
	ast := QueryAST{Order: SortNewest}
	textTerms := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		field, value, hasField, unary := splitExpression(token)
		if !hasField {
			textTerms = append(textTerms, token)
			if strings.HasPrefix(token, "-") && len(token) > 1 {
				ast.Excluded = append(ast.Excluded, strings.TrimPrefix(token, "-"))
			} else {
				ast.Terms = append(ast.Terms, token)
			}
			continue
		}
		if unary {
			if strings.EqualFold(field, "tag") || strings.EqualFold(field, "tags") {
				value = unquote(value)
				if value == "" {
					return Query{}, invalidQuery("tag expects a non-empty value")
				}
				ast.Tags = append(ast.Tags, TagPredicate{Value: value, Excluded: true})
				continue
			}
			return Query{}, invalidQuery("unary field expressions are not supported")
		}
		if err := addFieldFilter(&ast, field, value); err != nil {
			return Query{}, err
		}
	}
	return queryFromAST(ast, strings.Join(textTerms, " ")), nil
}

func queryFromAST(ast QueryAST, text string) Query {
	return Query{Text: text, Favorite: ast.Favorite, Score: ast.Score, Width: ast.Width, Height: ast.Height, FileSize: ast.FileSize, ID: ast.ID, MediaType: ast.MediaType, Source: ast.Source, Artist: ast.Artist, Tags: ast.Tags, Order: ast.Order, AST: &ast}
}

func addFieldFilter(ast *QueryAST, field, rawValue string) error {
	field = strings.ToLower(strings.TrimSpace(field))
	value := unquote(rawValue)
	if value == "" {
		return invalidQuery("field %q expects a value", field)
	}
	switch field {
	case "tag", "tags":
		ast.Tags = append(ast.Tags, TagPredicate{Value: value})
		return nil
	case "favorite":
		if ast.Favorite != nil || (value != "true" && value != "false") {
			return invalidQuery("favorite expects true or false")
		}
		favorite := value == "true"
		ast.Favorite = &favorite
	case "order":
		if ast.Order != SortNewest {
			return invalidQuery("duplicate order expression")
		}
		switch strings.ToLower(value) {
		case "score":
			ast.Order = SortScore
		case "newest", "recent", "id":
			ast.Order = SortNewest
		default:
			return invalidQuery("order expects score or newest")
		}
		return nil
	case "score", "width", "height", "file_size", "filesize", "id":
		filter, err := parseNumericFilter(value)
		if err != nil {
			return err
		}
		switch field {
		case "score":
			if ast.Score != nil {
				return invalidQuery("duplicate score filter")
			}
			ast.Score = &filter
		case "width":
			if ast.Width != nil {
				return invalidQuery("duplicate width filter")
			}
			ast.Width = &filter
		case "height":
			if ast.Height != nil {
				return invalidQuery("duplicate height filter")
			}
			ast.Height = &filter
		case "file_size", "filesize":
			if ast.FileSize != nil {
				return invalidQuery("duplicate file_size filter")
			}
			ast.FileSize = &filter
		case "id":
			if ast.ID != nil {
				return invalidQuery("duplicate id filter")
			}
			ast.ID = &filter
		}
		return nil
	case "media_type", "mediatype", "type", "source", "artist":
		filter := &StringFilter{Operator: "=", Value: value}
		switch field {
		case "media_type", "mediatype", "type":
			if ast.MediaType != nil {
				return invalidQuery("duplicate media_type filter")
			}
			ast.MediaType = filter
		case "source":
			if ast.Source != nil {
				return invalidQuery("duplicate source filter")
			}
			ast.Source = filter
		case "artist":
			if ast.Artist != nil {
				return invalidQuery("duplicate artist filter")
			}
			ast.Artist = filter
		}
		return nil
	default:
		return invalidQuery("unknown field %q", field)
	}
	return nil
}

func parseNumericFilter(raw string) (NumericFilter, error) {
	operator := ""
	for _, candidate := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(raw, candidate) {
			operator, raw = candidate, strings.TrimPrefix(raw, candidate)
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

func queryTokens(raw string) ([]string, bool) {
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
	return tokens, !quoted
}

func splitExpression(token string) (field, value string, hasField, unary bool) {
	if strings.HasPrefix(token, "-") {
		unary, token = true, token[1:]
	}
	quoted := false
	for index, r := range token {
		if r == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		if r == ':' {
			if index == 0 || index == len(token)-1 {
				return token, "", true, unary
			}
			return token[:index], token[index+1:], true, unary
		}
		if r == '>' || r == '<' || r == '=' {
			if index == 0 {
				return token, "", true, unary
			}
			return token[:index], token[index:], true, unary
		}
	}
	return "", "", false, unary
}

func unquote(value string) string {
	if len(value) >= 2 && strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		return value[1 : len(value)-1]
	}
	return value
}

func invalidQuery(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuery, fmt.Sprintf(format, args...))
}
