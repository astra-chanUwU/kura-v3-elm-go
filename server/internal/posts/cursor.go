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
	cursorVersion   = 1
	maxCursorLength = 512
)

var ErrInvalidCursor = errors.New("invalid search cursor")

// Cursor is the opaque keyset position for a query. IDs sort newest first.
type Cursor struct {
	Version   int
	QueryHash string
	ID        int64
}

type cursorPayload struct {
	Version int    `json:"v"`
	Query   string `json:"q"`
	ID      int64  `json:"i"`
}

func QueryHash(query string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return hex.EncodeToString(digest[:])
}

func EncodeCursor(query string, id int64) (string, error) {
	if id <= 0 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursorPayload{Version: cursorVersion, Query: QueryHash(query), ID: id})
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
	if payload.Version != cursorVersion || payload.ID <= 0 || len(payload.Query) != sha256.Size*2 || payload.Query != QueryHash(query) {
		return Cursor{}, ErrInvalidCursor
	}
	if _, err := hex.DecodeString(payload.Query); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{Version: payload.Version, QueryHash: payload.Query, ID: payload.ID}, nil
}

func (c Cursor) String() string {
	return strconv.FormatInt(c.ID, 10)
}
