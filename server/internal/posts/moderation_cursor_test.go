package posts

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestModerationQueueCursorRoundTripStateBound(t *testing.T) {
	token, err := EncodeModerationQueueCursor(" Hidden ", 2004)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, "hidden") || strings.Contains(token, "2004") {
		t.Fatalf("cursor leaks plaintext: %q", token)
	}
	id, err := DecodeModerationQueueCursor(token, "hidden")
	if err != nil || id != 2004 {
		t.Fatalf("decode = %d, %v", id, err)
	}
	if _, err := DecodeModerationQueueCursor(token, "published"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cross-state cursor accepted: %v", err)
	}
}

func TestModerationQueueCursorStrict(t *testing.T) {
	valid, _ := EncodeModerationQueueCursor("pending", 10)
	badJSON := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"pending","i":10,"x":1}`))
	wrongVersion := base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"s":"pending","i":10}`))
	zeroID := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"pending","i":0}`))
	trailing := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"pending","i":10} {}`))
	for name, token := range map[string]string{
		"empty": "", "garbage": "!!!", "padded": valid + "=", "extra field": badJSON,
		"wrong version": wrongVersion, "zero id": zeroID, "trailing": trailing,
		"too long": strings.Repeat("a", maxModerationCursorLength+1),
	} {
		if _, err := DecodeModerationQueueCursor(token, "pending"); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("%s: err=%v want ErrInvalidCursor", name, err)
		}
	}
	if _, err := EncodeModerationQueueCursor("banned", 1); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("bad state encode: %v", err)
	}
	if _, err := EncodeModerationQueueCursor("pending", 0); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("zero id encode: %v", err)
	}
}

func TestModerationPreviewSQLUsesStoredReadyThumbOnly(t *testing.T) {
	if !strings.Contains(moderationPreviewVariantSQL, "variant = 'thumb-320'") || !strings.Contains(moderationPreviewVariantSQL, "status = 'ready'") {
		t.Error("preview must use only the ready thumb-320 variant")
	}
	if !strings.Contains(moderationPreviewVariantSQL, "object_key") || strings.Contains(moderationPreviewVariantSQL, "original_url") {
		t.Error("preview must resolve the stored object_key and never the original")
	}
	if !strings.Contains(moderationPreviewVariantSQL, "deleted_at IS NULL") {
		t.Error("preview must exclude deleted posts")
	}
	if strings.Contains(moderationPreviewVariantSQL, "moderation_state = 'published'") {
		t.Error("preview must reach posts in any moderation state for moderators")
	}
	if !strings.Contains(moderationQueuePageSQL, "id < $3::bigint") || !strings.Contains(moderationQueuePageSQL, "ORDER BY posts.id DESC") {
		t.Error("queue page must be newest-first keyset on id")
	}
}
