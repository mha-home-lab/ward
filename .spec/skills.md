# skills — Brain-to-Chip Compilation

| | |
|---|---|
| Status | Active |
| Domain | skills |
| Version | 1.0.0 |

## Problem

The brain's knowledge only helps agents that read the store. Most coding
agents don't — they load *skill files*. The lessons we pay for (bounced tasks,
drifted claims, R&D verdicts) should compile into cheap, pluggable injects that
make a small model sharp on one domain: a chip, in the Matrix sense.

A hand-written skill repo drifts from reality the day it ships. WARD's chips
are therefore **derived artifacts**: compiled from gated store knowledge,
regenerable at will, staleness-detectable.

## Commands

- `ward skill pack <topic> [--out DIR] [--project P] [--include-unverified]`
  compiles eligible knowledge for a topic into `DIR/SKILL.md` (default
  `.opencode/skills/ward-<topic>/SKILL.md`, loader-compatible frontmatter).
- `ward skill check <chip-dir>` re-reads every source id recorded in the
  chip's audit table against live store state: any superseded / stale / error /
  missing source ⇒ verdict STALE (recompile), else FRESH.

## Inclusion rule (the gate travels with the chip)

An artifact enters a chip iff:

1. `status = accepted`, AND
2. it is trusted **by its own class**:
   - work captures (have `verify_cmd`): require live `verify_status = verified`;
   - verdict-knowledge (R&D promotions, architect procedures — no
     `verify_cmd`): acceptance via promotion IS the gate.

`--include-unverified` relaxes class 2 but marks such sections
`**[UNVERIFIED — treat as suspect]**` in the rendered body. A chip never
silently mixes evidence classes.

## Rendering rules

- Frontmatter: `name` (ward-prefixed, slugified topic), `description`.
- Body grouped by kind: solution → "Procedures", discovery → "Field notes",
  feedback → "Watch out", context → "Background".
- Header banner: DO NOT hand-edit; regenerate instead.
- Footer: sources table (`id | kind | gate | verify_at`) — the audit trail is
  part of the chip, so any claim is one `ward memory get <id>` from its
  evidence.

## Lifecycle

    harvest finds a gap → explorer proposes → architect promotes
      → ward skill pack → agents load the chip (cheap → sharp)
      → sources drift/get superseded → ward skill check says STALE
      → fix the brain → repack → FRESH

Chips are caches of the brain. Editing a chip by hand is editing a cache —
forbidden by banner and by pointlessness.

## Acceptance criteria

1. Packing a topic with only unverified captures yields nothing without
   `--include-unverified` (gate holds).
2. Superseding a source flips `skill check` to STALE naming that source;
   promoting a successor and repacking returns FRESH.
3. Every section in a chip cites its source ids in the footer table.

## Non-goals

- Auto-repacking (tick stays verification-only; chips refresh deliberately).
- Emitting formats beyond markdown-with-frontmatter (opencode/codex-style
  loaders and humans all read this).
