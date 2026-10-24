# 0014: Server-owned assistant runs

Status: **accepted architecture; implementation in bounded slices**. This record does not authorize or claim a completed production cutover. Slices 1–2 supply the durable host ledger, storage integration and startup/reacquisition recovery. Slice 3 adds the authenticated run protocol and a host execution lifecycle. Production purposes remain unbound; existing browser execution is still active.

Scope: [Desk #537](https://github.com/Judgment-Pack/judgment-pack-desk/issues/537), following the reviewed v0.3.0 prerequisite. The [32-row ownership matrix](../design/assistant-run-surfaces.json) records migration points and verification obligations, not already delivered behavior.

Published Go dependency: `github.com/Judgment-Pack/judgment-pack-agent/go v0.6.0`, a normal tagged module with no replace/vendor. The browser package also pins the attested `@judgment-pack/harness` v0.6.0 release asset through the component lock. Root and `go/v0.6.0` tags resolve to `3f308215afadd9d72ae963ebea1062247d1963ba`; the archive SHA-256 is `5bc4e0314d79b2f5c68296b0ebd0e0f8044444d4a9f50d68b6c0712c40b5c2b0`. Release-workflow asset attestations, tag/source identity and module origin/checksums are verified during adoption. This does not assert Git tag-signature verification. The approval framing-marker correction is included in this released pin. Optional skills are not declared or enabled by this component update; their Desk workflow remains separate. See the [v0.6.0 adoption note](../design/agent-v060-adoption.md) for compatibility evidence and limits. Browser/server production cutover remains pending.

## Decision

The Go server owns every conversational/tool run and every model-dispatch admission. API connections use published `adapter/eino` and `adapter/delegate`. ChatGPT uses Desk's actual `internal/codexbridge` with a Desk-owned transport adapter around the published context/gate/turn primitives. The browser keeps editing, rendering, human actions and a single authenticated run client. It cannot supply an executable adapter, model capability, host-tool declaration/classification, approval, trusted checkpoint, or arbitrary runtime dispatch.

Use **authenticated REST plus fetch-streamed SSE per durable run**, rather than extending `/api/agent/run`. The existing socket binds a run to its reader: `agent_run.go`'s read goroutine defers cancel; `runner.Run` forwards a tool call to that browser and waits for its answer. Socket closure ends/reaps the provider run. It has no durable cursor or reconnect semantics. A browser `EventSource` cannot add the existing bearer header, so use `deskFetch`/fetch plus an SSE parser. No credential in URLs, cookies or event IDs.

Preserve original conversations, draft/check journals, retained exact document bytes, typed facts, test expectations, proposal/review and operation receipts. A derived context or snapshot is never action authority. No release, browser fallback flag, or engine-choice selector is introduced between prerequisite and complete cutover.

## Verified published seams

The foundational context/gate exports below remain available in v0.6.0. Approval continuation and the provider-managed capability use the released additions described below.

| Purpose | Export verified in source | Desk use |
|---|---|---|
| API adapter | `adapter.Adapter.Run(context.Context,*adapter.Run) error`, `eino.New()`, `delegate.With(inner, delegate.Options)` | API conversational execution only; its model capability remains `relay.Call`. |
| Host gate | `gate.New(Options)`, `Gate.Open`, `SetOffer`, `Enter`, `Calls`, `Transport`, `TestCalls`; `PlanPhase`, `ChildPhase` | Host owns offers/phases/raw transport. Adapter gets `*gate.Calls`; children receive a new gate with `Options.Turn=parentGate`. |
| Model seam | `relay.Client{Handler,Family,Bearer,Seen}.Call()` | Handler is the actual selected Desk's `handleModelRelay`, through a bound host closure. Never replace it with package `relay.NewHandler`. |
| Managed agent context | `ContextualSession`, `Session{Agent:*AgentSession}`, `Input`, `SessionContext.AgentPrompt`, `Indexed`, `Check`, `Result`, `Frame`, `Snapshot`; `NewBoundary`, `Boundary.Event/Failed/Stopped/Close` | Basic Codex adapter uses canonical context, bounds and event/checkpoint behavior. No copied context/compaction engine. |
| Durable request accounting | `Input.Checkpoint(ctx,contract.Checkpoint) error`, `Input.Authorize`, `SessionContext.ClaimRequest` | API request claim/checkpoint must complete before dispatch; use one accounting owner, not two additive budgets. |
| Continuation | `ReadExecution`, `ExtendExecution`, `CanExtendInterruptedExecution`; `MaxCheckpointBytes`, `MaxRequestAllowance` | Server validates saved execution; only explicit user actions extend compute. Provider-managed runs do not get fictional model-request counts. |
| Domain corrections | `CorrectArtifact`, `CheckArtifact`, typed ports | Move existing feature correction orchestration using these helpers; keep exact candidates/findings and workflow ownership. |
| Contract parsing | `turn` proposal/fence/question/critic/delegation readers, `failure` normalization/completion helpers, `contract` generated types | Reuse these for transport conversion, not a new private wire dialect. Generated contract structs preserve shapes but are **not schema validators** (`contract/support.go`). |

Important limits: released v0.6.0 `adapter.Run` exposes `Model relay.Call` and the alternative `Agent *adapter.Agent`; the latter is set instead of `Model` for provider-managed invocations through the host-bound bridge. `Input.Connection` is a string, not the TS AITarget object; the Desk run binding must retain ID **and revision** separately and its models callback must resolve that frozen binding. `Emit func(contract.RunEvent)` cannot return persistence errors. Host emission must synchronously persist required records and latch/cancel on failure; every capability rechecks that latch. Never mistake a void Emit for an acknowledged durable save.

## Proposed Desk protocol v1

These are new Desk API shapes for review, not claims about upstream APIs. All routes use existing `guard`, sign-in/principal/session epoch, selected-Desk resolution, `authorizePrivateWorkspace` and current object/Policy authorization **before private reads**. Reject unknown fields/methods/query members; use existing rooted stores and bounded JSON reader. Authentication, CSRF Origin/Host rules and current Policy apply independently on every route.

| Route | Closed request/result |
|---|---|
| `POST /api/assistant-runs` | `{version:1,requestId,owner,expectedOwnerRevision,requestRef,connection:{id,revision,model},preferences}`. `owner` is a discriminated saved chat/draft, pane capture, brief reservation or admitted workflow reference. `requestRef` identifies an already saved current user request or a server-retained exact pane capture. Preferences are only known thinking/effort/review choices validated against saved policy. Result `{runId,revision,state,cursor,snapshot}` after durable admission; never accepts prompt/system/tool/grant/checkpoint authority from the caller. |
| `GET /api/assistant-runs/{run}` | Canonical authorized snapshot, revision and current cursor. Reading never starts or continues work. |
| `GET /api/assistant-runs/{run}/events` | Fetch SSE with bearer/Desk headers and `Last-Event-ID`. `{version,runId,attemptId,sequence,kind,payload}`. `kind=contract` carries an unchanged shared `contract.RunEvent`; `snapshot`, `host_call`, `host_state` are clearly separate Desk envelopes. |
| `POST /api/assistant-runs/{run}/actions` | `{version:1,requestId,expectedRevision,action}`; finite tagged actions: cancel, submit-question-answer, extend-budget, continue, retry, select-research, accept-plan, or a specific domain review/apply action reference. Server reads and verifies the actual pending state and action-specific token/revision. It does not accept an arbitrary tool name or arbitrary new checkpoint. |
| `POST /api/assistant-runs/{run}/host-results/{call}` | `{version:1,requestId,leaseId,expectedRevision,result}` for one pending **server-selected** client-only callback. Result must fit that callback's closed schema and original arguments/resource digest. Duplicate identical result returns the durable receipt; different result or wrong owner/lease/call refuses. |

The stable start idempotency key is `(selected project identity, principal, requestId)`, with a request-body digest. The session epoch, owner/revision and all other immutable bindings are compared on a matching key; changing them refuses instead of creating a second run. Read existing key **before** rejecting an already consumed expected revision; a retry after lost response returns the same run, never creates another paid operation. A mismatched body under the same key is 409. The saved current request/turn ID, phase, Plan revision, connection/revision, profile/model preferences, Runtime identity and relevant resource revisions are recorded together before dispatch. The server derives host instructions, tools, configuration, memory setting, current request and documents from authorized saved state. Unsaved editor text is captured as explicit user-supplied data into a private, immutable, digest-bound pane record before start; it cannot redefine permissions or test expectations.

One owner may have one active response; root and delegated invocations belong to that response. Capture ephemeral panes under an owning workspace handle with a proposed 30-second attachment lease renewed every 10 seconds; do not invent a saved conversation for the UI. Closing a pane stops its transient activity by explicit cancel/lease expiry while its proposal/receipts remain available until normal owner cleanup. A saved chat's run survives subscriber disconnect. Deleting a chat continues to refuse while active, unless the existing explicit stop/delete flow has completed.

## Durability and recovery

Proposed host states: admitted, running, awaiting-input, awaiting-client, paused-budget, cancelling, stopped, interrupted, complete, failed. These are an envelope around the shared checkpoint; do not add unsupported values to `contract.Checkpoint.Status`. A normal adapter `end` is not a process-wide permission to retry. Record whether it ended for a question, callback absence, explicit stop, storage fault, provider fault or completion.

Persist in the rooted private store (outside project watcher/exported files), using current custody modes, stable file locks, atomic stage/fsync/rename/directory-sync and readback (`private_data.go`). Short mutations take storage locks; no lock stays held over provider/Runtime/network calls or SSE writes. The held project descriptor and project-move admission lease remain pinned through calls, as existing runtime/project-move code requires. Integrate the run registry into move/backup and shutdown/recovery; it cannot live in an untracked ad-hoc directory.

Store one latest canonical checkpoint, exact original domain journal references, ordered durable semantic events, and call/dispatch receipts. Use existing 16 MiB private-record envelope and the published 4 MiB checkpoint cap; oversized state pauses with storage failure **before** another request/write. Proposed live ring: at most 128 events and 1 MiB per run; retain at most one whole progress snapshot per parent/task, bounded by existing task/report limits. Whole accumulated prose deltas are live only; persist their latest snapshot at semantic checkpoints/terminal boundaries, not every prefix. Durable tool operations, final messages/proposals/questions/recovery and task records are preserved. Full admission fails when required receipt retention cannot fit; do not silently discard uncertain operations to admit more runs. Original user history has its existing retention/deletion policy, not an invented TTL.

A cursor identifies run attempt and sequence. If its replay window is unavailable (including a process restart), send a canonical `snapshot` with the new head cursor; client replaces derived execution state and deduplicates semantic records by stable IDs. This is reattachment, never an implicit continue. Backpressure disconnects a slow subscriber and lets it reattach; it cannot block durable checkpointing indefinitely.

`Input.Checkpoint` saves synchronously. API `ClaimRequest` invokes it before relay dispatch. Tool-call events and host callback intent are persisted before side effects, then their exact result/receipt before returning to the adapter. Host `Emit` uses a serialized, synchronous journal sink; storage failure latches an error, cancels the execution context and makes all model/host/Runtime admission closures refuse. Do not write an event to SSE and rely on a browser autosave for acknowledgement.

Use the canonical execution operation IDs created by `ObserveToolCall`, correlated with run/attempt/call ID; add a host dispatch receipt for the provider or domain operation. Receipt states distinguish reserved/not-started, dispatched, completed, refused and uncertain. Never infer safe replay from matching text/arguments alone; mutable reads remain fresh. Duplicate HTTP action/callback delivery resolves its persisted operation ID. A server restart marks dispatched/unanswered model calls and running operations interrupted/uncertain. It does not resume an ephemeral Codex thread or resend a write. Retry/Continue is an explicit new attempt with known receipts projected through the canonical saved checkpoint. Unknown effects require existing readback/reconciliation or explicit user recovery, never an automatic second action.

Question answers and compute grants are separate journaled user actions. Use `turn.ReadUserQuestion`/the existing host question policy and `ExtendExecution`; persist the exact answer/grant before continuation. Plan acceptance uses current pinned references and a distinct user action. An Auto artifact write still runs `assistantApprovalTransaction`/its adapters, with session/project/chat/resource/check-suite binding, consume-before-commit and current storage/dirty/CAS/readback checks. It never follows merely from a proposal or accepted Plan. Session/Policy revocation cancels live work and all child contexts. A newly authenticated session may read authorized durable stopped results, but cannot inherit old session-bound grants or attach to an old live invocation; a current session for the same principal/project may inspect paused records and explicitly `abandon` an inactive pause (`awaiting-input`, `paused-budget`, or `awaiting-client`). Abandonment requires current private-workspace and Policy-history rights, the exclusive project executor lease and no active invocation. It records an idempotent action receipt with the current authorizing session/Policy digests, closes the old dispatch authority and releases its owner reservation, retaining the original binding, approvals and exact completed/uncertain receipts. It never continues work, transfers grants or repeats an operation. A fresh start is a separate explicit action with fresh saved-source and request admission; normal cancel and approval remain bound to the original session/Policy.

## Bound capabilities

**Model:** Build a `relay.Client` per immutable admitted connection binding. Its handler closure rewrites the synthetic `http://desk.invalid` request's Host/URL expectations to the actual accepted Desk origin, supplies the actual held session bearer, selected Desk and `X-Assistant-Connection`/`X-Assistant-Revision`, and binds private run ownership in `context.Context`. These values come from server state, not adapter headers. The existing implementation has no literal chat-authority header to preserve: chat/current-request identity is new host context and authorized journal state, while current project memory is read by `prepareModelContext`. Tests must prove dropping either binding fails, rather than inventing an already existing `X-Chat` wire contract.

Call actual `handleModelRelay` so current key custody/profile narrowing, Azure api-key, loopback/LAN rules, queue16/four slots, timeouts/body limits, no redirects, reflected credentials and header stripping remain. Read memory/project-brief settings at dispatch as today; let the context reserve reflect the current setting. Record queue wait/provenance at the trusted host handler seam: `relay.Client`'s diagnostic facade does not carry all Desk queue headers, so do not widen model-visible headers merely to supply UI metrics. Model selection on Codex remains any model in the enabled connection list for main/review/child runs, per accepted #533 policy.

**Runtime:** Factor a Go stdio request/response transport from existing `runtimeCommand`/`runtimeCommandAt`, `runtimeEnv`, `aimAtTheProject`, response pairing, `beginProjectRequest` and `admitRuntimeFrame` behavior. Preserve `manualRuntimeFrameAllowed`/Policy manual Jobs fence, descriptor-based project identity, signing environment and process reaping. `gate.New/Open` performs handshake/tools-list; host fixed-name prompts/get uses gated transport. `describe_runtime` is a **separate host metadata read** on the same held Runtime, never added to the model ceiling (the #536 P2 proved this distinction). Host builds the offer from served definitions intersected with saved grants, enters Plan before the adapter, and passes only `Calls`. Adapter cannot call SetOffer/Enter/Transport or see executable paths.

**Host tools:** Introduce a server-owned registry whose declarations/classifications are fixed by feature code and intersected with run purpose/phase/Policy. Reuse existing Go resource handlers and their authorization. Port current browser-only *orchestration* for proposal validation/retention, saved pack checks, dialect/test/graph review cards, link acquisition and Plan to Go with published `CheckArtifact`/`CorrectArtifact` where applicable. Keep the UI rendering and human actions. This is Desk domain workflow migration, not permission to copy package engines/context/delegation. The companion matrix enumerates the concrete factories and callers.

Only genuinely client-owned state uses callbacks: current unsaved editor snapshot/dirty veto and an explicitly reviewed apply-to-buffer action. Callback declaration is server-owned; a response is data and can veto, never mint a grant, enable a Runtime name or authorize a save. The server persists pending intent first. The browser keeps a receipt keyed by call/action ID and buffer revision for its own one-undo-entry application. A lost acknowledgement is uncertain: inspect/read back the buffer or ask for a new explicit action, never automatically apply again. Rendering, navigation and query invalidations are notifications, not paid host-tool executions. Model questions are `user_question` plus a typed human action, not arbitrary browser tool results.

Existing explicit proposal Apply controls remain local human actions, using the editor's synchronous conditional writer against exact baseline bytes and current path/generation/revision, pending operands, editability and save/reload latches. Only a confirmed application reports success and creates one undo action; observation/reconnect never applies a proposal. These controls need no server-selected apply callback. Request-time immutable pane capture is separate from the fresh dirty veto needed when server-owned save/Auto orchestration consults a live editor; a captured snapshot cannot establish that the editor remains clean or grant write permission.

When a required interactive client is absent, do not manufacture a tool result. Keep pending callback intent and stop the adapter context with a host `client-required` pause reason, preserving the canonical stopped checkpoint. Reattach only reads it; explicit continue after resolving/reconciling the callback starts a new attempt with its receipt. A callback already delivered cannot become automatically replayable just because the lease expired. A required callback uses the same proposed 30-second owner lease; expiry cancels the invocation and keeps awaiting-client state until an explicit reconciliation/continue action.

## Codex: bridge scope and released provider-managed delegation

The earlier v0.3.0 delegation gap tracked in [Agent #67](https://github.com/Judgment-Pack/judgment-pack-agent/issues/67) is resolved upstream and released. v0.5.0 includes `adapter.Run.Agent`, `adapter.Agent` and `adapter/agent`, with `delegate.With` supporting sequential provider-managed parent/child/parent continuation and durable checkpoint acknowledgement. The old synchronous model-only coordinator proof no longer describes this dependency. Desk must bind the released capability to its actual exclusive account/bridge; the dependency release does not certify Desk's binding.

A Desk-owned adapter translates saved connection/effort and a gated tool offer into `codexbridge.RunRequest`, uses real session/Policy ownership, and keeps native shell/web/fs tools disabled. It uses the published context, gate, turn and failure primitives; no copied context/coordinator engine. Provider-managed checkpoints record observable agent invocations and omit unavailable model-request figures. Actual bridge/account release between parent, sequential children and parent continuation remains a required Desk fixture and certification gate.

Approval uses the released `allow | refuse | ask` policy. `ask` emits `approval_requested` and retains one canonical `approval` in the stopped checkpoint. `Input.Approval` carries `CheckpointApprovalDecision{callId,approved,note}` beside that checkpoint. The durable callback must acknowledge an approved checkpoint before executing its exact pending call, and its result before the next provider request. An approved call recovered without a result has an unknown outcome and never runs automatically. Plan, compute allowance, questions and tool permission remain separate.

## One-step exceptions, browser reduction and proof

Approved bounded feature workflows may retain `runBackgroundModel`: input/portable research each ask for one host-authored closed action; the existing workflow owns phases, allowances, acquisition receipts, retries and durable operations. Integrate its dispatch into the same bound-model authorization/profile/context/queue/credential path and a one-use workflow reservation; map one acquisition to one accounting receipt without double charging an independent assistant budget. Preserve forced response contract/validation on subscription and API output, exact malformed-result history and typed fields. There is no generic browser model-call endpoint or another general agent/provider loop. Source/model acquisition broker remains nonce-bound to its admitted frozen operation. Existing research parent/child orchestration remains the explicit domain workflow and must not be confused with the generic subscription delegation lifecycle.

Model list reads are non-generative diagnostics. Endpoint capability probing is **not wholly non-generative**: `capabilityProbe.ts` sends one chat request; `probeEndpoint` sends a bounded Anthropic ping; AssistantSettings' Codex test currently starts an actual get_schema run. Move these to fixed host-owned diagnostic operations, with shared authorization/context/one-request-or-one-invocation accounting and no executable tools beyond the declared test. A UI diagnostic never gains a general raw relay capability. Brief summaries run tool-free on the same server run path using their existing frozen snapshot/reservation; context maintenance/compaction is deterministic canonical code, not an unlisted summarizer model.

Remove live imports/calls of `runAssistantSession`, `loadEngine`, `bindExecution`, `bindModelCall`, `bindAgentRun`, TS engine/delegation/context-session implementations from browser paths. Retain only pure contract/types/closed readers/failure renderers where needed; remove broad `harnessHost` entrypoint wiring. Existing serialized connection engine names may remain storage discriminants (`codex` vs API); they must not load old engines or create a selectable API engine. No destructive config rewrite is needed to erase old code.

Add an emitted-module dependency graph/bundle check, not just source strings: no runtime-reachable ai/@ai-sdk/langchain/deepagents/provider engine, model transport or model-controlled loop. Type-only imports/pure schema tables are permitted. Inventory completeness guard fails if a new run/model callsite appears outside the matrix. Remaining TS workflow code can reduce event state, edit user data and dispatch explicit actions; it cannot decide to launch another paid turn, retry a correction, or grant itself tools/compute. Port `AuthoringRun`'s model/research/correction orchestration to the server, not merely its single `turn` port.

Certification has two separate claims. The published `harness-check` fixture host builds its own capabilities. Run it as upstream parity evidence only. Provider-managed binding certification uses `certification.CertifyAgent` with the actual Desk bridge/account/tool boundary. A Desk-owned driver must reuse exported certification fixtures/readers/scenario checks but construct the **actual Desk host**. The shared scripted server's POST `/sessions` returns `id,relay,mcp` (`certification/server.go:290-305`); a small fixture harness may read those paths to configure temporary Desk keys/connections and Runtime transport. No forwarding through a stand-in model capability that reinjects credentials: omission tests must inspect what actual handleModelRelay delivered. Codex uses existing native bridge protocol helper fixtures plus the same gate/context vectors, real Owner/epoch/model admission and tool/result/event path, separately from Eino. No live provider quality claim.

## Bounded implementation slices and exit gates

1. **Durable foundation and protocol:** ledger/storage/recovery foundations plus authenticated start/read/events/actions, a concrete saved-chat resolver, exact action receipts and bounded execution/subscriber ownership. The production executor stays nil until the domain and final journal migration is complete; new production starts fail closed. Protocol fixtures use an inert host executor against the real authenticated routes and persisted service, without model calls.
2. **Host capabilities + API/Eino:** bind actual relay handler and rooted Runtime gate/metadata path; Input.Checkpoint/Authorize; API chat/pane thin client. Prove credential/Desk/revision/chat/memory/host-origin omission, phase/offer widening, rehearsal/test cap, queue literal16 and failure-before-dispatch. Gate snapshots and current Policy/official Runtime37 stay intact.
3. **Feature migration:** move chat/research/correction/check workflows and every host registry row in the matrix; preserve proposals/expectations/Plan/questions/Auto/reload. Move brief reservations and bounded diagnostics/background transport sharing. Tests per affected feature; no second request-accounting authority.
4. **Basic Codex adapter:** published context/turn/gate plus actual bridge; separate fixtures for subscription auth/model/phase, tool calls, question pause, proposal/reformat/completion, cancellation and lost-response receipts. No generic delegation claim yet. May be implemented on an unreleased lane independently, but does not satisfy final acceptance.
5. **Subscription delegation binding gate:** use the published canonical seam and bind it in Desk with sequential account admission, preserved provider-managed figures, scopes/test caps, parent handoff/child/parent continuation and explicit retry after restart. No user-visible regression or temporary fallback.
6. **Remove browser execution + final acceptance:** cut all engine transports/loops, update AGENTS/architecture and certification job. Migrate #529/#533 guards to the surviving admission seam (do not leave tests of deleted wiring). Verify complete matrix with browser bundle graph, focused Go race/mutations, UI persistence/recovery and required full CI/platform builds. Preserve named map-order/surrogate/image/child-phase differences without rewriting original bytes. One complete independent release-diff review. Only then release/install once.

## Durable foundation and remaining activation gates

[assistant_run_store.go](../../internal/desk/assistant_run_store.go) uses the existing private-data root, ownership checks, bounded cross-process storage lock and atomic verified writer. One `assistant-runs-<project-identity-digest>.json` holds both admissions and their keys, avoiding a multi-file index commit. It retains at most 128 runs, 512 operations and 1024 semantic events per run within a 16 MiB file. Full retention refuses new work; no eviction or garbage collection is introduced. Reads return independent snapshots. Original intent/result bytes are base64 fields so missing/null/false, decimals and formatting survive serialization. A nil result means absent; an empty result is retained distinctly.

`assistantRunLease.Admit` accepts only a host-resolved immutable binding plus stable admission ID/body digest and a read-only fresh owner-CAS callback. It resolves an existing key first; successful retry does not execute stale owner CAS. The callback runs under the storage lock and must not call another locking store operation or perform side effects. Principal/session/Policy/project, owner/revision, request/source digests, connection/profile/model and framework request key/phase/unit remain bound. This data model is not authentication: only later authenticated host admission may construct it. Original domain journals remain their existing authority; this ledger stores their immutable references, not copied history as permission.

A project executor holds a nonblocking stable namespace lease plus the existing shared `.data-work.lock`. Acquisition automatically recovers before returning a dispatch-capable lease; admission is available only through that lease. Acquire/recover precedes Admit, so acquiring ownership cannot interrupt a just-created admission. Backup, storage transfer and project move use the exclusive work lock and refuse while an executor is active. Startup briefly acquires/recovers/releases ownership, and a second server cannot interrupt a live owner. Blank and chat-only legacy startup does not create a ledger or a storage pointer. Recovery failure blocks acquiring dispatch ownership and fresh admissions until successful verified reacquisition; current production AI paths remain unchanged. It marks active host runs interrupted, dispatched receipts uncertain and canonical running checkpoints/operations stopped/interrupted. It preserves completed results and terminal runs, never calls a provider and never resumes automatically. Awaiting-input/client/budget host envelopes remain readable, but any outstanding dispatch becomes uncertain. Restart does not make an absent callback result successful.

[assistant_run_store_writer.go](../../internal/desk/assistant_run_store_writer.go) serializes each run's writes and checks its revision. A canonical checkpoint must exist before intent; `ReadExecution` validates checkpoint shape and bounds, and immutable request key/phase/unit must match. Intent is saved before a dispatch receipt. `Dispatch` returns success once only, after verified persistence; duplicate or uncertain dispatch refuses. Checkpoint/intent/event failures latch the writer and all later dispatch refuses. A future void `Emit` adapter must cancel immediately on that failure and check the latch at every capability boundary. Semantic events must have exactly one round-trippable union discriminator; generated contract structs are not full schema validators, so features still validate domain payloads. Whole `message_progress` prefixes and checkpoint events cannot enter this journal through `Event`.

Observed results can be retained after caller cancellation or lease loss, under a separate bounded storage context, without clearing a prior failure or granting dispatch. Identical completion delivery deduplicates; contradictory bytes/state refuse. A known late result may resolve an uncertain receipt while leaving the host run interrupted. No live provider, listener, arbitrary callback or executable adapter is supplied by these methods.

Before any production caller is enabled, subsequent slices **must** integrate:

- **Security (protocol delivered; production binding pending):** authenticated routes derive opaque principal/session/Policy bindings from the selected server and session store. The closed chat resolver checks saved owner/request/phase/connection/profile/model; execution dispatch reauthorizes these exact sources. Production adapters must enter these sinks and retain their existing host capability/Runtime/Plan checks.
- **Storage integration (delivered in slice 2):** the closed filename allowlist, bounded strict decoding and canonical checkpoint validation bind the filename to the immutable project digest and every run. Backup/restore require both private-workspace and existing Policy-history authorization because opaque hashes cannot classify retained Policy excerpts. Storage copy/rollback/readback preserves exact receipts. Project relocation and explicit history relink resolve the ledger through the existing conversation binding, retaining its original filename/digest and executor lock identity. Sibling-only history creates an empty old conversation anchor on commit; preview never creates one. Destination sibling history refuses a merge. Snapshots and transactions re-resolve the namespace under the data lock, rejecting stale store handles.
- **Shutdown/revocation (host lifecycle delivered; process binding pending):** saved execution contexts outlive HTTP readers and bind to live session and authentication-Policy cancellation. Explicit stop persists its receipt, stops dispatch and retains uncertain operations. Shutdown closes service admission and cancels/drains executions; the executor lease releases when active work ends. Production subprocess adapters must honor cancellation and return within shutdown bounds; a lease alone cannot terminate a child process.
- **Protocol and ownership (partially delivered):** start/read/fetch-SSE/cancel/exact approval delivery and explicit inactive-pause abandonment are implemented. Budget/question/client-callback actions remain refused until their domain workflows are bound. Domain response-journal CAS/readback, live-only progress and thin clients remain part of feature activation.
- **Release acceptance:** preserve the full matrix, enforce browser engine removal, pass surviving Desk-bound certification/omission/mutation checks and required CI, and certify the actual released Agent delegation binding. This foundation is not an intermediate release or fallback engine.

### Restore is historical recovery

Restore validates archive paths, manifest/checksums, bounded custody and ledger/checkpoint contents before promoting the destination pointer. It transforms only the destination ledger: every imported run has `historical: true`; all nonterminal states, including awaiting-input/client and paused-budget, become host `interrupted`; running/paused canonical checkpoints become `stopped`; unfinished dispatched receipts become `uncertain`; running canonical operations become `interrupted`. Completed/refused receipts, exact intent/result bytes and immutable session/request/Policy bindings remain evidence. Existing terminal states remain terminal. Historical nonterminal records are invalid, so restored old approvals cannot become dispatch authority even under the same still-authenticated session. Idempotent retry returns that historical record rather than starting work. No backup bytes, source ledger, original conversations or expectations are rewritten.

Crash recovery is narrower: it preserves paused host states, marks admitted/running host state interrupted and all unfinished dispatch/operations uncertain/interrupted, and never starts a provider/child/callback. Repeat recovery performs no write. A new explicit authorized attempt belongs to the later protocol/runner slice; these records do not authorize it. Tests use temporary rooted stores and local HTTP fixtures, with no paid provider. Three focused mutation rows cover restore authority, filename/content namespace and the storage work barrier; required full validation remains in CI.

The ledger JSON boundary rejects duplicate members and aliases of declared host/canonical struct fields (for example `state`/`State`), while preserving distinct names in arbitrary document/fact maps (`Fact`/`fact`). Original archive checkpoint bytes pass through `ReadExecution` before generated Go decoding can fill absent required fields. Serialized mutations pass the same bounded member guard before any file write. These checks do not replace domain event validators or confer authority. A blocked startup recovery is also exposed through the existing Storage problem field using a bounded host-authored message; private underlying record details are not displayed.


## Delivered protocol slice and activation contract

`POST /api/assistant-runs` accepts only `admissionId`, `purpose`, `ownerId`, `revision` and `requestId`. The closed registry currently resolves `chat` from the selected Desk's actual conversation journal, including the latest saved user-message ID. It derives connection/model/profile/source bindings on the server. Other purposes need their own resolver; an absent executor refuses before admission. Existing admission keys resolve before fresh owner CAS, so a lost response does not launch another execution after the journal changes. A conflicting body or old session cannot reuse admission authority.

`GET /api/assistant-runs/{run}` and `/events` require the actual session bearer header, current private/Policy-history authorization and matching principal. A current session for the same principal/project may inspect terminal/historical results and paused records. Access to a running execution and normal cancel/approval actions, including their duplicate deliveries, require the original session/Policy binding. The distinct `abandon` action permits the current authorized owner to close an inactive pause without inheriting its grants or continuing it. No bearer or cursor is accepted in the URL. Fetch SSE uses `Last-Event-ID`, durable revision/event-count cursors, an initial reconciliation snapshot on every subscription, a bounded subscriber registry and one coalesced wakeup per subscriber. Semantic data is read back from storage; no storage lock spans a stream write. Semantic suffix replay follows event count independently of revision. Data and heartbeat writes both renew a bounded write/flush deadline.

Cancel, approval and abandonment actions have durable, exact idempotency receipts. Abandonment requires no active invocation plus the exclusive executor lease, rechecks current authorization at persistence, and retains the authorizing session/Policy digests beside the original immutable run binding and completed/uncertain receipts. It releases the paused owner reservation only by stopping the old run; a fresh start remains a separate admission. Approval delivery additionally binds the current revision, pending call ID and digest of its arguments. The canonical decision is retained before a continuation is launched; the runner still owes the canonical approved-checkpoint and executed-result acknowledgements before capability/model dispatch. Duplicates return their receipt and cannot relaunch work; recovery of an interrupted delivery never retries it. The server does not infer compute grants or Plan transitions from approval.

The saved-chat source pin covers the exact selected chat bytes. The later thin client must keep active response progress/checkpoints in the run ledger and reconcile the final domain response with CAS. Old browser checkpoint autosaves must cease before that purpose is activated: changing the pinned chat during a run deliberately invalidates future dispatch. Explicit edits, phase/model/connection changes and stale request IDs continue to refuse. The protocol does not itself migrate the original journals or activate providers.

The private execution factory now binds published Eino and the delegation coordinator to the actual Desk relay and a rooted Runtime stdio child. Session, selected connection/revision and exact saved sources are retained by the host; admission is checked again after the real relay queue, with durable intent/dispatch before external I/O and exact transport receipts afterward. Interrupted provider bodies close admission and retain uncertainty even when the in-process reverse proxy suppresses a copy error. OpenAI-compatible, Anthropic and Gemini paths use the existing family-specific relay rules; Gemini stream query admission uses the path's method separately from its permitted query. Completed HTTP receipts establish transport completion only. Host operation and semantic event identities are allocated with their append in the existing persisted-run transaction, so rebuilt capabilities and concurrent child requests cannot reset a local counter or overwrite earlier receipts. Explicit-ID receipt deduplication and replay refusal retain their existing semantics. Closing a canceled or incomplete admitted response closes root dispatch unless its exact EOF receipt was successfully retained; closing a durably completed response remains healthy. Canonical budget pauses use the same root-identity-validated checkpoint acknowledgement as execution checkpoints, preserving the shared test count and the owning service's paused-budget state without granting allowance.

The Runtime binding uses existing command/environment custody and project/Policy frame guards, serializes host-generated request IDs across parent and child gates, bounds frames and cancels/reaps its child. Served prompt arguments and text come from prompts/list and prompts/get; canonical Host parts, Input, checkpoint acknowledgement and prompt digests stay in the published adapter/context boundary. The trusted assembled-prompt marker is private Go context, with legacy consumers retaining their context injection. Plan/child offers intersect existing grants. Desk's current configuration ceiling still excludes experimental_test_cases; fixtures verify refusal and the published shared count for refused child attempts. The merged private critic binding obtains Runtime guidance and cases and covers actual API/native critic execution. Complete production test-tool registry and domain readiness remain pending; the following private subscription fixture covers actual delegation transport completion.

The factory is unregistered and production starts still fail closed. Remaining gates include complete provider-managed binding certification, all feature owners and domain actions, final journal CAS, browser migration/removal, actual Desk binding certification, required CI and independent security/release review. Focused binding fixtures do not certify the complete cutover.


### Private subscription capability binding

The same unregistered factory now selects published `adapter/agent.New()` through
`delegate.With` for a subscription connection. It retains the exact server-selected
private `codexbridge.Manager` or `LazyManager`, session and Policy epoch; each model
(including critic and child preference) must remain enabled in the current profile.
The bridge has Go-only host fences after account admission and after native
account/model discovery, immediately before generation. No browser or model JSON
can provide either fence. The canonical Host assembles instructions, brief and
memory once; the existing socket path keeps its own preparation until cutover.
Reasoning effort comes from the retained validated project preference for the
selected v2 connection or legacy Codex connection. Inherited or absent preferences
use shared configuration; a non-inherited preference with omitted effort leaves
it omitted for native model-default resolution. The same effective effort reaches
parent and configured child invocations without changing model authorization.

Every physical invocation attempt gets a fresh run-lifetime operation identity and
an exact request intent. The acknowledged dispatch precedes `turn/start`. Native
callback identities/offers/duplicates remain guarded by the bridge; each accepted
tool callback additionally records its invocation pairing, exact arguments and
bounded typed answer or normalized unknown failure. The invocation receipt retains
bounded events and a normalized outcome after callback drainage and process reap.
A completed receipt describes the observed invocation outcome, including an unknown
provider outcome on failure; it never grants replay. Only the bridge's direct
pre-admission account-busy sentinel translates to published `adapter.ErrBusy`.
The published adapter owns its two bounded admission retries; admitted failures,
authorization failures and busy-shaped callback failures never restart work. Invocation receipts use the existing model operation kind and adapter callbacks use callback; the canonical usage unit remains provider-managed. Only a typed canonical delegation, question or approval event observed inside the matching callback can mark an intentional stop. Its callback result must be retained, its outer invocation and root ownership must remain live, and the actual Manager must finish/drain/reap before continuation. A private adapter wrapper observes these events before coordinator filtering; native provider text cannot construct the marker. Canceled or unknown work without that acknowledgement closes root dispatch.

Hand-authored local protocol processes exercise parent handoff, child, and parent
continuation through the actual Manager and rooted Runtime gate. Every new peer
refuses to start while the preceding peer PID exists, so a mere unlocked account
cannot satisfy the fixture. Published provider-managed accounting retains checkpoint Used at zero and reports
parent/child AgentRuns figures under the coordinator's shared invocation ceiling;
no model-request count or allowance grant is invented. A subscription budget
request remains unavailable under the published policy. Separate
fixtures cover changed authority during discovery, malformed/unoffered/duplicate
callbacks, exact typed receipts, cancellation drainage, question pause, unchanged allowance policy and retained
root/test identity on capability reconstruction. These are bounded binding checks,
not complete certification or production migration. Complete test-tool registry and workflow migration
still need their owning domain readiness; private critic fixtures do not certify a production test workflow. No production executor,
domain action, final chat journal projection or browser path is activated here.

### Private saved-chat input projection

The private factory now compiles only the exact authenticated chat and latest
non-note user request retained by the source binding. Original conversation and
request bytes remain unchanged. Duplicate keys, malformed text, unsupported typed
fields, ambiguous identities and values the canonical JSON representation would
change refuse before provider dispatch. Explicit statement/input fields preserve
empty values; absent fields fall back to text, rather than serializing the turn.

Canonical history documents retain original turns/questions, exact candidate and
source strings, typed cases and saved observations. The disposable transcript
matches the existing browser projection: earlier explicit authored statements
replace obsolete workspace snapshots, ambiguous legacy mixed inputs stay whole,
notes are skipped, question suggestions are labeled as suggestions, and the latest
request appears once. Saved drafts/checks/source replies and historical approvals
remain data, never current artifact authority, fresh verification or grants.

Runtime prompts in this saved-chat factory are method guidance with no arguments;
request-bearing arguments refuse. The host appends transcript and exact submitted
input once. Explicit authoring-pane arguments remain outside this private path.
Plain-text submitted attachments become canonical bounded page documents for
retrieval; composer-only attachments are not selected. Submitted document
references are shape-checked during source resolution and verified outside the
storage lock against the current permitted Gateway, signed receipt, extraction
digest, selected readable pages and explicit partial consent (Desk #566). The
execution holds private verified snapshots and rechecks exact attachment bytes,
trust and current owner immediately before every physical API/Codex dispatch,
including queued work and child/parent continuations. Reconstructed capabilities
share this custody; changed custody cancels the root. Exact repeated references
are deduplicated, while distinct page/consent tuples have distinct canonical
Document IDs and retain original page citations. No selection union, consent
upgrade, saved text or automatic acquisition substitutes for verification.

Saved interaction defaults to Work only when omitted. Explicit values must be
exactly `work` or `plan`; the resolver and compiler share that rule and the
compiler requires agreement with the immutable phase binding. Invalid persisted
controls refuse before a run is created, without rewriting saved bytes.

Validated chat review, API thinking and model-specific Codex reasoning choices
are applied over retained project/connection defaults. Explicit null reasoning
selects the native model default. Project context policy stays canonical; the
private memory store's use setting controls recall and its query is the authored
message. Host instructions/brief/memory remain assembled by the published Host
boundary. No second context budget, compactor or cache is introduced.

This remains an input-only private slice. Domain-specific authoring targets,
policy/test expectation workflows and their fresh artifact reads remain with the
owning later domain migration. No response journal CAS, output write, production
executor registration, browser removal or complete certification is delivered.


### Private Dialect research admission

`request_dialect_research` is the first private host-owned `workflow` tool, with
no external Source or framework approval. The released host-tool schema keeps
host-owned approval rules with the host. The existing reviewed research allowance
remains the only permission; Plan and delegated children cannot request it. Model
arguments contain only saved Dialect identity/revision. An earlier root recipe
read and a durable finite action operation fix chat, current user turn and request
association before the shared owning request service can consume an allowance.
The manual request endpoint uses that same service, retaining its existing
session or local launch-bearer guard, origin checks and sign-in policy. Private
host calls additionally require a real session and their full current authority
callback. The shared guard is rechecked immediately before allowance consumption.

Admission holds conversation/private, research admission, run and preparation
locks in that order. Preparation view acquisition uses the existing nonblocking
TryRead contract: contention refuses before write, so no private/preparation wait
cycle can form with projection or an injected owner. An acquired view survives
the queued journal commit.
Current Work/session/project/Policy/profile/source, critic custody, selected
recipe and linked companies, budget/slot and expiry are rechecked under their
owning boundaries. A same-turn used receipt is observed before fresh admission
checks, including when an earlier manual request or another host call consumed
it; its original provenance and timestamp cannot be rewritten.

An applied research card means the real owner admitted/queued that exact run,
not completed research. Chat Stop never stops the independent worker. Unknown
attempted persistence closes chat dispatch and retains a typed failure; raw run
GET/SSE/list/Stop remain observational. Explicit terminal `ProjectChat`/server
reconciliation can inspect the exact used-grant/turn receipt, retain historical
applied output and repeat only projection/write/readback/ack. It never calls the
request owner, queues again, acquires fresh permission or continues a model.
Unavailable receipts retain uncertainty. The original unknown tool result stays
unchanged even when the later independent receipt is observed. Historical
admission remains projectable after recipe/budget changes; an unadmitted review
card still requires its current recipe. Actual API/native, owning-journal and
existing browser consumer fixtures cover these distinctions in standard CI.
This private delivery does not activate an executor, migrate browser dispatch,
replace background research model adapters, or implement source/Plan/memory/
continuation workflows. Runtime #274 / Desk #582 and complete certification
remain separate gates.

### Private conversational search custody

The private server-owned `search_sources` tool is a Desk-authored read with
`research` delegation. It declares no external Source, SourceTool, EffectClass,
approval or immutable cache policy. Plan and research children retain the current
saved user request's consent. Only that exact request's recorded Research choice,
or its established web-research mode under enabled project settings, permits
acquisition. Ask retains a finite suggestion without acquisition; provided-only
refuses. Historical choices and model text grant no permission. The finite choice
continuation remains outside this slice and must preserve the original saved
TurnID as the source-reservation identity before activation.

The existing project run ledger atomically reserves three searches per saved
user TurnID across root, children, reconstruction and retries, and sixty-four
searches per chat, including retained and uncertain reservations. The identity
excludes an execution nonce and changing choice bytes. The ledger stays at its
existing 16 MiB limit and retains only finite producer/request/configuration,
Gateway session, storage IDs, effect state and exact storage receipts. Unknown
external or storage outcomes close dispatch; reads and reload do not repeat them.

`researchSourceCallWithAdmission` remains the sole acquisition/seal owner. Full
current Gateway, selected connection/revision and project preference are compared
after capacity admission and before further IO, alongside session, Policy,
profile, exact saved source and critic checks. Storage callbacks perform trusted
local inspection only. The canonical research resolver now has a no-start
snapshot mode for those callbacks: unavailable or changed local process facts
refuse without spawning or waiting for a companion. Existing manual and
background paths retain start-enabled resolution.

Completed response bytes are retained as a distinct host-generated attachment
original named `chat-search-acquisition.json`; a completed seal has its own
`chat-search-seal.json` packet. Neither is a user reference or source authority.
Both are immutable original bytes under the existing attachment CAS, namespace,
quota and backup owner. Their precise retention intent may finish despite chat
Stop within a bounded local write; it permits no next acquisition, seal or final
object write. The final `web-search.json` attachment retains its canonical result
original first and the complete exact proof second. No provisional Proof is
written and attachment proof immutability is unchanged. The result limit is one
MiB; the declared acquisition packet bound is six times that result bound plus
the existing 64 KiB receipt envelope bound, and the seal bound is 64 KiB. Excess
refuses without truncation; configured upload and aggregate storage limits still
apply.

The installed integrity-checked Gateway canonicalizer and offline verifier bind
session, source, call index, finalCount, salted request commitment, result digest
and signer. The final original must exactly equal the canonical verified result.
Search shape validation preserves signed extension members, provider/kind/time,
duplicate-hit checks and UTF-16 text limits, including grounded extras.
URL spelling is now shared with the private link reader through the pure-Go
`github.com/nlnwa/whatwg-url` v0.6.2 parser (Apache-2.0; Go 1.16 module;
no CGO/native runtime requirement). The shared spelling guard mirrors the
browser's HTTPS, credentials, fragment, port and byte-bound checks. Unicode and
punycode hostnames, browser numeric-host spellings, empty labels and raw-percent
paths are parsed by that standards-backed owner instead of the earlier ASCII
intersection. Historical URL vectors retain their original host/browser columns;
the current Go reader checks the existing browser expectations, and additive
normalization/extraction vectors cover explicit fetch preparation. This proves
these shared consumers and vectors, not comprehensive WHATWG conformance.
Gateway remains the owner of actual DNS, public-network and redirect admission.
Signed result strings and extension members are never normalized or rewritten;
an invalid hit refuses the entire result.
The private host requires RFC3339 timestamps; the pinned search producer
writes UTC RFC3339. Legacy browser Date.parse strings such as `2026-10-21` remain
readable in old browser history but are refused by this private producer. No
signed bytes are normalized to obtain acceptance. Explicit default HTTPS port
443 remains allowed as in the browser.

A finite search output binds the actual host/run/call, exact request/reference,
verified digest and exact stored final object. Current-run custody does not
weaken saved-document reference checks or admit arbitrary attachment IDs.
Explicit terminal projection verifies physical stage/final custody offline,
rechecks current configuration, then appends and deduplicates the existing
SearchReference/SearchStep browser shape. GET/SSE/reload leave ledger bytes and
revision unchanged. Only explicit terminal projection/recovery reconciles exact
already-declared storage receipts, using current-only configuration without
starting local processing. An explicitly recovered completed final write can retain
verified historical output while preserving the original unknown tool result;
it never reacquires, reseals, rewrites an attachment or restores current source
permission from history.

The search slice delivers conversational `search_sources`. The private link
reader below reuses its current verified receipts. Website exploration, research
workflow read/citation tools, choice continuation, Plan/memory migration and
production/browser activation remain separate work.


### Private conversational link reader (#593)

`read_link` is a fixed read/read offer in ordinary conversation and read children,
including Plan. The existing Policy-specific early return remains unchanged. A
supplied URL is available with broader research disabled: nomination is limited
to link extraction from saved user text and exact `document` attachment `link.url`
fields. Assistant text, arbitrary nested fields, an attachment ID and unverified
redirect strings grant nothing. Other links require the saved Research choice
and a currently verified search receipt; normalization creates the explicit fetch
request while its authority remains bound to that verified original URL. Retained
redirect authorization verifies requested and resolved URLs together. Discovery
is available through the bounded private website slice below; arbitrary same-origin guesses still grant nothing.

The link preparer accepts public HTTPS spelling and unambiguous bare ASCII or
punycode website names, keeps the trimmed display spelling and records fragments
as section/route anchors beside the fragment-free fetch request. It uses the same
standards-backed spelling owner as search. No JavaScript runtime, CGO, component
pin or production registration is added.

A separate typed link-custody record retains exact operation/turn/request identity,
Gateway/configuration commitments, random one-result session and immutable
stage/seal/original/final attachment receipts. The existing outbound source,
attachment CAS/quota/namespace/backup and canonical offline-verifier owners perform
effects. The web response/proof bound is 16 MiB, and the retained original is
bounded at the smaller of the configured file limit and 4 MiB; complete packets
remain outside the run journal and under the attachment object quota. Every effect
has durable intent. Completed stage/seal custody may finish after cancellation,
but permits no next effect. Pending durable external/storage intents close fresh
acquisition even if a process died before recording an explicit unknown flag.
Generic GET/list/SSE/Stop remain observational. Only explicit terminal projection
may attach exact existing receipts and recover an already completed final packet,
keeping the original refused result; no acquisition, seal or attachment write is
replayed.

Ordinary new reads reserve one durable counter per saved user turn across roots,
children, retries and fresh admissions; the limit is three. Retained verified
windows do not reserve again. Verified-discovery reads use the same counter with a threshold of ten total;
ordinary reads still stop at three. No second additive pool is introduced. The existing 256 retained-document limit applies.
Verified search/redirect authority also retains the exact attachment receipt in
link custody and compares its bytes after queue and storage waits; removing or
substituting that object cannot preserve permission. Current configuration,
source/session, Policy, profile and critic fences are
rechecked after waits; no private storage lock spans verification or external IO.

New acquisitions use a separate current-run custody reader. Saved-reference
checks are not relaxed to arbitrary IDs, and the pinned saved chat is not rewritten
after each call. The full proof, original digest and document shape are verified
before exposure. New partial/unreadable documents are retained for review and yield
no page text. Saved explicit partial consent and selected readable pages remain
required. Finite producer-bound output projects through the shared terminal chat
owner into existing attachment and link-work shapes, including read-child output.

The window uses the canonical context result/read-window byte policy, selected
pages joined by two newlines, UTF-16 find/range offsets and exact page citations.
Find is literal, case-insensitive ECMAScript non-Unicode matching with ECMAScript
whitespace, at most 200 units and three matches. A narrow browser correction is
shared by both engines: after explicit/default-find clamping, a start between a
valid high/low surrogate pair backs up one UTF-16 unit and reports that actual
offset. The old browser could emit half an emoji, which Agent's UTF-8 result value
would replace with U+FFFD. The new contract returns the intact character; the
shared encoded-prefix policy already keeps the end safe and continuation advances
from the actual end. If bounded page-span metadata alone would exceed the shared
result limit, both readers return a bounded review refusal; newly acquired data
remains attached for selecting fewer pages. Handwritten vectors cover explicit
and find-based starts,
BMP/combining text, page spans, non-ASCII whitespace/folding and byte-cutoff
continuation. Existing composer/UI and manual/background authorization are kept.


### Private conversational website discovery (#595)

`explore_website` is a fixed read/research offer in ordinary conversation and
research descendants, including Plan; Policy's early return is preserved. The
saved Research choice and current document/discovery capability must permit it.
The seed must be supplied or authorized by a current verified search or the
existing exact verified retained-redirect path. A redirect string alone grants
nothing: requested/resolved URLs must share the verified retained proof.
Assistant prose, attachment IDs and navigation maps cannot nominate another seed.

The reader validates the browser discovery contract: ten discovered pages, depth
two, one hundred listed links, eight MiB and forty-five seconds. It checks all
counters, statuses and stop reasons, the exact seed/origin, unique ordered URLs
and each previously listed parent at the next depth. Depth-three entries must be
skipped. A discovery is navigation data, never page evidence. The canonical
original is at most one MiB; exact response, seal and original/final packets remain
in attachments. Existing canonical Gateway verification owns signatures and
argument commitments; no new verifier, JavaScript runtime or dependency is added.

Website custody is a separate typed ledger collection using the existing finite
link effect/receipt dialect and shared acquisition/seal/storage pipeline. One
exploration is reserved per stable saved user turn across roots, children and
fresh admissions. Sixteen distinct retained seeds include immutable run history
and current entries; replacing a seed does not consume another distinct slot.
Terminal chat projection retains the latest reference per seed, while historical
response references and exact attachment packets remain unchanged.

Only the latest saved/current-run map for a seed authorizes new page acquisition.
Each seed is reauthorized against current supplied/search/verified-redirect facts;
historical response maps do not supply seeds or pages. An exact `discovered`
entry and the current same-origin/discovery settings authorize a site-bound web
request. Blocked/failed/skipped entries and same-origin guesses grant nothing.
The Gateway request and signed page proof bind both URL and site seed. Shared
standards-backed origin comparison handles Unicode/punycode/default-port spellings
without rewriting signed bytes. Saved document acceptance is not broadened.

New-page reads share one attempts counter: ordinary three, verified-discovery ten
total, including ordinary reads already spent. Retained verified pages cost no new
reservation; the existing 256-document, selected-page/partial-consent and citation
window rules remain. Current-run references are separately typed; the pinned saved
chat is not rewritten after every tool call.

Current configuration, source, saved choice, session, Policy, profile and critic
fences are rechecked after waits. The managed local source capability comes from
the verified companion catalog; its pinned bundle is revalidated at commit without
executing a companion under a private lock. External sources require the declared
discovery capability. Exact map and any seed-search/redirect authority packet bytes
are bound in custody and rechecked after queues and at storage commit. Retained
page/recovery readers verify map ownership and exact discovered membership again.

Cancelled or unknown effects never replay. Only completed stage/seal custody may
finish its exact predeclared packet despite cancellation. Explicit terminal
projection can reconcile exact known owned receipts and recover a completed final
map while retaining the original refused result. GET/list/SSE/Stop stay
observational; no storage lock spans outbound work or proof verification.

Actual API/native roots and a research child project website/document references
through existing ChatStore, verifyWebsite, verifyDocument, selected-page and link
window consumers. This completes private conversational discovery and discovered
page reads only. Finite source-choice continuation, workflow read/citation tools,
Plan/memory migration and production activation remain separate; the executor
stays unregistered and component pins/canonical Agent schemas are unchanged.

### Private reviewable Plan host (#597)

The unregistered private conversation registry now supplies `propose_plan` only
in Plan phase, with the exact `plan` effect and no delegation, source, approval or
context-policy override. Policy-linked conversations receive this single Plan
capability alongside their existing Policy tools; their registry still excludes
ordinary artifact, search and link tools. The published Agent owns dispatch,
context, checkpoint accounting and event production. This binding does not copy
an engine or the package's proposal-edit or memory protocols.

Desk accepts only the canonical objective, ordered steps, questions and additional
reference arguments. It refuses unknown, duplicate and case-aliased members,
invalid Unicode, whitespace-only text and malformed saved phases/plans. Objective
and step/question bounds use UTF-16 units (2000 and 1000); steps number 1–20,
questions at most 10, additional references at most six, distinct resolved
references at most eight, and sequential revisions at most 24. File paths and
Dialect IDs remain exact. The linked pack/graph path and non-list Dialect ID come
from the current saved chat, and revisions come from the existing rooted file
reader and consistent Dialect collection view. Model and saved-prose revision
claims provide no authority and no referenced file content is returned. Validated
saved Plan revisions enter canonical context as history, retaining open questions
without restoring approval or allowance.

The existing host journal persists deterministic call identity and Plan metadata
before work, then exact result and review-only domain output before terminal
projection. Reconstructed registries share one execution's Plan serialization.
The current chat/source/session/Policy/profile binding is rechecked after waits;
projection rechecks current reference revisions before preparation and final
write. Current-chat CAS applies prior-plan supersession and append atomically.
Cancellation after observation retains the result. Unknown or interrupted
operations cannot reissue an effect; lost projection acknowledgement repeats
only the saved selected-chat bytes and readback. Repeated unchanged terminal
projection is idempotent.

The existing ChatStore, canonical `validPlans`, `currentChat`, `checkPlan`,
`acceptPlan` and PlanCard consume the projected model. Proposed revisions retain
questions and prior history. Human conversation saves alone can change a current
unchanged proposed Plan to accepted, without unresolved questions; the existing
server conversation owner verifies exact linked identity and current revisions
under its normal CAS/write locks, and checks again before rename. Accepted to
started retains the same Plan and acceptance timestamp. Existing chat-triggered
action admission also refuses changed or missing accepted-Plan references.
Known stale human acceptance returns the shared `stale` conflict (HTTP 409), with
guidance to review the current Plan references again. Missing-reference and
storage failures keep their own classifications; no refusal changes saved state.
Ordinary saves of an unchanged already accepted snapshot remain ordinary saves;
these checks never infer an approval, allowance or automatic Start work action.

Handwritten shared model vectors, actual API/native producers, canonical browser
readers, actual malformed refusal events against the pinned Agent v0.6 schemas,
private member omissions, queued authority loss, stale references, interrupted
custody and acknowledgement recovery cover this bounded host completion. The
private executor remains unregistered; browser dispatch removal, complete
cutover/certification and released Agent v0.8 adoption remain separate work.

### Explicit question and compute continuation (#631)

The private protocol now admits closed `answer` and `extend-budget` actions with
stable IDs and current revisions, exact persisted receipts, exclusive executor
ownership and fresh source/session/Policy checks before continuation. Answers use
canonical history documents and a labeled transcript segment while preserving the
original request key; terminal projection retains the question, exact answer and
saved resolution. Compute extensions use published `ExtendExecution` only for
safe canonical model-request pauses. Provider-managed grants, interrupted replay
and the completed-question checkpoint at the API ceiling remain refused. The
released ceiling seam is recorded in
[the continuation design](../design/assistant-run-continuation.md). This paragraph
updates actual implementation status; it does not activate the production factory
or complete the broader swap described by the early target table.

### Bounded canonical live observations (Desk #639)

The private server execution forwards canonical `message_progress` observations
for API and native subscription adapters through the existing authenticated run
SSE endpoint. Each value is a whole prose prefix; an empty value clears that
view. `taskId` identifies a child view and can never replace the parent answer.
The transport retains at most 17 current views and 1 MiB of escaped observation
data per active run, with a 64 KiB UTF-8 limit per prefix. It replaces each view's
value and uses the existing one-slot subscriber wake signal. A slow or disconnected
reader cannot create an executor-facing text queue. The existing eight active
execution and 32 subscriber limits and ten-second write deadlines still apply.

A `progress` frame has no SSE `id`. Its closed JSON envelope contains `id`,
`owner`, `requestId`, a fresh execution `epoch`, a safe positive `sequence`,
`cursor`, `text`, and optional `taskId`. The payload cursor binds the display
observation to the durable snapshot the server just sent; accepting it never
advances the client's durable cursor. Epoch/sequence are ephemeral ordering,
not semantic identities, completed messages or dispatch authority. The browser
requires a reconciled snapshot of its exact run/owner/revision/request, rejects a
changed epoch or cursor, ignores duplicate/older sequences, and validates both
per-view and aggregate size. The server rechecks current session, principal,
Policy, project and exact saved execution source on emission and read. Only the
active execution object can replace its observations.

Every explicit reconnect clears live display state and reconciles durable events
and a snapshot before it can receive current authorized prefixes. A semantic
suffix or terminal/pause snapshot clears stale live display state. Cancelling a
reader or ending its session releases observation state. Callers cancel the
reader when leaving its project/chat. Reader cancellation does not cancel the execution or schedule a model/tool replay. Cancellation
of the run closes its own context and removes its active observations when the
executor exits. A new execution receives a fresh epoch. No provider/model-name
exception, extra retry loop or engine registration is involved.

Unfinished prose is not present in canonical checkpoints, so the host also keeps
optional `displayPreview` custody in its private run envelope. These rows contain
only nonempty `text` and optional `taskId`, with the same view/prefix limits and
an aggregate 1 MiB limit. Checkpoint and semantic event writes capture the latest
rows atomically with that boundary; cancellation/exit captures the latest rows at
the terminal boundary. Prefix receipt itself never writes the ledger. A completed
parent message removes the parent preview, and an empty child prefix removes
that child's preview. Strict Go storage and browser snapshot readers validate the
new optional field, including exact member spelling, UTF-8/Unicode, duplicate
views and bounds.

Restored preview rows remain display-only retained partial prose. They never
populate live observation state, canonical checkpoint messages, final response
journals, model context, request allowances, approvals or completed-operation
receipts. Explicit continuation preserves their display custody until replaced
or cleared, but does not assign them an epoch/sequence or replay their generation.
A process crash can lose prefixes since the last semantic boundary; recovery uses
the retained checkpoint, receipts, events and optional prior display preview.
There is no promise to recover transient text by repeating model or tool work.

Hand-authored cross-reader vectors cover the `progress` envelope and private
preview shape. Focused tests exercise chunked Unicode, missing/foreign/oversized
values, whole-prefix replacement and empty clears, sequence/epoch/cursor guards,
child isolation, bounded coalescing and authenticated session termination. Actual
API and native adapter fixtures hold their completion until the registered SSE
handler has delivered a genuine canonical prefix; the browser fetch reader uses
the same frame grammar. Main chat UI and production hook activation remain in the
parent cutover work.

Progress follows the canonical output policy. Both the published Go boundary and
browser boundary suppress raw `message_progress` when `outputGuard` is enabled;
the default is `redact`. This transport forwards only emitted canonical values
and does not weaken that guard or create progress from provider text. The actual
API/native streaming fixtures admit `outputGuard: off` consistently before the
checkpoint and adapter run; matching default/redact cases prove zero raw prefixes
and durable completed prose/checkpoint delivery. Running status and recovery
continue to come from shared durable snapshots under either policy.
`scripts/dev/assistant-run-progress.sh` sends temporary observations from those
actual registered SSE runs through the browser's real frame and observation
readers. The temporary outputs are evidence inputs, not generated golden vectors.
