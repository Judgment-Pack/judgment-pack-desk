# Operational jobs for Desk

Date: 2026-09-25
Status: proposal for review. No implementation, repository creation, deployment, or external action is authorized by this document.

Stage 1 artifacts: [draft contracts, API boundary and design screens](operational-jobs-contracts.md). The preview is isolated and does not implement operational execution.

## Product direction

Extend Desk from authoring and testing decisions into operating repeatable business decisions. Add **Jobs** below Graphs in the main navigation. A job selects a released pack or Judgment Graph, obtains inputs, evaluates them, preserves the result, and applies explicitly configured follow-up handling.

A pack describes a decision. A Judgment Graph composes decisions. A job describes when, on which inputs, and under whose authority those decisions run. A workflow is the sequence of operational steps within a job. These are separate objects: scheduling, credentials, retries, approvals, and vendor writes do not become pack rules or Judgment Graph nodes.

Start with a bounded job pipeline. Keep the public job/run API usable from an existing business orchestrator; adopting Desk must not require replacing n8n, Activepieces, or another scheduler.

## What exists and what is missing

This review examined the current local checkouts, not live provider acceptance.

| Area | Existing foundation | Required addition |
| --- | --- | --- |
| Desk | Pack/draft authoring, cases, rehearsal, retained run comparisons, verified document snapshots, local owner sign-in | Jobs, operational runs, releases, review work items, job permissions, runner connection |
| Runtime | Pack evaluation, experimental graph CLI, optional audit JSONL, reviewed-set lock | Only narrowly scoped invocation/correlation contracts needed by a reliable caller; no scheduler or provider clients |
| Gateway | Acquisition, receipts, independent verification, configured adapter bindings, authenticated `/act`, n8n/Activepieces client packages | Automation-capability discovery, unattended grants where supported, defined delegated action authority, action reconciliation/idempotency where supported |
| Durable operations | No durable job scheduler/worker found in Desk | Persistent job/run store, dispatch, recovery, schedules, input cursors, review state, attempts, action intents |

Evidence in the repositories:

- Runtime `docs/architecture.md`, ADR-0018 (audit), ADR-0019 (reviewed-set lock), ADR-0028 (rehearsal), ADR-0015 (graph composition), and `internal/mcp/tools.go`.
- Desk `web/src/packs/test-workspace/TestsWorkspace.tsx` explicitly supplies `rehearsal: true`; `docs/design/authentication.md` describes single-owner local OIDC, with hosted multi-user workspaces out of scope.
- Gateway `go/act.go`, `docs/design/executor.md`, `docs/design/plugins.md`, and ADR-0001.

Important boundaries:

- Runtime evaluation is still an experimental surface. A job release must pin its supported runtime/contract version, retain that label, and reject unsupported combinations.
- A test is not an operational decision. Preserve the existing rehearsal behavior and test storage.
- Runtime audit records describe completed decisions, not scheduling attempts or failed acquisitions. The runner needs its own operational ledger and links to decision records.
- Gateway `/act` checks authenticated identity and cited record/receipt existence. It does not establish a person's approval of exact action arguments, interpret the decision record as permission, or guarantee a business operation succeeded.
- Current graph composition is a deterministic DAG of pack evaluations, not an execution engine for timers, connectors, or human waits. Its operational evaluation is available through the CLI; do not invent an MCP graph execution method.
- Browser source selection is not a standing automation grant. A connected provider or a short-lived picker grant must not be treated as permission for unattended account-wide reads.

## User model

| Object | Meaning |
| --- | --- |
| Pack / Graph | Editable decision logic and its tests |
| Release | Immutable execution bundle: exact pack/graph bytes, all referenced packs, configuration/lock, runtime contract pin, validation and test evidence, release actor/time |
| Job | Stable business purpose, owner, released job revision, trigger, inputs, run-as identity, handling and operational limits |
| Job revision | Immutable configuration of a job, including release digest, mapping revision and permitted actions |
| Run | One accepted invocation of one job revision, with trigger identity, correlation/idempotency key, timestamps and status |
| Item | One business subject within a batch; owns its input snapshot and decision, independent of other subjects |
| Attempt | One execution attempt at a step, retaining failure and recovery history |
| Review | Durable human work item tied to exact inputs, decision and proposed action |
| Action intent / receipt | Planned operation and its separately observed request/response outcome |

Use IDs and content digests for identity. Friendly version labels remain useful but do not freeze bytes. Editing version `0.1.0` must not change a released job. A new release is an explicit promotion; existing runs retain their old release and job revision. Rollback selects an earlier release for future runs and never rewrites history.

For the first pilot, single-item runs are enough. Preserve the run/item distinction so batch support can checkpoint each item without duplicating successful decisions.

## End-to-end behavior

1. In a saved pack, choose **Create job**. Validate the exact candidate release and present matching test evidence and remaining coverage gaps. Coverage remains advisory unless an organization explicitly adds its own release policy.
2. Select manual/API inputs or an eligible connected source. Map source fields to facts and evidence requirements with a preview. Preserve the difference between omitted, unknown, false, and absent evidence.
3. Choose when to run, the owner and run-as identity, and what should happen for each outcome, unresolved result, requested handoff, and operational error.
4. Run a sample in preview mode. Show inputs, citations, decision and proposed handling; send no external action. Persist preview runs distinctly from operational runs and Tests.
5. Activate a job revision. The runner accepts triggers durably and processes them independently of an open browser.
6. Review runs in Desk. Resolve missing data through a linked new evaluation; approve a proposed action only when that action is permitted and bound to the reviewed snapshot.
7. Release later policy changes deliberately. Compare before/after on retained examples before switching future runs.

Example pilot: contribution intake review. A submitted contribution and its check/evidence records are mapped into the existing contribution pack. A completed evaluation can return accept, reject, or unresolved. An unresolved result enters Needs review. External repository updates remain disabled until the approved-action milestone. A GitHub-specific source or write is a separate adapter qualification, not assumed from the example.

## Inputs and integrations

Offer two authoring paths over the same persisted mapping contract:

- **Manual:** structured fields, uploaded JSON/CSV, or selection from a configured integration.
- **AI assisted:** propose mappings, extract candidate facts, and explain missing fields using the same sources and preview. The user reviews the proposal before activation.

Every run retains exact evaluated facts, evidence availability, source snapshots/receipts, mapping revision, source acquisition times and business record IDs. Receipt verification proves integrity/provenance under its stated contract, not truth or adequacy of evidence.

Separate reference sources used to author policy from operational data describing today's case. Refreshing a policy source must not automatically modify a released pack. Operational source bindings can request fresh inputs under an explicit freshness policy; each acquisition creates a new immutable snapshot.

If AI extraction later runs unattended, it is a named, bounded step with model/prompt/schema versions, response retention, validation, citations, timeout and cost limits. Missing/ambiguous facts remain unknown or enter review. It may not silently change a pack, invent evidence, or alter test expectations. Persist extraction output before downstream retries; deterministic decision replay uses retained inputs instead of asking the model again.

Gateway should advertise which operations actually support unattended use, input listing/cursors, stable resource identities, change detection, and writes. Start with one qualified source. Add more only after provider-specific behavior and live acceptance are verified. Avoid claiming all current chat integrations can run as scheduled jobs.

## Triggers and execution policy

Support incrementally:

| Trigger | Semantics |
| --- | --- |
| Run now | Authenticated user submits explicit inputs |
| Submit API | Authenticated system supplies one input or a source locator; persist acceptance before returning run ID |
| Schedule | Interval/calendar schedule with explicit timezone, daylight-saving and missed-run policy |
| Event | Verified webhook or provider notification; durable deduplication and replay protection |
| Source polling | Persistent cursor/watermark, bounded pages, item-level deduplication and retention |

Initial defaults: one active executing item per job, bounded queue, read/evaluation timeouts, bounded transient retries with backoff, and pause after repeated operational failures. A waiting human review releases execution capacity while remaining a tracked work item. Queue limits and review backlog limits are separate.

A scheduling pause stops future scheduled admission; existing runs continue unless explicitly cancelled. Source event intake while paused must follow a declared retention policy. Manual run permission and schedule pause are separate. Cancellation is cooperative and does not undo an external write.

On a local machine, the runner can continue after the browser closes, but cannot work while the host is off. Show runner health, last heartbeat, next run and missed runs. Start with no automatic historical backfill; offer an explicit bounded catch-up. Define DST behavior instead of relying on the host's local clock.

Keep retry, repair, replay and new-input runs separate:

- Retry: another attempt of a failed step using the same pinned run data.
- Repair: resume eligible unsuccessful steps, retaining successful checkpoints.
- Replay: reproduce an evaluation with retained inputs, with external actions disabled.
- Run again with fresh inputs: new run, acquisition and identity.

## Results, review and authority

Track three separate dimensions:

- **Execution:** queued, running, waiting, completed, failed, cancelled.
- **Decision:** the Runtime's exact disposition/outcome/reasons and requested handoff.
- **Follow-up:** no action, needs review, approved, declined, sent, confirmed, failed, or delivery unknown.

A successful run that decides Reject is not a failed run. Unknown facts are not a network failure and must not trigger blind retries. A requested handoff is not proof anyone received or accepted it.

Needs review should show the business subject, reason, assigned reviewer, age/due date and next action. Reviewers can request corrected inputs, decline a proposed action, or approve a permitted action. Their decision is a separate record; it never rewrites the Runtime disposition. Human overrides, if introduced, need an explicit organizational policy and remain separately visible.

Bind action approval to the run/item, release digest, input digest, destination, operation, canonical argument digest, approver and expiry. Input/action changes invalidate that approval. Recheck destination permissions and relevant preconditions at dispatch. Keep an authenticated requester separate from the person who approved and from the service identity that executes.

Desk's local owner flow supports a personal pilot. Team review requires workspace membership, role permissions, review assignment, tenant/project isolation, retention and a real identity source. Do not treat the placeholder email or a local session as enterprise identity. OIDC sign-in, provider OAuth and unattended workload credentials remain separate capabilities.

Initially require explicit per-action review. Standing authorization for low-risk automated actions is a later policy feature. Gateway's current person-requested action contract needs an explicit design/ADR for delegated service execution and approval linkage; a machine token alone must not silently redefine approval.

## Durable execution and side effects

Create a dedicated runner service, invoked through a versioned API. Persist job revision, accepted run, step state, item checkpoints, review state and action intents transactionally. Persist large immutable artifacts separately by digest.

Use at-least-once dispatch with idempotency and reconciliation. Never advertise exactly-once vendor writes based on a receipt or queue alone. An outbox records an authorized action intent before dispatch, but cannot by itself close the crash window after the provider commits and before its response is recorded. Prefer provider idempotency keys and queryable operation IDs. An ambiguous write becomes **Delivery unknown** and waits for reconciliation; it is not automatically repeated.

The current Runtime audit call and runner database do not share a transaction or caller-selected invocation ID. Audit the result/record correlation boundary before enabling automatic decision retries. Retain exact subprocess output and reconcile any durable audit record; if correlation is uncertain, mark the attempt interrupted instead of claiming there was only one decision. Introduce a small versioned Runtime correlation contract if needed. Keep audit semantics and scheduling separate.

Gateway sessions also have restart/seal constraints. Use a new session for a new acquisition/action attempt; independently verify and bind the retained artifacts before use. Link sessions to run/item/attempt IDs without pretending an old receipt session is resumable.

Required crash scenarios include failure before/after trigger acceptance, acquisition, evaluation, audit append, result persistence, approval persistence, external dispatch, provider completion and receipt persistence. Test duplicate events, concurrent workers, expired credentials, revoked permissions, source drift, release drift and cancellation races.

Restoring a runner backup starts jobs paused and requires reconciliation of pending/in-flight actions. Backup restoration must not replay notifications or writes automatically. Audit/export and operational retention are explicit, with sensitive source access kept separate from general job-list visibility.

## Ownership and repository recommendation

Recommend a new **`judgment-pack-runner`** repository for the headless operations service when implementation begins. No repository is created by this planning work.

| Repository | Ownership |
| --- | --- |
| `judgment-pack-desk` | Jobs UI, release/review UX, run explorer, Assistant proposals, connection to runner |
| `judgment-pack-runner` (proposed) | Job schema/API, release storage, scheduling, execution/recovery, authorization policy, run ledger, reviews and action intents |
| `judgment-pack-runtime` | Validate/evaluate packs and graphs, reviewed locks, decision output/audit; narrowly scoped correlation additions |
| `judgment-pack-gateway` | Provider adapters, credentials, acquisition/action transport, receipt generation/verification, capability contracts |
| `judgment-pack-spec` | Only if interoperable decision/receipt semantics actually change; no scheduler configuration in JPS |

This is a separate deployment/lifecycle boundary from Desk's web chassis and from the Gateway signer. A runner can serve Desk, CLI/API callers and existing workflow tools. Use Runtime CLI/MCP and Gateway's versioned interfaces rather than importing either repository's internal Go packages. Desk can bundle/manage a local runner companion and connect to an always-on server later.

Keep platform deployment neutral. Local process/service first; an always-on container host can later be on Azure, AWS, Google Cloud, DigitalOcean or a private network. Hosting Desk's UI does not keep a worker alive or provide durable state.

For a bounded single-host pilot, recommend a Go service with a transactional SQLite job store, a single dispatcher, and an explicit fixed step machine. Define the storage and dispatch seams, but do not promise a free migration to distributed execution. Before shared production or complex multi-step workflows, run a focused orchestration spike: Temporal is a candidate for durable waits, retries and recovery; n8n/Activepieces are candidates when a customer already operates them. Prefer one owner of scheduling and retries. No additional AI agent framework is needed to schedule deterministic evaluations.

Existing Gateway n8n/Activepieces clients can remain useful. They carry receipts but do not verify them or implement the new release/run/approval semantics. Their templates must call the runner API or explicitly perform the full verification flow; installing the plugin is not sufficient acceptance.

## Desk interface

Main navigation: **Packs · Graphs · Jobs**. Keep job-level cross-cutting views under Jobs: **Jobs · Runs · Needs review**. Add a separate review Inbox later only if users need cross-product review work.

- **Jobs list:** compact rows showing name, state, target release, trigger, last run, next run and owner; search/filter with one New job action.
- **Job page:** shared header and restrained tabs for Overview, Runs, Configuration. Run now is primary; Pause/Edit live beside it or in the existing overflow pattern.
- **Job setup:** Target → Inputs → Trigger → Handling → Review/activate. Advanced retry/concurrency settings stay collapsed.
- **Run page:** result and next action first, then input/source snapshot and step timeline. Show batch item counts when batching exists. Raw JSON and diagnostic logs are disclosures.
- **Review page:** enough full-height main-pane space for evidence and action comparison. Assistant, Details and Activity reuse the current right tool rail, one tool at a time.
- **Pack page:** compact Create job action and a Used by jobs link/count. Tests stay dedicated to testing.

Reuse current spacing tokens, buttons, tabs, breadcrumbs, tooltips, keyboard navigation and resizable pane behavior. Do not introduce another always-visible left pane or bottom console. Linear's current refresh supports consistent headers, compact view controls and quieter navigation; the above layout is an adaptation for Desk, not a claim that Linear prescribes this exact navigation. [Linear design explanation](https://linear.app/now/behind-the-latest-design-refresh).

## Delivery sequence and acceptance

| Stage | Delivery | Acceptance |
| --- | --- | --- |
| 0. Contracts and static mock | Release/job/run/item shapes, ownership ADR, one pilot definition; Jobs list, job setup and run/review mock | One scenario traced end to end; uncertainties about audit correlation and unattended source authority explicitly resolved |
| 1. Recorded manual jobs | Runner, durable store, immutable release, Run now/API, structured inputs, decision ledger, Desk Runs | Close browser without losing work; restart recovers or explicitly marks interrupted attempts; same request key does not create duplicate runs; editing pack does not affect release; no external actions |
| 2. Recurring pilot | Schedule, pause/resume, qualified source adapter, input preview/mapping, freshness, retry/queue limits, runner health | Repeated operation on real pilot inputs; timezone/missed-run and credential expiry tests; duplicate delivery and source pagination do not lose or repeat accepted items |
| 3. Human review and approved actions | Review work items, exact approval binding, one qualified action adapter, outbox/reconciliation | Requested handoff becomes a tracked assignment; changed arguments cannot use old approval; ambiguous delivery is visible and never blindly retried; live provider test has a named owner |
| 4. Business workflows | Released Judgment Graph targets, bounded branching, batching, event triggers, controlled repair, external orchestrator templates | Per-item recovery, pinned graph closure, independent review waits, no duplicated successful side effects; compatibility tests for the chosen orchestrator |
| 5. Shared operations | Team access, service identities, production deployment, retention, alerts, release promotion, rollback | Isolation and permissions verified; backup/restore and recovery exercised; operational ownership and measurable run latency/failure/queue targets agreed |

If multiple reviewers or hosted team operation is required for the first pilot, move the identity/access work in stage 5 ahead of stage 3. It is a prerequisite, not optional polish. Graphs follow the single-pack vertical slice; the release model supports both from the start.

First usable milestone: a saved contribution pack becomes a job, takes a real submitted case, records the exact decision durably and exposes the result in Desk with no chat session required. First recurring milestone: the same job runs on a schedule or submitted event under a declared identity while the browser is closed.

## External guidance informing the proposal

Databricks separates a reusable job from its triggers and individual runs; it supports schedules, events and manual execution. Adopt that separation and explicit pause behavior. [Jobs triggers](https://docs.databricks.com/aws/en/jobs/triggers).

Databricks distinguishes permission to manage/run a job from the identity used to access its resources, recommending service principals for stable production execution. Adopt separate owner, trigger actor, run-as principal and reviewer records. [Job identities and permissions](https://docs.databricks.com/aws/en/jobs/privileges).

Databricks repair can rerun unsuccessful work, but its documentation explicitly warns that repair does not make side effects idempotent. Desk should preserve completed work and make repair safety a property of each action adapter. [Repair job failures](https://docs.databricks.com/aws/en/jobs/repair-job-failures).

Temporal separates durable workflow state from activities that perform external work and recommends idempotent activities. This informs the recovery boundary; it is not a decision to add Temporal as a dependency now. [Workflow execution](https://docs.temporal.io/workflow-execution), [Activities](https://docs.temporal.io/activities).

## Planning assumptions to revisit at the pilot boundary

- Begin with one owner, one pack, one real input source, no external writes, and modest volume.
- Select the first real business process, input system, expected daily volume/latency, operational owner and execution location before committing to connector and hosting work.
- Unattended processing of sensitive or consequential decisions needs an explicit organizational policy for release and actions; the tool must record that policy rather than infer it from a pack outcome.
- No timeline or enterprise readiness claim is made before the source/identity and durability spikes are complete.
