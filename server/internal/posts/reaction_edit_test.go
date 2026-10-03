package posts

import (
	"errors"
	"testing"
)

func TestValidateReactionRequest(t *testing.T) {
	favorite := true
	score := 4
	validated, err := ValidateReactionRequest(ReactionRequest{
		Posts: []ReactionTarget{{ID: "2004", Version: 2}}, Favorite: &favorite, Score: &score,
	})
	if err != nil {
		t.Fatal(err)
	}
	if validated.Posts[0].ID != "2004" || *validated.Favorite != true || *validated.Score != 4 {
		t.Fatalf("request was not normalized: %#v", validated)
	}

	cases := []ReactionRequest{
		{Posts: []ReactionTarget{{ID: "2004"}}},
		{Posts: []ReactionTarget{{ID: "0"}}, Favorite: &favorite},
		{Posts: []ReactionTarget{{ID: "2004", Version: -1}}, Favorite: &favorite},
		{Posts: []ReactionTarget{{ID: "2004"}, {ID: "2004"}}, Favorite: &favorite},
		{Posts: []ReactionTarget{{ID: "2004"}}, Score: func() *int { n := -1; return &n }()},
	}
	for _, request := range cases {
		if _, err := ValidateReactionRequest(request); !errors.Is(err, ErrInvalidReactions) {
			t.Errorf("request %#v: expected ErrInvalidReactions, got %v", request, err)
		}
	}
}
