package posts

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	cursorVersion       = 2
	legacyCursorVersion = 1
	maxCursorLength     = 512
)

var ErrInvalidCursor = errors.New("invalid search cursor")

// Cursor is the opaque keyset position for a query. Score ordering stores the
// complete (score,id) key; newest ordering stores id. QueryHash binds a token
// to the exact query, including its order expression.
type Cursor struct {
	Version   int
	QueryHash string
	ID        int64
	Order     SortOrder
	Score     int
	HasScore  bool
}

type cursorPayload struct {
	Version int    `json:"v"`
	Query   string `json:"q"`
	ID      int64  `json:"i"`
	Order   string `json:"o,omitempty"`
	Score   *int   `json:"s,omitempty"`
}

func QueryHash(query string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return hex.EncodeToString(digest[:])
}

// EncodeCursor retains the original newest-by-id API.
func EncodeCursor(query string, id int64) (string, error) {
	return EncodeSortCursor(query, SortNewest, 0, id)
}

// EncodeSortCursor encodes a sort-aware keyset position. Score is used only
// for score ordering; zero is a valid score key.
func EncodeSortCursor(query string, order SortOrder, score int, id int64) (string, error) {
	if id <= 0 || (order != SortNewest && order != SortScore) {
		return "", ErrInvalidCursor
	}
	var key *int
	if order == SortScore {
		value := score
		key = &value
	}
	payload, err := json.Marshal(cursorPayload{Version: cursorVersion, Query: QueryHash(query), ID: id, Order: string(order), Score: key})
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeCursor(token string, query string) (Cursor, error) {
	if token == "" || len(token) > maxCursorLength {
		return Cursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return Cursor{}, ErrInvalidCursor
	}
	var payload cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Cursor{}, ErrInvalidCursor
	}
	if payload.Query != QueryHash(query) || payload.ID <= 0 || len(payload.Query) != sha256.Size*2 {
		return Cursor{}, ErrInvalidCursor
	}
	if _, err := hex.DecodeString(payload.Query); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	if payload.Version == legacyCursorVersion {
		if payload.Order != "" || payload.Score != nil {
			return Cursor{}, ErrInvalidCursor
		}
		return Cursor{Version: payload.Version, QueryHash: payload.Query, ID: payload.ID, Order: SortNewest}, nil
	}
	if payload.Version != cursorVersion || (payload.Order != string(SortNewest) && payload.Order != string(SortScore)) {
		return Cursor{}, ErrInvalidCursor
	}
	cursor := Cursor{Version: payload.Version, QueryHash: payload.Query, ID: payload.ID, Order: SortOrder(payload.Order)}
	if cursor.Order == SortScore {
		if payload.Score == nil {
			return Cursor{}, ErrInvalidCursor
		}
		cursor.Score, cursor.HasScore = *payload.Score, true
	} else if payload.Score != nil {
		return Cursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func (c Cursor) String() string { return strconv.FormatInt(c.ID, 10) }
