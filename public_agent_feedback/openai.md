I inspected the uploaded repository snapshot directly. I could not `git clone` from this runtime because outbound GitHub/DNS access was unavailable, but the ZIP is a full source tree and I reviewed the actual Go code, tests, workflows, specs, and README—not the changelog.

I also tried the test suite. The source is formatted, but this environment only has Go 1.23.2 while the project requires Go 1.26.3, so `go test ./...` attempted to download Go 1.26.3 and failed because this runtime has no network access. So I am **not claiming the tests pass**.

# Executive verdict

**Technically: 7.5/10**

**Product: 6/10 today, potentially 8/10 with a sharper integration model.**

The core idea is good.

The current product is **too broad and too tied to its own agent protocol**, and there is one major conceptual problem:

> Ward's "memory" currently changes **routing**, but generally does not change what the agent actually knows.

That means a verified hit can cause a task to run on `cheap`, but the cheap agent may still have to rediscover the solution.

That is the biggest product-level weakness I found.

---

# 1. Technical review

## What is genuinely good

### The core architecture is surprisingly coherent

The important dependency direction is good:

```text
CLI
 │
 ▼
orchestration
 │
 ├── routing      pure policy
 ├── verification trust gate
 ├── adapter      execution
 ├── observe      repo state
 └── store        durable state
```

`routing.Route()` is genuinely pure. It has no database access, model calls, or filesystem effects. That's exactly how I'd want the policy engine designed.

The router has a nice property:

```text
inputs → decision
```

rather than:

```text
inputs → LLM → probably-a-decision
```

That makes the important policy testable.

---

## The verification boundary is the strongest part

`memoryHitForNode()` does something important:

```text
candidate artifact
       ↓
live verification
       ↓
verified?
       ↓
routing signal
```

rather than trusting:

```text
artifact.verify_status == "verified"
```

That is the right architecture for your thesis.

And the imported-artifact boundary is sensible:

```go
if !a.Local {
    return unknown
}
```

so an imported artifact can't simply smuggle an arbitrary `verify_cmd` into the execution path.

That's a real security consideration, not decorative security.

---

# 2. The biggest technical issue: state transitions aren't atomic

This is the thing I'd fix before calling Ward a reliable control plane.

The engine does things like:

```go
_ = e.Store.UpsertRunNode(...)
_ = e.Store.SaveRun(...)
_ = e.Store.AddEvent(...)
```

There are **many** ignored persistence errors in `engine.go`.

That is dangerous because those operations aren't telemetry.

They're the state machine.

For example:

```text
execute succeeds
      ↓
UpsertRunNode(done) fails
      ↓
SaveRun(completed) succeeds
```

Now the durable database can say:

```text
run = completed
node = running
```

Or:

```text
execute fails
      ↓
node failed persistence fails
      ↓
escalation event succeeds
```

Now the audit trail says one thing and the state says another.

For an ordinary CLI, I'd call this poor error handling.

For **Ward**, whose entire value proposition is durable state + trustworthy transitions, it's an architectural problem.

### Recommendation

Have the engine return errors for state mutations.

Better still, group logically inseparable transitions in SQLite transactions:

```text
BEGIN

routing decision
run_node state
run event
run state

COMMIT
```

You don't need a giant transaction framework.

Just make the important state transitions atomic.

---

# 3. Run reproducibility is incomplete

You persist:

```text
workflow_path
```

which fixed the resume bug.

But you still reload the YAML from disk.

That means:

```text
T0:
    workflow.yaml = version A
    run starts

T1:
    workflow.yaml modified → version B

T2:
    ward resume
```

Ward resumes **version B**, not necessarily the workflow that created the run.

That's a reproducibility problem.

You need at least:

```text
workflow_path
workflow_hash
```

and ideally eventually the workflow definition itself.

At resume:

```text
hash(current workflow)
        ==
hash(run.workflow)
```

If not:

```text
workflow changed since run creation
```

and refuse or require an explicit override.

For an orchestration system, this is important.

---

# 4. The task pool is good, but its semantics are "at least once"

This part is actually quite nice:

```text
open
  ↓ atomic claim
claimed
  ↓ execute
done
```

The conditional update is a reasonable SQLite approach for cross-process claiming.

But:

```text
agent executes task
      ↓
process dies
      ↓
CompleteTask never happens
```

leaves:

```text
claimed
```

even though the work may have completed.

Then another agent can `take` it and execute it again.

That's not necessarily wrong.

It is an **at-least-once execution model**.

But the project currently doesn't explicitly call it that.

You should define this.

Because once agents are modifying repositories, users need to know whether:

```text
ward task run
```

is:

* exactly once
* at most once
* at least once

Right now it's effectively **at least once**.

That's acceptable for many workflows, but then `run:` tasks need idempotency guidance.

---

# 5. I found a real weakness in topic compounding

You implemented:

```text
task A
tags = topic:foo

       ↓

artifact
tags = topic:foo

       ↓

task B
tags = topic:foo

       ↓

cheap
```

Good idea.

But `memoryHitForNode()` discovers candidates by searching:

```text
node.ID
node.Kind
```

and only **after retrieval** checks topic-tag intersection.

So topic tags aren't actually the retrieval key.

The current test happens to work because the captured artifact's summary contains the node kind (`test`), which gets it into the candidate set.

That's accidental coupling.

The clean solution is to query the store directly:

```text
exact node tag
OR
any node topic tag
```

rather than:

```text
FTS search node ID/kind
→ filter tags afterwards
```

This becomes important once topic compounding is a central product feature.

---

# 6. The biggest thesis problem: Ward doesn't actually give the verified knowledge to the model

This is the most important observation from reading the implementation.

You persist:

```json
["493d80f1"]
```

as routing context.

Excellent.

But when you actually execute a prompt:

```go
adapter.Run(
    e.repo(),
    adapter.ModelForTier(string(dec.Tier)),
    node.Prompt,
)
```

the adapter receives:

```text
repo
model
prompt
```

**not the verified artifact content.**

So:

```text
verified knowledge exists
       ↓
Ward chooses cheap
       ↓
cheap agent receives original prompt
       ↓
cheap agent solves it again
```

That's inconsistent with the README slogan:

> "never re-solve a solved problem"

What you actually have is:

> **"Don't spend an expensive model on a problem whose prior solution is currently verified."**

That's still useful.

But it is a different product.

And honestly, I think the second formulation is **more defensible**.

You don't necessarily want Ward to dump previous model output into the next model's context. That can create anchoring and prompt-injection problems.

Instead, I'd make the verified artifact **available**, not automatically injected:

```text
ward context <topic>
```

or perhaps give the adapter structured evidence:

```json
{
  "verified_facts": [
    {
      "id": "493d80f1",
      "summary": "...",
      "content": "..."
    }
  ]
}
```

Then the agent decides whether it needs it.

---

# 7. The adapter is currently the biggest product/architecture mismatch

Your routing layer claims:

```text
cheap
mid
strong
```

provider-independent.

Good.

But the actual adapter is:

```go
Binary = "opencode"

cheap = opencode/hy3-free
mid   = opencode/mimo-v2.5-free
strong = opencode/nemotron-3-ultra-free
```

So Ward is currently **an opencode orchestrator with an abstract router on top**.

That is not inherently bad.

But it conflicts with your stated product:

> agent-agnostic orchestration.

The adapter interface needs to become a genuine seam.

Something like:

```text
Router
  ↓
ExecutionRequest
  ↓
Runner
  ├── opencode
  ├── codex
  ├── claude
  ├── shell
  └── custom
```

Ward should not care which one executes the work.

---

# 8. Product review

Now the more important question:

## Would people actually use this?

### Solo developer using one strong agent?

**Probably not.**

If I'm using Codex or Claude Code and I'm working interactively, Ward adds:

```text
ward init
ward brief
ward task next
ward task run
ward memory handoff
```

to a workflow that already works reasonably well.

That's friction.

And modern coding agents already have persistent instructions, skills, tools, sandboxing and long-running workflows. Codex, for example, explicitly consumes `AGENTS.md` instructions and supports persistent agent workflows. ([OpenAI][1])

So Ward isn't going to win by saying:

> "I provide an agent loop."

The agent already has one.

---

# 9. Where I think Ward *could* be valuable

### Multiple agents working on one repository.

This is where the project becomes interesting.

Imagine:

```text
                 ┌──────────────┐
                 │     WARD     │
                 │              │
                 │ task pool    │
                 │ claims       │
                 │ verification │
                 │ routing      │
                 └──────┬───────┘
                        │
          ┌─────────────┼─────────────┐
          │             │             │
       Codex          Claude        Qwen
       cheap           mid          strong
          │             │             │
          └─────────────┼─────────────┘
                        │
                     repo
```

Now Ward isn't replacing Codex.

It's **coordinating Codex**.

That is a much stronger proposition.

And it aligns with where coding agents are going: Codex itself is explicitly being positioned for multi-agent workflows and background engineering work. ([OpenAI][2])

---

# 10. Would Codex "like" Ward?

### Conceptually: yes.

### Technically today: not really.

Codex understands `AGENTS.md`, so your `ward init` protocol is a viable integration mechanism. ([OpenAI][1])

Codex can also use CLI tools and custom commands, so Ward can be presented as a composable local tool rather than requiring a custom model integration. OpenAI's own Codex use cases explicitly include building CLIs that Codex can use. ([OpenAI Developers][3])

But **Ward currently drives opencode**, not Codex.

That's backwards for the product you are describing.

I'd want:

```text
ward task next --by codex --max-tier mid
```

and then Codex itself performs the work.

Ward shouldn't spawn opencode unless opencode is the selected worker.

---

# 11. Would GPT models like it?

Yes, **if Ward becomes a tool/control-plane interface rather than a competing agent harness**.

GPT coding models already operate well with structured tools, shell, MCP/custom tools, and durable instructions. ([OpenAI Developers][4])

The attractive interface is:

```text
GPT
 │
 ├── ward brief
 ├── ward task next
 ├── ward memory context
 ├── edit files
 ├── tests
 └── ward task run
```

rather than:

```text
Ward
 └── secretly launches another LLM
```

The former complements the model.

The latter competes with its existing harness.

---

# 12. There is a security problem with the current product model

This deserves emphasis.

Codex locally runs inside sandboxing by default on macOS/Linux, with restrictions around filesystem and network access. ([OpenAI CDN][5])

Ward's execution path is:

```text
sh -c <workflow.run>
```

with no sandbox.

So if you integrate Ward directly into an agent workflow, you've potentially created:

```text
agent
  ↓
ward
  ↓
arbitrary shell
  ↓
host
```

That's a substantially weaker boundary than the agent itself.

Your artifact `verify_cmd` trust boundary is good, but **the task/run command boundary is still intentionally unrestricted**.

That's fine for a personal local Unix tool.

It is not fine as the security model for a fleet product.

If you eventually want:

```text
Codex + Ward
Claude + Ward
remote agents + Ward
```

then Ward needs an explicit execution policy:

```text
trusted local
sandboxed local
container
worktree
remote worker
```

rather than one universal `sh -c`.

---

# 13. I would also cut the product surface

The repository has accumulated:

```text
memory
tasks
skills
fleet
wave
sync
harvest
scorecard
timeline
dossier
doctor
brief
claims
routing
verification
```

That's a lot.

The core product is actually only:

```text
        task
         ↓
       route
         ↓
      execute
         ↓
      verify
         ↓
      persist
         ↓
       learn
```

Everything else should justify itself against that loop.

I'd temporarily demote:

* `skill`
* `harvest`
* `scorecard`
* `fleet`
* `wave`
* R&D telemetry

until actual users demonstrate they need them.

You don't need more machinery to prove the core idea.

---

# My product positioning

I would **not** market Ward as:

> "An agent orchestration framework."

Too crowded.

I'd position it closer to:

> **A local control plane for multiple coding agents: verified memory, task coordination, deterministic escalation, and evidence-backed completion.**

That is much more interesting.

The killer scenario is:

```text
10 coding agents
1 repository
100 tasks

Ward decides:

who can take what
what knowledge is still valid
when cheap models are safe
when work needs escalation
when agents conflict
when a task actually succeeded
when a human must intervene
```

That is a real problem.

---

# My priority list

If this were my repository, I'd do these in this order:

### P0 — Fix correctness

1. **Stop ignoring state-persistence errors.**
2. Make critical state transitions transactional.
3. Persist workflow hash/version for resumability.
4. Fix topic-tag retrieval so it isn't accidentally dependent on FTS text.

### P1 — Fix the product boundary

5. Introduce a real `Runner` interface.
6. Make `opencode` one adapter, not *the* adapter.
7. Add a Codex adapter or make Codex the external worker.
8. Make verified artifacts accessible to the worker as optional evidence.

### P2 — Prove the product

Run a real experiment:

```text
20 tasks
3 agents
cheap / mid / strong
```

Measure:

```text
cheap-hit %
tokens/task
wall-clock/task
failure rate
escalation rate
duplicate work
human interventions
```

**That data is far more valuable than another 1,000 lines of Ward.**

---

## Final assessment

I think you have **something worth continuing**.

But I would change the thesis slightly.

Today Ward is strongest as:

> **a verified state + task coordination layer around coding agents.**

It is **not yet** a compelling replacement for the agent harness itself.

And I would specifically avoid trying to make Codex "use Ward's agent loop." Let Codex remain Codex. OpenAI's current direction already emphasizes persistent instructions, tools, skills, background work and multi-agent workflows. ([OpenAI][1])

Instead:

```text
                    WARD
        ┌─────────────────────────┐
        │ state / routing / proof │
        │ tasks / claims / memory │
        └────────────┬────────────┘
                     │
       ┌─────────────┼─────────────┐
       ▼             ▼             ▼
     Codex         Claude         Qwen
       │             │             │
       └─────────────┼─────────────┘
                     ▼
                  git/repo
```

**That's the product I would bet on.**

The biggest thing I'd want you to prove next isn't another feature. It's whether **three heterogeneous agents coordinated through Ward measurably outperform those same three agents working independently**—fewer duplicate edits, fewer stale-context failures, lower token spend, or faster completion.

If Ward wins that experiment, you've got a product. If it doesn't, the architecture is interesting but the product thesis isn't.

[1]: https://openai.com/index/unrolling-the-codex-agent-loop/?utm_source=chatgpt.com "Unrolling the Codex agent loop | OpenAI"
[2]: https://openai.com/codex/?utm_source=chatgpt.com "Codex in ChatGPT | AI Coding Agents for Software Engineering | OpenAI"
[3]: https://developers.openai.com/codex/use-cases?category=data&category=engineering&category=front-end&category=integrations&category=ios&category=macos&search=Automation&task_type=analysis&task_type=code&task_type=testing&team=engineering&team=operations&team=sales&utm_source=chatgpt.com "Codex use cases"
[4]: https://developers.openai.com/api/docs/guides/latest-model?model=gpt-5.2&utm_source=chatgpt.com "Model guidance | OpenAI API"
[5]: https://cdn.openai.com/pdf/97cc5669-7a25-4e63-b15f-5fd5bdc4d149/gpt-5-codex-system-card.pdf?utm_source=chatgpt.com "<visual_element id=\"e1\">"

