package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
)

// Result is the outcome of running an artifact's verify_cmd.
type Result struct {
	Status string // verified | stale | error | unknown
	Detail string
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
	default:
		return Result{Status: "unknown", Detail: "unknown verify_kind " + a.VerifyKind}
	}
}

func grep(a store.Artifact, repoRoot string) Result {
	pattern, path := split(a.VerifyCmd)
	if pattern == "" || path == "" {
		return Result{Status: "error", Detail: "grep verify_cmd must be 'pattern::path'"}
	}
	cmd := exec.Command("grep", "-rq", "--", pattern, path)
	if repoRoot != "" {
		cmd.Dir = repoRoot
	}
	if err := cmd.Run(); err != nil {
		return fail(a, fmt.Sprintf("pattern %q not found in %s (repo=%s)", pattern, path, repoRoot))
	}
	return ok(a)
}

func shell(a store.Artifact, repoRoot string) Result {
	cmd := exec.Command("sh", "-c", a.VerifyCmd)
	if repoRoot != "" {
		cmd.Dir = repoRoot
	}
	if err := cmd.Run(); err != nil {
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
