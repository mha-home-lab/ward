package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Sidecar logs make verification evidence human-readable without prying open the
// SQLite db. Every node `run` writes `.ward/logs/<runID>_<nodeID>_<ts>.log` with
// the command, exit code, elapsed time, and full combined output. This is the
// transparency layer that answers "what actually ran, and why did it fail?".
//
// Note: the sidecar is ADDITIVE evidence. The db remains the system of record for
// state; the log exists so a human or agent can audit a run directly.

// WriteSidecar records one execution's evidence to a timestamped log file under
// WARD_HOME/logs and returns the path written. It is best-effort: a failed write
// is reported (not silently swallowed) so a missing log is itself a signal.
func WriteSidecar(runID, nodeID, cmd string, exitCode int, elapsed time.Duration, out []byte) (string, error) {
	dir := filepath.Join(Home(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(dir, fmt.Sprintf("%s_%s_%s.log", runID, nodeID, ts))
	var b strings.Builder
	fmt.Fprintf(&b, "run=%s node=%s\n", runID, nodeID)
	fmt.Fprintf(&b, "cmd=%s\n", cmd)
	fmt.Fprintf(&b, "exit_code=%d\n", exitCode)
	fmt.Fprintf(&b, "elapsed_ms=%d\n", elapsed.Milliseconds())
	fmt.Fprintf(&b, "time=%s\n", ts)
	b.WriteString("--- output ---\n")
	b.WriteString(string(out))
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// FindSidecar returns the most recent sidecar log for a run (across nodes), and
// whether one exists. For single-node task runs this is the single evidence file.
func FindSidecar(runID string) (string, bool) {
	dir := filepath.Join(Home(), "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	prefix := runID + "_"
	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".log") {
			matches = append(matches, filepath.Join(dir, e.Name()))
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches) // timestamp suffix makes lexical order == chronological
	return matches[len(matches)-1], true
}

// ReadSidecar returns the full content of the most recent sidecar for a run.
func ReadSidecar(runID string) (string, bool) {
	path, ok := FindSidecar(runID)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// SidecarExitCode parses the exit_code from a sidecar's header. Returns (0,false)
// if not present (e.g. a malformed or partial log).
func SidecarExitCode(content string) (int, bool) {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "exit_code=") {
			var n int
			if _, err := fmt.Sscanf(line, "exit_code=%d", &n); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// Tail returns the last n lines of s (split on newlines), for dossier/show output.
func Tail(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
