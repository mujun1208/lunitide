package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Check all build variants, not just the current host's selected files. Domain
// rules and resource workers must stay usable without importing engine wiring.
func TestProductionDependencyDirection(t *testing.T) {
	root := filepath.Join("..")
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		layer := strings.Split(relative, "/")[0]
		source, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		count++
		for _, ref := range source.Imports {
			target, err := strconv.Unquote(ref.Path.Value)
			if err != nil {
				return err
			}
			internal := strings.TrimPrefix(target, "github.com/lunitide/lunitide/internal/")
			if internal == target {
				continue
			}
			targetLayer := strings.Split(internal, "/")[0]
			switch {
			case layer == "domain" && targetLayer != "domain":
				t.Errorf("%s: domain imports implementation %s", relative, internal)
			case layer != "app" && strings.HasSuffix(layer, "app") && (targetLayer == "app" || targetLayer == "bootstrap" || targetLayer == "storage"):
				t.Errorf("%s: application service imports outer layer %s", relative, internal)
			case layer == "commandworker" || layer == "doctext" || layer == "browsernetwork":
				switch targetLayer {
				case "app", "bootstrap", "storage", "secret", "secretlease", "providerapp":
					t.Errorf("%s: resource worker imports privileged engine service %s", relative, internal)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 100 {
		t.Fatalf("architecture scan incomplete: %d files", count)
	}
	t.Logf("checked %d production Go files across all build variants", count)
}
