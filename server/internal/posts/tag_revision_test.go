package posts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRevisionPreservesNullVersusEmptyTargetTags(t *testing.T) {
	// NULL target_tags should marshal as JSON null with revertible false
	nullRev := Revision{
		Version:     1,
		Kind:        "tag_edit",
		AddedTags:   []string{"a"},
		RemovedTags: []string{},
		TargetTags:  nil,
		Revertible:  false,
		CreatedAt:   "2026-01-01 00:00:00+00",
	}
	data, err := json.Marshal(nullRev)
	if err != nil {
		t.Fatalf("marshal null revision: %v", err)
	}
	raw := string(data)
	if !strings.Contains(raw, `"target_tags":null`) {
		t.Errorf("null target_tags should encode as null, got %s", raw)
	}
	if !strings.Contains(raw, `"revertible":false`) {
		t.Errorf("null revision should be non-revertible, got %s", raw)
	}
	var decodedNull Revision
	if err := json.Unmarshal(data, &decodedNull); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if decodedNull.TargetTags != nil {
		t.Errorf("null target_tags should decode as nil, got %v", decodedNull.TargetTags)
	}
	if decodedNull.Revertible != false {
		t.Errorf("decoded revertible should be false for legacy")
	}

	// Valid empty tags should marshal as [] with revertible true
	empty := []string{}
	emptyRev := Revision{
		Version:     2,
		Kind:        "tag_edit",
		AddedTags:   []string{},
		RemovedTags: []string{"a"},
		TargetTags:  &empty,
		Revertible:  true,
		CreatedAt:   "2026-01-02 00:00:00+00",
	}
	data, err = json.Marshal(emptyRev)
	if err != nil {
		t.Fatalf("marshal empty revision: %v", err)
	}
	raw = string(data)
	if !strings.Contains(raw, `"target_tags":[]`) {
		t.Errorf("empty target_tags should encode as [], got %s", raw)
	}
	if !strings.Contains(raw, `"revertible":true`) {
		t.Errorf("empty revision should be revertible, got %s", raw)
	}
	var decodedEmpty Revision
	if err := json.Unmarshal(data, &decodedEmpty); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if decodedEmpty.TargetTags == nil {
		t.Fatal("empty target_tags should decode as non-nil")
	}
	if len(*decodedEmpty.TargetTags) != 0 {
		t.Errorf("empty target_tags should decode as 0 length, got %v", *decodedEmpty.TargetTags)
	}

	// Non-empty target_tags
	tags := []string{"cat", "demo"}
	fullRev := Revision{
		Version:     3,
		Kind:        "tag_revert",
		AddedTags:   []string{"cat"},
		RemovedTags: []string{},
		TargetTags:  &tags,
		Revertible:  true,
		CreatedAt:   "2026-01-03 00:00:00+00",
	}
	data, err = json.Marshal(fullRev)
	if err != nil {
		t.Fatalf("marshal full revision: %v", err)
	}
	raw = string(data)
	if !strings.Contains(raw, `"target_tags":["cat","demo"]`) {
		t.Errorf("full target_tags should encode correctly, got %s", raw)
	}
	var decodedFull Revision
	if err := json.Unmarshal(data, &decodedFull); err != nil {
		t.Fatalf("unmarshal full: %v", err)
	}
	if decodedFull.TargetTags == nil || len(*decodedFull.TargetTags) != 2 {
		t.Errorf("full target_tags decode failed: %v", decodedFull.TargetTags)
	}
}

func TestRevisionNullJSONDecodesAsLegacy(t *testing.T) {
	// Incoming JSON with explicit null should be preserved as nil
	raw := `{"version":1,"kind":"tag_edit","added_tags":["a"],"removed_tags":[],"target_tags":null,"revertible":false,"created_at":"2026-01-01"}`
	var rev Revision
	if err := json.Unmarshal([]byte(raw), &rev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rev.TargetTags != nil {
		t.Errorf("null should be nil, got %v", rev.TargetTags)
	}
	if rev.Revertible != false {
		t.Error("revertible should be false for legacy")
	}

	// Missing field historically would also be legacy; omitting target_tags should decode as nil in our struct (json missing => nil)
	rawMissing := `{"version":1,"kind":"tag_edit","added_tags":[],"removed_tags":[],"revertible":false,"created_at":"2026-01-01"}`
	var revMissing Revision
	if err := json.Unmarshal([]byte(rawMissing), &revMissing); err != nil {
		t.Fatalf("unmarshal missing: %v", err)
	}
	if revMissing.TargetTags != nil {
		t.Errorf("missing target_tags should be nil, got %v", revMissing.TargetTags)
	}
}

func TestGetPostRevisionsSQLPreservesNullSemantics(t *testing.T) {
	if !strings.Contains(GetPostRevisionsSQL, "target_tags IS NOT NULL") {
		t.Errorf("GetPostRevisionsSQL must preserve NULL versus empty via IS NOT NULL, got %q", GetPostRevisionsSQL)
	}
	if !strings.Contains(GetPostRevisionsSQL, "target_tags") {
		t.Errorf("GetPostRevisionsSQL must select target_tags, got %q", GetPostRevisionsSQL)
	}
}

func TestPostDetailJSONPreservesRevisionRevertible(t *testing.T) {
	empty := []string{}
	detail := PostDetail{
		PostSummary: PostSummary{ID: "2004", Tags: []string{"cat"}},
		TagVersion:  3,
		History: []Revision{
			{Version: 1, Kind: "tag_edit", AddedTags: []string{"cat"}, RemovedTags: []string{}, TargetTags: &[]string{"cat"}, Revertible: true, CreatedAt: "2026-01-01"},
			{Version: 2, Kind: "tag_edit", AddedTags: []string{}, RemovedTags: []string{"cat"}, TargetTags: &empty, Revertible: true, CreatedAt: "2026-01-02"},
			{Version: 0, Kind: "tag_edit", AddedTags: []string{}, RemovedTags: []string{}, TargetTags: nil, Revertible: false, CreatedAt: "2026-01-03"},
		},
	}
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	raw := string(data)
	if !strings.Contains(raw, `"target_tags":null`) {
		t.Errorf("detail should preserve null for legacy, got %s", raw)
	}
	if !strings.Contains(raw, `"target_tags":[]`) {
		t.Errorf("detail should preserve [] for empty, got %s", raw)
	}
	var decoded PostDetail
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if len(decoded.History) != 3 {
		t.Fatalf("expected 3 revisions, got %d", len(decoded.History))
	}
	if decoded.History[2].TargetTags != nil {
		t.Errorf("third revision should be legacy nil, got %v", decoded.History[2].TargetTags)
	}
	if decoded.History[1].TargetTags == nil || len(*decoded.History[1].TargetTags) != 0 {
		t.Errorf("second revision should be empty non-nil, got %v", decoded.History[1].TargetTags)
	}
}
