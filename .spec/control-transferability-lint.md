# Control: transferability lint for the portable-knowledge pipeline

## Purpose
`portable:<topic>` chips are meant to hold a **generalized mechanism + the hard
lesson + why it matters** — something a fresh agent in a *different* repo can
use. Nothing today distinguishes that from a **cheat-sheet**: verbatim output
strings, exact error text, per-exercise file paths and argv shapes that only
mean something inside the repo that produced them. A cheat-sheet that reaches
the global vault doesn't just fail to help a new project — because a memory
hit votes cheap and gets injected as evidence, it actively misleads the next
agent that routes on it. Field case: a 23-exercise `exercism` bash batch's
first knowledge-transfer pass produced exactly this — `"collatz prints
exactly 'Error: Only positive integers are allowed'"` — and a human had to
reject the whole pass before the chip was worth compiling.

This is a linter, not a truth oracle. It follows the project's existing rule
that gates are deterministic and pattern-based (`verify_cmd`, drift, tier
escalation) — **no model call decides transferability**. It will be wrong
sometimes, the same way `go vet` is wrong sometimes; it's cheap, auditable,
and testable, not a black-box quality judgment.

## Signals (what good looks like)

New package `internal/transferability` (pure logic, no I/O — same split as
`internal/routing` and `internal/verification`; CLI wiring stays in
`internal/cli`):

```go
type LintResult struct {
    Score      int      // generalization signals − cheat-sheet signals
    CheatSheet bool      // Score <= 0
    Signals    []string  // human-readable, one per pattern that fired — the --why output
}

func Score(topic, summary, content string) LintResult
```

Four regex-based signals, each independently testable and independently
listed in `Signals` so a flagged result is inspectable, not a mystery number:

- **Verbatim-output phrasing** (cheat-sheet, −1 each): `prints? exactly`,
  `outputs? exactly`, `returns? exactly` immediately followed by a quoted
  string. Matches the `collatz` example verbatim.
- **Instance-specific path/argv** (cheat-sheet, −1 each): path-shaped tokens
  (`\w[\w\-]*/[\w\-\.]+\.\w+`), `argv[`, or a bare exercise-slug identifier
  repeated 2+ times with no nearby generalization word.
- **Generalization language** (portable signal, +1 each, capped at +3 so one
  well-placed "because" can't paper over five verbatim strings): `idiom`,
  `the pattern`, `in general`, `any time`, `whenever`, `because`, `the trap
  is`, `the mechanism`, `the lesson`.
- `Score = generalization_hits − min(cheatsheet_hits, 5)`; `Score <= 0` →
  `CheatSheet = true`.

Baked-in test fixtures (from the field case, verbatim — stronger than anything
invented):
- `"collatz prints exactly 'Error: Only positive integers are allowed'"`,
  `"bowling take '-' for a miss"` → must score `CheatSheet: true`.
- `"positive-mod idiom (( x % m + m ) % m ); bash % truncates toward zero"`
  → must score `CheatSheet: false`.

## Decisions (resolved before code)

- **Scope is the portable pipeline only.** Ordinary local captures (e.g. this
  project's own `rd:checks` chip) are legitimately instance-specific by
  design and must not be linted — scoring only runs when the destination is
  `portable:*` or the pack target is the global skills dir.
- **`capture` warns, `pack` blocks.** A heuristic firing at the point of
  least context (mid-capture) shouldn't hard-fail a legitimate edge case;
  the hard gate belongs at the point knowledge actually leaves the repo
  (`pack` → global dir / `skill-sync`). This matches the asymmetry in the
  original request (capture "warns (or refuses)"; pack "vetoes or visibly
  quarantines").
- **Override exists, and it's logged, not silent.** `pack --force --reason
  "<text>"` bypasses the gate for a specific artifact and stores the reason
  on the artifact record. A wall with no escape hatch either gets a false
  positive silently mangled to dodge the linter, or gets the regex patched
  under pressure with no trace of why. An override with a mandatory reason
  keeps both the gate and its exceptions auditable.

## What's kept / changed

- **New**: `internal/transferability/lint.go` — pure `Score()` function, unit
  tested against the fixtures above (repo convention: pure logic gets its own
  package, mirroring `internal/routing`, `internal/verification`).
- **Changed**: `internal/cli/capture.go` — after building `content`, if any
  tag is `portable:*`, call `transferability.Score` and print a non-fatal
  warning (stderr) with the fired signals if `CheatSheet`. Does not touch the
  artifact's status or block the write.
- **Changed**: `internal/cli/skill.go` `skillPackCmd` — when `--tag` starts
  `portable:` or `--out` targets `~/.config/opencode/skills/...`, score every
  candidate source before compiling. Failing artifacts are excluded from the
  bundle and listed in the pack summary as `"instance-specific, not synced to
  the global vault"` (visible failure, not silent). Passing `--force
  --reason` includes a specific artifact anyway and records the reason.
- **New**: `ward skill lint <chip>` in `internal/cli/skill.go` — reuses the
  existing `chipSourceIDs(body)` to resolve a compiled chip back to its live
  source artifacts, re-scores each against **current** artifact content (not
  the frozen chip text, so it also catches a chip that drifted into
  cheat-sheet territory after compilation), prints a scorecard: portable /
  cheat-sheet / borderline counts, `--why` for per-source signal detail.
- **Changed**: `internal/cli/agentdoc.go` `protocolBody`'s `PORTABLE
  KNOWLEDGE:` block gets one added sentence, same terse imperative register
  as the rest of the block:
  > Chips distill hard-won lessons and reusable shapes into the cross-project
  > knowledge vault. Answers to a particular problem stay with that
  > problem's repo/artifacts, never in a portable chip.

## Deliberately NOT built

- No model call anywhere in the scoring path — pattern match only, consistent
  with every other gate in this project.
- No blocking at `capture` — only at `pack`, where knowledge actually leaves
  the repo boundary.
- No auto-rewrite/auto-distillation of a flagged cheat-sheet — the linter
  reports, a human or agent rewrites. Silently "fixing" the content would
  hide exactly the failure this spec exists to surface.
- No retroactive re-lint of chips already synced to the global dir before
  this ships — `ward skill lint <chip>` is available on demand, not run as a
  background sweep over the existing vault.

## Verification gate

```bash
# Unit — the field-case fixtures are the real acceptance test
go test ./internal/transferability/... -run TestScore -v

# capture: warns, does not block
ward capture --node <done-node> --workflow <wf> --tag portable:bash \
  --content "collatz prints exactly 'Error: Only positive integers are allowed'"
# Expected: artifact IS created; stderr shows a CheatSheet warning + fired signals

# pack: blocks and surfaces the failure
ward skill pack portable:bash --tag portable:bash \
  --out ~/.config/opencode/skills/ward-bash --json
# Expected: cheat-sheet source excluded from bundle; JSON summary lists it
# under "instance_specific" / "not synced to the global vault"

# lint: re-scan an existing chip
ward skill lint ward-bash --why
# Expected: per-source score + fired signals, non-zero exit if any source
# in the chip is currently cheat-sheet-scored

# protocol text
ward init --json | jq -r '.written' # or grep the target AGENTS.md
grep -q "never in a portable chip" AGENTS.md
```
