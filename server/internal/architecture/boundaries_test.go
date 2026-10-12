package architecture

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/astra-chanUwU/kura-v3-elm-go/server"

func TestBackendImportBoundaries(t *testing.T) {
	rules := []struct {
		name        string
		directories []string
		forbidden   func(string) bool
	}{
		{
			name:        "httpapi does not own database or pgx imports",
			directories: []string{"httpapi"},
			forbidden: func(path string) bool {
				return path == "database/sql" || hasImportPrefix(path, "github.com/jackc/pgx")
			},
		},
		{
			name:        "domain packages do not depend on chi or httpapi",
			directories: []string{"posts", "auth", "jobs", "media"},
			forbidden: func(path string) bool {
				return hasImportPrefix(path, "github.com/go-chi/chi") || hasImportPrefix(path, modulePath+"/internal/httpapi")
			},
		},
	}

	for _, rule := range rules {
		t.Run(rule.name, func(t *testing.T) {
			for _, directory := range rule.directories {
				dir := filepath.Clean(filepath.Join("..", directory))
				err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
						return nil
					}
					file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
					if err != nil {
						return fmt.Errorf("parse %s imports: %w", path, err)
					}
					for _, imported := range file.Imports {
						importPath, err := strconv.Unquote(imported.Path.Value)
						if err != nil {
							return fmt.Errorf("parse import in %s: %w", path, err)
						}
						if rule.forbidden(importPath) {
							t.Errorf("%s imports forbidden package %q", path, importPath)
						}
					}
					return nil
				})
				if err != nil {
					t.Errorf("scan %s: %v", dir, err)
				}
			}
		})
	}
}

func hasImportPrefix(importPath, prefix string) bool {
	return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
}
