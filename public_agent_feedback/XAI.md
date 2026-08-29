HEAD is [`cb1a859`](https://github.com/mha-home-lab/ward/commit/cb1a859) (2026-08-29). Tags are still **only** `v0.1.0` / `v0.2.0` / `v0.3.0`. Forty-seven commits after the last real tag, including a commit titled `v0.8.0`. I could not run `go test` in this sandbox (`go` missing). Critique is from the tree, history, and tests as written.

---

## The one-line verdict

You did the right product pivot (solo, not fleet) and some real honesty work (drift refusal, persistence errors, force-closed ≠ done). You then spent today polishing **ceremony around the gate** while the **release still does not exist**, the **docs still describe a fleet**, and the **protocol got longer in the same breath you admitted it is a social bet**. That is the opposite of consolidation.

---

## What is actually good

**The fleet retirement is the most adult decision in the repo.** R12/R13 falsified parallel dispatch. Moving `fleet-launch` to `attic/` and saying so in the README is rare. Keep that spine.

**Three engineering fixes from the public-feedback pass are real:**

| Fix | Why it mattered |
|---|---|
| Tag-first compounding | L6 was a lucky test. FTS-then-filter silently missed. |
| Engine persistence errors returned | ~25 ignored writes meant “completed” could be a lie. |
| `workflow_hash` + refuse resume | Mutated YAML executing under an old run id is a real bug. |

**`force-closed` vs `done` is the right audit distinction.** Sidecar logs as *human-readable* evidence are fine. `task show` is the command that should have existed when you added `task run`.

**README’s two honesty notes are the best writing in the project:** routing changes who executes, not what the worker knows; the protocol is instruction-following, not mechanical enforcement. That is the product, named.

---

## Hard problems, in order of damage

### 1. You still have not shipped v0.8.0

This was P0 a week ago. The commit exists. The tag does not. `git describe` will stamp binaries as `v0.3.0-<n>-g<sha>`. `CHANGELOG.md` has a `[v0.8.0] — 2026-08-23` section for a release that is not on the tag list. `go install @latest` does not mean “v0.8.”

The v0.8.0 commit also **tracked `.ward/ward.db`**, then a follow-up untracked it. That is the class of mistake consolidation is supposed to prevent. The blob remains in history.

Until `git tag v0.8.0` exists and CI runs `make check`, every other claim about “release engineering” is a diary entry.

### 2. The repo disagrees with itself about what Ward is

Same README:

- Pool: “failure bumps a task's floor so **stronger agents pick it up**”
- Claims: “keep **parallel agents** out of each other's way”
- Ops: “**Ward does not do parallel agent dispatch**”

`.arch/CHARTER.md` still says: use `scripts/fleet-launch`, definition of done includes “fleet view shows healthy estates,” command-surface sweep includes `fleet`. That file is what `AGENTS.md` tells the next agent to read first.

`CHANGELOG.md` still documents `ward fleet` and fleet tests. `internal/cli/fleet.go` still exists (wave lives there). Harvest, scorecard, timeline, skill-sync are still top-level commands after a “solo tool” pivot.

You did not retire parallel dispatch. You deleted one command and left the cosmology.

### 3. Protocol v5 is now the problem you named

You wrote: the protocol is a social bet. Then you added rules 4b, 4c, and 9.

- **4b** restates the phantom-gate policy the CLI already enforces (and the hole it doesn’t).
- **4c** tells agents to poll GitHub Actions. That is a different product (CI oracle), not a session protocol.
- **9** `Review: <who verified>` on every commit. HEAD ends with `Review: self-review`. That is the ceremony of verification without the act. The thesis is *live check against the repo*, not a footer. Agents will copy the footer. You trained them to.

A protocol agents actually follow is ~5 lines. Yours is a policy manual. Longer protocol + “we know they might not read it” = you are papering over the social bet with more paper.

### 4. The phantom gate is a denylist of three tokens

`isTrivialVerify` rejects `true`, `false`, `:`. Tests **explicitly accept** `echo done`. The error string still says `true/echo/:`. Docs and matcher already disagree.

Your own task tests use `go version` as a gate. Always green. Does not exercise the change. Same class as `true`, more respectable-looking.

An agent that wants to cheat will write `echo ok`, `test -f README.md`, or `go test ./...` in a package the diff did not touch. You cannot lint intent. Pretending three bash no-ops are the “neverphantom” product oversells a filter.

The real gate is: **the check is about the change**. That is a human/spec problem. Sidecars prove *something ran*, not *the right thing ran*.

### 5. Sidecar is a second, forgeable source of truth

Comments say the DB is system of record. `gateEvidence` then closes tasks by **reading `.ward/logs/`**. Consequences:

- Delete the logs → a verified run cannot `task done`.
- Write a fake `exit_code=0` file → close without a real run (local CLI, the agent *is* the attacker).
- Multi-node run: `FindSidecar` takes the **lexically last** log for that `runID`. A later passing node can vouch for an earlier failure.

Fatal sidecar write on exec is the right call (no silent success). Using the file as the close-gate without hashing it into the DB is not. If evidence matters, persist `exit_code` + output hash **in SQLite** and treat the file as a convenience copy.

### 6. Surface area is still a campaign platform

After “solo tool,” the default `ward` still grows:

`brief init memory verify route router run capture task explain reject doctor workflow tick harvest skill timeline wave scorecard sync version`

A solo loop needs: **init, brief, task, memory, tick, maybe wave**. Harvest/scorecard/timeline/skill-sync are R&D. Keeping them at the top level is how identity stretch survives a pivot.

`.spec/` is 16 files including drafts you promised not to implement (flake, regrading, scoped-vouching, runner-seam, execution-policy) plus `broker.md` for a retired idea. Specs without code are a second unfinished product.

`public_agent_feedback/` (17k OpenAI dump + this review series) does not belong in the installable tree. Research notes go in `.arch/` or a wiki. Shipping other models’ essays next to `main.go` is not transparency; it is clutter with a halo.

`.arch/tasks.md` is 1,518 lines. That is a lab notebook. Nobody will find the live open items.

### 7. Execution is still one vendor, and it already blocked you

`c0fd425`: “H2 attempt blocked at pre-declared gate: opencode gateway still down.” The adapter is still `opencode` + three free model slugs. `runner-seam.md` is a draft. For a tool whose value is *routing and verification*, being unable to run work because a gateway is down is an existence failure of the execution seam, not a weather report.

### 8. CI does not match the Makefile story

CI: `go build` + `go test`. Not `make check`. `gofmt` is local-only. Go 1.26 in CI vs whatever users have. Fine for a lab; not for “Makefile is the release gate.”

---

## Today’s commits, specifically

| Commit | Take |
|---|---|
| sidecar + `task show` + neverphantom | Right direction. Overclaimed. |
| derive evidence from disk, soften echo, fatal sidecar | Softening echo is honest; fatal sidecar is correct; disk-as-gate is the design bug. |
| `force-closed` + Review footer + rename feedback file | Status split: good. Footer: theater. Rename: good, directory still shouldn’t ship. |

`--force` **always** writes `force-closed`, even if evidence exists. If agents learn `--force` as the way to close, you destroy the signal you just added.

---

## What I would believe if I cloned this tomorrow

| Claim | Reality |
|---|---|
| v0.8.0 shipped | Commit exists, tag does not |
| Solo tool | Fleet command gone; fleet language, charter, changelog, harvest/scorecard remain |
| Never phantom success | `true` blocked; `echo done` and `go version` allowed |
| Protocol briefs every agent | Long, optional, self-admittedly unenforced |
| Consolidation | More spec drafts, more protocol, more feedback files |

Thesis is still intact in the router. The product around it is a week of good engineering sitting on an untagged release and a docs layer that has not caught up to the pivot.

---

## If you want this to be a tool, not a lab

Do these and **stop**:

1. **Tag `v0.8.0`** on a tree that does not contain `ward.db`. Update CHANGELOG for the pivot (delete fleet bullets). Point CI at `make check`.
2. **Rewrite CHARTER** to match solo. Kill fleet-launch from definition of done.
3. **Fix the README contradiction** in the pool paragraph. Escalation is *the same agent, next session, higher floor* — not “stronger agents.”
4. **Cut or hide** harvest, scorecard, timeline, skill-sync behind `ward rd …` or leave them undocumented internals. Solo default path: brief → task → tick.
5. **Persist evidence in SQLite** (exit code, cmd, output hash). Sidecar stays a dump. Close-gate reads the DB.
6. **Delete protocol 4c and 9.** Keep brief / trust / pool / no-hand-typed-put / handoff. Five rules.
7. **Move `public_agent_feedback/` out of the repo** (or under `.arch/reviews/` and gitignore the dumps).

Do **not**: more phantom-lint, more spec drafts, more Review: footers, reopen the broker, poll GitHub from `task run`.

The gap is no longer “does the thesis work.” It is whether you will freeze a small honest tool or keep annotating a large honest lab. Right now it is still the lab, with better footnotes.