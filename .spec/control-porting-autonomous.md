# Control: autonomous porting ergonomics (transferability pipeline)

## Purpose

A field session porting knowledge to the global vault hit three concrete,
reproducible frictions that turned "capture -> pack -> sync" from one clean
pass into a capture -> pack -> read-which-was-excluded -> rewrite -> supersede
-> re-pack loop. The lint, the gate, and the verify path each fought the
writer instead of teaching the dialect. This spec fixes the three that are
self-contained, deterministic, and low-risk so the happy path collapses to a
single pass; items D/E from the feedback are deliberately deferred (below).

The three root causes, in the words of the reporter, and their mechanical
origin in this repo:

1. **The lint is vocabulary-blind, not semantic.** `Score()` computes
   `min(generalization_hits, 3) - min(cheat_hits, 5)`. The generalization
   trigger is a *preset word list*; the slug-repeat penalty is disabled only
   when `hasGeneralization(text)` (any one preset word). A dense, concrete,
   transferable mechanism about Keycloak reuses domain nouns (`config`, `key`,
   `new`, `crds`) that are legitimately unstoppable, so the slug penalty fires
   and a perfect mechanism is expelled. The grader rewards puffy prose
   ("the pattern ... because ... whenever") over dense, concrete mechanism
   knowledge — the opposite of what a vault should curate.
2. **No fast feedback at the point of writing.** The score only surfaces deep
   in `pack`/`skill-lint` output. `put`'s `warnIfCheatSheet` prints one line on
   stderr and the fired signals, but not whether the artifact *would* pass at
   pack time. Agents can't learn the dialect without reading `lint.go`.
3. **`put` vs `verify` disagree on what "declared" means.** `put --verify-cmd`
   stores `VerifyCmd` but leaves `VerifyKind` empty; `verification.Run` returns
   `"no verify_cmd declared"` whenever `VerifyKind == "" || VerifyCmd == ""`.
   So `ward verify --all` reports "no verify_cmd declared" for artifacts that
   provably have a `verify_cmd` in the DB. The two subsystems define the same
   fact differently and route an agent into dead DB surgery.

## Signals (what good looks like)

- A dense, concrete, transferable mechanism body that repeats unstoppable
  domain nouns — but does NOT copy exact output/path/argv — scores as NOT a
  cheat-sheet, even with zero preset generalization words.
- `ward memory put --dry-run` prints the transferability score + fired signals
  BEFORE writing, so an agent writes it once, correctly.
- `ward memory put --verify-cmd "go test ./..."` (no `--verify-kind`) results
  in `ward verify <id>` actually running that command as a shell line — the two
  subsystems agree.

## Decisions

- **Density is a structural generalization signal.** Introduce a `densityHits`
  term to `Score`: a content body that is *substantively long* (enough distinct
  sentence/clause material) and free of verbatim/path/argv cheat signals reads
  as a real mechanism, not a repeated slug. Concretely: count non-stopword slug
  tokens; when that density crosses a floor and there is no verbatim/path/argv
  signal, it contributes to the generalization side just like a preset
  generalization word. This keeps the `go vet` "sometimes wrong" posture but
  stops expelling dense domain knowledge and stops rewarding fluff.
- **The slug-repeat penalty stays**, but it must not outvote a dense body.
  Today `hasGeneralization` (one word) disables it entirely. We keep that
  behavior and ADD density as a second legal way to disable it — so "the
  pattern...because" text and genuinely dense mechanism text are both accepted,
  while short slug-bullet cheat-sheets (the collatz/bowling field case) are
  still expelled. The field fixtures must remain red.
- **`put --dry-run` is a preview, not a write.** It runs the same pipeline as
  a real portable put — compute score + signals, print them (score, pass/fail,
  hints), and return without opening a store write. It is the same pure
  `Score()` call the pack gate uses, so what you preview is what pack will
  decide.
- **`verify` honors a declared `VerifyCmd`.** When `put` is given `--verify-cmd`
  without `--verify-kind`, default `VerifyKind` to `"shell"` so the command is
  executed (respecting the existing `Local` trust boundary). This is the
  smallest reconcile of the two subsystems: `put` stops producing an
  unprefixable claims and `verify` stops contradicting it. It does NOT run the
  command at put time (the D0.3 boundary stays: put is guilty by default;
  only `local`/`human` run it), and `verify` still refuses for non-local
  artifacts.
- **Scope:** portable pipeline only, matching the existing lint. Dry-run shows
  the score for any put but only scores/prints the portable sub-path the same
  as `warnIfCheatSheet`.

## What's kept

- `Score()` stays pure, deterministic, pattern-based; no model call.
- The collatz/bowling fixtures still score cheat-sheet; the positive-mod idiom
  still scores non-cheat-sheet.
- `pack` and `sync` gates, the `--force --reason` override, capture-warns /
  pack-blocks asymmetry — all unchanged.
- D0.3 trust boundary: `put` never executes a verify_cmd; only `verify`/`tick`
  for `local`/trusted artifacts, and always gated on `Local`.

## What's changed and why

- **`internal/transferability/lint.go`**: add a density generalization term and
  make density a valid way to disable the slug-repeat penalty. Update the
  scorecap bookkeeping so the generalization side still caps, preventing a
  dense-but-verbatim body from smuggling past (density requires NO verbatim /
  path / argv signal in the same body).
- **`internal/transferability/lint_test.go`**: new fixtures for (a) dense domain
  body with no preset generalization words -> NOT cheat-sheet; (b) dense body
  WITH a verbatim string -> still cheat-sheet (density must not mask verbatim);
  (c) short slug bullet with no generalization -> still cheat-sheet; (d) the
  existing fixtures unchanged.
- **`internal/cli/transferlint.go`**: add `previewTransferability(tags, summary,
  content)` that prints the score + signals + a pass/fail verdict and a
  write-once hint. Kept adjacent to `warnIfCheatSheet` so both paths share the
  same first-portable-tag resolution.
- **`internal/cli/memory.go` `memoryPutCmd`**: add `--dry-run` flag that calls
  `previewTransferability` and returns before any store write; when
  `--verify-cmd` is set and `--verify-kind` is empty, default the kind to
  `"shell"` so verify can actually run it.
- **Tests** in `internal/cli`: dry-run preview fires for a portable tag and
  writes nothing; put with verify-cmd and no verify-kind yields a shell-able
  artifact (verify reports a real execution detail, not "no verify_cmd
  declared").

## Deliberately NOT built (deferred from the field feedback)

- **D. Consensus/neo-veto gate instead of per-artifact veto.** Changing the
  pack/sync veto from "any source <=0 strips it" to "majority of sources <=0
  strips the bundle" would re-tier the existing hardened gate and its tests.
  With A + B, borderline sources no longer get expelled for density alone, and
  the dry-run preview makes a bad phrasing visible before it ever reaches pack.
  Revisit if real data shows consensus matters beyond the density fix.
- **E. `--kind knowledge` / `--by-experience` verify-free path.** A separate
  capture class with "gate = promoted by definition" plus provenance-only
  verification is a new concept touching status/verify semantics and the
  scorecard. The feedback's own minimal recipe already avoids the verify trap
  by simply not passing `--verify-cmd` for knowledge nuggets; with C fixed,
  that recipe is fully supported. A first-class kind remains a larger, separate
  spec.
- No auto-rewrite / auto-distillation of flagged content. The linter reports;
  a writer rewrites. Silent fixing hides the exact failure the lint exists to
  surface.

## Open questions

- Density floor: is a raw non-stopword token count the right proxy, or should
  it also require a minimum clause/sentence count? Starting with a conservative
  token floor; the field fixtures plus a new dense-domain fixture will tell us
  if it's too hot or too cold.

## Verification gate

```bash
go build ./... && go test ./... -race && gofmt -l . && go vet ./...

# A: dense domain mechanism with no preset generalization words -> NOT cheat-sheet
#    (new unit fixture in internal/transferability/lint_test.go)

# B: preview, no write
ward memory put --dry-run --tags portable:keycloak \
  --summary "realm config key rollout" --content "<dense mechanism>" --json
# Expected: score + signals printed; NO new artifact in the store

# C: put declares verify-cmd without kind; verify runs it as shell (local/trusted)
ward memory put --local --tags portable:test \
  --summary "s" --content "the mechanism is reusable because X" \
  --verify-cmd "printf ok"
ward verify <id>
# Expected: detail reflects running the command, NOT "no verify_cmd declared"
```
