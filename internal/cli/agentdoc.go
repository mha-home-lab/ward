package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The agent-doc injection makes WARD auto-consulted: any project that runs
// `ward init` carries the protocol inside its agent instruction file, so every
// future agent session picks it up without a human repeating it. The block is
// marker-delimited and idempotent: re-running init refreshes it in place and
// never touches content outside the markers.

const (
	// docStartPrefix is matched by prefix, not equality, so the block version
	// can advance (v1 -> v2 ...) and older files are still detected and
	// refreshed in place instead of accumulating duplicate blocks.
	docStartPrefix = "<!-- ward:protocol"
	docStart       = docStartPrefix + " v5 -->"
	docEnd         = "<!-- /ward:protocol -->"
)

// agentFiles are the instruction files coding agents read. AGENTS.md is the
// cross-tool convention and is created if missing; the others are only updated
// when they already exist (never invented).
var agentFiles = []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"}

const protocolBody = `
## WARD — verified project memory (managed block; do not edit between markers)

This project is ward-managed. Follow this protocol exactly; it exists so you
never re-solve solved problems and never trust stale claims.

1. SESSION START (always, before planning): run

       ward brief [topic]

   It re-verifies store-local results live, frees expired reservations, and
   prints prior knowledge, open runs, active claims, the task pool, and
   suggested next actions. Do what it says before planning.
2. SPEC-FIRST: for anything beyond a trivial edit, draft the spec before
   code: .spec/<topic>.md with Purpose / Signals / What's kept / What's
   changed and why / Open questions. Develop incrementally and keep the spec
   honest as you close each increment - a stale spec is a stale claim.
   Load relevant domain knowledge before writing it (see chips below).
3. TRUST RULE: only verified artifacts are facts. A memory hit votes for the
   cheap tier ONLY when live-verified against repo state; unverified, stale, or
   imported artifacts count as a MISS -> work at full attention. Treat a
   routing decision's verified context ids as truth, never a recap.
4. WORK FROM THE POOL (loop, do not ask permission): while brief lists open
   tasks within your budget —

       ward task next --by <your-name> --max-tier <budget>

   implement the pulled task's title in this repo, prove it with

       ward task run <task-id>

   THE RUN IS THE GATE: a task's --run/--verify-cmd is its acceptance check
   and must exercise the real change end-to-end (field lesson: a placeholder
   run like 'true' closes the task while proving nothing - that is a phantom
   success, the exact failure ward exists to prevent). For concurrent Go
   work make the check 'go test ./... -race'; default tests hide data races.
   Then repeat until the pool is empty or every remaining task is beyond your
   ability or blocked. On failure the task re-enters the pool one tier higher —
   do not retry it yourself; pull different work or stop. To resume a task a
   dead session left claimed: ward task take <id> --by <your-name>.
   When nothing is left: ward memory handoff, then stop.
5. EXCLUSIVE WORK: before touching a shared topic outside the pool (file,
   migration, release), run: ward memory claim add <topic> --ttl 60
   A conflict is a hard stop: pick different work, never proceed in parallel.
6. RECORDING RESULTS IS AUTOMATIC: successful runs capture store-local
   artifacts tagged by node id. Do NOT hand-type ward memory put; never write
   a verify_cmd you would not run yourself. THE ONE EXCEPTION: work done
   OUTSIDE ward task run (a green-field batch, an exploratory session) that
   surfaces a new, generalizable lesson must be captured manually — as
        ward memory put --local --tags portable:<topic> \
            --summary "..." --content "..." --verify-cmd "<cmd>"
    before ward memory handoff. This is the case hand-typing is correct, not
    a violation of this step: compounding requires knowledge to flow back in
    from off-pool work, not just out. If this lesson is the same underlying
    trap as an existing portable artifact in different wording, link it with
    '--recurs <id>' instead of filing an unrelated duplicate.
7. BEFORE ENDING: run  ward memory handoff  so the next session inherits
   incomplete work, open runs, and stale candidates.
8. FAILURE POLICY: two escalating failures exhaust the budget and the run
   stops for a human with a dossier (ward reject <run>). Never retry past it.

PORTABLE KNOWLEDGE: accepted lessons tagged portable:<topic> are synced to
the global skills directory by 'ward skill-sync' - load any that match your
task before planning. Chips distill hard-won lessons and reusable shapes into
the cross-project knowledge vault. Answers to a particular problem stay with
that problem's repo/artifacts, never in a portable chip.

Every command accepts --json for machine-readable output. If a command errors,
fix the cause; never bypass the store or the trust boundary.
`

// agentDocBlock is the entire contract an agent needs to cooperate with ward:
// bootstrap with brief, trust only verified context, claim exclusive topics,
// let auto-capture record results, hand off before ending.
func agentDocBlock() string {
	return docStart + protocolBody + docEnd + "\n"
}

// ensureAgentDocs injects or refreshes the ward protocol block in AGENTS.md
// (created if missing) and in any other existing agent instruction file.
// Returns the file paths written and their action ("created", "updated").
func ensureAgentDocs(dir string) (map[string]string, error) {
	written := map[string]string{}
	for i, name := range agentFiles {
		path := filepath.Join(dir, name)
		if i > 0 { // non-canonical files are only updated, never created
			if _, err := os.Stat(path); err != nil {
				continue
			}
		}
		action, err := upsertAgentBlock(path)
		if err != nil {
			return written, fmt.Errorf("%s: %w", name, err)
		}
		if action == "unchanged" {
			continue
		}
		written[path] = action
		printLine(action + ": " + path)
	}
	return written, nil
}

// upsertAgentBlock splices the managed block into path: replaces an existing
// marked region, appends to a file without one, or creates the file fresh.
// Returns the action taken: created | updated | refreshed | unchanged.
func upsertAgentBlock(path string) (string, error) {
	body, err := os.ReadFile(path)
	existed := err == nil
	switch {
	case existed:
	case os.IsNotExist(err):
		body = []byte("# " + strings.TrimSuffix(filepath.Base(path), ".md") + "\n")
	default:
		return "", err
	}
	s := string(body)
	block := agentDocBlock()
	start := strings.Index(s, docStartPrefix)
	end := strings.Index(s, docEnd)
	var out, action string
	switch {
	case start >= 0 && end > start:
		out = strings.TrimRight(s[:start], "\n") + "\n\n" +
			strings.TrimRight(block, "\n") + "\n" +
			strings.TrimLeft(s[end+len(docEnd):], "\n")
		action = "refreshed"
	case start >= 0 || end >= 0:
		// Half-present markers would corrupt the file; refuse rather than guess.
		return "", fmt.Errorf("conflicting ward markers in %s", path)
	default:
		out = strings.TrimRight(s, "\n") + "\n\n" + block
		action = "updated"
	}
	if out == s {
		return "unchanged", nil
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return "", err
	}
	if !existed {
		action = "created"
	}
	return action, nil
}
