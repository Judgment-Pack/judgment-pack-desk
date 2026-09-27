# Jobs: operation, inputs, and automation

Design review • 26 September 2026 • Proposal only. The mock uses illustrative jobs, connections, releases, and results. No job, trigger, connection, or model invocation is created.

## Recommendation

Keep **Jobs** in the main navigation. Use a compact, table-led workspace with **Jobs** and **Runs** views. Job creation is a full page with four steps: **Job → Inputs → Trigger → Review**. Job detail has **Runs, Inputs, Triggers, Release**; secondary settings live in its More menu. Use the existing single-tool right rail for Assistant, Details, Activity, and the saved one-page Brief. Never stack long chat and details vertically.

Borrow the dense operational table, trigger visibility, recent-run indicators, and contextual settings from the supplied Databricks screenshot. Apply Linear's restrained contrast, predictable header actions, small neutral icons, and reduced framing. Avoid copying Databricks' extra pipeline types, compute-cluster configuration, or a task graph that this product does not yet need. A pack is the decision policy; a job is its recurring application. Graphs remain a separate authoring destination until graph execution exists.

## What I checked

Read the live `/jobs` and `/jobs/new` screens, Desk `JobsView.tsx`, `MappedInputFields.tsx`, source input components, client types, mapping design, Runner README and mapping-v2 contract. Current capability:

| Area | Exists today | Required change |
| --- | --- | --- |
| Runs | Durable local queue, immutable release, snapshots, audit, idempotent admission | Trigger occurrences, preparation before admission, configurable limits, cancellation/recovery states |
| Inputs | Manual JSON; one selected local/Drive JSON record; advanced v2 ordered case/file/MCP mappings | Typed forms, graphical mapping, saved source configuration, unattended acquisition |
| Source trust | Runner verifies v2 receipts, request commitments and target classes | Keep those guarantees under scheduling; add supported HTTP/SQL/model adapters |
| Release readiness | Exact snapshot validation and saved tests; coverage advisory | Present a compact checklist; add versioned job configuration and release promotion |
| Briefs | On-demand persisted job/run briefs | Make the rail entry and Generate/Regenerate actions discoverable |
| Triggers | Manual and authenticated API submission | Local interval/calendar scheduler, events, file monitoring; cloud adapters later |
| Operations | Desk-owned Runner companion; interrupted work remains visible | Optional OS service and host health; no browser timers |
| Retention | Local files and SQLite, manual stopped-store backup | Consistent backup/restore and bounded retention before sustained unattended use |

The current list search filters only fetched pages, so it can hide matching older jobs. The manual Run form starts from release sample values, risking accidental reuse. Jobs cannot currently be promoted to another release in place; their only revision is 1. Direct HTTP/model APIs are not yet supported in mapped-source catalog operations. A model-backed MCP tool can already be used with the required trusted profile and explicit generated-value admission. A selected Drive file uses an interactive grant, which is not an unattended authorization.

## Screens and hierarchy

1. **Jobs list**: title and one Create job action; Jobs/Runs tabs; one filter row with search, state, trigger and tags. Columns: Job/pack release, Trigger, Runs on, Recent executions, Next run, More. Recent icons describe execution, never acceptance of the business decision. Rows expose Run now and More on hover AND keyboard focus; all icons have tooltips. Add a restrained host/queue summary. Empty state has a small explanation and Create job, not a dashboard of empty cards.
2. **All runs**: filter by execution, decision, job and time; Needs review is a saved filter, not a third primary navigation item. Execution (completed/failed/interrupted) and Decision (accept/reject/unresolved/not applicable) remain separate columns. Search/filter/sort must query all records with stable cursor pagination.
3. **Job step**: name, saved pack, description, tags, execution target. Local computer is the initial target. The exact checked release is frozen later; never silently choose a new version on each run.
4. **Inputs step**: named sources plus a Facts/Evidence mapping table. Users can combine manual or event parameters, constants, files, API/MCP operations, database queries and model extraction in one job. An individual source opens in one full-height right panel. JSON is an advanced disclosure. Preview values and their provenance beside the target field.
5. **Trigger step**: Manual/API, Schedule, or Event. An advanced Scheduler selection starts with Local and can later attach a cloud provider without changing execution location. Allow multiple named triggers in the underlying model; creation starts with one. Each trigger has exactly one scheduling authority.
6. **Review step**: pack snapshot, input preview, saved test results, mapping completeness, credential readiness, trigger preview, target health. One Create job action. Create automatic triggers paused by default; activation is explicit. Running a sample is not an operational run.
7. **Job detail**: compact title, paused/active indicator, Run now and More. Default to runs, with a short configuration summary; Inputs, Triggers and Release are sibling views. Advanced controls live in Settings. Switching release means review, check, then promote an immutable job revision; existing runs stay linked to their original revision.
8. **Run review**: execution and decision at the top; facts, evidence and source lineage are readable tables. Optional collapsed acquisition/evaluation/audit stages. Brief occupies the existing right pane and is generated only on demand. A generated one-page brief retains its source revision and timestamp; opening it never reruns AI. Correct inputs creates a linked follow-up run, not an edit of history.

## Input configuration

Use pack-referenced facts and evidence requirements to build the form. Pack references are not a complete input schema: infer boolean/enumeration controls only when justified, show suggested types as suggestions, and allow reviewed type/label configuration. Structured or ambiguous values retain an advanced JSON editor. Per-run values start empty. **Use sample** is an explicit action; sample values do not become defaults unless the author explicitly configures constants/default parameters.

Each source configuration has: name, existing connection, allowed read operation/resource, typed parameters, frozen request/query/prompt template, response schema/selection, freshness limit, timeout, bounded retry policy, and per-field mapping. Secrets stay in the connection credential store. Preview/read is explicit and may contact the provider; source selection alone does not. Parameter binding is structural. SQL strings use real bind parameters; never interpolate arbitrary strings into query text.

| Kind | User-facing configuration | Value class and behavior |
| --- | --- | --- |
| Manual / event parameters | Human labels, allowed types/values, required parameters, explicit defaults | Asserted; blank/unknown stays omitted. False, zero, empty, null, absent and unknown are distinct. |
| Local file / storage | Select file or authorized location; file pattern, format, record pointer; standing access for unattended use | Local upload is asserted; acquired sources use the installation profile's class. Initial scope one JSON record; CSV and multi-record batches later. |
| API / MCP | Existing connection, approved operation, typed request fields, response paths | Record or generated according to trusted source profile; class cannot be relabeled by the mapping author. |
| Database | Connection, parameterized query or approved query operation, row selection | Record; wrong subject or ambiguous rows remain unresolved/error according to the reviewed rule. |
| Model | Provider/model, prompt template, structured output schema, temperature if supported, token/spend cap, timeout | Generated. Explicit target admission required, including generated influence passed through a later source. Invalid output becomes unknown; proof failure never becomes valid evidence. |

A model helps derive facts; Runtime still evaluates the frozen pack. A separate Design with AI authoring action may propose mappings and edge tests, but needs review before persistence. It is not an unrestricted agent loop running every scheduled occurrence.

Evidence has **Present / Absent / Unknown**, a linked document/record, subject match, and acquisition metadata. A transport receipt verifies how bytes were acquired, not their truth. Missing evidence is not silently promoted to present because a connection succeeded. Proof/schema failures block preparation and show no decision. A reviewed source rule may legitimately return unknown or absent and let the pack handle it. Do not silently substitute manual values after an integration error.

Sources are ordered with explicit dependencies. Runner's planner expands requests and derives values, not browser JavaScript. Reject cycles, duplicate target writers, unsupported transformations and unbound parameters. Every read fact must be mapped or deliberately omitted. Export/import advanced mappings without lossy number conversion.

## Local schedules and events

Scheduling and acquisition must work without an open browser. First implement a durable scheduler in Runner, which can initially remain Desk's companion; Desk must stay running. Offer a separately managed OS service for genuine background operation. The machine must be awake. Closing the browser is safe; sleeping/stopping the host is not continued execution. Display host heartbeat and offline state on Jobs, not a generic message after every form.

Schedule controls: interval, daily/weekly calendar, one-time; IANA timezone; start/end; preview of the next three actual timestamps with offsets. Cron is advanced, with a declared dialect. Define DST behavior explicitly: default skip nonexistent calendar times and run once for repeated times; interval schedules use elapsed time. Default missed runs to Skip with a visible occurrence history; optional run latest once on recovery; bounded catch-up later. Missed/skipped occurrences are not fabricated runs.

Event controls: authenticated API/webhook, local file arrival/change initially; storage notifications, table changes, queues and polling later. A local-file watcher uses authorized roots, path/pattern filtering, ready/stable-file delay, debounce, bounded pending events, a persistent cursor and content/version identity. Reconcile after watcher loss; an event that expired or whose content changed cannot use a newer file under the old identity. Uploaded sample files and temporary picker grants cannot power an active recurring trigger.

For webhooks: scope credentials to the job/trigger, validate signature/token and timestamp, persist the event envelope before acknowledging, bind the event ID to its payload digest, and reject conflicting repeats. Retain a cursor for polling and paginate before advancing it. API credentials never appear in URL parameters. Public sources cannot reach a loopback-only endpoint; choose a managed reachable ingress or an outbound queue consumer when remote delivery is introduced.

Execution defaults: one active run per job, bounded FIFO queue, queue expiry, preparation deadline and evaluator timeout. Separate provider delivery retries, safe read acquisition retries, and evaluation attempts. Repeat delivery returns the original occurrence/run. Acquisition retries freeze successful earlier responses; changed/stale responses restart preparation explicitly. Once evaluation may have happened, do not automatically replay: preserve interrupted status and reconcile/ask for a linked follow-up. An unresolved or rejected decision is not a technical failure to retry away. Pause stops new automatic occurrences; active runs continue. Cancellation is separate and best effort; show cancelling until confirmed.

## Cloud compatibility and repository ownership

Use an adapter contract, not cloud-specific job definitions. A trigger produces a durable occurrence with workspace/job ID, trigger ID/revision, event ID or scheduled time, authenticated principal, payload digest, expiry and resolved job release revision. Runner snapshots the execution revision at admission; a queued occurrence never drifts to a later release. Preserve the occurrence-to-run relationship and attempt history.

Scheduler and execution target are independent. An AWS schedule may wake a local Runner through a queue bridge; selecting AWS does not move compute or artifacts to S3. A remote worker is a separate future capability with its own artifact-store policy. Do not promise exactly-once delivery; deduplicate admission and expose uncertain evaluation recovery.

| Repository | Responsibility |
| --- | --- |
| Desk | List/detail/forms, typed mapping UI, connection picker, preview/release review, trigger settings, briefs and run review |
| Runner | Job revisions, triggers/occurrences, durable scheduling and queue, server-side acquisition orchestration via Gateway, verification, local storage, recovery and retention |
| Gateway | Credentials, read-only provider adapters, normalized acquisition responses/receipts; extend catalog HTTP support before advertising HTTP SQL/model sources |
| Runtime | Pack/test evaluation and audit. No scheduling or provider credentials. |
| Spec | No change needed for this UI. Pack-level acceptable-source-class declarations remain a separate future proposal. |

No new repository/framework is needed initially. Put scheduler integrations behind Runner interfaces. Initial future adapters: AWS EventBridge Scheduler → SQS/bridge; Azure Logic Apps Recurrence / Event Grid → queue/bridge; Google Cloud Scheduler / Pub/Sub → queue/bridge. Support authenticated HTTP admission where the target is reachable. Persist provider IDs, health and reconciliation state, not just a cron string. Cloud options in the prototype are explicitly future concepts; production hides unsupported providers or labels them unavailable. Keep one source of scheduling authority and record provider changes to avoid double firing.

## Other improvements and boundaries

- Saved filters and tags before hierarchical Jobs folders; do not add another inner left browser by default.
- Release changes show a diff, rerun exact tests, and require review; coverage remains advisory. Failed/incomplete tests block activation, no tests needs explicit acknowledgement. Legacy jobs remain runnable with their recorded release.
- Runtime trust guarantees do not imply multi-user permissions. Add stable run-as identities and scoped machine credentials before unattended shared/cloud use; the current installation has one owner.
- Review queue records missing evidence, unresolved decisions and interruptions. Assignment/comments are future collaboration features, not implied by a local owner label.
- In-app attention state first; optional email/webhook destinations later, explicitly configured. No business-system write/action execution in this scope.
- Backup/restore must cover SQLite + retained artifacts consistently, not just chat. Retention is opt-in and explains audit consequences; live releases, pending runs, unresolved reviews and holds cannot be pruned. Store lifecycle events and disk usage.
- Compact does not mean tiny targets: 32px desktop controls, 40–48px rows (two-line rows up to 56px), 16px icons, 8px local gaps, 24px section spacing. Keyboard focus, text labels/tooltips, light/dark contrast and reduced motion all remain supported. Narrow screens hide optional columns via Display and show panels as full-height sheets with an explicit Back action.

## Delivery order and acceptance

1. **UI and input authoring**: compact Jobs/Runs, fresh typed forms, mapping builder over v2, source preview and exact release review. Reuse current backend contracts where possible; add server-side query filtering. Hide unsupported providers.
2. **Durable local automation**: independent preparation tasks, standing credentials, interval/calendar schedules, occurrence deduplication, local file and authenticated API events, host health, pause/queue/missed-run policy, backup/restore. Then OS service packaging. This is backend work, not a UI-only switch.
3. **Cloud adapters and collaboration**: provider bridge, service identity, connection health and reconciliation; remote execution only as a separate explicit rollout. More source formats/batch workloads, notifications and lifecycle management expand as validated.

Acceptance examples: closing a browser does not halt preparation; duplicate event delivery creates one occurrence/run; host sleep follows the configured missed-run policy; a stale/invalid receipt cannot run; changed pack or mapping invalidates release approval; missing evidence can produce Completed + Unresolved; generated data cannot silently become record-class data; rerun never overwrites the original; briefs generate once and persist. Verify DST transitions, queue saturation, source revocation, file replacement, shutdown during acquisition/evaluation and recovery after acknowledgement loss.

## References

- [Linear: A calmer interface for a product in motion](https://linear.app/now/behind-the-latest-design-refresh) — hierarchy, restrained color, less visual framing, consistent control placement.
- [Linear: How we redesigned the Linear UI](https://linear.app/now/how-we-redesigned-the-linear-ui) — density, navigation and neutral themes.
- [Databricks: Configure and edit jobs](https://docs.databricks.com/aws/en/jobs/configure-job) — job properties, parameters and contextual settings.
- [Databricks: Schedules and triggers](https://docs.databricks.com/aws/en/jobs/triggers) — manual, scheduled and event triggers; pause semantics.
- [Databricks: Scheduled jobs](https://docs.databricks.com/aws/en/jobs/scheduled) — interval/calendar schedules, timezone and DST considerations.
- [Databricks: File arrival](https://docs.databricks.com/aws/en/jobs/file-arrival-triggers) — debounce and minimum trigger interval.
- [AWS EventBridge Scheduler](https://docs.aws.amazon.com/scheduler/latest/UserGuide/managing-schedule.html) — schedule targets, retries and dead-letter queues.
- [Azure Logic Apps recurrence](https://learn.microsoft.com/en-us/azure/logic-apps/concepts-schedule-automated-recurring-tasks-workflows) — recurring workloads and missed recurrence behavior.
- [Google Cloud Scheduler](https://docs.cloud.google.com/scheduler/docs/overview) — at-least-once delivery and request deduplication.

These references inform the proposal; they do not imply feature parity or endorsement. The architectural choices and defaults above are recommendations for Desk.
