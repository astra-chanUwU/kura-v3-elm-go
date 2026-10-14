package posts

import (
	"context"
	"strings"
	"testing"
)

func canonicalHashForTest(seed byte) string {
	return "sha256:" + strings.Repeat(string([]byte{seed}), 64)
}

func TestDuplicateGroupsCursorRoundTrip(t *testing.T) {
	hash := "sha256:" + strings.Repeat("a", 64)
	token, err := EncodeDuplicateGroupsCursor(hash)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeDuplicateGroupsCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Hash != hash {
		t.Fatalf("cursor hash = %q, want %q", decoded.Hash, hash)
	}
}

func TestDuplicateGroupsCursorRejectsMemberCursor(t *testing.T) {
	hash := "sha256:" + strings.Repeat("b", 64)
	member, err := EncodeDuplicateMembersCursor(hash, 42)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeDuplicateGroupsCursor(member); err != ErrInvalidCursor {
		t.Fatalf("member cursor as group cursor: got %v, want ErrInvalidCursor", err)
	}
}

func TestDuplicateGroupsCursorValidation(t *testing.T) {
	for _, token := range []string{"", "bad", strings.Repeat("a", 600)} {
		if _, err := DecodeDuplicateGroupsCursor(token); err != ErrInvalidCursor {
			t.Errorf("token %q: got %v, want ErrInvalidCursor", token, err)
		}
	}
	for _, hash := range []string{"", "sha256:xyz", "SHA256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("a", 63)} {
		if _, err := EncodeDuplicateGroupsCursor(hash); err != ErrInvalidCursor {
			t.Errorf("hash %q: got %v, want ErrInvalidCursor", hash, err)
		}
	}
}

func TestDuplicateMembersCursorBoundToHash(t *testing.T) {
	hashA := "sha256:" + strings.Repeat("a", 64)
	hashB := "sha256:" + strings.Repeat("b", 64)
	token, err := EncodeDuplicateMembersCursor(hashA, 7)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeDuplicateMembersCursor(token, hashA)
	if err != nil || decoded.ID != 7 || decoded.Hash != hashA {
		t.Fatalf("round trip = %#v, err=%v", decoded, err)
	}
	if _, err := DecodeDuplicateMembersCursor(token, hashB); err != ErrInvalidCursor {
		t.Fatalf("foreign hash accepted: %v", err)
	}
	group, err := EncodeDuplicateGroupsCursor(hashA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeDuplicateMembersCursor(group, hashA); err != ErrInvalidCursor {
		t.Fatalf("group cursor as member cursor: got %v, want ErrInvalidCursor", err)
	}
	if _, err := EncodeDuplicateMembersCursor(hashA, 0); err != ErrInvalidCursor {
		t.Fatalf("zero id accepted: %v", err)
	}
	if _, err := DecodeDuplicateMembersCursor(token, "bad-hash"); err != ErrInvalidCursor {
		t.Fatalf("bad expected hash: got %v, want ErrInvalidCursor", err)
	}
}

func TestDuplicateReaderLimitBounds(t *testing.T) {
	ctx := context.Background()
	backend := &PostgresBackend{}
	for _, limit := range []int{0, -1, 31, 100} {
		if _, err := backend.ListDuplicateGroups(ctx, limit, nil); err != ErrInvalidQuery {
			t.Errorf("groups limit %d: got %v, want ErrInvalidQuery", limit, err)
		}
	}
	for _, limit := range []int{0, -1, 61} {
		if _, err := backend.ListDuplicateMembers(ctx, "sha256:"+strings.Repeat("c", 64), limit, nil); err != ErrInvalidQuery {
			t.Errorf("members limit %d: got %v, want ErrInvalidQuery", limit, err)
		}
	}
	if _, err := backend.ListDuplicateMembers(ctx, "not-a-hash", 10, nil); err != ErrInvalidQuery {
		t.Errorf("bad hash: got %v, want ErrInvalidQuery", err)
	}
}
