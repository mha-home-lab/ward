package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mha-home-lab/ward/internal/observe"
	"github.com/mha-home-lab/ward/internal/store"
)

// gitInitRepo creates a repro git repo in dir with an initial commit.
func gitInitRepo(t *testing.T, dir string) {
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
}

func mustCommit(t *testing.T, dir, msg string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package y\n"), 0o644)
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	if out, err := cmd.Output(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-qm", msg)
	cmd.Dir = dir
	if out, err := cmd.Output(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// TestHandoffCaptureGap: no prior handoff -> no check; a commit with no capture
// -> gap suspected; a capture clears the gap.
func TestHandoffCaptureGap(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("WARD_HOME", t.TempDir())
	gitInitRepo(t, dir)

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	// No prior handoff row -> no gap.
	_, _, gap, prevAt, err := detectCaptureGap(s, ".")
	if err != nil {
		t.Fatal(err)
	}
	if gap || prevAt != "" {
		t.Fatalf("first call must not detect a gap (prevAt=%q gap=%v)", prevAt, gap)
	}

	// Seed a handoff row at the current HEAD, then add a commit with no capture.
	// Use a slightly-past "at" so a capture created in the same wall-clock second
	// is still strictly > it (created_at has 1s resolution).
	firstAt := time.Now().Add(-2 * time.Second).UTC().Format("2006-01-02T15:04:05Z")
	s.LogHandoff(firstAt, observe.GitHeadSHA("."), false, 0)
	mustCommit(t, dir, "off-pool work")

	c, n, gap, prevAt, err := detectCaptureGap(s, ".")
	if err != nil {
		t.Fatal(err)
	}
	if !gap {
		t.Fatalf("expected capture gap after a commit with no artifacts (commits=%d captures=%d)", c, n)
	}
	if c < 1 {
		t.Fatalf("expected >=1 commit since last handoff, got %d", c)
	}
	if n != 0 {
		t.Fatalf("expected 0 new captures, got %d", n)
	}
	if prevAt != firstAt {
		t.Fatalf("expected prevAt=%q got %q", firstAt, prevAt)
	}

	// Add a capture -> no gap.
	cmd := memoryPutCmd()
	setPutFlags(cmd, t, "a lesson", "true", "shell", "agent", "true")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	c, n, gap, _, err = detectCaptureGap(s, ".")
	if err != nil {
		t.Fatal(err)
	}
	if gap {
		t.Fatalf("must NOT flag a gap when a capture happened (commits=%d captures=%d)", c, n)
	}
}

// captureStdout redirects os.Stdout (brief/printHuman report to it) into a
// buffer and returns it plus a restore func.
func captureStdout() (*capturedBuffer, func()) {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	buf := &capturedBuffer{}
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()
	return buf, func() {
		w.Close()
		<-done
		os.Stdout = orig
	}
}

// TestBriefFlagsSkippedCapture: when the most recent handoff_log row was flagged
// (commits happened, no captures since), brief's next actions surface the gap —
// the loop-closer that lets the NEXT session backfill off-pool lessons.
func TestBriefFlagsSkippedCapture(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("WARD_HOME", t.TempDir())
	gitInitRepo(t, dir)

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	// Seed a handoff row in the past, then commit without capturing.
	s.LogHandoff(time.Now().Add(-2*time.Second).UTC().Format("2006-01-02T15:04:05Z"), observe.GitHeadSHA("."), true, 1)
	s.DB.Close()
	mustCommit(t, dir, "off-pool work")

	buf, restore := captureStdout()
	bc := briefCmd()
	bc.SetOut(&bytes.Buffer{})
	bc.SetErr(&bytes.Buffer{})
	if err := bc.Execute(); err != nil {
		t.Fatal(err)
	}
	restore()
	if !strings.Contains(buf.String(), "may have skipped capture") {
		t.Fatalf("brief must surface the prior capture gap, got:\n%s", buf.String())
	}
}
