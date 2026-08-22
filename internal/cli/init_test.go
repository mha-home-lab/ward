package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldWorkflow(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldWorkflow(dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "workflows", "default.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected %s to exist: %v", p, err)
	}
	// Idempotent: re-running must not error and must skip the existing file.
	if err := scaffoldWorkflow(dir); err != nil {
		t.Fatalf("second scaffold must be safe: %v", err)
	}
}

func TestScaffoldDocsGated(t *testing.T) {
	dir := t.TempDir()
	// scaffoldWorkflow does NOT write markdown by default.
	if err := scaffoldWorkflow(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".spec", "blueprint.md")); err == nil {
		t.Fatal(".spec/blueprint.md must NOT be written by --scaffold")
	}
	// --docs writes the markdown skeletons.
	if err := scaffoldDocs(dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".spec/blueprint.md", ".arch/tasks.md"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}
	// Idempotent.
	if err := scaffoldDocs(dir); err != nil {
		t.Fatalf("second docs scaffold must be safe: %v", err)
	}
}
