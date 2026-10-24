# Standalone authoring server runs

This boundary preserves AssistantPane, DescribeIt and GraphAuthor embeddings while moving their execution into the canonical host. It does not mount these components, install the production executor, or replace the current NewPackView/DraftPackView creation flow.

## Request and capture

`POST /api/assistant-authoring/captures` accepts one closed purpose: `author-pack`, `describe-pack` or `author-graph`. Person-supplied prompt, purpose-specific arguments, draft and source locators are request data. They are not system instructions, evidence, permission or an interchangeable model transport.

Desk independently captures current authenticated identity, session, project, policy, connection revision, profile and selected model. Rooted readers capture saved pack/graph declarations, selected packs and typed saved cases. Unsaved buffers are retained separately. A saved graph's original digest must agree with the browser comparison basis. Sources and controls are checked again before admission and physical dispatch.

The host returns an exact owner, revision and admission reference. A client request key provides scoped idempotency only; it grants nothing. `GET /api/assistant-authoring/captures/{request}` discovers that reference without writing or starting work. The ordinary canonical run endpoint admits at most one run for the capture's lifetime, including after completion. A lost capture acknowledgement is reconciled by read; an uncertain start is discovered, never resent.

## Execution and acceptance

Purpose-specific preparation uses the shared input compiler, Runtime guidance, adapter, context, tool registry and dispatch gate. API and native execution use this same boundary. Native provider-managed usage remains provider-managed; Desk does not invent an internal request count.

Canonical snapshots and events are projected through the shared browser readers. Proposal data is plain and frozen; completed snapshot state owns the proposal. Current local buffer generation, revision, dirty/loading/saving state and compare-and-swap checks still control Apply, Undo and Create. Graph proposal, review and write remain separate. A check or generated proposal never authorizes a write.

Plan, paused question/budget, stopped, interrupted, historical and ended-session states withdraw proposal acceptance. These standalone embeddings have no new answer/continuation interface in this slice. Their retained paused state does not silently continue work.

## Cancellation, persistence and recovery

`POST /api/assistant-authoring/captures/{request}/cancel` requires the exact returned owner and revision. It cancels only the capture's associated admission/run and signals its active context after durable mutation. There is no permission derived solely from a client request key and no pre-capture cancellation tombstone. A canceled late browser continuation cannot start a run.

Route unmount detaches observation. Explicit Stop or dismissal cancels; discarding a local view during navigation does not create another request. Session end clears private displayed state. Restart retains completed work and marks unfinished operations interrupted without replay.

Optional `authoringCaptures` is part of the existing private run ledger. Captures and runs share its 128-entry capacity and aggregate byte bound. Exact request bytes and independently captured source bytes survive backup, move and restore. Historical restore preserves those bytes while revoking execution authority. Completed/canceled entries remain retained; capacity exhaustion refuses new work rather than evicting uncertain work. Reference reads never perform maintenance writes.

## Verification and remaining gates

Handwritten strict-reader and lifecycle tests cover all three purposes, identity/source drift, malformed/duplicate/oversized requests, current policy/model binding, cancellation, single admission, restore, local Apply/CAS/Undo/Create and graph review/write. Temporary observations produced by actual API/native fixtures pass the canonical client, snapshot, SSE, proposal and execution-report readers. They are not recorded golden vectors. `assistant-authoring-readers` registers that producer/consumer step in CI.

The three safeguards for source drift, cancellation before admission and late continuation, plus the seven migrated guidance/proposal/capture/dismissal/tool-offer mutation probes, must fail their corresponding behavioral tests when removed. Ordinary build/test evidence does not substitute for those verdicts.

The live application creates packs through saved chats in NewPackView/DraftPackView.
Their [generic creation workflow](creation-server-runs.md) now owns historical
display, checks, independent cases, expectation review, fresh research choices and
current readiness. Global engine deletion and factory activation still wait for
the remaining release gates. No new route or compatibility provider loop is introduced.
