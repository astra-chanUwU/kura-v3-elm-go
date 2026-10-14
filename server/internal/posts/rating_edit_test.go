package posts

import (
	"encoding/json"
	"errors"
	"testing"
)

func intPointer(value int) *int { return &value }

func TestRatingEditRequiresExplicitNonNullIntegerVersion(t *testing.T) {
	for _, body := range []string{`{"id":"10"}`, `{"id":"10","version":null}`, `{"id":"10","version":-1}`, `{"id":"10","version":0.5}`, `{"id":"10","version":"0"}`, `{"id":"10","version":0,"extra":true}`} {
		var target RatingEdit
		if err := json.Unmarshal([]byte(body), &target); !errors.Is(err, ErrInvalidRating) {
			t.Errorf("%s: got %v, want ErrInvalidRating", body, err)
		}
	}
	var target RatingEdit
	if err := json.Unmarshal([]byte(`{"id":"10","version":0}`), &target); err != nil || target.Version == nil || *target.Version != 0 {
		t.Fatalf("version zero should be preserved: %#v, err=%v", target, err)
	}
}

func TestValidateRatingRequest(t *testing.T) {
	valid, err := ValidateRatingRequest(RatingRequest{
		Posts:  []RatingEdit{{ID: "10", Version: intPointer(0)}},
		Rating: RatingSafe,
	})
	if err != nil || valid.Rating != RatingSafe {
		t.Fatalf("valid request rejected: %#v, err=%v", valid, err)
	}
	for _, request := range []RatingRequest{
		{Posts: []RatingEdit{{ID: "10", Version: intPointer(0)}}},
		{Posts: []RatingEdit{{ID: "10", Version: intPointer(0)}}, Rating: "SAFE"},
		{Posts: []RatingEdit{{ID: "10", Version: intPointer(0)}}, Rating: "unknown"},
		{Posts: []RatingEdit{{ID: "010", Version: intPointer(0)}}, Rating: RatingSafe},
		{Posts: []RatingEdit{{ID: "10 ", Version: intPointer(0)}}, Rating: RatingSafe},
		{Posts: []RatingEdit{{ID: "10", Version: intPointer(0)}, {ID: "10", Version: intPointer(0)}}, Rating: RatingSafe},
		{Posts: []RatingEdit{{ID: "10", Version: nil}}, Rating: RatingSafe},
		{Posts: makeRatingTargets(101), Rating: RatingSafe},
	} {
		if _, err := ValidateRatingRequest(request); !errors.Is(err, ErrInvalidRating) {
			t.Errorf("request %#v: got %v, want ErrInvalidRating", request, err)
		}
	}
}

func makeRatingTargets(count int) []RatingEdit {
	targets := make([]RatingEdit, count)
	for i := range targets {
		targets[i] = RatingEdit{ID: "10", Version: intPointer(0)}
	}
	return targets
}
