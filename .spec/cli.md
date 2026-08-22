# cli — Cobra Command Tree & Flag Conventions

| | |
|---|---|
| Status | Draft (v1 planning) |
| Domain | cli |
| Version | 0.1.0 |

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

## Proposed command tree

```
ward
├── init [--root .]
├── validate
├── run <workflow> [--title T]        # alias: intake
├── resume <run_id>
├── approve <approval_id> | reject <approval_id>
├── route <node|text>                 # router introspection
├── verify [--project X]
├── tick "<msg>" [--status FIELDS]
├── memory
│   ├── put|propose <kind> <summary> [--content|--file] [--tags] [--verify CMD] [--project X] [--ceremony light|full]
│   ├── promote <id>... [--reason R] [-b WHO]
│   ├── supersede <id> [--with <new>] [--reason R]
│   ├── search <q> [-n] [-k kind] [--project X] [--digest]
│   ├── list [-k kind] [-s status] [-n] [--project X] [--digest]
│   ├── stale [-n] [-d days] [--project X]
│   ├── get <id>
│   ├── context [q] [-n] [--project X] [--digest]
│   ├── overview [-n]
│   ├── claim <topic> [--ttl D] [--project X]
│   ├── handoff [--session S] [-s summary] [--incomplete JSON]
│   └── fsck
├── workflow list|status <id>|resume <id>
├── channel list|inspect <name>
├── agent list|inspect <name>|process <name>
├── skill list|inspect <name>
└── watch
```

All commands honor global `--json` and `--root`. Exit codes: `0` ok, non-zero on
unknown id / validation failure / verify ✗ (when `--strict`).

## Flag conventions

- `--project X` / env `WARD_PROJECT` — lens scoping (chef retrieval-004).
- `--json` — machine-readable; applies to every command.
- `--digest` — print `sha256(block)[:8]` of the rendered block (cache check).
- `--root .` — repository root (ciao convention).
- `--verify "<shell cmd>"` — attach a verification check to an artifact.
- `--ceremony light|full` — override derived ceremony level.

## Open questions / risks

- **Command namespace collision.** `run` (ciao) vs chef's verbs; resolved by
  nesting memory under `ward memory` and orchestration at top level. Alternative:
  flatten everything (chef-style). Proposal: nested `memory` subtree to keep the
  two domains readable. Open.
- **`intake` vs `run`.** Is a free-text intake command worth a separate verb, or
  is `workflow run` enough? Lean: keep `run`, add `intake` only if a default
  workflow + parse is wanted. Open.
- **`verify --strict` exit code.** Should a ✗ fail the command (non-zero)? Useful
  in CI; risky in interactive use. Proposal: `--strict` opt-in. Open.
- **MCP later.** When added, it should expose `propose`/`route`/`verify`, never
  `put`/`promote` (chef rule: agent path proposes, never auto-promotes). v2.
