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
	_, _, _, gap, prevAt, err := detectCaptureGap(s, ".")
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

	c, _, newPortable, gap, prevAt, err := detectCaptureGap(s, ".")
	if err != nil {
		t.Fatal(err)
	}
	if !gap {
		t.Fatalf("expected capture gap after a commit with no captures (commits=%d portable=%d)", c, newPortable)
	}
	if c < 1 {
		t.Fatalf("expected >=1 commit since last handoff, got %d", c)
	}
	if newPortable != 0 {
		t.Fatalf("expected 0 new portable captures, got %d", newPortable)
	}
	if prevAt != firstAt {
		t.Fatalf("expected prevAt=%q got %q", firstAt, prevAt)
	}

	// A NON-portable capture must NOT clear the gap (the sharpening: ordinary
	// on-pool work can't mask an off-pool discovery that was never recorded).
	cmd := memoryPutCmd()
	setPutFlags(cmd, t, "a lesson", "true", "shell", "agent", "true")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	_, _, newPortable, gap, _, err = detectCaptureGap(s, ".")
	if err != nil {
		t.Fatal(err)
	}
	if !gap {
		t.Fatalf("a non-portable capture must NOT clear the off-pool gap (portable=%d)", newPortable)
	}

	// A PORTABLE capture clears the gap.
	cmd2 := memoryPutCmd()
	setPutFlags(cmd2, t, "a portable lesson", "true", "shell", "agent", "true")
	if err := cmd2.Flags().Set("tags", "portable:test"); err != nil {
		t.Fatal(err)
	}
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}
	c, _, newPortable, gap, _, err = detectCaptureGap(s, ".")
	if err != nil {
		t.Fatal(err)
	}
	if gap {
		t.Fatalf("must NOT flag a gap once a portable capture happened (commits=%d portable=%d)", c, newPortable)
	}
	if newPortable < 1 {
		t.Fatalf("expected >=1 new portable capture, got %d", newPortable)
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

// TestBriefLiveGapWithoutHandoff: a session that READ the vault (via brief /
// skill install) but NEVER ran `ward memory handoff` leaves NO fresh handoff_log
// row — yet brief must still surface the gap at the next session start via the
// live check over commits since the LAST logged handoff. This is the exact case
// that inspired the spec: agent 3 stopped and reported to a human without
// handing off, so its lessons were invisible.
func TestBriefLiveGapWithoutHandoff(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("WARD_HOME", t.TempDir())
	gitInitRepo(t, dir)

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	// A PRIOR session ended cleanly (no flagged gap). The "dropped" session then
	// made commits and never called handoff — so no new handoff_log row exists.
	s.LogHandoff(time.Now().Add(-2*time.Second).UTC().Format("2006-01-02T15:04:05Z"), observe.GitHeadSHA("."), false, 0)
	s.DB.Close()
	mustCommit(t, dir, "off-pool discovery never captured")

	// Confirm there is no flagged persisted row to fall back on.
	s2, _ := store.Open()
	last, _ := s2.LastHandoff()
	s2.DB.Close()
	if last == nil || last.CaptureGap {
		t.Fatalf("test setup: expected an unflagged persisted row, got %+v", last)
	}

	buf, restore := captureStdout()
	bc := briefCmd()
	bc.SetOut(&bytes.Buffer{})
	bc.SetErr(&bytes.Buffer{})
	if err := bc.Execute(); err != nil {
		t.Fatal(err)
	}
	restore()
	if !strings.Contains(buf.String(), "may have skipped capture") {
		t.Fatalf("brief must surface a LIVE capture gap even when the prior session never handed off, got:\n%s", buf.String())
	}
}
