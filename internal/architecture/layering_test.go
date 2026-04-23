package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationDoesNotImportDatabaseAdapters(t *testing.T) {
	const (
		rootDir         = ".."
		applicationDir  = "../application"
		forbiddenImport = "\"stds_backend/internal/platform/database/"
		allowedImport   = "\"stds_backend/internal/platform/database/txrunner\""
	)

	fset := token.NewFileSet()
	err := filepath.Walk(applicationDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			if imported.Path.Value == allowedImport {
				continue
			}
			if strings.HasPrefix(imported.Path.Value, forbiddenImport) {
				relativePath, relErr := filepath.Rel(rootDir, path)
				if relErr != nil {
					relativePath = path
				}
				t.Errorf("%s imports forbidden database adapter package %s", relativePath, imported.Path.Value)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
}
