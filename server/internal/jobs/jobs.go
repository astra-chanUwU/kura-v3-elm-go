package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Job statuses. Pending rows are claimable once next_retry_at is due;
// running rows are lease-held by one worker; the remaining three are
// terminal and carry completed_at.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

const (
	maxKindLength           = 120
	maxIdempotencyKeyLength = 256
	maxPayloadBytes         = 1 << 20
	defaultMaxAttempts      = 25
	maxMaxAttempts          = 100
)

// Backoff bounds for failed attempts. The delay after attempt N (1-based)
// is baseRetryDelay doubled N-1 times, capped at maxRetryDelay. No jitter:
// with a single in-process worker there is no thundering herd to spread.
const (
	baseRetryDelay = 5 * time.Second
	maxRetryDelay  = 10 * time.Minute
)

var (
	// ErrUnavailable indicates the store has no database pool. The HTTP
	// layer maps it to a service-unavailable response.
	ErrUnavailable = errors.New("jobs unavailable")
	// ErrInvalidJob indicates malformed enqueue input or a malformed id.
	ErrInvalidJob = errors.New("invalid job")
	// ErrNotFound indicates that no job row matches the request.
	ErrNotFound = errors.New("job not found")
	// ErrLeaseConflict indicates the caller no longer holds the row lease:
	// the row moved on, expired, or belongs to another worker.
	ErrLeaseConflict = errors.New("job lease conflict")
)

// Job is the durable row served by the status endpoint and owned by the
// in-process worker. Nullable lease and timestamp fields serialize as null
// so polling clients can distinguish "never attempted" from a timestamp.
type Job struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	IdempotencyKey string          `json:"idempotency_key"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	NextRetryAt    *time.Time      `json:"next_retry_at"`
	LastAttemptAt  *time.Time      `json:"last_attempt_at"`
	LockedBy       *string         `json:"locked_by"`
	LockedAt       *time.Time      `json:"locked_at"`
	LeaseExpiresAt *time.Time      `json:"lease_expires_at"`
	Progress       int             `json:"progress"`
	LastError      string          `json:"last_error"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at"`
}

// Reader serves one job row for status polling. Store implements it; the
// HTTP layer depends only on this interface.
type Reader interface {
	GetJob(ctx context.Context, id string) (Job, error)
}

// IsTerminal reports whether status ends the job lifecycle.
func IsTerminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// RetryDelay returns the backoff before the next attempt after attempt
// completed attempts (1-based). Attempt numbers below 1 use the base delay.
func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		return baseRetryDelay
	}
	delay := baseRetryDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

// ParseID validates a polling id: a canonical positive integer matching the
// BIGINT primary key. Surrounding whitespace is trimmed like the posts tag
// convention; leading zeros and negatives are rejected so ids round-trip
// exactly.
func ParseID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || id <= 0 || trimmed != strconv.FormatInt(id, 10) {
		return "", fmt.Errorf("%w: id must be a positive integer", ErrInvalidJob)
	}
	return strconv.FormatInt(id, 10), nil
}

// EnqueueRequest is the validated enqueue shape. Payload defaults to an
// empty object and MaxAttempts defaults to defaultMaxAttempts; callers pass
// their own idempotency key so retries of the same logical work collapse to
// one row per kind.
type EnqueueRequest struct {
	Kind           string
	Payload        json.RawMessage
	IdempotencyKey string
	MaxAttempts    int
}

// ValidateEnqueue normalizes enqueue input so invalid requests never open a
// transaction. It trims kind and key, defaults empty payload to '{}', and
// defaults non-positive MaxAttempts to defaultMaxAttempts.
func ValidateEnqueue(request EnqueueRequest) (EnqueueRequest, error) {
	kind := strings.TrimSpace(request.Kind)
	if kind == "" || len([]rune(kind)) > maxKindLength {
		return EnqueueRequest{}, fmt.Errorf("%w: kind must contain 1 to 120 characters", ErrInvalidJob)
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" || len([]rune(key)) > maxIdempotencyKeyLength {
		return EnqueueRequest{}, fmt.Errorf("%w: idempotency key must contain 1 to 256 characters", ErrInvalidJob)
	}
	payload := request.Payload
	if len(bytesTrimSpace(payload)) == 0 {
		payload = json.RawMessage("{}")
	}
	if len(payload) > maxPayloadBytes {
		return EnqueueRequest{}, fmt.Errorf("%w: payload exceeds 1MiB", ErrInvalidJob)
	}
	if !json.Valid(payload) {
		return EnqueueRequest{}, fmt.Errorf("%w: payload must be valid JSON", ErrInvalidJob)
	}
	maxAttempts := request.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	if maxAttempts > maxMaxAttempts {
		return EnqueueRequest{}, fmt.Errorf("%w: max attempts must be between 1 and 100", ErrInvalidJob)
	}
	return EnqueueRequest{Kind: kind, Payload: payload, IdempotencyKey: key, MaxAttempts: maxAttempts}, nil
}

// ValidateProgress rejects out-of-range progress reports before they reach
// storage.
func ValidateProgress(progress int) error {
	if progress < 0 || progress > 100 {
		return fmt.Errorf("%w: progress must be between 0 and 100", ErrInvalidJob)
	}
	return nil
}

func bytesTrimSpace(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}
