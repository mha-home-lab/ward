//go:build !darwin && !linux

package verification

import (
	"bytes"
	"context"
	"os/exec"
)

// runVerify is the non-Unix fallback: no process-group control, so a timeout
// only kills the immediate command. Process-group kill (exec_unix.go) is the
// darwin/linux hardening; other platforms sacrifice reaping grandchildren.
func runVerify(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return buf.Bytes(), nil
}
