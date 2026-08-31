package observe

import (
	"os/exec"
	"strings"
)

// GitChangedFiles returns files changed relative to HEAD (observation only).
// Never fails the caller: if not a git repo, returns empty list.
func GitChangedFiles(repoRoot string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return []string{}, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	return lines, nil
}

// GitCommitsSince counts commits reachable from HEAD but not from since
// (i.e. `git log --oneline <since>..HEAD`). Observation only: never fails the
// caller — if since is empty, not a git repo, or the range is invalid, it
// returns 0. used to detect "commits happened but captures didn't" between
// handoffs.
func GitCommitsSince(repoRoot, since string) int {
	if since == "" {
		return 0
	}
	cmd := exec.Command("git", "log", "--oneline", since+"..HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

// GitHeadSHA returns the current HEAD commit hash (observation only). Never
// fails the caller: returns "" if not a git repo.
func GitHeadSHA(repoRoot string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GitRepoRoot returns the absolute top-level directory of the git repo that
// contains dir (normally the cwd, so it works whether the agent runs from the
// repo root or a subdirectory). Falls back to dir itself if it is not inside a
// git repo — observation only, never fails the caller.
func GitRepoRoot(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return dir
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return dir
	}
	return root
}

// GitUntracked returns untracked-but-not-ignored files (observation only).
func GitUntracked(repoRoot string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return []string{}, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	return lines, nil
}
