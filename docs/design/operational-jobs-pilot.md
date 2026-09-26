# Operational Jobs: implemented local pilot

Date: 2026-09-25. Stage 2 of the [Jobs plan](operational-jobs-plan.md).
The earlier [contracts and mock](operational-jobs-contracts.md) remain design proposals
for the broader system; their `0.1-design` record schema is not the implemented API.

## Ownership and implemented flow

- `judgment-pack-runner`: independent Go module, SQLite queue, immutable release data,
  pinned Runtime binaries, API admission/idempotency, one dispatcher, audit binding,
  durable run history and conservative crash recovery. The new repository is local
  and uncommitted; no remote release is implied.
- Desk: Jobs navigation, create/preview/review flow, manual input forms, run history,
  result/audit views, installation-owned companion lifecycle and a narrow authenticated
  proxy at `/api/operations/`. UI copy uses the existing 12 locale catalogs and shared
  controls, spacing, page headers and contained scrolling.
- Runtime: unchanged. Validation, reviewed-set lock generation and experimental
  evaluation through its CLI, never internal package imports or duplicated evaluation.
- Gateway: unchanged and unused in the manual/API pilot. Future connected inputs and
  action dispatch require actual source/delegation/approval contracts.

Open **Jobs → Create job**, or a saved pack's **More → Create job**. **Check release** creates a
retained release snapshot, evaluates a sample as rehearsal, and runs saved expectations. Creating the job requires
explicit review. Editing its input clears approval. Repeating creation for the same
preview/name returns the same job. Each job has one fixed revision in this pilot;
new pack content requires a newly reviewed job. The release keeps its exact test matrix,
Runtime report, case names, suite revision, timestamp and pack/matrix/Runtime digests.
Runtime owns test comparisons; Runner checks report completeness and identity. Passing
suites can create jobs after review even with advisory coverage gaps. Failed or incomplete
checks cannot create jobs, including through the API. No saved expectations remains
**Not run**, allowed only after explicit review as untested. Existing jobs are unchanged.
Desk rereads saved inputs before checking and before creation; edits invalidate review.
Registered matrices are read before their first Tests-page import; after import, the
Tests workspace is authoritative. Unsaved proposals and exploratory cases are not tests.
Read failures never silently become an untested release. See [release readiness](release-readiness.md).

Run job accepts nested facts JSON and optional tri-state evidence availability. These
are declarations, not provider-verified documents. A successful submission returns only
after durable acceptance. Closing the browser does not cancel it. Runtime appends an
audit for operational runs; the runner verifies its binding to the supplied snapshot
and response before recording Completed. Reject, unresolved, or not-applicable are
normal decisions, separate from execution Failed/Interrupted.

## API and durability

The runner repository's `openapi.json` describes `/v1` operations. Desk maps the suffix
to `/api/operations/` under its existing session/owner bearer and origin guards.
Workspace and actor are derived by the installation, not accepted in request bodies.
There are no action credentials or browser-controlled subprocess addresses.

Run submissions require `Idempotency-Key`. Same job, owner, key and input returns the
same run across restarts; changed input returns 409. Pending work is bounded to 100,
requests to 2 MiB and each Runtime invocation to 30 seconds. Lists are paginated, newest
first. Raw inputs/audits stay on run details rather than list responses.

The local companion starts when Desk starts. The queue resumes without a browser.
It holds an exclusive process lock and uses SQLite WAL with synchronous FULL. After a
restart, queued work continues; work that was running becomes Interrupted. A run whose
audit was appended just before a crash is never silently reevaluated. Its attempt files
remain available for inspection, without an exactly-once or recovered-result claim.

## Explicit limits

- Manual/API, single pack, single installation owner. Linux/macOS/FreeBSD host support.
- No schedules, provider polling, graph/batch jobs, review inbox, notifications or writes.
- No automated rerun, cancellation UI, in-place job revision activation or retention UI.
- Runtime's supported evaluator remains experimental `0.2.0-draft`, outputVersion 2.
- Desk must remain running; browser closure is safe, host shutdown stops processing.
- Job storage is `<Desk config>/jobs/<project-path digest>/`, independent of movable
  chat storage and **excluded from current Desk workspace backups**. Stop Desk/runner
  and back up the complete directory, not just a live SQLite file. The runner README
  explains restore scope. Project relocation requires a later explicit migration.

## Verification

Real Runtime tests cover successful and missing-evidence decisions, retained audits,
release immutability, executable drift, durable idempotency, queue restart, duplicate
process exclusion, unauthorized requests and a forced process exit after real audit
append before completion persistence. Desk tests cover proxy authorization and companion
restart. Browser verification creates an isolated job, submits a run, closes the tab
immediately after acceptance, reopens its completed decision/audit, then restarts Desk
and reopens the same history. Desktop, light theme, 390px and 320px screenshots check
layout and horizontal containment. Test fixtures do not create jobs in the user's project.

Next: add one connected input with explicit
mapping and delegated authority before enabling its schedule. Keep approval/action
execution as a separate stage from merely recording a judgment.

## Local verification record

- Runner: all five real-Runtime tests pass under the Go race detector; `go vet` passes.
- Desk: complete Go test suite and vet pass, including companion restart tests.
- Frontend full sweep: 4,233 passed, one skipped; it identified a route-shell placement
  issue and an undefined font token. Both were fixed, then all 610 affected route,
  palette, shared-style, containment and Jobs interaction checks passed.
- Production TypeScript/Vite build and all 12 locale catalogs pass. The existing
  large-chunk build advisory remains.
- Browser: eight desktop/light/mobile screenshots, no page errors or horizontal
  overflow; browser closure and a subsequent Desk restart both preserve the run/audit.
- Live Desk: `/api/operations/jobs` returns 200; Create job lists the three existing
  saved packs. No sample business job or run was created in the user's workspace.

Local verification artifacts are retained under `/tmp/desk-jobs-pilot/`. Changes remain
uncommitted. The installed Desk and runner were restarted on the existing local port.

## Follow-up requirement: shared evidence briefs

The 2026-09-25 user interview adds persistent, explicitly generated evidence briefs for
test cases, job releases and operational runs. See [the requirements and ownership review](evidence-briefs-plan.md).
This moves frozen evidence records and saved briefs ahead of scheduled execution; it
does not claim that the current pilot already stores connected-source evidence or shared
organization reviews. No operational behavior changed in this requirements review.
