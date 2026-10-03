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
		})
	}
}

func TestParseQueryRejectsMalformedAndUnknownFields(t *testing.T) {
	for _, raw := range []string{"order:score", "rating:5", "favorite:yes", "favorite:", "score:5", "score:>>5", "score:>=", "score:>=5x", "width:>1 height:>=2 width:=3", "-favorite:true"} {
		if _, err := ParseQuery(raw); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("ParseQuery(%q) error = %v, want ErrInvalidQuery", raw, err)
		}
	}
}

func TestSearchPostsSQLUsesBoundedParametersAndIDCursor(t *testing.T) {
	for _, fragment := range []string{"websearch_to_tsquery('simple', $1)", "favorite = $2::boolean", "score >= $4::integer", "width <= $6::integer", "height = $8::integer", "id < $9::bigint", "ORDER BY id DESC", "LIMIT $10"} {
		if !strings.Contains(SearchPostsSQL, fragment) {
			t.Errorf("SearchPostsSQL missing %q", fragment)
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
