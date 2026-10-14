package posts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxMetadataPosts = 100
const maxMetadataValueLength = 512

var (
	// ErrInvalidMetadata indicates malformed source/artist mutation input.
	ErrInvalidMetadata = errors.New("invalid post metadata")
	// ErrMetadataConflict indicates that a metadata edit used a stale version.
	ErrMetadataConflict = errors.New("post metadata version conflict")
)

// UnmarshalJSON requires the version property to be present and non-null. A
// plain int would otherwise collapse a missing version into the valid zero.
func (target *MetadataTarget) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return fmt.Errorf("%w: invalid target", ErrInvalidMetadata)
	}
	for key := range fields {
		if key != "id" && key != "version" {
			return fmt.Errorf("%w: unknown target field %q", ErrInvalidMetadata, key)
		}
	}
	idRaw, ok := fields["id"]
	if !ok || json.Unmarshal(idRaw, &target.ID) != nil {
		return fmt.Errorf("%w: target id is required", ErrInvalidMetadata)
	}
	versionRaw, ok := fields["version"]
	if !ok || bytes.Equal(bytes.TrimSpace(versionRaw), []byte("null")) {
		return fmt.Errorf("%w: target version is required", ErrInvalidMetadata)
	}
	var version int
	if err := json.Unmarshal(versionRaw, &version); err != nil || version < 0 {
		return fmt.Errorf("%w: target version must be a non-negative integer", ErrInvalidMetadata)
	}
	target.Version = version
	return nil
}

// ValidateMetadataRequest checks the bounded request shape and canonicalizes
// source/artist whitespace consistently with upload metadata.
func ValidateMetadataRequest(request MetadataRequest) (MetadataRequest, error) {
	if len(request.Posts) == 0 || len(request.Posts) > maxMetadataPosts {
		return MetadataRequest{}, fmt.Errorf("%w: posts must contain 1 to 100 targets", ErrInvalidMetadata)
	}
	if request.Source == nil && request.Artist == nil {
		return MetadataRequest{}, fmt.Errorf("%w: source or artist is required", ErrInvalidMetadata)
	}
	seen := make(map[string]struct{}, len(request.Posts))
	for index, target := range request.Posts {
		id, err := strconv.ParseInt(strings.TrimSpace(target.ID), 10, 64)
		if err != nil || id <= 0 || strings.TrimSpace(target.ID) != strconv.FormatInt(id, 10) {
			return MetadataRequest{}, fmt.Errorf("%w: posts[%d].id must be a canonical positive integer", ErrInvalidMetadata, index)
		}
		if target.Version < 0 {
			return MetadataRequest{}, fmt.Errorf("%w: posts[%d].version must be non-negative", ErrInvalidMetadata, index)
		}
		if _, exists := seen[target.ID]; exists {
			return MetadataRequest{}, fmt.Errorf("%w: duplicate post id %q", ErrInvalidMetadata, target.ID)
		}
		seen[target.ID] = struct{}{}
	}
	for label, field := range map[string]*string{"source": request.Source, "artist": request.Artist} {
		if field == nil {
			continue
		}
		value := strings.TrimSpace(*field)
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxMetadataValueLength || strings.ContainsRune(value, '\x00') {
			return MetadataRequest{}, fmt.Errorf("%w: %s must be valid text of at most 512 characters", ErrInvalidMetadata, label)
		}
		if label == "source" {
			request.Source = &value
		} else {
			request.Artist = &value
		}
	}
	return request, nil
}
