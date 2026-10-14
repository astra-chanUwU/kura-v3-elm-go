package posts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
)

// DuplicateGroup is one exact-copy group: the canonical content hash, the
// visible member count, and up to three oldest-ID sample summaries.
type DuplicateGroup struct {
	Hash  string        `json:"hash"`
	Count int           `json:"count"`
	Posts []PostSummary `json:"posts"`
}

// DuplicateGroupsPage is the response body for GET /api/posts/duplicates.
// NextCursor is null when there is no next page. Groups are ordered by
// hash ASC.
type DuplicateGroupsPage struct {
	Groups     []DuplicateGroup `json:"groups"`
	NextCursor *string          `json:"next_cursor"`
}

// DuplicateMembersPage is the response body for
// GET /api/posts/duplicates/{hash}. Members are ordered by id ASC.
type DuplicateMembersPage struct {
	Hash       string        `json:"hash"`
	Count      int           `json:"count"`
	Posts      []PostSummary `json:"posts"`
	NextCursor *string       `json:"next_cursor"`
}

// DuplicateGroupsCursor is the opaque keyset position for the hash-ordered
// group listing. It carries only the last returned hash.
type DuplicateGroupsCursor struct {
	Hash string
}

// DuplicateMembersCursor is the opaque keyset position for one hash group.
// It is bound to its hash; a token issued for another hash or route fails.
type DuplicateMembersCursor struct {
	Hash string
	ID   int64
}

// DuplicateReader is the narrow storage boundary for the public exact-copy
// review reads. Both methods serve published, non-deleted posts only.
type DuplicateReader interface {
	ListDuplicateGroups(context.Context, int, *DuplicateGroupsCursor) (DuplicateGroupsPage, error)
	ListDuplicateMembers(context.Context, string, int, *DuplicateMembersCursor) (DuplicateMembersPage, error)
}

const (
	duplicateCursorVersion = 1
	duplicateGroupsRoute   = "dup-groups"
	duplicateMembersRoute  = "dup-members"

	maxDuplicateGroupsLimit  = 30
	defaultDuplicateGroups   = 20
	maxDuplicateMembersLimit = 60
	defaultDuplicateMembers  = 60
)

type duplicateGroupsCursorPayload struct {
	Version int    `json:"v"`
	Route   string `json:"r"`
	Hash    string `json:"h"`
}

type duplicateMembersCursorPayload struct {
	Version int    `json:"v"`
	Route   string `json:"r"`
	Hash    string `json:"h"`
	ID      int64  `json:"i"`
}

// EncodeDuplicateGroupsCursor encodes the page position after hash.
func EncodeDuplicateGroupsCursor(hash string) (string, error) {
	if !validCanonicalSHA256(hash) {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(duplicateGroupsCursorPayload{Version: duplicateCursorVersion, Route: duplicateGroupsRoute, Hash: hash})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeDuplicateGroupsCursor decodes a group-listing cursor. Member cursors
// and foreign routes fail with ErrInvalidCursor.
func DecodeDuplicateGroupsCursor(token string) (DuplicateGroupsCursor, error) {
	if token == "" || len(token) > maxCursorLength {
		return DuplicateGroupsCursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return DuplicateGroupsCursor{}, ErrInvalidCursor
	}
	var payload duplicateGroupsCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return DuplicateGroupsCursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return DuplicateGroupsCursor{}, ErrInvalidCursor
	}
	if payload.Version != duplicateCursorVersion || payload.Route != duplicateGroupsRoute {
		return DuplicateGroupsCursor{}, ErrInvalidCursor
	}
	if !validCanonicalSHA256(payload.Hash) {
		return DuplicateGroupsCursor{}, ErrInvalidCursor
	}
	return DuplicateGroupsCursor{Hash: payload.Hash}, nil
}

// EncodeDuplicateMembersCursor encodes the page position after (hash, id).
func EncodeDuplicateMembersCursor(hash string, id int64) (string, error) {
	if !validCanonicalSHA256(hash) || id <= 0 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(duplicateMembersCursorPayload{Version: duplicateCursorVersion, Route: duplicateMembersRoute, Hash: hash, ID: id})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeDuplicateMembersCursor decodes a member cursor issued for hash. A
// token bound to another hash or route fails with ErrInvalidCursor.
func DecodeDuplicateMembersCursor(token, hash string) (DuplicateMembersCursor, error) {
	if token == "" || len(token) > maxCursorLength {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	if !validCanonicalSHA256(hash) {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	var payload duplicateMembersCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	if payload.Version != duplicateCursorVersion || payload.Route != duplicateMembersRoute {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	if !validCanonicalSHA256(payload.Hash) || payload.Hash != hash {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	if payload.ID <= 0 {
		return DuplicateMembersCursor{}, ErrInvalidCursor
	}
	return DuplicateMembersCursor{Hash: payload.Hash, ID: payload.ID}, nil
}
