package posts

import (
	"strings"
	"testing"
)

func TestCursorRoundTripAndQueryBinding(t *testing.T) {
	token, err := EncodeCursor("  cat blue  ", 42)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := DecodeCursor(token, "cat blue")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.ID != 42 || cursor.Version != cursorVersion || cursor.QueryHash != QueryHash("cat blue") {
		t.Fatalf("unexpected cursor: %#v", cursor)
	}
	if _, err := DecodeCursor(token, "cat red"); err != ErrInvalidCursor {
		t.Fatalf("expected query mismatch to fail, got %v", err)
	}
}

func TestCursorRejectsMalformedAndUnboundedTokens(t *testing.T) {
	cases := []string{"", "!", strings.Repeat("a", maxCursorLength+1)}
	for _, token := range cases {
		if _, err := DecodeCursor(token, "cat"); err != ErrInvalidCursor {
			t.Errorf("token %q: expected ErrInvalidCursor, got %v", token, err)
		}
	}
	if _, err := EncodeCursor("cat", 0); err != ErrInvalidCursor {
		t.Fatalf("expected non-positive id to fail, got %v", err)
	}
}

func TestScoreCursorRoundTripAndSortBinding(t *testing.T) {
	token, err := EncodeSortCursor("cat order:score", SortScore, 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := DecodeCursor(token, "cat order:score")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Order != SortScore || !cursor.HasScore || cursor.Score != 7 || cursor.ID != 42 {
		t.Fatalf("unexpected score cursor: %#v", cursor)
	}
	if _, err := DecodeCursor(token, "cat order:newest"); err != ErrInvalidCursor {
		t.Fatalf("expected sort/query mismatch to fail, got %v", err)
	}
	if _, err := EncodeSortCursor("cat", SortScore, 0, 0); err != ErrInvalidCursor {
		t.Fatalf("expected invalid score cursor id to fail, got %v", err)
	}
}
