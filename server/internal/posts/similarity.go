package posts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SimilarityAlgorithm is the frozen algorithm id served on every
// similarity response, job payload, and fingerprint row.
const SimilarityAlgorithm = "dhash-v1"

const (
	// DefaultSimilarDistance is the frozen public default max Hamming
	// distance for GET /api/posts/{id}/similar.
	DefaultSimilarDistance = 8
	// MaxSimilarDistance bounds the distance parameter (0..16). Wider
	// radii stop meaning "near copy" and would widen the candidate scan.
	MaxSimilarDistance = 16
	// DefaultSimilarLimit and MaxSimilarLimit bound the public page
	// (1..60, default 30).
	DefaultSimilarLimit = 30
	MaxSimilarLimit     = 60
	// MaxSimilarityIndexPosts bounds one protected index batch (<=100
	// canonical distinct ids).
	MaxSimilarityIndexPosts = 100
	// DefaultSimilarityStatusLimit and MaxSimilarityStatusLimit bound the
	// protected status page (cap 100).
	DefaultSimilarityStatusLimit = 100
	MaxSimilarityStatusLimit     = 100
)

// SimilarMatch pairs one visible candidate with its Hamming distance to
// the anchor. Fingerprint hashes never leave the server: only the post
// summary and the integer distance are exposed.
type SimilarMatch struct {
	Post     PostSummary `json:"post"`
	Distance int         `json:"distance"`
}

// SimilarPage is the response body for GET /api/posts/{id}/similar.
// NextCursor is null when there is no next page. Indexed is false when
// the anchor is visible but has no current fingerprint (not yet indexed
// or stale); matches are empty then.
type SimilarPage struct {
	PostID     string         `json:"post_id"`
	Algorithm  string         `json:"algorithm"`
	Indexed    bool           `json:"indexed"`
	Matches    []SimilarMatch `json:"matches"`
	NextCursor *string        `json:"next_cursor"`
}

// SimilarCursor is the opaque keyset position for a similarity listing.
// It is bound to the anchor, the anchor's original content hash, the
// algorithm, and the requested max distance: a token issued for another
// anchor, another original, or another radius fails.
type SimilarCursor struct {
	AnchorID     string
	AnchorHash   string
	Algorithm    string
	MaxDistance  int
	LastDistance int
	LastID       int64
}

// SimilarityStatusItem is one eligible post's index state. It carries no
// media URLs and no hashes: only identity, index presence, and the
// backing job pointer for the explicit index tool.
type SimilarityStatusItem struct {
	ID      string  `json:"id"`
	Indexed bool    `json:"indexed"`
	JobID   *string `json:"job_id"`
	Status  *string `json:"status"`
}

// SimilarityStatusPage is the response body for
// GET /api/posts/similarity/status. It pages ALL eligible visible image
// posts by id ascending.
type SimilarityStatusPage struct {
	Algorithm     string                 `json:"algorithm"`
	IndexedCount  int                    `json:"indexed_count"`
	EligibleCount int                    `json:"eligible_count"`
	Posts         []SimilarityStatusItem `json:"posts"`
	NextCursor    *string                `json:"next_cursor"`
}

// SimilarityStatusCursor is the opaque keyset position for the status
// listing: the last post id of the previous page.
type SimilarityStatusCursor struct {
	LastID int64
}

// SimilarityIndexResult reports one enqueued index job.
type SimilarityIndexResult struct {
	ID     string `json:"id"`
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// SimilarityIndexResponse is the response body for
// POST /api/posts/similarity/index.
type SimilarityIndexResponse struct {
	Posts []SimilarityIndexResult `json:"posts"`
}

// SimilarityReader is the narrow storage boundary for the public
// near-copy read. It serves published, non-deleted posts only. The cursor
// travels as the opaque token: the backend binds it to the anchor, the
// anchor's original hash, and the radius after reading the anchor row.
type SimilarityReader interface {
	ListSimilarPosts(ctx context.Context, anchorID string, maxDistance, limit int, cursor string) (SimilarPage, error)
}

// SimilarityIndexer is the narrow storage boundary for the protected
// index controls. IndexSimilarityPosts validates every id before
// enqueuing any job; GetSimilarityStatus pages every eligible post.
type SimilarityIndexer interface {
	IndexSimilarityPosts(ctx context.Context, ids []string) (SimilarityIndexResponse, error)
	GetSimilarityStatus(ctx context.Context, limit int, cursor string) (SimilarityStatusPage, error)
}

const (
	similarityCursorVersion = 1
	similarityRoute         = "sim"
	similarityStatusRoute   = "sim-status"
)

type similarCursorPayload struct {
	Version     int    `json:"v"`
	Route       string `json:"r"`
	AnchorID    string `json:"a"`
	AnchorHash  string `json:"h"`
	Algorithm   string `json:"alg"`
	MaxDistance int    `json:"d"`
	LastDist    int    `json:"ld"`
	LastID      int64  `json:"i"`
}

type similarityStatusCursorPayload struct {
	Version int    `json:"v"`
	Route   string `json:"r"`
	LastID  int64  `json:"i"`
}

// EncodeSimilarCursor encodes the page position after
// (lastDistance, lastID), bound to the anchor, its original hash, the
// algorithm, and the requested radius.
func EncodeSimilarCursor(anchorID, anchorHash string, maxDistance, lastDistance int, lastID int64) (string, error) {
	if !isCanonicalPostID(anchorID) || !validCanonicalSHA256(anchorHash) {
		return "", ErrInvalidCursor
	}
	if maxDistance < 0 || maxDistance > MaxSimilarDistance || lastDistance < 0 || lastDistance > maxDistance || lastID <= 0 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(similarCursorPayload{
		Version:     similarityCursorVersion,
		Route:       similarityRoute,
		AnchorID:    anchorID,
		AnchorHash:  anchorHash,
		Algorithm:   SimilarityAlgorithm,
		MaxDistance: maxDistance,
		LastDist:    lastDistance,
		LastID:      lastID,
	})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeSimilarCursor decodes a cursor issued for this anchor, original
// hash, and radius. A token bound to another anchor, hash, algorithm, or
// radius fails with ErrInvalidCursor.
func DecodeSimilarCursor(token, anchorID, anchorHash string, maxDistance int) (SimilarCursor, error) {
	if token == "" || len(token) > maxCursorLength {
		return SimilarCursor{}, ErrInvalidCursor
	}
	if !isCanonicalPostID(anchorID) || !validCanonicalSHA256(anchorHash) {
		return SimilarCursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return SimilarCursor{}, ErrInvalidCursor
	}
	var payload similarCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return SimilarCursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return SimilarCursor{}, ErrInvalidCursor
	}
	if payload.Version != similarityCursorVersion || payload.Route != similarityRoute {
		return SimilarCursor{}, ErrInvalidCursor
	}
	if payload.AnchorID != anchorID || payload.AnchorHash != anchorHash {
		return SimilarCursor{}, ErrInvalidCursor
	}
	if payload.Algorithm != SimilarityAlgorithm || payload.MaxDistance != maxDistance {
		return SimilarCursor{}, ErrInvalidCursor
	}
	if payload.MaxDistance < 0 || payload.MaxDistance > MaxSimilarDistance {
		return SimilarCursor{}, ErrInvalidCursor
	}
	if payload.LastDist < 0 || payload.LastDist > payload.MaxDistance || payload.LastID <= 0 {
		return SimilarCursor{}, ErrInvalidCursor
	}
	return SimilarCursor{
		AnchorID:     payload.AnchorID,
		AnchorHash:   payload.AnchorHash,
		Algorithm:    payload.Algorithm,
		MaxDistance:  payload.MaxDistance,
		LastDistance: payload.LastDist,
		LastID:       payload.LastID,
	}, nil
}

// EncodeSimilarityStatusCursor encodes the status page position after
// lastID.
func EncodeSimilarityStatusCursor(lastID int64) (string, error) {
	if lastID <= 0 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(similarityStatusCursorPayload{Version: similarityCursorVersion, Route: similarityStatusRoute, LastID: lastID})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeSimilarityStatusCursor decodes a status cursor. Tokens from other
// routes fail with ErrInvalidCursor.
func DecodeSimilarityStatusCursor(token string) (SimilarityStatusCursor, error) {
	if token == "" || len(token) > maxCursorLength {
		return SimilarityStatusCursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return SimilarityStatusCursor{}, ErrInvalidCursor
	}
	var payload similarityStatusCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return SimilarityStatusCursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return SimilarityStatusCursor{}, ErrInvalidCursor
	}
	if payload.Version != similarityCursorVersion || payload.Route != similarityStatusRoute || payload.LastID <= 0 {
		return SimilarityStatusCursor{}, ErrInvalidCursor
	}
	return SimilarityStatusCursor{LastID: payload.LastID}, nil
}

// ValidateSimilarParams bounds the public distance/limit pair. Distance
// defaults to 8 and limit to 30 when the caller omits them (present=false).
func ValidateSimilarParams(distance int, distancePresent bool, limit int, limitPresent bool) (int, int, error) {
	if !distancePresent {
		distance = DefaultSimilarDistance
	}
	if !limitPresent {
		limit = DefaultSimilarLimit
	}
	if distance < 0 || distance > MaxSimilarDistance {
		return 0, 0, fmt.Errorf("%w: distance must be an integer from 0 to 16", ErrInvalidQuery)
	}
	if limit < 1 || limit > MaxSimilarLimit {
		return 0, 0, fmt.Errorf("%w: limit must be an integer from 1 to 60", ErrInvalidQuery)
	}
	return distance, limit, nil
}

// ValidateSimilarityIndexIDs normalizes the protected batch: 1 to 100
// canonical distinct ids. Visibility is checked later against the
// database; malformed ids fail here as invalid input.
func ValidateSimilarityIndexIDs(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > MaxSimilarityIndexPosts {
		return nil, fmt.Errorf("%w: post_ids must contain 1 to 100 ids", ErrInvalidQuery)
	}
	seen := make(map[string]struct{}, len(ids))
	clean := make([]string, 0, len(ids))
	for index, raw := range ids {
		id := strings.TrimSpace(raw)
		if !isCanonicalPostID(id) {
			return nil, fmt.Errorf("%w: post_ids[%d] must be a positive integer", ErrInvalidQuery, index)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("%w: duplicate post id %q", ErrInvalidQuery, id)
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	return clean, nil
}

// ValidateSimilarityStatusLimit bounds the protected status page (cap 100,
// default 100 when omitted).
func ValidateSimilarityStatusLimit(limit int, present bool) (int, error) {
	if !present {
		return DefaultSimilarityStatusLimit, nil
	}
	if limit < 1 || limit > MaxSimilarityStatusLimit {
		return 0, fmt.Errorf("%w: limit must be an integer from 1 to 100", ErrInvalidQuery)
	}
	return limit, nil
}

// isCanonicalPostID reports whether raw is a canonical positive integer id
// (no whitespace, no leading zeros, no sign).
func isCanonicalPostID(raw string) bool {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 || raw != strconv.FormatInt(parsed, 10) {
		return false
	}
	return true
}
