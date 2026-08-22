package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldSpecs(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldSpecs(dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".spec/blueprint.md", ".arch/tasks.md"} {
		full := filepath.Join(dir, p)
		if _, err := os.Stat(full); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}
	// Idempotent: re-running must not error and must skip existing files.
	if err := scaffoldSpecs(dir); err != nil {
		t.Fatalf("second scaffold must be safe: %v", err)
	}
}
