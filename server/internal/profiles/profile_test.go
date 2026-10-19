package profiles

import (
	"errors"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
)

func TestNormalizeUsername(t *testing.T) {
	got, err := NormalizeUsername("  Ada_Lane ")
	if err != nil || got != "ada_lane" {
		t.Fatalf("normalize=%q err=%v", got, err)
	}
	for _, raw := range []string{"", " ", ".", "..", "a/b", `a\b`, "a.b", "-ada", "_ada", "local", "System", "sign-in", "api", "u", "profile", strings.Repeat("a", 33), "has space", "café"} {
		if _, err := NormalizeUsername(raw); err == nil {
			t.Errorf("username %q accepted", raw)
		}
	}
	if _, err := NormalizeUsername("local"); !errors.Is(err, ErrReservedName) {
		t.Fatalf("local err=%v", err)
	}
	if !ValidPathSegment("Ada") || ValidPathSegment("a/b") || ValidPathSegment("..") || ValidPathSegment(".hidden") || ValidPathSegment("") {
		t.Fatal("path segment rules drifted")
	}
	if ActorID := auth.ActorIDForUser(9, "local"); ActorID != "local" {
		t.Fatalf("actor key changed: %s", ActorID)
	}
	if auth.ActorIDForUser(9, "ada") != "user:9" {
		t.Fatal("account actor key must stay user id")
	}
}

func TestNormalizeWebsite(t *testing.T) {
	got, err := NormalizeWebsite("  https://example.com/path  ")
	if err != nil || got != "https://example.com/path" {
		t.Fatalf("website=%q err=%v", got, err)
	}
	empty, err := NormalizeWebsite("  ")
	if err != nil || empty != "" {
		t.Fatalf("empty website=%q err=%v", empty, err)
	}
	for _, raw := range []string{"javascript:alert(1)", "http://user:pass@example.com", "ftp://example.com", "example.com", "https://example.com/a b", "//example.com"} {
		if _, err := NormalizeWebsite(raw); !errors.Is(err, ErrInvalidWebsite) {
			t.Errorf("website %q err=%v", raw, err)
		}
	}
}

func TestNormalizeProfileRejectsControls(t *testing.T) {
	_, err := NormalizeProfile(Input{Username: "ada", DisplayName: "Ada\nLane", ExpectedVersion: 1})
	if err == nil {
		t.Fatal("newline display name accepted")
	}
	got, err := NormalizeProfile(Input{Username: "Ada", DisplayName: "  ", Bio: "  ", Website: "", ExpectedVersion: 2})
	if err != nil || got.Username != "ada" || got.DisplayName != "" || got.Bio != "" {
		t.Fatalf("cleared profile=%#v err=%v", got, err)
	}
}
