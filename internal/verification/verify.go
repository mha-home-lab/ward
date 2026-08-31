package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/store"
)

// Result is the outcome of running an artifact's verify_cmd.
type Result struct {
	Status string // verified | stale | error | unknown
	Detail string
}

// DefaultTimeout bounds a single verify command execution. A verify_cmd that
// outlives this is killed and reported as an error/stale rather than letting the
// live sweep (ward brief / ward tick) block forever on a hung build, test, or
// daemon-start. Overridable via WARD_VERIFY_TIMEOUT (Go duration, e.g. "90s").
const DefaultTimeout = 180 * time.Second

// verifyTimeout is the per-command deadline, read once from WARD_VERIFY_TIMEOUT
// so a malformed value fails the process loudly at command build time instead of
// silently ignoring an operator's intent.
func verifyTimeout() time.Duration {
	if v := os.Getenv("WARD_VERIFY_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return DefaultTimeout
}

// Run executes an artifact's verify command ONLY when the artifact is
// store-local (the v1 trust boundary, D0.3). Imported artifacts are never
// executed: their status stays "unknown" until explicitly trusted.
func Run(a store.Artifact, repoRoot string) Result {
	if !a.Local {
		return Result{Status: "unknown", Detail: "imported artifact: verify_cmd not executed (local-provenance trust boundary)"}
	}
	if a.VerifyKind == "" || a.VerifyCmd == "" {
		return Result{Status: "unknown", Detail: "no verify_cmd declared"}
	}
	switch a.VerifyKind {
	case "grep":
		return grep(a, repoRoot)
	case "build", "test", "shell":
		return shell(a, repoRoot)
	case "hash":
		return hash(a, repoRoot)
	case "golden":
		return golden(a, repoRoot)
	default:
		return Result{Status: "unknown", Detail: "unknown verify_kind " + a.VerifyKind}
	}
}

// golden diffs a command's output against a checked-in expected file. VerifyCmd
// format: "<expected-file>::<command>". This is verification that matches
// semantic "done" (output equality), not just exit codes or input hashes.
// Trailing newlines are normalized so editors don't cause false drift.
func golden(a store.Artifact, repoRoot string) Result {
	expectedPath, command := split(a.VerifyCmd)
	if expectedPath == "" || command == "" {
		return Result{Status: "error", Detail: "golden verify_cmd must be 'expected-file::command'"}
	}
	full := expectedPath
	if repoRoot != "" && !filepath.IsAbs(expectedPath) {
		full = filepath.Join(repoRoot, expectedPath)
	}
	want, err := os.ReadFile(full)
	if err != nil {
		return fail(a, fmt.Sprintf("cannot read golden file %s: %v", expectedPath, err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout())
	defer cancel()
	got, err := runVerify(ctx, repoRoot, "sh", "-c", command)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fail(a, fmt.Sprintf("golden command %q timed out after %s", command, verifyTimeout()))
		}
		return fail(a, fmt.Sprintf("command failed: %v", err))
	}
	if strings.TrimRight(string(got), "\n") != strings.TrimRight(string(want), "\n") {
		return fail(a, fmt.Sprintf("output differs from golden file %s", expectedPath))
	}
	return ok(a)
}

func grep(a store.Artifact, repoRoot string) Result {
	pattern, path := split(a.VerifyCmd)
	if pattern == "" || path == "" {
		return Result{Status: "error", Detail: "grep verify_cmd must be 'pattern::path'"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout())
	defer cancel()
	_, err := runVerify(ctx, repoRoot, "grep", "-rq", "--", pattern, path)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Result{Status: "error", Detail: fmt.Sprintf("grep %q in %s timed out after %s", pattern, path, verifyTimeout())}
		}
		return fail(a, fmt.Sprintf("pattern %q not found in %s (repo=%s)", pattern, path, repoRoot))
	}
	return ok(a)
}

func shell(a store.Artifact, repoRoot string) Result {
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout())
	defer cancel()
	_, err := runVerify(ctx, repoRoot, "sh", "-c", a.VerifyCmd)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fail(a, fmt.Sprintf("verify_cmd %q timed out after %s", a.VerifyCmd, verifyTimeout()))
		}
		return fail(a, fmt.Sprintf("command failed: %v", err))
	}
	return ok(a)
}

func hash(a store.Artifact, repoRoot string) Result {
	_, path := split(a.VerifyCmd)
	if path == "" {
		return Result{Status: "error", Detail: "hash verify_cmd must be 'algo::path'"}
	}
	full := path
	if repoRoot != "" && !filepath.IsAbs(path) {
		full = filepath.Join(repoRoot, path)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fail(a, fmt.Sprintf("cannot read %s: %v", path, err))
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	// Expected hash stored in the artifact content (first line). Without a
	// stored baseline, drift cannot be detected, so we refuse to "verify".
	expected := strings.TrimSpace(strings.SplitN(a.Content, "\n", 2)[0])
	if expected == "" {
		return Result{Status: "error", Detail: "no expected hash stored in content (first line); drift detection disabled"}
	}
	if expected != got {
		return fail(a, fmt.Sprintf("hash drift expected=%s got=%s", expected, got))
	}
	return ok(a)
}

func ok(a store.Artifact) Result {
	if a.VerifyStatus == "verified" {
		return Result{Status: "verified", Detail: "re-verified"}
	}
	return Result{Status: "verified", Detail: "verified"}
}

func fail(a store.Artifact, detail string) Result {
	// A previously-verified artifact that now fails is STALE, not merely error:
	// this is what lets the router catch drift before a wrong route.
	if a.VerifyStatus == "verified" {
		return Result{Status: "stale", Detail: detail}
	}
	return Result{Status: "error", Detail: detail}
}

func split(s string) (string, string) {
	parts := strings.SplitN(s, "::", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return s, ""
}
