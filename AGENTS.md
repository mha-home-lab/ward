# AGENTS.md

Before continuing work on this repo, do two things:

1. `ward tick` — re-verify store-local artifacts live and free any expired
   claims, so a stale or timed-out reservation can't block you.
2. `ward memory context <topic>` — pull relevant prior knowledge (ids, kind,
   summary, tags, verify status) into your context. Prefer verified facts over
   guessing.

On success, `ward` already auto-captures: a `run:` node that succeeds writes a
store-local accepted artifact (tagged by node id), so the next session can route
it cheap without you doing anything. You do not need to record results by hand.

Do not trust unverified summaries. A memory hit only votes for the cheap tier
when it is both memory-resident AND verified against real repo state — an
unverified, stale, or imported artifact counts as a miss and routes to a stronger
tier. Treat a routing decision's `context` (verified artifact ids) as the source
of truth, not a human-written recap.

Do not hand-type `ward memory put`. The auto-capture path is the supported way
results enter the store; `put` defaults to untrusted and is for humans to
deliberately cross the trust boundary. Never write a `verify_cmd` you wouldn't
run yourself.
