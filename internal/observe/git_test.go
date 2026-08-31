package observe

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo creates a fresh git repo in dir with an initial commit, returns
// the initial HEAD sha.
func initGitRepo(t *testing.T, dir string) string {
	t.Helper()
	must := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.Output(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	must("git", "init", "-q")
	must("git", "config", "user.email", "t@t")
	must("git", "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n"), 0o644)
	must("git", "add", ".")
	must("git", "commit", "-qm", "initial")
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestGitCommitsSince(t *testing.T) {
	dir := t.TempDir()
	first := initGitRepo(t, dir)

	// No commits since the current HEAD -> 0.
	if n := GitCommitsSince(dir, first); n != 0 {
		t.Fatalf("expected 0 commits since current HEAD, got %d", n)
	}
	// Empty "since" -> 0 (never fails the caller).
	if n := GitCommitsSince(dir, ""); n != 0 {
		t.Fatalf("expected 0 with empty since, got %d", n)
	}
	// Not a git repo -> 0 (never fails the caller).
	if n := GitCommitsSince(t.TempDir(), first); n != 0 {
		t.Fatalf("expected 0 outside a git repo, got %d", n)
	}

	// Add one commit; now 1 since `first`.
	must := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.Output(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package y\n"), 0o644)
	must("git", "add", ".")
	must("git", "commit", "-qm", "second")
	if n := GitCommitsSince(dir, first); n != 1 {
		t.Fatalf("expected 1 commit since %s, got %d", first, n)
	}

	// HEAD sha is non-empty within the repo, empty outside.
	if GitHeadSHA(dir) == "" {
		t.Fatal("expected a non-empty HEAD sha in a git repo")
	}
	if GitHeadSHA(t.TempDir()) != "" {
		t.Fatal("expected empty HEAD sha outside a git repo")
	}
}
