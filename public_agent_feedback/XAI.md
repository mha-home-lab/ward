**Review of Ward at `5992df9` (2026-08-23)**

This is a different project than the one we froze at v0.2. The core thesis still holds, and you have real field evidence. You also accumulated a lot of surface area faster than release hygiene, which is exactly what your own charter now says.

---

### What improved (real)

**The usefulness path landed.**  
`ward init` → `ward brief` → `ward run start --auto-approve` is a coherent agent loop. Protocol injection into `AGENTS.md` is the right integration point. Exclusive claims, `verify_cmd` from `run:`, default workflow, tick expiry sweep — those earlier review items are reflected in the docs and command tree.

**Dispatch pool is the right product shape.**  
`task add / next / run` with tier floors is more useful than “write a YAML DAG for everything.” That is closer to how agents actually work.

**Dogfooding is serious.**  
Four repos, greenfield A/B, quality battery, fleet vs solo with a pre-declared falsifier. The R12/R13 result is the most important finding in the repo:

| Mode | Small greenfield + strong model |
|------|----------------------------------|
| Solo | ~20 min, quality parity |
| Fleet | ~45 min (~2.3× slower), quality parity |

Fleet did not degrade quality; it paid coordination tax. Default **solo**; fleet only when scope exceeds one session or lanes need different tiers. That is product guidance, not vibes.

**Accounting stayed honest.**  
Failed work visible, no silent success, takeover path worked. That is still the differentiator vs “another agent framework.”

---

### What’s weak or risky

**1. Identity stretch**  
Ward is no longer “verify-gated routing.” It is memory + routing + task pool + skills (“chips”) + harvest + timeline + wave + fleet + scorecard + skill-sync. The thesis is still the load-bearing idea; the CLI is becoming a multi-agent ops console. That is fine if intentional — dangerous if every R&D wave adds a verb.

**2. Release state lags the narrative**  
- Only tag present: `v0.3.0`  
- Commits and tasks talk about v0.5–v0.7, charter aims at **v0.8.0**  
- No `Makefile`, no `CHANGELOG.md`  
- `ward-bin` (~12MB) checked into the tree is a smell  

Charter P0 is correct: stop features, stamp version, ship a clean install path.

**3. Command surface grew faster than the contract**  
Roughly 20 top-level commands, ~5k lines under `internal/cli/`. Fast-built paths (timeline, wave, fleet, scorecard, skill pack/check, explain, reject) need the audit the charter lists: `--json` always valid, flag parity, one-line errors, Example on help. Until that is green, this is a lab tool, not a product other people install once.

**4. Scaffold vs dogfood workflows**  
README promises `workflows/default.yaml` from `--scaffold`; the repo still only ships demos (`go-test-demo`, `agent-demo`, …). Either commit a real default or make the README path match what ships.

**5. Scope of “fleet”**  
`scripts/fleet-launch` shells `opencode` per lane. Useful for *your* dogfood. It is not yet a general multi-agent runtime. Positioning should stay: “supervisor script for pool work,” not “distributed agent OS.”

---

### Verdict

| Dimension | Grade |
|-----------|--------|
| Thesis integrity | Strong — still the spine |
| Field proof | Strong — solo/fleet A/B is real science |
| Single-agent usefulness | Good — brief + task + capture is usable |
| Product cleanliness | Weak — tags, Makefile, CHANGELOG, binary in tree |
| Surface discipline | At risk — too many verbs relative to docs/tests |

You did not lose the plot. You proved the plot, then built a campaign platform on top of it. The charter’s “consolidation over capability” is the right next move; do not reopen the engine for a broker or MCP.

---

### What I would do next (aligned with your charter)

1. **P0 release engineering** — `Makefile` (`build` with ldflags, `test`, `fmt`, `vet`, `install`), remove checked-in `ward-bin`, `CHANGELOG.md`, tag **v0.8.0** (or whatever matches reality; don’t invent version numbers that aren’t on the tag list).  
2. **Command-surface audit** — every subcommand: valid empty JSON, consistent flags, Example in help.  
3. **README as product** — keep the current quickstart; add a one-page concepts map (brain / pool / chips) and demote campaign/fleet to Ops.  
4. **Do not** start flake quarantine, federation, or pickup-loop productization until (1)–(3) are done.

**Bottom line:** Ward is past “interesting design.” It is a dogfooded control plane with measured limits on when parallelism helps. Ship a clean v0.8 and treat everything else as optional until a fresh clone → `make test && make install` → brief → task close is boringly reliable.
