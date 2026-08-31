//go:build darwin || linux

package verification

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
)

// runVerify executes name (optionally in dir) in its own process group so a
// timed-out verify can SIGKILL the whole tree (a hung `sh -c "go test ./... &&
// …"` grandchild), not just the immediate shell. Returns the captured combined
// output (for golden comparison) and the command error, or ctx.Err() when the
// deadline expired.
func runVerify(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		// Kill the process group: SIGKILL the shell AND any orphaned child it
		// left running (a long build/test daemon). Negative pid = the group.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return nil, ctx.Err()
	case err := <-done:
		return buf.Bytes(), err
	}
}
