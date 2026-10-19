package posts

import (
	"testing"
	"time"
)

func TestProfileListCursorIsAccountBound(t *testing.T) {
	cursor := ProfileListCursor{UserID: 4, CreatedAt: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC), ID: 8}
	token, err := EncodeProfileListCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeProfileListCursor(token, 4)
	if err != nil || !got.CreatedAt.Equal(cursor.CreatedAt) || got.ID != 8 {
		t.Fatalf("cursor=%#v err=%v", got, err)
	}
	if _, err := DecodeProfileListCursor(token, 5); err != ErrInvalidCursor {
		t.Fatalf("foreign cursor err=%v", err)
	}
	if _, err := DecodeProfileListCursor("%%%", 4); err != ErrInvalidCursor {
		t.Fatalf("malformed cursor err=%v", err)
	}
	if _, err := NormalizeProfileListLimit(0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeProfileListLimit(25, true); err == nil {
		t.Fatal("oversized page accepted")
	}
	if safeProfileCover("https://example.com/a.jpg") != "" || safeProfileCover("/media/avatars/../x") != "" {
		t.Fatal("unsafe cover accepted")
	}
	if safeProfileCover("/media/derivatives/thumb.jpg") != "/media/derivatives/thumb.jpg" {
		t.Fatal("library cover rejected")
	}
}
