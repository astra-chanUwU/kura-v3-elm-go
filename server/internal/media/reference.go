package media

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SameOriginMediaPrefix is the URL prefix upload, derivative, and import
// writers persist when the browser must fetch media through this API.
// LocalStore uses it by default. Private S3 mode forces it so object URLs
// stay on the API origin.
const SameOriginMediaPrefix = "/media"

// PrivateLibraryEnabled reports KURA_PRIVATE_LIBRARY. Invalid or empty
// values are false; the server process rejects invalid values before it
// builds a store.
func PrivateLibraryEnabled() bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("KURA_PRIVATE_LIBRARY")))
	return err == nil && value
}

// PublishesExternalObjectURLs reports whether store.Put returns a URL the
// browser would load from outside this API. Same-origin /media URLs are not
// external. A nil or local store is not external.
func PublishesExternalObjectURLs(store Store) bool {
	s3, ok := store.(*S3Store)
	if !ok || s3 == nil {
		return false
	}
	return strings.TrimRight(strings.TrimSpace(s3.urlPrefix), "/") != SameOriginMediaPrefix
}

// KeyFromStoredMediaURL maps a media reference persisted by the local store
// or by private-mode S3 back to its object key. Both writers set Object.URL
// to SameOriginMediaPrefix + "/" + key after validateKey. External URLs,
// static icons, traversal, and encoded paths are rejected and must not be
// fetched.
func KeyFromStoredMediaURL(reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.ContainsRune(reference, '\x00') || strings.ContainsAny(reference, "\\?#") {
		return "", errNotStoreReference
	}
	if strings.Contains(reference, "://") || strings.HasPrefix(reference, "//") || strings.Contains(reference, "%") {
		return "", errNotStoreReference
	}
	prefix := SameOriginMediaPrefix + "/"
	if !strings.HasPrefix(reference, prefix) {
		return "", errNotStoreReference
	}
	key := strings.TrimPrefix(reference, prefix)
	if key == "" || strings.Contains(key, ":") || strings.HasPrefix(key, "/") || strings.Contains(key, "//") {
		return "", errNotStoreReference
	}
	if err := validateKey(key); err != nil {
		return "", err
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(key)))
	if clean != key {
		return "", errNotStoreReference
	}
	if prefix+key != reference {
		return "", errNotStoreReference
	}
	return key, nil
}

var errNotStoreReference = errors.New("media reference is not a store object")
