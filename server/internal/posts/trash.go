package posts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidTrash = errors.New("invalid trash request")

const (
	maxTrashPosts        = 100
	maxTrashReasonLength = 512
	trashCursorVersion   = 1
)

// TrashRequest is a bounded reversible trash or restore command. The actor
// is supplied separately from the request by the capability owner.
type TrashRequest struct {
	PostIDs []string `json:"post_ids"`
	Reason  string   `json:"reason,omitempty"`
}

// TrashResult describes whether a target changed state. Missing targets are
// returned as ErrNotFound so a batch remains atomic.
type TrashResult struct {
	ID      string `json:"id"`
	Changed bool   `json:"changed"`
}

type TrashResponse struct {
	Posts []TrashResult `json:"posts"`
}

// TrashPost carries display metadata for capability-protected recovery.
type TrashPost struct {
	PostSummary
	DeletedAt       string `json:"deleted_at"`
	ModerationState string `json:"moderation_state"`
}

// TrashCursor is the opaque keyset position for the deleted_at,id ordering.
type TrashCursor struct {
	DeletedAt time.Time
	ID        int64
}

type trashCursorPayload struct {
	Version   int       `json:"v"`
	DeletedAt time.Time `json:"d"`
	ID        int64     `json:"i"`
}

type TrashPage struct {
	Posts      []TrashPost `json:"posts"`
	NextCursor *string     `json:"next_cursor"`
}

// PostTrashStore is the independent storage capability for reversible trash.
type PostTrashStore interface {
	ListTrash(context.Context, string, int, *TrashCursor) (TrashPage, error)
	Trash(context.Context, string, TrashRequest) (TrashResponse, error)
	Restore(context.Context, string, TrashRequest) (TrashResponse, error)
}

func ValidateTrashRequest(request TrashRequest) (TrashRequest, error) {
	if len(request.PostIDs) == 0 || len(request.PostIDs) > maxTrashPosts {
		return TrashRequest{}, fmt.Errorf("%w: post_ids must contain 1 to %d ids", ErrInvalidTrash, maxTrashPosts)
	}
	seen := make(map[string]struct{}, len(request.PostIDs))
	ids := make([]string, len(request.PostIDs))
	for i, raw := range request.PostIDs {
		id := strings.TrimSpace(raw)
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 || id != strconv.FormatInt(parsed, 10) {
			return TrashRequest{}, fmt.Errorf("%w: post_ids[%d] must be a canonical positive integer", ErrInvalidTrash, i)
		}
		if _, ok := seen[id]; ok {
			return TrashRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidTrash, id)
		}
		seen[id] = struct{}{}
		ids[i] = id
	}
	reason := strings.TrimSpace(request.Reason)
	if utf8.RuneCountInString(reason) > maxTrashReasonLength {
		return TrashRequest{}, fmt.Errorf("%w: reason must be at most %d characters", ErrInvalidTrash, maxTrashReasonLength)
	}
	return TrashRequest{PostIDs: ids, Reason: reason}, nil
}

func ValidateTrashActor(actor string) (string, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || utf8.RuneCountInString(actor) > maxModerationActorLength {
		return "", fmt.Errorf("%w: actor must contain 1 to %d characters", ErrInvalidTrash, maxModerationActorLength)
	}
	return actor, nil
}

func ValidateTrashLimit(limit int) error {
	if limit < 1 || limit > 60 {
		return ErrInvalidQuery
	}
	return nil
}

func EncodeTrashCursor(deletedAt time.Time, id int64) (string, error) {
	if deletedAt.IsZero() || id <= 0 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(trashCursorPayload{Version: trashCursorVersion, DeletedAt: deletedAt.UTC(), ID: id})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeTrashCursor(token string) (TrashCursor, error) {
	if token == "" || len(token) > maxCursorLength {
		return TrashCursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return TrashCursor{}, ErrInvalidCursor
	}
	var payload trashCursorPayload
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return TrashCursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return TrashCursor{}, ErrInvalidCursor
	}
	if payload.Version != trashCursorVersion || payload.DeletedAt.IsZero() || payload.ID <= 0 {
		return TrashCursor{}, ErrInvalidCursor
	}
	return TrashCursor{DeletedAt: payload.DeletedAt.UTC(), ID: payload.ID}, nil
}
