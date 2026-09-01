package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// capturedBuffer is a mutex-guarded buffer: the capture goroutine fills it via
// ReadFrom WHILE the test reads it via String, so without serialization the
// race detector flags it (and -race is a hard gate for concurrent work).
type capturedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *capturedBuffer) ReadFrom(r io.Reader) (int64, error) {
	// Pump manually: bytes.Buffer.ReadFrom blocks on the pipe read for the whole
	// call, so holding the mutex across it would deadlock String(). Lock only
	// per chunk; goroutine ends when w.Close() EOFs the pipe.
	var total int64
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			c.mu.Lock()
			c.buf.Write(tmp[:n])
			c.mu.Unlock()
			total += int64(n)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func (c *capturedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// captureStderr swaps os.Stderr for a pipe drained into a synchronized buffer
// and returns it plus a restore func (which waits for the drain to finish).
func captureStderr() (*capturedBuffer, func()) {
	orig := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	buf := &capturedBuffer{}
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

// TestMisplacementGuardInsideWardStore: filing a portable:-tagged item from INSIDE
// ward's own store (current store path == registered ward path) must NOT warn —
// this is the inside-ward-repo case the guard must suppress. It also exercises the
// relative-vs-absolute fix: before the fix, a relative Home() misc-compared
// against the absolute registered path and falsely warned.
func TestMisplacementGuardInsideWardStore(t *testing.T) {
	wardHome := t.TempDir()
	t.Setenv("WARD_HOME", wardHome)              // current store = absolute temp dir
	t.Setenv("WARD_PROJECT_WARD_HOME", wardHome) // register "ward" -> same dir
	t.Chdir(t.TempDir())

	buf, restore := captureStderr()
	defer restore()
	root := NewRoot()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"memory", "put", "--summary", "inside ward store lesson", "--kind", "solution",
		"--tags", "topic:portable:probe", "--content", "a generalizable mechanism that applies anywhere"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(buf.String(), "this request is tagged for WARD itself") {
		t.Fatalf("writing portable:-tagged item in ward's own store must NOT warn, got: %s", buf.String())
	}
}
