package posts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	defaultProfileListLimit = 12
	maxProfileListLimit     = 24
)

// ProfileList is one collection owned by an account, with a visible-media
// cover and a visible-item count. Hidden and trashed posts are omitted.
type ProfileList struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	VisibleCount int64  `json:"visible_count"`
	CoverURL     string `json:"cover_url"`
}

// ProfileListPage is a keyset page of one account's lists.
type ProfileListPage struct {
	Lists      []ProfileList `json:"lists"`
	NextCursor *string       `json:"next_cursor"`
}

// ProfileListCursor continues after a (created_at, id) key for one account.
type ProfileListCursor struct {
	UserID    int64     `json:"u"`
	CreatedAt time.Time `json:"t"`
	ID        int64     `json:"i"`
}

// ProfileListReader lists collections stamped with an account id.
type ProfileListReader interface {
	ListProfileLists(context.Context, int64, *ProfileListCursor, int) (ProfileListPage, error)
}

// NormalizeProfileListLimit bounds a requested page size.
func NormalizeProfileListLimit(limit int, supplied bool) (int, error) {
	if !supplied {
		return defaultProfileListLimit, nil
	}
	if limit < 1 || limit > maxProfileListLimit {
		return 0, ErrInvalidQuery
	}
	return limit, nil
}

// EncodeProfileListCursor serializes a continuation token.
func EncodeProfileListCursor(cursor ProfileListCursor) (string, error) {
	if cursor.UserID <= 0 || cursor.ID <= 0 || cursor.CreatedAt.IsZero() {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeProfileListCursor rejects tokens that belong to another account.
func DecodeProfileListCursor(token string, userID int64) (ProfileListCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return ProfileListCursor{}, ErrInvalidCursor
	}
	var cursor ProfileListCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return ProfileListCursor{}, ErrInvalidCursor
	}
	if cursor.UserID != userID || cursor.ID <= 0 || cursor.CreatedAt.IsZero() {
		return ProfileListCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func (s *PostgresBackend) ListProfileLists(ctx context.Context, userID int64, cursor *ProfileListCursor, limit int) (ProfileListPage, error) {
	if s == nil || s.pool == nil {
		return ProfileListPage{}, ErrUnavailable
	}
	if userID <= 0 {
		return ProfileListPage{}, ErrNotFound
	}
	if limit < 1 || limit > maxProfileListLimit {
		return ProfileListPage{}, ErrInvalidQuery
	}
	if cursor != nil && cursor.UserID != userID {
		return ProfileListPage{}, ErrInvalidCursor
	}
	var cursorAt any
	var cursorID any
	if cursor != nil {
		cursorAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := s.pool.Query(ctx, listProfileListsSQL, userID, cursorAt, cursorID, limit+1)
	if err != nil {
		return ProfileListPage{}, err
	}
	defer rows.Close()
	type listed struct {
		id        int64
		name      string
		createdAt time.Time
		visible   int64
		coverID   *string
		coverURL  *string
	}
	found := make([]listed, 0, limit+1)
	for rows.Next() {
		var row listed
		if err := rows.Scan(&row.id, &row.name, &row.createdAt, &row.visible, &row.coverID, &row.coverURL); err != nil {
			return ProfileListPage{}, err
		}
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		return ProfileListPage{}, err
	}
	hasMore := len(found) > limit
	if hasMore {
		found = found[:limit]
	}
	covers := make([]PostSummary, 0, len(found))
	for _, row := range found {
		if row.coverID == nil || row.coverURL == nil {
			continue
		}
		covers = append(covers, PostSummary{ID: *row.coverID, PreviewURL: *row.coverURL})
	}
	if err := s.overlayReadyPreviews(ctx, covers); err != nil {
		return ProfileListPage{}, err
	}
	ready := make(map[string]string, len(covers))
	for _, cover := range covers {
		ready[cover.ID] = cover.PreviewURL
	}
	page := ProfileListPage{Lists: make([]ProfileList, 0, len(found))}
	for _, row := range found {
		item := ProfileList{ID: strconv.FormatInt(row.id, 10), Name: row.name, VisibleCount: row.visible}
		if row.coverID != nil {
			item.CoverURL = safeProfileCover(ready[*row.coverID])
		}
		page.Lists = append(page.Lists, item)
	}
	if hasMore && len(found) > 0 {
		last := found[len(found)-1]
		token, err := EncodeProfileListCursor(ProfileListCursor{UserID: userID, CreatedAt: last.createdAt, ID: last.id})
		if err != nil {
			return ProfileListPage{}, err
		}
		page.NextCursor = &token
	}
	return page, nil
}

func safeProfileCover(raw string) string {
	if strings.HasPrefix(raw, "/media/") && !strings.Contains(raw, "\\") && !strings.Contains(raw, "..") && !strings.Contains(raw, "//") {
		return raw
	}
	return ""
}
