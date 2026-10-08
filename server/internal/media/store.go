// Package media contains the durable object-storage boundary used by posts.
// The local implementation is the default development provider; remote
// providers can implement Store without changing HTTP or database contracts.
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Object is the stable result of storing one media object.
type Object struct {
	Key         string
	URL         string
	ContentType string
	Size        int64
	Created     bool
}

// Store owns durable media bytes. Keys are provider-relative and must use
// slash-separated paths. Put is idempotent for an existing key.
type Store interface {
	Put(ctx context.Context, key, contentType string, content io.Reader) (Object, error)
	Delete(ctx context.Context, key string) error
}

// LocalStore stores objects below Root and exposes them through URLPrefix.
// Objects are copied to a temporary file and atomically linked into place so
// a request cannot publish a partially copied object.
type LocalStore struct {
	Root      string
	URLPrefix string
}

func NewLocalStore(root string) *LocalStore {
	return &LocalStore{Root: root, URLPrefix: "/media"}
}

func (s *LocalStore) Put(ctx context.Context, key, contentType string, content io.Reader) (Object, error) {
	if err := validateKey(key); err != nil {
		return Object{}, err
	}
	if content == nil {
		return Object{}, errors.New("media content is required")
	}
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	path := filepath.Join(s.Root, filepath.FromSlash(key))
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return Object{}, fmt.Errorf("media object path is a directory: %s", key)
		}
		return s.object(key, contentType, info.Size(), false), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Object{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".object-*")
	if err != nil {
		return Object{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, content); err != nil {
		_ = temp.Close()
		return Object{}, err
	}
	if err := temp.Close(); err != nil {
		return Object{}, err
	}
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return Object{}, statErr
			}
			return s.object(key, contentType, info.Size(), false), nil
		}
		return Object{}, err
	}
	_ = os.Remove(tempPath)
	info, err := os.Stat(path)
	if err != nil {
		return Object{}, err
	}
	return s.object(key, contentType, info.Size(), true), nil
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(s.Root, filepath.FromSlash(key)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *LocalStore) object(key, contentType string, size int64, created bool) Object {
	prefix := strings.TrimRight(s.URLPrefix, "/")
	return Object{Key: key, URL: prefix + "/" + key, ContentType: contentType, Size: size, Created: created}
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" || strings.ContainsRune(key, '\x00') || filepath.IsAbs(key) {
		return errors.New("invalid media object key")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(key)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return errors.New("invalid media object key")
	}
	return nil
}
