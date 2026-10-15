package posts

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSimilarCursorBindsAnchorHashAndRadius(t *testing.T) {
	hash := "sha256:" + strings.Repeat("a", 64)
	otherHash := "sha256:" + strings.Repeat("b", 64)
	token, err := EncodeSimilarCursor("7", hash, 8, 3, 42)
	if err != nil {
		t.Fatalf("EncodeSimilarCursor: %v", err)
	}
	decoded, err := DecodeSimilarCursor(token, "7", hash, 8)
	if err != nil {
		t.Fatalf("DecodeSimilarCursor: %v", err)
	}
	if decoded.AnchorID != "7" || decoded.AnchorHash != hash || decoded.Algorithm != SimilarityAlgorithm ||
		decoded.MaxDistance != 8 || decoded.LastDistance != 3 || decoded.LastID != 42 {
		t.Fatalf("cursor mismatch: %#v", decoded)
	}
	// Foreign bindings and bad tokens all fail.
	for _, foreign := range [][3]string{
		{"9", hash, "8"},
		{"7", otherHash, "8"},
		{"7", hash, "9"},
	} {
		distance := 8
		if foreign[2] != "8" {
			distance = 9
		}
		if _, err := DecodeSimilarCursor(token, foreign[0], foreign[1], distance); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("foreign cursor %v decoded without error", foreign)
		}
	}
	for _, bad := range []string{"", "!!!", strings.Repeat("a", 513)} {
		if _, err := DecodeSimilarCursor(bad, "7", hash, 8); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("bad cursor %q decoded without error", bad)
		}
	}
	// A duplicates cursor is a foreign route and must fail here.
	groups, err := EncodeDuplicateGroupsCursor("sha256:" + strings.Repeat("c", 64))
	if err != nil {
		t.Fatalf("EncodeDuplicateGroupsCursor: %v", err)
	}
	if _, err := DecodeSimilarCursor(groups, "7", hash, 8); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("duplicate-route cursor decoded as similar cursor")
	}
	if _, err := EncodeSimilarCursor("0", hash, 8, 0, 1); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("expected bad anchor to fail encoding")
	}
	if _, err := EncodeSimilarCursor("7", hash, 8, 9, 1); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("expected lastDistance beyond radius to fail encoding")
	}
}

func TestSimilarityStatusCursorRoundTrip(t *testing.T) {
	token, err := EncodeSimilarityStatusCursor(42)
	if err != nil {
		t.Fatalf("EncodeSimilarityStatusCursor: %v", err)
	}
	decoded, err := DecodeSimilarityStatusCursor(token)
	if err != nil || decoded.LastID != 42 {
		t.Fatalf("status cursor mismatch: %#v %v", decoded, err)
	}
	similar, err := EncodeSimilarCursor("7", "sha256:"+strings.Repeat("a", 64), 8, 0, 42)
	if err != nil {
		t.Fatalf("EncodeSimilarCursor: %v", err)
	}
	if _, err := DecodeSimilarityStatusCursor(similar); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("similar-route cursor decoded as status cursor")
	}
	if _, err := DecodeSimilarityStatusCursor("broken"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("expected broken status cursor to fail")
	}
}

func TestValidateSimilarParams(t *testing.T) {
	distance, limit, err := ValidateSimilarParams(0, false, 0, false)
	if err != nil || distance != DefaultSimilarDistance || limit != DefaultSimilarLimit {
		t.Fatalf("defaults got distance=%d limit=%d err=%v", distance, limit, err)
	}
	if _, _, err := ValidateSimilarParams(17, true, 30, true); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("distance 17 err=%v", err)
	}
	if _, _, err := ValidateSimilarParams(-1, true, 30, true); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("negative distance err=%v", err)
	}
	if _, _, err := ValidateSimilarParams(8, true, 0, true); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("limit 0 err=%v", err)
	}
	if _, _, err := ValidateSimilarParams(8, true, 61, true); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("limit 61 err=%v", err)
	}
	if _, _, err := ValidateSimilarParams(0, true, 1, true); err != nil {
		t.Fatalf("distance 0 limit 1 rejected: %v", err)
	}
	if _, _, err := ValidateSimilarParams(16, true, 60, true); err != nil {
		t.Fatalf("distance 16 limit 60 rejected: %v", err)
	}
}

func TestValidateSimilarityIndexIDs(t *testing.T) {
	clean, err := ValidateSimilarityIndexIDs([]string{"3", "1", "2"})
	if err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	if len(clean) != 3 || clean[0] != "3" {
		t.Fatalf("order not preserved: %v", clean)
	}
	for _, batch := range [][]string{
		{},
		{"1", "1"},
		{"0"},
		{"-4"},
		{"01"},
		{"  "},
		{"abc"},
		{"1.5"},
		make([]string, MaxSimilarityIndexPosts+1),
	} {
		if len(batch) == MaxSimilarityIndexPosts+1 {
			for i := range batch {
				batch[i] = "1"
			}
		}
		if _, err := ValidateSimilarityIndexIDs(batch); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("batch %v accepted", batchSummary(batch))
		}
	}
	full := make([]string, MaxSimilarityIndexPosts)
	for i := range full {
		full[i] = strconv.Itoa(i + 1)
	}
	if _, err := ValidateSimilarityIndexIDs(full); err != nil {
		t.Fatalf("100-id batch rejected: %v", err)
	}
}

func batchSummary(batch []string) string {
	if len(batch) > 4 {
		return "oversized batch"
	}
	return strings.Join(batch, ",")
}

func TestValidateSimilarityStatusLimit(t *testing.T) {
	limit, err := ValidateSimilarityStatusLimit(0, false)
	if err != nil || limit != DefaultSimilarityStatusLimit {
		t.Fatalf("default limit=%d err=%v", limit, err)
	}
	if _, err := ValidateSimilarityStatusLimit(0, true); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("limit 0 err=%v", err)
	}
	if _, err := ValidateSimilarityStatusLimit(101, true); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("limit 101 err=%v", err)
	}
	if _, err := ValidateSimilarityStatusLimit(100, true); err != nil {
		t.Fatalf("limit 100 rejected: %v", err)
	}
}

func TestSimilaritySQLStaysBoundedAndParameterized(t *testing.T) {
	for _, clause := range []string{
		"bit_count(f.fingerprint # $2::bit(64))",
		"f.original_hash = p.hash",
		"moderation_state = 'published'",
		"ORDER BY distance ASC, p.id ASC",
	} {
		if !strings.Contains(similarCandidatesSQL, clause) {
			t.Fatalf("candidate SQL lost %q", clause)
		}
	}
	if strings.Contains(similarCandidatesSQL, "fingerprint # '") || strings.Contains(similarCandidatesSQL, "%s") {
		t.Fatal("candidate SQL must stay parameterized (no interpolated hash)")
	}
	if !strings.Contains(similarityStatusPageSQL, "'fingerprint:' || p.id::text || ':dhash-v1:' || p.hash") {
		t.Fatal("status SQL must join jobs on the enqueue idempotency key")
	}
	if !strings.Contains(similarityStatusPageSQL, "ORDER BY p.id ASC") {
		t.Fatal("status SQL must page by id ascending")
	}
}

func TestSimilarityMigration018(t *testing.T) {
	path := filepath.Join("..", "..", "..", "db", "migrations", "018_similarity_fingerprints.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for _, clause := range []string{
		"CREATE TABLE IF NOT EXISTS similarity_fingerprints",
		"BIT(64)",
		"REFERENCES posts (id) ON DELETE CASCADE",
		"original_hash",
		"dhash-v1",
	} {
		if !strings.Contains(string(sql), clause) {
			t.Fatalf("migration 018 lost %q", clause)
		}
	}
}
