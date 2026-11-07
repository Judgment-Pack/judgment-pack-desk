# Prompt digests in run reports

The shared `RunReport` optionally carries `prompts`, copied from the canonical
Agent execution checkpoint. Each row contains only `request` and `sha256`.
Browser summaries and server-owned saved-chat projections retain exact values
and ordering; saved response-history reads clone the rows so later display
changes cannot mutate the checkpoint or stored input.

The saved report reader accepts at most the Agent's 300 retained rows, each with
a positive safe integer request ordinal and a lowercase 64-character SHA-256.
Ordinals strictly increase. A retained window can begin above one and contain
gaps; ordinals are not constrained by report requests, retries or the accounting
unit. An absent field remains absent, and an empty array stays empty. Unknown
row members, malformed hashes, invalid ordinals and oversized arrays refuse the
saved history with its existing fixed validation message, without displaying
input contents. Older reports without this field remain readable.

A small native disclosure beside the request summary is closed initially. When
opened, it shows request ordinals and exact selectable, wrapping digests in a
shared-token table. Missing or empty rows show no disclosure. The display is
identical for model-request and provider-managed counters. It does not infer an
omitted-row count from ordinal gaps.

Paused and interrupted browser checkpoints retain rows through summary, save
and reload. Server pending continuations remain pinned in their existing ledger;
this change does not create a historical response for them. Terminal projection
copies retained rows into both the displayed checkpoint and saved response
report, including recovery after a prepared write.

Digests are historical facts. Desk does not reconstruct prompt text, recompute
digests, renumber requests, infer permission or change execution, admission,
approval, continuation, allowance or provider behavior from them.
