package profiles

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MinUsernameLength = 1
	MaxUsernameLength = 32
	MaxDisplayName    = 50
	MaxBio            = 160
	MaxWebsite        = 200
)

var (
	ErrInvalidProfile  = errors.New("invalid profile")
	ErrInvalidUsername = errors.New("username must be 1-32 characters of letters, digits, '_' or '-'")
	ErrReservedName    = errors.New("username is reserved")
	ErrInvalidWebsite  = errors.New("website must be empty or an http(s) URL")
	ErrUsernameTaken   = errors.New("username is already in use")
	ErrNotFound        = errors.New("profile not found")
	ErrConflict        = errors.New("profile changed")
	ErrForbidden       = errors.New("you can only edit your own profile")
	ErrUnavailable     = errors.New("profiles unavailable")
	ErrAvatar          = errors.New("profile picture was rejected")
)

var reservedUsernames = map[string]struct{}{
	"local": {}, "system": {},
	"sign-in": {}, "sign-up": {}, "trouble": {},
	"api": {}, "media": {}, "health": {},
	"static": {}, "dist": {}, "u": {},
	"profile": {}, "profiles": {},
	"index": {}, "assets": {}, "icons": {}, "brand": {}, "favicon": {},
}

type Input struct {
	DisplayName     string `json:"display_name"`
	Username        string `json:"username"`
	Bio             string `json:"bio"`
	Website         string `json:"website"`
	ExpectedVersion int64  `json:"expected_version"`
}

type Profile struct {
	UserID      int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	Website     string `json:"website"`
	AvatarURL   string `json:"avatar_url"`
	Version     int64  `json:"version"`
	CanEdit     bool   `json:"can_edit"`
	AvatarKey   string `json:"-"`
}

func (p Profile) WithViewer(viewerUserID int64) Profile {
	p.CanEdit = viewerUserID > 0 && viewerUserID == p.UserID
	p.AvatarURL = ""
	if p.AvatarKey != "" && p.Username != "" && p.Version > 0 {
		p.AvatarURL = "/api/profiles/" + p.Username + "/avatar?v=" + fmt.Sprint(p.Version)
	}
	return p
}

func NormalizeUsername(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" || len(name) < MinUsernameLength || len(name) > MaxUsernameLength {
		return "", ErrInvalidUsername
	}
	for i, r := range name {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		extra := r == '_' || r == '-'
		if i == 0 && !alnum {
			return "", ErrInvalidUsername
		}
		if !alnum && !extra {
			return "", ErrInvalidUsername
		}
	}
	if _, reserved := reservedUsernames[name]; reserved {
		return "", ErrReservedName
	}
	return name, nil
}

func ValidPathSegment(raw string) bool {
	if raw == "" || len(raw) > MaxUsernameLength || strings.Contains(raw, "/") || strings.Contains(raw, "\\") {
		return false
	}
	for i, r := range raw {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		extra := r == '_' || r == '-'
		if i == 0 && !alnum {
			return false
		}
		if !alnum && !extra {
			return false
		}
	}
	return true
}

func NormalizeProfile(input Input) (Input, error) {
	if input.ExpectedVersion < 1 {
		return Input{}, fmt.Errorf("%w: expected_version", ErrInvalidProfile)
	}
	username, err := NormalizeUsername(input.Username)
	if err != nil {
		return Input{}, err
	}
	display, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return Input{}, err
	}
	bio, err := normalizeBio(input.Bio)
	if err != nil {
		return Input{}, err
	}
	website, err := NormalizeWebsite(input.Website)
	if err != nil {
		return Input{}, err
	}
	return Input{DisplayName: display, Username: username, Bio: bio, Website: website, ExpectedVersion: input.ExpectedVersion}, nil
}

func NormalizeWebsite(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > MaxWebsite || strings.ContainsAny(value, " \t\r\n") {
		return "", ErrInvalidWebsite
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" || strings.Contains(parsed.Host, "\\") {
		return "", ErrInvalidWebsite
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidWebsite
	}
	normalized := parsed.String()
	if len(normalized) > MaxWebsite {
		return "", ErrInvalidWebsite
	}
	return normalized, nil
}

func normalizeDisplayName(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if utf8.RuneCountInString(value) > MaxDisplayName {
		return "", fmt.Errorf("%w: display name", ErrInvalidProfile)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: display name", ErrInvalidProfile)
		}
	}
	return value, nil
}

func normalizeBio(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if utf8.RuneCountInString(value) > MaxBio {
		return "", fmt.Errorf("%w: bio", ErrInvalidProfile)
	}
	for _, r := range value {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: bio", ErrInvalidProfile)
		}
	}
	return value, nil
}
