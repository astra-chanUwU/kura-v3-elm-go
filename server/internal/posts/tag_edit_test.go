package posts

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidateTagEditNormalizesAndRejectsInvalidShape(t *testing.T) {
	validated, err := ValidateTagEdit(TagEditRequest{
		Posts:  []TagTarget{{ID: "2004", Version: 0}},
		Add:    []string{" night ", "portrait"},
		Remove: []string{" demo "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(validated.Add, []string{"night", "portrait"}) || !reflect.DeepEqual(validated.Remove, []string{"demo"}) {
		t.Fatalf("tags were not trimmed: %#v", validated)
	}

	cases := []TagEditRequest{
		{Posts: []TagTarget{{ID: "0"}}, Add: []string{"x"}},
		{Posts: []TagTarget{{ID: "2004"}}, Add: []string{" "}},
		{Posts: []TagTarget{{ID: "2004"}}, Add: []string{"x", "x"}},
		{Posts: []TagTarget{{ID: "2004"}}, Add: []string{"x"}, Remove: []string{"x"}},
		{Posts: []TagTarget{{ID: "2004"}, {ID: "2004"}}, Add: []string{"x"}},
	}
	for _, request := range cases {
		if _, err := ValidateTagEdit(request); !errors.Is(err, ErrInvalidTags) {
			t.Errorf("request %#v: expected ErrInvalidTags, got %v", request, err)
		}
	}
}

func TestUpdateSearchTextReplacesTagTermsAndPreservesTerms(t *testing.T) {
	got := updateSearchText("kson demo portrait", []string{"demo"}, []string{"night"})
	if got != "kson portrait night" {
		t.Fatalf("unexpected search text: %q", got)
	}
	got = updateSearchText("ruin explorers demo", []string{"ruin explorers"}, []string{"night"})
	if got != "demo night" {
		t.Fatalf("phrase tag was not replaced: %q", got)
	}
}
