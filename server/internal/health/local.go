package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// LocalFiles checks objects under root. Errors stay generic so a caller
// cannot learn the storage path from a failure.
type LocalFiles struct {
	Root string
}

func (l LocalFiles) Present(ctx context.Context, key string) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if strings.TrimSpace(key) == "" {
		return 0, false, nil
	}
	path, err := objectPath(l.Root, key)
	if err != nil {
		return 0, false, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, errBadObject
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return 0, false, nil
	}
	return info.Size(), true, nil
}

var errBadObject = errString("media check failed")

type errString string

func (e errString) Error() string { return string(e) }

func objectPath(root, key string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return "", errBadObject
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, `\`) || strings.Contains(key, "..") || strings.HasPrefix(key, "/") || strings.ContainsRune(key, 0) {
		return "", errBadObject
	}
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(key)))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errBadObject
	}
	return path, nil
}
