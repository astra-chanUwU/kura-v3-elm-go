package jobs

import (
	"strings"
	"time"
)

// Public actions tell a caller what to do next. They are stable tokens, not
// sentences, so clients can branch without parsing last_error.
const (
	// ActionNone means the row is queued, succeeded, or cancelled, and no
	// failure is waiting on the caller.
	ActionNone = "none"
	// ActionWait means the job will be claimed again after next_retry_at.
	// Callers should not enqueue a second copy.
	ActionWait = "wait"
	// ActionIndexPost means a fingerprint row is terminally failed. The
	// existing similarity index request is the explicit retry; this view
	// does not add another one.
	ActionIndexPost = "index_post"
	// ActionThumbnailFailed means thumbnail generation is terminally failed.
	// The action makes no claim about whether the original is still available.
	ActionThumbnailFailed = "thumbnail_failed"
	// ActionStopped means some other kind is terminally failed.
	ActionStopped = "stopped"
)

// kindDerivative and kindFingerprint match media.KindDerivative and
// media.KindFingerprint. This package cannot import media.
const (
	kindDerivative  = "derivative"
	kindFingerprint = "fingerprint"
)

// PublicJob is the status payload. It omits the worker payload, idempotency
// key, and lease owner so object keys, content hashes, and host names stay
// on the server.
type PublicJob struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	Progress      int        `json:"progress"`
	NextRetryAt   *time.Time `json:"next_retry_at"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	LastError     string     `json:"last_error"`
	Action        string     `json:"action"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at"`
}

// safeSentences is the only text SanitizeError may return. Inputs are never
// copied into the result, including inputs that already look safe.
var safeSentences = map[string]struct{}{
	"The job failed.":                          {},
	"The file could not be read.":              {},
	"The file is not a supported image.":       {},
	"The image has no pixels.":                 {},
	"The file is empty or larger than 25 MiB.": {},
	"The thumbnail could not be encoded.":      {},
	"The thumbnail could not be stored.":       {},
	"The result could not be saved.":           {},
	"This file cannot be indexed.":             {},
	"The job data is invalid.":                 {},
	"Processing is not configured.":            {},
	"The worker stopped before this finished.": {},
	"Processing was interrupted.":              {},
}

// exactErrors maps known worker and lease sentences onto safeSentences.
// Both the historical raw sentence and the safe sentence are keys so a
// second pass is stable.
var exactErrors = map[string]string{
	"job failed":                                     "The job failed.",
	"The job failed.":                                "The job failed.",
	"lease expired without heartbeat":                "The worker stopped before this finished.",
	"The worker stopped before this finished.":       "The worker stopped before this finished.",
	"derivative processor is not configured":         "Processing is not configured.",
	"fingerprint processor is not configured":        "Processing is not configured.",
	"Processing is not configured.":                  "Processing is not configured.",
	"original must be between 1 byte and 25 MiB":     "The file is empty or larger than 25 MiB.",
	"The file is empty or larger than 25 MiB.":       "The file is empty or larger than 25 MiB.",
	"decode original: image has no pixels":           "The image has no pixels.",
	"The image has no pixels.":                       "The image has no pixels.",
	"derivative post_id must be a positive integer":  "The job data is invalid.",
	"fingerprint post_id must be a positive integer": "The job data is invalid.",
	"derivative original_key is required":            "The job data is invalid.",
	"fingerprint original_key is required":           "The job data is invalid.",
	"fingerprint original_hash must be sha256 followed by 64 lowercase hexadecimal characters": "The job data is invalid.",
	"invalid media object key":            "The job data is invalid.",
	"The job data is invalid.":            "The job data is invalid.",
	"context canceled":                    "Processing was interrupted.",
	"context deadline exceeded":           "Processing was interrupted.",
	"Processing was interrupted.":         "Processing was interrupted.",
	"The file could not be read.":         "The file could not be read.",
	"The file is not a supported image.":  "The file is not a supported image.",
	"The thumbnail could not be encoded.": "The thumbnail could not be encoded.",
	"The thumbnail could not be stored.":  "The thumbnail could not be stored.",
	"The result could not be saved.":      "The result could not be saved.",
	"This file cannot be indexed.":        "This file cannot be indexed.",
}

// prefixErrors classifies wrapped worker errors. The matched text is
// discarded; only the fixed sentence is kept.
var prefixErrors = []struct {
	prefix  string
	message string
}{
	{"open original:", "The file could not be read."},
	{"read original:", "The file could not be read."},
	{"media object not found:", "The file could not be read."},
	{"media object path is a directory:", "The file could not be read."},
	{"decode original:", "The file is not a supported image."},
	{"encode thumbnail:", "The thumbnail could not be encoded."},
	{"store thumbnail:", "The thumbnail could not be stored."},
	{"record derivative variant:", "The result could not be saved."},
	{"record fingerprint:", "The result could not be saved."},
	{"fingerprint unsupported for ", "This file cannot be indexed."},
	{"derivative variant ", "The job data is invalid."},
	{"fingerprint algorithm ", "The job data is invalid."},
	{"derivative payload must be valid JSON", "The job data is invalid."},
	{"fingerprint payload must be valid JSON", "The job data is invalid."},
}

// SanitizeError reduces a worker failure to one fixed sentence. Empty input
// becomes a generic failure. Text that is not a known class also becomes
// that generic failure, which drops paths, object keys, URLs, credentials,
// and stack traces instead of trimming them.
func SanitizeError(raw string) string {
	message := strings.TrimSpace(raw)
	if message == "" {
		return "The job failed."
	}
	if safe, ok := exactErrors[message]; ok {
		return mustSafe(safe)
	}
	for _, rule := range prefixErrors {
		if strings.HasPrefix(message, rule.prefix) {
			return mustSafe(rule.message)
		}
	}
	return "The job failed."
}

func mustSafe(message string) string {
	if _, ok := safeSentences[message]; ok {
		return message
	}
	return "The job failed."
}

// Public builds the status view. Succeeded and cancelled rows drop any
// earlier failure text. A non-empty last_error is sanitized again so rows
// written before sanitization, or by the lease sweep, cannot leak.
func (job Job) Public() PublicJob {
	return PublicJob{
		ID:            job.ID,
		Kind:          job.Kind,
		Status:        job.Status,
		Attempts:      job.Attempts,
		MaxAttempts:   job.MaxAttempts,
		Progress:      job.Progress,
		NextRetryAt:   job.NextRetryAt,
		LastAttemptAt: job.LastAttemptAt,
		LastError:     publicLastError(job),
		Action:        publicAction(job),
		CreatedAt:     job.CreatedAt,
		UpdatedAt:     job.UpdatedAt,
		CompletedAt:   job.CompletedAt,
	}
}

func publicLastError(job Job) string {
	switch job.Status {
	case StatusSucceeded, StatusCancelled:
		return ""
	}
	if strings.TrimSpace(job.LastError) == "" {
		return ""
	}
	return SanitizeError(job.LastError)
}

func publicAction(job Job) string {
	switch job.Status {
	case StatusSucceeded, StatusCancelled:
		return ActionNone
	case StatusRunning:
		return ActionWait
	case StatusPending:
		if job.Attempts > 0 || strings.TrimSpace(job.LastError) != "" {
			return ActionWait
		}
		return ActionNone
	case StatusFailed:
		switch job.Kind {
		case kindDerivative:
			return ActionThumbnailFailed
		case kindFingerprint:
			return ActionIndexPost
		default:
			return ActionStopped
		}
	default:
		return ActionStopped
	}
}
