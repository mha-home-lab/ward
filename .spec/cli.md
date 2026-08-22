# cli — Cobra Command Tree & Flag Conventions

| | |
|---|---|
| Status | Implemented (tree below reflects v0.5) |
| Domain | cli |
| Version | 0.5.0 |

## Purpose

A single Cobra command tree under `ward` that merges the chef memory surface
and the ciao orchestration surface, adds the router/verification commands, and
follows chef's output discipline (deterministic, parseable, `--json`, one-line
errors). CLI-first; MCP/TUI are deferred (blueprint non-goals).

## What's kept from chef

- Memory commands adapted: `init`, `put`/`propose`, `promote`, `supersede`,
  `search`, `list`, `stale`, `get`, `context`, `overview`, `activity` (future),
  `claim`, `handoff`, `resume`, `tick`, `verify`, `fsck`.
- Global `--json`; `--project`/`WARD_PROJECT` lens; `--digest` cache-stability;
  one-line errors (`no artifact <id>`, `error: <detail>`).
- `search`/`list`/`context` never print full content; `get` is authoritative and
  bumps `used_count`.

## What's kept from ciao

- Orchestration commands adapted: `workflow run|status|resume|list`,
  `approve`, `reject`, `channel list|inspect`, `agent list|inspect|process`,
  `skill list|inspect`, `watch` (deferred priority).
- `--root` persistent flag (repository root); `validate` for workflow/agent defs.

## What's changed and why

- **One binary `ward`.** chef and ciao are separate tools; WARD unifies them so
  the router (routing.md) can call memory + orchestration in-process.
- **New commands:**
  - `ward route <node|text>` — run the classifier on a work unit and print the
    chosen `{tier, model, ceremony_level}` + the signals used. Introspection /
    debugging for the router.
  - `ward verify [--project X]` — run every artifact's `verify_cmd` against repo
    state, report ✓/✗/⚠, mark `verify_status`, queue stale `accepted` artifacts
    for supersede (verification.md).
  - `ward intake "<request>"` — convenience: create a run from a free-text
    request (alias over `workflow run` with a default workflow). Open naming.
  - `ward tick "<msg>" [--status FIELDS]` — lightweight solo progress update
    (auto-accepted `note`, no promote step). Directly answers qwen-auth ask #2.
- **`ceremony` flag** on memory writes (`--ceremony light|full`) lets a caller
  force the path; otherwise the engine derives it from DAG contention
  (orchestration.md). Default: derived.
- **`--verify "<cmd>"`** accepted on `put`/`propose` to attach a check (chef
  v1.0.0 feature, surfaced as first-class here).

## Shipped command tree (v0.5)

```
ward
├── brief [topic] [--repo R] [-n hits]      # session bootstrap: sweep + knowledge + runs + claims + next actions
├── init [--scaffold] [--docs] [--no-agents-md]  # store + agent-protocol injection into AGENTS.md/CLAUDE.md/GEMINI.md
├── task                                    # dispatch pool (broker.md §4)
│   ├── add <title> [--tier F] [--kind K] [--run CMD] [--verify-cmd CMD]
│   ├── next --by AGENT [--max-tier BUDGET] # atomic pull, budget admission
│   ├── run <id>                            # execute end-to-end: generate+run+capture+close
│   ├── list [--status S]
│   ├── done <id> | fail <id>               # close / release one tier higher
│   └── workflow <id> [--out P]             # generate runnable single-node DAG
├── run <start|status|approve|resume>
├── capture (--run ID | --node ID --workflow P)
├── explain <runID> [node]                  # routing evidence chain (routing.md)
├── reject <runID>                          # reject dossier reader
├── route <node> / router [--workflow W] [--seed|--seed-stale]
├── verify <id>|--all [--trust]
├── tick [--heal] [--repo R]                # drift sweep; heal supersedes drift
├── memory put|get|search|list|promote|supersede|handoff|context|stale|claim...
├── workflow show|validate
├── doctor
├── version
└── completion <shell>
```

Deviations from the original proposal: `intake`/`validate`/`channel`/`agent`/
`skill`/`watch` were not built — the dispatch pool (`task`) replaced free-text
intake with explicit flags; `explain` + `reject` grew out of the audit needs of
the router and escalation path; `brief` subsumed chef's `resume`/`overview`
intent as a single session-start command. `--digest` was never needed. All
commands honor global `--json`; exit `0` ok / non-zero error.

## Flag conventions

- `--project X` / env `WARD_PROJECT` — lens scoping (chef retrieval-004).
- `--json` — machine-readable; applies to every command.
- `--digest` — print `sha256(block)[:8]` of the rendered block (cache check).
- `--root .` — repository root (ciao convention).
- `--verify "<shell cmd>"` — attach a verification check to an artifact.
- `--ceremony light|full` — override derived ceremony level.

## Open questions / risks

- **Command namespace collision — RESOLVED.** Memory nested under `ward memory`,
  orchestration at top level (`run`), dispatch pool as `task`. Held through v0.5.
- **`intake` vs `run` — RESOLVED differently.** Free-text intake became
  `ward task add "<title>"` with explicit flags (broker.md §4): a sentence is a
  title, never a parsed spec. No NLP inference in the CLI.
- **`verify --strict` exit code.** Never built; conflicts are hard errors and
  verify failures surface via status, which proved sufficient. Closed by
  disuse.
- **MCP later.** When added, it should expose read-only tools (`is_verified`,
  `route`, `context`, claim acquire/release) over the same store — one
  invariant, many doors. Evaluated in the v0.5 wish triage: deferred to its own
  session with the official SDK. Still v2.
