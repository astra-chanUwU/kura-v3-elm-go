package posts

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
)

// collectionCursorVersion is the only accepted payload version for
// collection cursors. It is independent of the search cursor versions so
// either scheme can evolve without invalidating the other.
const collectionCursorVersion = 1

// CollectionCursor is the opaque keyset position for one collection page.
// It binds the collection ID, the membership/order version the issuing
// snapshot observed, and the (position, post_id) key of the last returned
// row. Ordering is deterministic position then post_id, so ties share one
// total order across pages.
type CollectionCursor struct {
	CollectionID int64
	Version      int64
	Position     int64
	PostID       int64
}

type collectionCursorPayload struct {
	Version    int   `json:"v"`
	Collection int64 `json:"c"`
	CVersion   int64 `json:"cv"`
	Position   int64 `json:"p"`
	PostID     int64 `json:"i"`
}

// EncodeCollectionCursor encodes the page position after (position, postID)
// at the given collection version. Position zero is a valid key: the first
// row of a fresh collection sits at position zero.
func EncodeCollectionCursor(collectionID, version, position, postID int64) (string, error) {
	if collectionID <= 0 || version < 0 || position < 0 || postID <= 0 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(collectionCursorPayload{
		Version:    collectionCursorVersion,
		Collection: collectionID,
		CVersion:   version,
		Position:   position,
		PostID:     postID,
	})
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeCollectionCursor decodes a cursor issued for collectionID. A token
// bound to another collection is indistinguishable from a malformed token:
// both fail with ErrInvalidCursor so callers answer 400. A well-formed
// token carrying an older version decodes successfully; the reader compares
// versions against its snapshot and reports ErrCollectionChanged.
func DecodeCollectionCursor(token string, collectionID int64) (CollectionCursor, error) {
	if token == "" || len(token) > maxCursorLength || collectionID <= 0 {
		return CollectionCursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return CollectionCursor{}, ErrInvalidCursor
	}
	var payload collectionCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return CollectionCursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return CollectionCursor{}, ErrInvalidCursor
	}
	if payload.Version != collectionCursorVersion {
		return CollectionCursor{}, ErrInvalidCursor
	}
	if payload.Collection != collectionID {
		return CollectionCursor{}, ErrInvalidCursor
	}
	if payload.CVersion < 0 || payload.Position < 0 || payload.PostID <= 0 {
		return CollectionCursor{}, ErrInvalidCursor
	}
	return CollectionCursor{
		CollectionID: payload.Collection,
		Version:      payload.CVersion,
		Position:     payload.Position,
		PostID:       payload.PostID,
	}, nil
}
