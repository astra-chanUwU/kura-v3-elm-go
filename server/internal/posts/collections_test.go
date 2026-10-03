package posts

import (
	"strconv"
	"testing"
)

func TestValidateCreateCollection(t *testing.T) {
	got, err := ValidateCreateCollection(CreateCollectionRequest{Name: "  Favorites  "})
	if err != nil || got.Name != "Favorites" {
		t.Fatalf("normalized collection: %#v, %v", got, err)
	}
	for _, name := range []string{"", "   ", string(make([]rune, maxCollectionNameLength+1))} {
		if _, err := ValidateCreateCollection(CreateCollectionRequest{Name: name}); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
}

func TestValidateAddCollectionPosts(t *testing.T) {
	got, err := ValidateAddCollectionPosts(AddCollectionPostsRequest{PostIDs: []string{"2", "10"}})
	if err != nil || len(got.PostIDs) != 2 {
		t.Fatalf("valid post ids rejected: %#v, %v", got, err)
	}
	for _, ids := range [][]string{{"0"}, {"01"}, {"2", "2"}, {"bad"}, {}} {
		if _, err := ValidateAddCollectionPosts(AddCollectionPostsRequest{PostIDs: ids}); err == nil {
			t.Errorf("post ids %v accepted", ids)
		}
	}
}

func TestValidateReorderCollection(t *testing.T) {
	got, err := ValidateReorderCollection(ReorderCollectionRequest{PostIDs: []string{"3", "2", "1"}})
	if err != nil || len(got.PostIDs) != 3 {
		t.Fatalf("valid reorder rejected: %#v, %v", got, err)
	}
	empty, err := ValidateReorderCollection(ReorderCollectionRequest{PostIDs: []string{}})
	if err != nil || len(empty.PostIDs) != 0 {
		t.Fatalf("empty reorder rejected: %#v, %v", empty, err)
	}
	for _, ids := range [][]string{{"0"}, {"01"}, {"2", "2"}, {"bad"}} {
		if _, err := ValidateReorderCollection(ReorderCollectionRequest{PostIDs: ids}); err == nil {
			t.Errorf("reorder ids %v accepted", ids)
		}
	}
	over := make([]string, maxCollectionPosts+1)
	for i := range over {
		over[i] = strconv.Itoa(i + 1)
	}
	if _, err := ValidateReorderCollection(ReorderCollectionRequest{PostIDs: over}); err == nil {
		t.Errorf("over-length reorder accepted")
	}
	maxIDs := make([]string, maxCollectionPosts)
	for i := range maxIDs {
		maxIDs[i] = strconv.Itoa(i + 1)
	}
	if _, err := ValidateReorderCollection(ReorderCollectionRequest{PostIDs: maxIDs}); err != nil {
		t.Fatalf("max-length reorder rejected: %v", err)
	}
}
