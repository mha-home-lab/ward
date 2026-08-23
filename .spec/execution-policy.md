# execution policy — Draft

| | |
|---|---|
| Status | **Draft** — deferred design, deliberately unbuilt |
| Domain | security / orchestration |
| Origin | openai.md public review (#12): `sh -c <workflow.run>` with no sandbox is a weaker boundary than the coding agents Ward would coordinate |

## Purpose
Ward's verify_cmd trust boundary (store-local only) is real, but node
`run:` commands execute verbatim via `sh -c` in the repo root. For a personal
local Unix tool that is an honest, documented stance; for a fleet control
plane coordinating third-party agents it becomes the weakest link in the
chain: agent sandbox → ward → unrestricted shell → host.

## Signals
- Any deployment where the agent or task author is less trusted than the
  machine owner (fleets, CI, shared machines, remote workers).
- Integration with sandboxed harnesses whose guarantees Ward currently
  discards by shelling out directly.

## Design sketch
An explicit per-task/per-node execution policy ladder instead of one universal
`sh -c`:

```
trusted-local      (today's behavior; default)
sandboxed-local    (OS sandbox: seatbelt on macOS, landlock/bubblewrap on Linux)
container          (docker run with repo mount + egress policy)
worktree           (isolated git worktree + any of the above)
remote             (worker daemon; out of scope until Runner seam exists)
```

Rules:
1. Policy is declared at task/workflow authoring time and recorded in the run;
   escalation may tighten but never silently loosen it.
2. The default stays `trusted-local` so nothing changes for solo dogfood.
3. Policy violations fail like any check failure: visible, attributed,
   dossier if the budget exhausts.

## What's kept
The existing trust boundary for verify_cmd is unchanged and composes with this
(checks run under the same policy). Honest failure accounting unchanged.

## Open questions
- Sandbox profiles per platform are a maintenance burden — worth it only when
  a real untrusted-author scenario exists.
- Interaction with worktree isolation already noted as a fleet-scale risk in
  tasks.md accepted risks.
