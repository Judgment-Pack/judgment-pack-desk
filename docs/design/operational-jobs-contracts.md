# Jobs contracts and interaction decisions

Date: 2026-09-25. Status: design proposal, stage 1. This document and the isolated mock implement no runner, schedule, approval, provider call or production API.

The approved direction is in [the operational jobs plan](operational-jobs-plan.md). The draft [record schema](jobs/records.schema.json) and [example records](jobs/example-records.json) make the first slice reviewable. Schema version `0.1-design` deliberately claims no released compatibility promise. All example data, digests, grants and identities are fictional. Schema validation is structural, not artifact or authorization verification.

## Open the design

With the local Vite server running:

- [Jobs](http://localhost:5173/jobs-design.html#screen=jobs)
- [Job overview](http://localhost:5173/jobs-design.html#screen=job)
- [Create job](http://localhost:5173/jobs-design.html#screen=setup)
- [Input mapping](http://localhost:5173/jobs-design.html#screen=setup&step=1)
- [Run details](http://localhost:5173/jobs-design.html#screen=run)
- [Needs review](http://localhost:5173/jobs-design.html#screen=jobs&tab=reviews)
- [Missing evidence review](http://localhost:5173/jobs-design.html#screen=review)
- [Action approval review](http://localhost:5173/jobs-design.html#screen=review&case=action)

The preview toolbar and footer label every page as fictional. Search, tabs, setup steps, theme and contextual rail work in memory. Operational controls only produce a preview notice. No session bootstrap, API calls, credential access, model requests, storage writes or application routes are imported. Vite serves the separate HTML entry; the production entry does not import it. Preview copy is English; shipped UI must use the locale catalogs.

The review includes future scheduled and approved-action examples to assess the complete journey. Stage 2 implementation remains single-item manual/API, no external writes. These examples are not claims of installed integrations or capabilities.

## Object identity and ownership

Every record has `schemaVersion`, a discriminating `kind`, stable opaque ID and workspace ID. The runner derives workspace authorization from its authenticated caller, never from a trusted-looking request body. The object fields identify records and do not grant permission.

- **Release**: immutable manifest, target, runtime pin and validation/test artifact references. The manifest retains exact pack bytes; graph releases retain the full referenced pack/configuration closure. A human version label cannot replace the digest.
- **Job**: mutable name/owner/state and active revision pointer. Changes use optimistic concurrency. Archived jobs keep history; archiving stops future automatic admission.
- **Job revision**: immutable release, mappings, source binding/grant references, trigger, run-as identity, limits and handling. Editing produces a draft revision; activation atomically moves the job pointer.
- **Run**: accepted invocation pinned to a job revision and release, with one item in the first pilot. Large input, decision and disposition artifacts are addressed by digest.
- **Review**: work item for an actual run/item/input/release. A resolution links a new evaluation rather than rewriting the original result.

All cross-record IDs and artifact references are checked within the workspace. Storage hashes the exact retained artifact bytes and verifies their length; the schema's well-formed digest string is not verification. The examples use placeholder artifacts and therefore cannot execute.

`createdBy`, `requestedBy`, `runAs` and review actors are populated from verified identities. The local pilot uses a stable installation-bound owner ID, not the mock email or a browser token. Shared workspaces and machine delegation need a separate authorization contract. Credentials are references to Gateway custody; none are serialized into these records.

## Release admission

1. Capture exact bytes in a private, immutable bundle. Resolve every dependency before calculating the manifest digest. Canonicalize the manifest under a versioned rule; do not canonicalize the retained source files in place.
2. Validate under the pinned Runtime binary/artifact and supported experimental evaluator contract.
3. Bind validation and saved-test evidence to this bundle and its inputs. Show mismatch/not-run/coverage gaps honestly; they cannot be described as passing. Organizational release policy can require tests, but coverage remains advisory in the Runtime.
4. Record release author and review time. A local release is a review event, not a cryptographic attestation unless signing is explicitly added.
5. Run the target from a private per-release project with the reviewed lock and audit configuration. Never evaluate the author's mutable project path for an active job.

Activation verifies that the referenced release and permissions still exist, that grants support the configured trigger, that mappings are valid and that the runner supports this schema/runtime combination. A job whose source is only selectable interactively cannot activate a schedule. Pack edits and source refreshes never move an active release pointer.

The preview enables activation only after its example preview is shown. In production, a preview is bound to the configuration digest and relevant sample inputs. Editing any relevant field invalidates it; a CSS-enabled button is never activation authorization.

## Proposed API boundary

Paths below belong to the runner's `/v1` API, not Desk's current chassis. Desk proxies or connects through a narrowly authenticated runner client. An API schema/OpenAPI definition is delivered with stage 2, after the invocation boundary spike.

| Operation | Request and concurrency | Result |
| --- | --- | --- |
| `POST /workspaces/{w}/releases` | Exact bounded bundle upload plus digest; authorized release actor | Immutable release ID and validation/test references |
| `POST /workspaces/{w}/jobs` | Name, release and draft configuration; no arbitrary run-as impersonation | Draft job and revision IDs |
| `POST /workspaces/{w}/jobs/{j}/revisions` | Base revision plus configuration; `If-Match` job ETag | New immutable draft revision, no activation |
| `POST /workspaces/{w}/jobs/{j}/activate` | Exact revision ID and current ETag; release/run-as/source preflight | Atomically updated active pointer and new ETag |
| `POST /workspaces/{w}/jobs/{j}/runs` | Input or permitted locator, pinned revision selection, required `Idempotency-Key` | `202` and persisted run ID after durable acceptance |
| `GET /workspaces/{w}/runs` | Bounded cursor pagination and explicit filters | Summaries without source text or credentials |
| `GET /workspaces/{w}/runs/{r}` | Run-read permission; source artifacts require their own access | Run, item, attempts, decision and permitted artifact links |
| `POST /workspaces/{w}/jobs/{j}/pause` | ETag and configured pause scope | Future scheduled admission stops; active work remains |
| `POST /workspaces/{w}/runs/{r}/cancel` | Current run ETag | Cancellation requested; no promise to undo completed work |
| `POST /workspaces/{w}/runs/{r}/replay` | Retained input references; a new idempotency key | New preview run linked to its original, external actions disabled |
| `POST /workspaces/{w}/reviews/{q}/requests` | Current review ETag and note | Durable missing-input request; external delivery is a separate action |

Use `401` unauthenticated, `403` unauthorized, `404` missing or concealed resource, `409` incompatible state/key reuse, `412` stale ETag, `422` semantically invalid configuration, `429` capacity and `503` unavailable runner. Responses carry stable error code, safe message, retryability and correlation ID. No secret/provider response dump in an error message.

Use job/revision IDs rather than names in routes. Run links and review links remain valid after renaming the job. Listing a job does not imply permission to view its confidential evidence.

## Admission, leases and crash behavior

`Idempotency-Key` is scoped to workspace, job and authenticated caller. Hash the validated request in a versioned deterministic representation. Persist the key, request digest, resolved job revision, run and dispatch intent in one transaction. A repeat key plus identical request returns the original run; a repeat key with different input fails with `409`. Resolve an existing key before consulting a newly active revision. Retain keys at least as long as their runs; never quietly recycle one while a caller could retry it.

For scheduled runs use a stable key derived from job revision and scheduled occurrence. Source polling also requires a stable source record/version business key. Persist cursor advancement with per-item durable admission, not before it. Queue capacity is bounded; overload is visible rather than silently dropping accepted work.

The first dispatcher has one durable store and renewable leases with fencing generations. Store attempts separately from logical runs. A stale worker cannot commit over a newer lease; input acquisition may have happened twice, but only the selected verified snapshot becomes the evaluated input. Check cancellation before starting every step.

State path: queued → running → completed / failed / cancelled / interrupted. Transient bounded retries remain attempts under running with a visible next retry time. Interrupted recovery returns to queued only when safe; a new rerun or replay creates a new run ID. Completed results never revert to running.

Completion means the decision pipeline and configured recording/review routing finished. A review may still be open. External action delivery has its own state and never masquerades as the Runtime's decision or a green execution label.

## Audit correlation decision for the pilot

Runtime's existing audit append and the runner's transaction do not share an atomic commit or caller-selected evaluation ID. Do not pretend that an API idempotency key closes that gap.

Use one private invocation directory per attempt, containing its pinned project/lock, exact input snapshot and configured audit destination. Hold one live evaluator invocation per attempt. Retain bounded stdout/stderr and the complete audit output. After a crash, require the old process to be stopped and inspect retained artifacts before considering another invocation. A complete validated record can be reconciled only when it binds to the expected pack/input/runtime and the complete response can be safely recovered. A partial record or missing output remains interrupted with an explicit recovery choice; never silently rerun a non-rehearsal call and label it the same decision.

Before stage 2 acceptance, fault-inject crashes around evaluation, audit append and result persistence using the pinned real Runtime. If those artifacts cannot provide an unambiguous binding/recovery path, add a narrowly versioned invocation/correlation result contract to Runtime. This is a release gate, not resolved by document schema validation. Gateway action dispatch is not enabled in this pilot, so ambiguous external writes are deferred rather than exposed.

## Input and source authority

A mapping snapshot binds source fields to facts and evidence availability; it preserves omitted facts, false values and explicit evidence absence. Record any AI-proposed extraction as a reviewed mapping or a separately bounded step. Never mark evidence present merely because an attachment exists.

Connected source bindings name a specific eligible operation/resource scope and a Gateway grant. Interactive picker grants do not become unattended grants. Before schedules, Gateway must report unattended support and validate a job-scoped read grant with resource bounds, principal, expiry/revocation and freshness. Revoked access fails before another acquisition. New source bytes form a new snapshot; they cannot mutate previous run inputs.

The draft schema admits connected schedules to describe stage 3, but the first runner must reject that configuration until the installed Gateway contract supports it. Manifest capability checks are separate from schema parsing. Manual/API inputs and connected acquisition share mapping and recorded provenance; they do not claim the same origin.

## Review behavior and future action contract

Missing evidence: keep original decision unchanged; record request text and assignment. A supplied correction creates a linked run using the same release unless an explicit release change is chosen. Resolving a review requires a linked completed evaluation and reviewer event; never make a checkbox overwrite unknown to true. Dismissing a work item preserves its unresolved business outcome.

The future action-approval example is deliberately separate. Approval binds workspace, job revision, run/item, release and input digests, destination, operation, canonical argument digest, authenticated approver and expiry. A changed destination, argument, source or policy requires new approval. A token proves an identity; it is not proof of this approval.

Approve action initially authorizes dispatch of exactly the reviewed operation. It does not rewrite Accept/Reject, edit a pack, or grant standing permissions. The runner records action intent transactionally before dispatch. Provider idempotency or reconciliation is required; a timeout after a possible write becomes delivery-unknown. Decline preserves both the decision and proposed action record.

The Gateway executor currently requires authenticated requester lineage but does not bind this approval. Before enabling stage 4 actions, agree delegated identity and approval enforcement with Gateway, including who can bypass the runner, credential scope, revoke/expire behavior and provider response interpretation. The schema intentionally has `externalActions: none` and contains no misleading implementable approval object yet.

## Stage 1 review and stage 2 acceptance

The static design fixes these interaction choices: Jobs has collection views; setup is five compact steps; context tools share one full-height pane; missing-input review and action approval are different screens; Completed plus Reject is normal; failure before evaluation says Not evaluated. Raw identifiers/JSON remain secondary.

Before implementation, the bounded next slice is one owner, one released pack, manual/API input, one runner, and no external action. Stage 2 requires actual crash/recovery checks, bounded API admission, immutable release isolation, idempotency conflict handling, identity checks, retained decision records and browser-closed completion. No mock screenshot or structural schema validation substitutes for those acceptance tests.

## Static screenshots

- [Jobs list](../../web/mockups/jobs-design/jobs.png)
- [Job overview](../../web/mockups/jobs-design/job.png)
- [Create job](../../web/mockups/jobs-design/setup.png)
- [Input mapping](../../web/mockups/jobs-design/inputs.png)
- [Run details](../../web/mockups/jobs-design/run.png)
- [Missing evidence review](../../web/mockups/jobs-design/review.png)
- [Action approval review](../../web/mockups/jobs-design/action.png)
- [Run with Details open](../../web/mockups/jobs-design/run-details-pane.png)
- [Light theme](../../web/mockups/jobs-design/run-light.png)
- [Phone Jobs list](../../web/mockups/jobs-design/jobs-mobile.png)

The same images are directly viewable through Vite at `/mockups/jobs-design/<name>.png`.

## Verification

- TypeScript check passes.
- Existing shared-style and scroll-containment suites: 511 tests pass.
- Chrome: 15 captures at 1440, 390 and 320 pixel widths, with dark and light themes. No page overflow or page errors; no backend API calls. Browser storage writes are refused by the verification harness.
- Search, setup progression, sample-preview activation gate, review notices, Completed/Reject versus Failed/Not evaluated, contextual pane close, and theme selection are exercised.
- JSON Schema Draft 2020-12 checks all five fictional records. Six malformed examples are refused: unknown field, active job without revision, schedule without connected source, connected source without grant, completed run without decision record, and resolved review without a linked run.
- These checks validate the design artifacts only; no scheduler, Runtime operational call, Gateway acquisition or live provider was exercised.

See [browser measurements](jobs/browser-check.json) and [contract checks](jobs/contracts-check.json).
