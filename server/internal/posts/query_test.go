package posts

import (
	"errors"
	"strings"
	"testing"
)

func TestParseQuery(t *testing.T) {
	favorite := true
	unfavorite := false
	tests := []struct {
		name string
		raw  string
		want Query
	}{
		{name: "empty", raw: "", want: Query{}},
		{name: "terms and unary exclusion", raw: "demo -kson", want: Query{Text: "demo -kson"}},
		{name: "all filters", raw: "ruin favorite:true score:>=3 width:<1200 height:=836", want: Query{
			Text: "ruin", Favorite: &favorite,
			Score: &NumericFilter{Operator: ">=", Value: 3}, Width: &NumericFilter{Operator: "<", Value: 1200}, Height: &NumericFilter{Operator: "=", Value: 836},
		}},
		{name: "quoted phrase", raw: `"red fox" favorite:false`, want: Query{Text: `"red fox"`, Favorite: &unfavorite}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseQuery(test.raw)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", test.raw, err)
			}
			if got.Text != test.want.Text || !sameBool(got.Favorite, test.want.Favorite) || !sameFilter(got.Score, test.want.Score) || !sameFilter(got.Width, test.want.Width) || !sameFilter(got.Height, test.want.Height) {
				t.Fatalf("ParseQuery(%q) = %#v, want %#v", test.raw, got, test.want)
			}
			if got.Order != SortNewest {
				t.Fatalf("ParseQuery(%q) order = %q, want newest", test.raw, got.Order)
			}
		})
	}
}

func TestParseQueryRejectsMalformedAndUnknownFields(t *testing.T) {
	for _, raw := range []string{"rating:5", "favorite:yes", "favorite:", "score:5", "score:>>5", "score:>=", "score:>=5x", "width:>1 height:>=2 width:=3", "-favorite:true"} {
		if _, err := ParseQuery(raw); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("ParseQuery(%q) error = %v, want ErrInvalidQuery", raw, err)
		}
	}
}

func TestParseQueryTagPredicates(t *testing.T) {
	query, err := ParseQuery(`tag:cat -tag:dog tag:"ruin explorers"`)
	if err != nil {
		t.Fatalf("ParseQuery tags: %v", err)
	}
	if len(query.Tags) != 3 {
		t.Fatalf("tags len = %d, want 3", len(query.Tags))
	}
	if query.Tags[0].Value != "cat" || query.Tags[0].Excluded {
		t.Fatalf("tag 0 = %#v", query.Tags[0])
	}
	if query.Tags[1].Value != "dog" || !query.Tags[1].Excluded {
		t.Fatalf("tag 1 = %#v", query.Tags[1])
	}
	if query.Tags[2].Value != "ruin explorers" || query.Tags[2].Excluded {
		t.Fatalf("tag 2 = %#v", query.Tags[2])
	}
	if query.AST == nil || len(query.AST.Tags) != 3 {
		t.Fatalf("AST tags missing: %#v", query.AST)
	}
	if _, err := ParseQuery(`tag:`); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("empty tag should fail")
	}
	if _, err := ParseQuery(`tag:""`); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("empty quoted tag should fail")
	}
}

func TestParseQueryOrderScore(t *testing.T) {
	query, err := ParseQuery(`cat order:score`)
	if err != nil {
		t.Fatalf("order:score: %v", err)
	}
	if query.Order != SortScore || query.AST.Order != SortScore {
		t.Fatalf("order = %q, want score", query.Order)
	}
	if query.Text != "cat" {
		t.Fatalf("text = %q, want cat", query.Text)
	}
	newest, err := ParseQuery(`cat order:newest`)
	if err != nil {
		t.Fatalf("order:newest: %v", err)
	}
	if newest.Order != SortNewest {
		t.Fatalf("newest order = %q", newest.Order)
	}
	if _, err := ParseQuery(`order:score order:newest`); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("duplicate order should fail")
	}
	if _, err := ParseQuery(`order:popular`); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("unknown order should fail")
	}
}

func TestParseQueryASTBoundary(t *testing.T) {
	query, err := ParseQuery(`demo -kson tag:cat favorite:true score:>=5 order:score`)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if query.AST == nil {
		t.Fatalf("AST nil")
	}
	if len(query.AST.Terms) != 1 || query.AST.Terms[0] != "demo" {
		t.Fatalf("AST terms = %#v", query.AST.Terms)
	}
	if len(query.AST.Excluded) != 1 || query.AST.Excluded[0] != "kson" {
		t.Fatalf("AST excluded = %#v", query.AST.Excluded)
	}
	if len(query.AST.Tags) != 1 || query.AST.Tags[0].Value != "cat" {
		t.Fatalf("AST tags = %#v", query.AST.Tags)
	}
	if query.AST.Favorite == nil || !*query.AST.Favorite {
		t.Fatalf("AST favorite = %#v", query.AST.Favorite)
	}
	if query.AST.Score == nil || query.AST.Score.Operator != ">=" || query.AST.Score.Value != 5 {
		t.Fatalf("AST score = %#v", query.AST.Score)
	}
	if query.AST.Order != SortScore {
		t.Fatalf("AST order = %q", query.AST.Order)
	}
	if query.Order != query.AST.Order || query.Text != "demo -kson" {
		t.Fatalf("Query copy mismatch: %#v", query)
	}
	if _, err := ParseQuery(`"unclosed`); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("unclosed quote should fail, got %v", err)
	}
}

func TestParseQueryAdditionalFilters(t *testing.T) {
	query, err := ParseQuery(`cat file_size:>=100 id:>42 media_type:image/jpeg source:demo/foo artist:"Kura Demo"`)
	if err != nil {
		t.Fatalf("ParseQuery additional: %v", err)
	}
	if query.FileSize == nil || query.FileSize.Operator != ">=" || query.FileSize.Value != 100 {
		t.Fatalf("file_size = %#v", query.FileSize)
	}
	if query.ID == nil || query.ID.Operator != ">" || query.ID.Value != 42 {
		t.Fatalf("id = %#v", query.ID)
	}
	if query.MediaType == nil || query.MediaType.Value != "image/jpeg" {
		t.Fatalf("media_type = %#v", query.MediaType)
	}
	if query.Source == nil || query.Source.Value != "demo/foo" {
		t.Fatalf("source = %#v", query.Source)
	}
	if query.Artist == nil || query.Artist.Value != "Kura Demo" {
		t.Fatalf("artist = %#v", query.Artist)
	}
	// Numeric shorthand without colon.
	short, err := ParseQuery(`score>=3 width<=100`)
	if err != nil {
		t.Fatalf("shorthand: %v", err)
	}
	if short.Score == nil || short.Score.Operator != ">=" {
		t.Fatalf("short score = %#v", short.Score)
	}
	if short.Width == nil || short.Width.Operator != "<=" {
		t.Fatalf("short width = %#v", short.Width)
	}
}

func TestSearchPostsSQLUsesBoundedParametersAndCursors(t *testing.T) {
	for _, fragment := range []string{"websearch_to_tsquery('simple', $1)", "favorite = $2::boolean", "score >= $4::integer", "width <= $6::integer", "height = $8::integer", "file_size >= $10::bigint", "id < $12::bigint", "media_type = $13::text", "source = $14::text", "artist = $15::text", "tags @> $16::text[]", "NOT (tags && $17::text[])", "ORDER BY id DESC", "LIMIT $19"} {
		if !strings.Contains(SearchPostsSQL, fragment) {
			t.Errorf("SearchPostsSQL missing %q", fragment)
		}
	}
	for _, fragment := range []string{"websearch_to_tsquery('simple', $1)", "ORDER BY score DESC, id DESC", "score < $18::integer", "id < $19::bigint", "LIMIT $20"} {
		if !strings.Contains(SearchPostsScoreSQL, fragment) {
			t.Errorf("SearchPostsScoreSQL missing %q", fragment)
		}
	}
}

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameFilter(a, b *NumericFilter) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
