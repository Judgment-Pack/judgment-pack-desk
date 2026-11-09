# Canonical background model operations

The original input-research workflow reserves one model attempt before dispatch.
Its journal, approval, Plan/Work admission, source proofs, acquisition, retry and
recovery rules remain the owners of that attempt. `runBackgroundModel` is an
internal capability for that reservation, not an HTTP endpoint or another
provider loop. This change does not activate the conversational executor, remove
browser engines, or enable memory lifecycle work.

API execution uses the released Agent `adapter/eino` with trusted
`Input.RequestCap = 1`. The cap is configured before initialization and persists
as `requestCap: 1` in the canonical checkpoint. The checkpoint must round-trip
through `context.ReadExecution`; no detached controller state, synthetic prior
requests, grants or private checkpoint reader establish an allowance. The last
reserved request has tool dispatch closed. Empty, truncated, filtered, tool-call,
unknown and omitted completion metadata cannot trigger another model capability
call. The host accepts only one canonical completion with finish reason `stop`.
Unknown and omitted stop metadata were accepted by the older background path;
this structured one-request binding intentionally refuses both. The canonical
completion and shared failure helper retain reported counters and safe failure
metadata without exposing raw provider errors.

Native execution uses the released `adapter/agent` around Desk's existing private
bridge. Its accounting remains provider-managed: used zero, limit one, no request
cap or fictional request counts. Only the fixed host response tool is offered;
it validates the bounded host-authored schema before accepting one response.
Native final prose cannot bypass that tool. A final native message is still
required by the canonical adapter. Delegation, proposals and domain writes are
unavailable. The bridge's current admission and before-generation callbacks
consume the same reserved operation exactly once.

Agent owns request encoding, response parsing, prompt framing, context management
and output guards. The selected project's canonical prompt host supplies explicit
brief and project instructions, with memory disabled for this workflow. The
existing `aiContext` policy performs a final byte/size refusal on canonical API
request bytes or native prompt/instructions/tool schemas. It does not reinject
legacy brief or memory text or parse a second provider response.

The optional `background` member belongs to the original attempt. It retains the
canonical checkpoint, exact accepted action, successful API status/body or native
events and tool argument/answer pairs, and normalized failure. The complete encoded receipt is capped at 4 MiB (the canonical checkpoint
maximum), including its checkpoint, wire and action members together. This bound
plus enclosing JSON framing is reserved
before model admission, alongside the original bounded result/terminal reserve.
All bytes pass through the original journal encoder and its existing 16 MiB
record limit; field limits apply as well. Near-capacity journals refuse before
any model capability is dispatched. Required members, canonical checkpoint validity and fixed
accounting are checked on every decode. Receipt identity, dispatched status and
observed completed result bytes cannot be cleared or replaced. Checkpoint identity
and cap cannot change or move the used count backward. Finalized attempts retain
the complete receipt unchanged. A present receipt is one-use even after
reconstruction, including an unacknowledged save or uncertain delivery. The
owning workflow can reuse its original completed result; this capability never
replays the provider operation.

Before physical dispatch and receipt writes, the capability rechecks the current
project location/identity, sign-in epoch, saved connection/revision/model/effort,
profile, configured brief, credential binding and native account. It compares the
live original `Recipe.AI` or `Portable.Frozen.Choice` with the retained task choice,
and preserves the workflow revision and Gateway pin. Portable checks reuse the
owning workflow's current Runner/profile/Gateway and chat Work admission. Source
proof reads remain with the original research store. Companion reads occur
outside journal transactions; the short original transaction rechecks exact run,
attempt, choice, revision and Gateway before saving. Failed custody closes the
operation without accepting a stale response. Cancellation and unknown delivery
remain distinct from host acceptance refusal; neither authorizes replay.

Focused fixtures cover all three API families, actual private native Manager
execution, cap/checkpoint reload and closed finishing, strict journal bounds and
immutability, current choice/source/profile/credential/epoch changes while queued
and after dispatch, cancelled admission, lost acknowledgements and uncertain
transport. Focused safeguard mutants must be rejected. Full deterministic suites,
independent contract review and production activation remain separate gates.
