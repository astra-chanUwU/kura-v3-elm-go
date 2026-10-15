package jobs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSanitizeErrorDropsSecretsAndStaysIdempotent(t *testing.T) {
	samples := []string{
		"",
		"job failed",
		"open original: media object not found: uploads/s6secretpath.png",
		`read original: open C:\libraries\s6secretpath\photo.png: Access is denied`,
		"store thumbnail: s3 put derivatives/thumb-320/s6secretpath.jpg: 403 <Code>SignatureDoesNotMatch</Code> password=s6hunter2",
		"record fingerprint: postgres://postgres:s6hunter2@127.0.0.1/kura",
		"fingerprint unsupported for video/mp4: dhash-v1 covers images only, video is excluded",
		"decode original: image has no pixels",
		"lease expired without heartbeat",
		"goroutine 7 [running]:\nserver/internal/media/derivative.go:141 +0x2a s6secretpath",
		"The file could not be read.",
	}
	for _, sample := range samples {
		got := SanitizeError(sample)
		if _, ok := safeSentences[got]; !ok {
			t.Fatalf("output %q is not an allowlisted sentence", got)
		}
		if strings.Contains(got, "s6secretpath") || strings.Contains(got, "s6hunter2") || strings.Contains(got, "goroutine") || strings.Contains(got, ".go:") {
			t.Fatalf("sanitized %q still exposes the input", got)
		}
		if again := SanitizeError(got); again != got {
			t.Fatalf("second pass changed %q to %q", got, again)
		}
	}
}

func TestPublicJobOmitsPayloadAndNamesTheNextAction(t *testing.T) {
	when := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	next := when.Add(5 * time.Second)
	rawKey := "uploads/s6secretpath.png"
	job := Job{
		ID:             "7",
		Kind:           kindDerivative,
		Payload:        json.RawMessage(`{"original_key":"` + rawKey + `","original_hash":"sha256:s6hunter2"}`),
		Status:         StatusPending,
		IdempotencyKey: "derivative:7:thumb-320:s6hunter2",
		Attempts:       1,
		MaxAttempts:    25,
		NextRetryAt:    &next,
		LastError:      "open original: media object not found: " + rawKey,
		CreatedAt:      when,
		UpdatedAt:      when,
	}
	owner := "worker-host-s6secretpath"
	job.LockedBy = &owner

	public := job.Public()
	if public.Action != ActionWait || public.LastError != "The file could not be read." {
		t.Fatalf("pending failure view = %#v", public)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, leaked := range []string{"s6secretpath", "s6hunter2", "original_key", "idempotency_key", "locked_by", "payload"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("public status contains %q: %s", leaked, body)
		}
	}

	job.Status = StatusFailed
	job.Attempts = job.MaxAttempts
	job.NextRetryAt = nil
	failed := job.Public()
	if failed.Action != ActionOriginalSaved || failed.LastError != "The file could not be read." {
		t.Fatalf("terminal thumbnail view = %#v", failed)
	}

	job.Kind = kindFingerprint
	if job.Public().Action != ActionIndexPost {
		t.Fatalf("terminal fingerprint action = %q", job.Public().Action)
	}

	job.Status = StatusSucceeded
	job.LastError = "open original: " + rawKey
	if succeeded := job.Public(); succeeded.Action != ActionNone || succeeded.LastError != "" {
		t.Fatalf("succeeded view kept a failure: %#v", succeeded)
	}

	fresh := Job{ID: "1", Kind: kindDerivative, Status: StatusPending, MaxAttempts: 25}
	if fresh.Public().Action != ActionNone || fresh.Public().LastError != "" {
		t.Fatalf("unattempted job looks failed: %#v", fresh.Public())
	}
}
