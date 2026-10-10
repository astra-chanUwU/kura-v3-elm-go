package posts

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCollectionCursorRoundTrip(t *testing.T) {
	token, err := EncodeCollectionCursor(7, 4, 12, 11)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := DecodeCollectionCursor(token, 7)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.CollectionID != 7 || cursor.Version != 4 || cursor.Position != 12 || cursor.PostID != 11 {
		t.Fatalf("unexpected cursor: %#v", cursor)
	}
}

func TestCollectionCursorAllowsZeroPositionAndVersion(t *testing.T) {
	token, err := EncodeCollectionCursor(7, 0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := DecodeCollectionCursor(token, 7)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Version != 0 || cursor.Position != 0 || cursor.PostID != 1 {
		t.Fatalf("unexpected cursor: %#v", cursor)
	}
}

func TestCollectionCursorRejectsMalformedTokens(t *testing.T) {
	valid, err := EncodeCollectionCursor(7, 4, 12, 11)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"empty":      "",
		"bad base64": "!",
		"too long":   strings.Repeat("a", maxCursorLength+1),
		"not json":   base64.RawURLEncoding.EncodeToString([]byte("nope")),
		"array":      base64.RawURLEncoding.EncodeToString([]byte(`[]`)),
		"unknown":    base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"c":7,"cv":4,"p":12,"i":11,"x":1}`)),
		"trailing":   base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"c":7,"cv":4,"p":12,"i":11} ` + "\n" + `{"v":1}`)),
		"bad ver":    base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"c":7,"cv":4,"p":12,"i":11}`)),
		"neg ver":    base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"c":7,"cv":-1,"p":12,"i":11}`)),
		"neg pos":    base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"c":7,"cv":4,"p":-1,"i":11}`)),
		"zero post":  base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"c":7,"cv":4,"p":12,"i":0}`)),
		"truncated":  valid[:len(valid)-2],
		"whitespace": " " + valid,
	}
	for name, token := range cases {
		if _, err := DecodeCollectionCursor(token, 7); err != ErrInvalidCursor {
			t.Errorf("%s: expected ErrInvalidCursor, got %v", name, err)
		}
	}
}

func TestCollectionCursorBindsCollection(t *testing.T) {
	token, err := EncodeCollectionCursor(8, 4, 12, 11)
	if err != nil {
		t.Fatal(err)
	}
	// A cursor issued for another collection is a 400, not a page: the
	// reader must never decode it as its own position.
	if _, err := DecodeCollectionCursor(token, 7); err != ErrInvalidCursor {
		t.Fatalf("cross-collection cursor: expected ErrInvalidCursor, got %v", err)
	}
	if _, err := DecodeCollectionCursor(token, 0); err != ErrInvalidCursor {
		t.Fatalf("zero target collection: expected ErrInvalidCursor, got %v", err)
	}
	if _, err := EncodeCollectionCursor(0, 4, 12, 11); err != ErrInvalidCursor {
		t.Fatalf("zero collection encode: expected ErrInvalidCursor, got %v", err)
	}
	if _, err := EncodeCollectionCursor(7, -1, 12, 11); err != ErrInvalidCursor {
		t.Fatalf("negative version encode: expected ErrInvalidCursor, got %v", err)
	}
}
