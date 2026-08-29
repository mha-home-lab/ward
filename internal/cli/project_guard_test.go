package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// captureStderr swaps os.Stderr for a buffer and returns it plus a restore func.
func captureStderr() (*bytes.Buffer, func()) {
	orig := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()
	return buf, func() {
		w.Close()
		<-done
		os.Stderr = orig
	}
}

// TestMisplacementGuard: a request tagged `ward` filed into a NON-ward store must
// emit the misplacement warning; the same request with --project ward must not.
func TestMisplacementGuard(t *testing.T) {
	// A store we treat as "not ward": an isolated temp .ward.
	otherHome := t.TempDir()
	t.Setenv("WARD_HOME", otherHome)
	// Ensure "ward" is NOT a registered project, so the guard can't be confused
	// into thinking the temp dir is ward's store.
	t.Setenv("WARD_PROJECT_WARD_HOME", "")

	buf, restore := captureStderr()
	root := NewRoot()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	// 1) ward-tagged add WITHOUT --project -> must warn.
	root.SetArgs([]string{"task", "add", "--tags", "ward,probe", "probe misplace"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(buf.String(), "this request is tagged for WARD itself") {
		t.Fatalf("expected misplacement warning, got: %s", buf.String())
	}
	restore()

	// 2) ward-tagged add WITH --project ward -> no misplacement warning.
	// point the ward project at a real temp store so OpenForName succeeds
	wardHome := t.TempDir()
	if err := os.MkdirAll(wardHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_PROJECT_WARD_HOME", wardHome)
	buf2, restore2 := captureStderr()
	defer restore2()
	root = NewRoot()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"task", "add", "--project", "ward", "--tags", "ward,probe", "probe via project"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(buf2.String(), "this request is tagged for WARD itself") {
		t.Fatalf("--project ward should suppress the misplacement warning, got: %s", buf2.String())
	}
}
