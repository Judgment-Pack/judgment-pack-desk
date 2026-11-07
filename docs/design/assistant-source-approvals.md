# Assistant source approvals

Source approval records consent to one complete, observed Desk source adapter
configuration. It is separate from a tool's action approval, a research compute
allowance, and the canonical acceptance of a returned acquisition receipt.
Approving a source does not itself start a model, acquire a page, create a
research job, or grant additional requests.

This document describes source review, its standalone browser control, and the
external-read control display contract. Execution integration must preserve these
contracts; a ledger entry or displayed control alone does not establish that an
execution path enforces current approval or verifies acquisition proof.

## Adapter declarations

Each adapter owns a complete singleton declaration. These declarations are Desk
custom sources, not copies of a Gateway MCP inventory.

| Adapter | Tool | Operation | Effect | Action approval |
| --- | --- | --- | --- | --- |
| `desk-search` | `search_sources` | `discover` | `read-external` | none |
| `desk-link` | `read_link` | `read` | `read-external` | none |
| `desk-website` | `explore_website` | `discover` | `read-external` | none |
| `desk-research-workflow` | `request_dialect_research` | `act` | `act` | required |

The complete declaration includes `name`, `description`, `inputSchema`,
`taxonomy`, `effectClass`, `delegation`, and `approval`. Approval binds the whole
list, including descriptions, schemas and taxonomy. A client must not review
only a subset or replace the declaration with a tool name.

## Scope and currentness

The server derives project, chat, signed-in session, principal and policy scope.
Clients cannot supply or change these fields. The chat must exist in the current
private store. Research Auto preferences do not create source approval.

A snapshot binds the adapter version, complete declaration digest, observed
configuration digest and identity digest. Configuration includes the selected
endpoint and its authority/key digests; search additionally binds the actual
selected connection, revision, provider and preference. A managed source also
binds its checked bundle, verifier and launched build where applicable.

Review does not start a Gateway or companion. It can observe an already-running
connection companion. Managed website capability comes from a retained complete
catalog obtained by an explicit setup read, bound to the actual owner, bundle,
build and policy. A manifest alone cannot establish that capability. Observed
owner changes, shutdown, failed currentness, or bundle/policy/build drift
invalidate the retained observation.

Observation occurs outside the private-data lock. The server checks its local
scope and configuration again under the lock before persisting approval. This
records the exact observed snapshot; it cannot exclude a later external
connection change. Execution must check current identity and authority at final
dispatch. An unchanged endpoint label is insufficient.

## Executable source enrollment

Ordinary conversation assembly enrolls each of the four source owners only
when the current authoritative private store contains the exact unrevoked,
nonhistorical approval for the current authenticated scope and complete freshly
observed snapshot. Assembly does not create review records or approvals, start a
source companion, acquire material, or grant an allowance.

Sources requiring setup or review, and changed or revoked sources, are withheld
from the executable offer. All four capabilities remain available through the
existing source review/status API and Source approvals control. Unavailable
external sources do not block local chat, Graph reads or Plan work. After an
explicit source approval, a new execution assembly can offer that source; an
already assembled run does not gain tools automatically.

The approved source wrapper declares provenance and taxonomy together. The
three external reads retain their existing read/research delegation and use the
real scoped receipt verifier. The workflow declares act/act, has no delegation,
and retains its required per-call policy, exact allowance disposition and root
Work restriction. Source consent alone never authorizes that workflow. Current
scope, approval revision, revocation, configuration and identity are rechecked at
dispatch and receipt acceptance; retained callbacks cannot revive stale consent.
The canonical adapter keeps each external-read capability scoped to its actual
original Writer/Store and root or child invocation.

This activation uses the existing interactive plain-store lifecycle. It does
not activate unattended lease execution or claim effect-host certification.
Fenced non-read dispatch must still use Agent's authority-bound effect host.

## HTTP contract

All routes require the normal authenticated Desk session and a usable private
workspace. The path selects an existing chat. Query parameters are rejected;
responses use `Cache-Control: no-store`.

| Method and route | Meaning |
| --- | --- |
| `GET /api/assistant-source-approvals/{chat}` | Observe the complete current catalog and obtain exact review references. |
| `POST /api/assistant-source-approvals/{chat}/approve` | Approve the exact unconsumed review. |
| `POST /api/assistant-source-approvals/{chat}/revoke` | Revoke the exact active approval. |

GET may persist a new review record. Repeated observation reuses an unconsumed
review only when its complete snapshot and scope still match. It performs no
source acquisition or model execution. The response is an object with
`version: 1` and `sources`, containing one row for each adapter.

Each row contains `snapshot` and `status`, and may contain `reviewId`,
`reviewRevision`, and `approval`. Row status is `setup-required`,
`review-required`, `approved`, `changed`, or `revoked`. A changed or revoked row
can carry a fresh review reference for the newly observed snapshot. Display
status alone never authorizes a write.

A snapshot contains `adapterId`, `adapterVersion`, `kind`, `trust`,
`configurationDigest`, `toolListDigest`, `completeDeclarations`, `configuration`
and `status`; `identity` and `identityDigest` are present when available. Its
status describes observation readiness; the row status additionally describes
approval state. Snapshot configuration carries `target`, `targetDigest`,
`authorityDigest`, `publicKeyDigest`, `deskDigest`, `preferenceDigest`,
`connection`, `revision`, `provider`, `bundleDigest`, `verifierDigest`,
`gatewayBuildDigest`, `ownerDigest`, `catalogDigest`, and `catalogOwnerDigest`.
An empty configuration member denotes no observed value, not an inferred one.

The browser response reader requires the complete four-adapter catalog and
refuses duplicate or unknown members, incomplete declarations, mismatched
adapter/tool classifications and inconsistent review or approval references.
Schema numbers retain their original JSON literals when displayed. These
checks protect the review presentation; they do not verify receipt signatures
or establish current execution authority. The server must still perform its
own approval and dispatch checks. The reader is connected to the standalone
`SourceApprovalControl` described below.

Approve accepts exactly:

```json
{"reviewId":"0123456789abcdef0123456789abcdef","reviewRevision":1}
```

Revoke accepts exactly:

```json
{"approvalId":"0123456789abcdef0123456789abcdef","approvalRevision":1}
```

These are illustrative selectors, not real authority. Record IDs are 32 lowercase
hexadecimal characters. Request bodies are bounded to 4096 bytes and reject
unknown or duplicate members, wrong case, malformed JSON and invalid selectors.
The server owns all IDs, revisions, snapshots, timestamps and scope fields.

Approve requires an unconsumed, nonhistorical review in the current scope and an
exact match to the freshly observed complete snapshot. It consumes that review
and persists a new revision-1 approval. Replacing an existing approval revokes
that record; it does not erase its history. Approve returns the complete catalog.
A repeated consumed selector refuses rather than granting twice. After a lost
acknowledgement, observe the catalog before offering another explicit decision.

The browser client captures one chat and authenticated session. It serializes
explicit review and decision requests, sends only observed selectors, and never
retries a mutation. Refused or uncertain decisions require another explicit
catalog observation before a later decision. Approve acknowledgements must
identify the same complete snapshot; revoke acknowledgements must identify the
original approval. Response bodies are bounded and decoded as strict UTF-8.
Because GET can persist a review, it is an explicit observation rather than a
periodically polled private snapshot. The mounted standalone review uses this
client; its presence does not establish source enrollment or execution authority.

An approval reference contains `id`, `revision`, `approvedAt`, and optional
`revokedAt`. Revocation requires the exact active revision, needs no source-owner
observation, and returns `version: 1`, `status: "revoked"` and the persisted
approval reference at revision 2. A repeated or stale revocation refuses. GET
then supplies the complete current catalog.

Invalid request selectors return 400. Scope refusal returns 403. Changed,
consumed, stale or unavailable reviews return 409; bounded ledger exhaustion
returns 413. These refusals grant no fallback approval and must not trigger
source execution or automatic model retry.

## Persistence and transfer

Reviews and approvals reside in the private project source-approval ledger,
outside the project watcher. The ledger retains at most 128 reviews and 128
approvals within the existing bounded record size. Capacity refusal does not
evict active authority. Writes require exact persisted-byte read-back and a
successful closed decode before acknowledgement.

Backup/restore, storage relocation, project move and manual history relink mark
transferred reviews and approvals historical before committing the destination
binding. Original scope and snapshot bytes remain evidence. Historical records
cannot authorize a new session or reactivate after a move; even the same session
needs a fresh exact review. Corrupt ledger or failed historicalization refuses
the transfer before the destination binding is committed.

Execution must reopen the current authoritative store and re-read the approval's
scope, revision, historical state and revocation state. A retained old root or a
cached approval cannot survive a storage-pointer change as authority. A valid
source approval does not make a rejected or missing acquisition receipt valid,
and a generic action decision does not grant a research compute allowance.

## Standalone browser review control

`SourceApprovalControl` connects explicit browser review to the ChatPanel toolbar
through the existing source-review client. Execution activation remains separate.
Opening or mounting the control sends no request. Only Review sources or Refresh source review explicitly observes the
catalog, because GET can persist review records. No query polling, focus refresh,
automatic retry or automatic post-revocation GET is used.

The review shows all four singleton declarations and the full decoded snapshot,
including configuration, identity, descriptions, input schemas, operation, effect,
delegation and action-approval classification. Schemas use the shared `wireJSON`
formatter with member order retained; CodeBlock renders this text verbatim.
Original number literals survive display. The formatter recursively traverses
schemas and can exceed the JavaScript call stack on sufficiently deep trees;
there is no portable numeric depth threshold or claim of arbitrary-depth support.
All complete displays are prepared before any decision is enabled. A formatting
error retains prior display as stale and disables all decisions. No partial,
rounded or truncated representation is substituted, and no new depth or size
limit is introduced. Qualification concerns the actual four Desk schemas.

Source approval records consent to the observed configuration only. The research
workflow remains an act source requiring its own action approval and compute
allowance. Reviewing or approving this source starts neither workflow nor model,
and creates no additional action grant, request allowance or receipt evidence.

Decisions send only exact observed review or approval selectors. The control
waits for acknowledged results and never displays optimistic approval. A complete
approve acknowledgement replaces the displayed catalog; selectors missing from
that result are not retained from an earlier review. A revoke acknowledgement is
an exact receipt, not a synthesized catalog. It disables all decisions until a
new explicit review. Refused, malformed or uncertain acknowledgements likewise
require an explicit refresh and then a separate explicit decision; no mutation
is replayed automatically.

Review state is local to one chat, owning ChatStore instance and signed-in client
session. Changing scope or ending the session aborts observation and invalidates
late results. Selectors are not saved into the conversation or browser storage.
The control never checkpoints or edits the composer, claims response ownership,
or starts a source/provider. A missing persisted chat remains an ordinary scope
refusal until the normal save path completes and the user explicitly refreshes.

## External-read physical controls and failures

For a sourced `search_sources`, `read_link`, or `explore_website` call, the private
host operation result can retain `externalControl` and `externalFailure` beside
its normal `result`. `externalFailure` contains exact separately encoded shared
normalized Failure bytes. It does not extend the Agent failure schema. The
physical control and failure survive run-store reload and remain distinct from
canonical Agent events, acquisition receipts, and successful tool results.

A version-1 control has exactly `version`, `kind`, `hostOperationId`,
`sourceBindingDigest`, `requestId`, and `reason`, with optional `query` and
`provider`. The server computes `sourceBindingDigest` as SHA-256 of its exact
serialized closed SourceBinding. It binds the control to the physical host
operation, source declaration and current request. Search query must match the
call's decoded query; provider, when present, is a finite provider identifier.
Link and website controls carry neither query nor provider.

| Kind | Allowed reason | Display meaning |
| --- | --- | --- |
| `research-choice` | `choice-required` | An explicit research choice is required. |
| `setup-required` | `configuration-required`, `connection-required`, `verifier-required` | External-source setup is required. |
| `provided-only` | `provided-only` | Use the conversation and supplied sources for this request. |

These are closed enums; an allowed protocol reason does not establish that a
particular producer emits it. Readers reject unknown, duplicate, aliased, null,
or malformed members and disallowed kind/reason combinations. Controls have
no URL, raw-error, proof, receipt, or authorization-grant members.

Only an explicit producer exit before acquisition can supply a not-started
control witness. The retaining transaction rechecks exact source binding and
custody: reserved/not-started acquisition state, no dispatch closure, no retained
acquisition or seal packet, and no acquired digest for that operation. It retains
the control with its normalized `not-started` failure atomically. Choice and
provided-only controls use a permission failure; setup controls use a
configuration failure.

Sourced input, authorization and host errors remain real nonnil failures. A
sourced read returning no receipt becomes an `unknown` failure even if its result
has `IsError`; neither that flag nor a nil producer error establishes receipt
acceptance. Errors after acquisition dispatch, uncertain storage, or missing custody do not
create a not-started control. Existing acquisition packets, reservations and
`DispatchClosed` state remain retained. A replayed refusal returns the retained
failure without entering the producer again. Successful sourced-result replay
still requires independent proof revalidation and restoration of a real receipt.

## Private external-read evidence contract

The E3 implementation defines optional `externalScope` on a private physical
host intent and `externalReadEvidence` beside its retained `result`. These are
private custody members, not additions to public tool-result JSON or approval
records. Their presence alone does not establish source acceptance.

`externalScope` contains `execution`, `role`, `task`, and `generation`. The
canonical adapter captures it from the actual coordinator binding and original
Writer/Store/root for that invocation. Copied Source, Execute and verifier
closures share a live capability that expires when the adapter returns. Checks
refuse missing, stale or foreign scope, generation, Writer, Store or root
bindings. Saved scope fields and imported history cannot reconstruct this
capability or grant current source approval.

Version-1 `externalReadEvidence` contains `version`, `id`, `hostOperationId`,
`durableDispatchId`, `scope`, `sourceBindingDigest`, `ownTool`, `argsCanonical`,
`argsDigest`, `resultCanonical`, `resultDigest`, `outputDigest`,
`projectionVersion`, `projectionLimit`, and `proof`. Its closed proof contains
`kind`, `acquisitionId`, `packet` (`id` and `sha256`), optional `document`,
`configurationDigest`, and `custodyDigest`. Proof kinds are `search`, `link`,
`website`, and `saved-link`; saved links use the exact document reference and
an empty acquisition ID.

The association binds the exact physical operation, durable dispatch and its
fingerprint/scope, complete SourceBinding approval/configuration/declaration
binding, original argument bytes, complete result bytes, and entire retained
domain output when present. Argument and result digests cover the original
bytes separately from Agent canonical evidence; canonical strings do not
replace those bytes or preserve every numeric literal. Projection version and
the existing tool-result limit are also bound. Evidence and the physical
result/output are retained in one transaction under the existing record bound.

Proof locates the original typed final packet by ID and SHA-256 and binds the
complete acquisition custody, including stage/seal and selected authority
references. Revalidation uses the real proof verifiers and deterministic
search, website or link-window projection, including selected document pages
and partial permissions. It re-reads original packets around verification and
checks the originally selected search, redirect, map or seed authority rather
than substituting another source with the same URL or output. Original signed
and unknown source bytes remain in their attachments.

The bounded `desk-proof-v1-` association ID is a locator, not a signature or a
manufactured Gateway/MCP receipt. It is returned through `ToolResult.Receipt`
only after physical association commit and a final currentness check; it stays
outside content, structured content and retained canonical result JSON. The
custom verifier requires the original live capability, exact call ID, Source,
owned tool, original arguments, complete result, one exact retained association
and committed durable result. It revalidates proof/projection, re-reads physical
custody after waits, and repeats current identity and authorization checks.

Source success requires exactly one final original scoped SourceGate-decorated
`tool_result` with the exact accepted receipt, Source, wire name, call ID and
complete result projection. A physical result, receipt locator, claimed or
unknown durable row, or absent/altered final event is insufficient. Successful
Plan custody additionally requires the existing completed nonerror physical
result and successful durable observation.

The separate read-only replay selector requires the current original capability
and exact call/callback, SourceBinding, intent, scope and generation. Matching
new claimed work follows fresh admission. Replay creates no intent, reaches no
dispatch and performs no acquisition or seal. Successful retained-result reuse
must revalidate original proof before restoring the genuine association ID;
exact retained refusals replay as errors. Unknown work without an exact retained
physical result remains refused. Neither reuse nor retained accepted history
creates authority; `IsError` and nil producer errors do not exempt receipt checks.

## Public external-call display contract

Authenticated assistant-run snapshots can include optional `externalCalls`.
Each finite row contains `callId`, `hostOperationId`, `ownTool`, `wireName`, and
optional `control` and `outcome`. `ownTool` is one of `search_sources`, `read_link`,
or `explore_website`; `wireName` is the exact distinct sourced wire name retained
in SourceBinding. An outcome, when present, is `not-started` or `unknown` and must
match the separately retained normalized failure. `not-started` requires its
physical control. `unknown` has no control. A row without outcome is a name
association only, not a success claim.

The server derives rows from exact authenticated source and host-call
relationships. The browser checks unique call/operation identities, exact
operation name and SourceBinding association, and matching private failure and
control records before presentation. The DTO is a display projection, not a
second source registry. No prefix stripping, model-supplied tool name, or
response prose can establish the mapping. Canonical event names and source
records remain unchanged.

Both the saved-chat server projection and the fresh browser projection use the
same call/wire relationship to display the owned tool name. They retain finite
controls on failed work items and show choice, setup, or provided-only notices.
Unknown outcomes stay failed with an instruction to review retained work before
starting another request. Reloaded history does not turn these displays into
live permission, successful acquisition, or an automatic retry.

When the current request needs a research choice, ChatPanel offers Choose
research through the existing controller's explicit `research-choice` creation
route. This records a new user choice; it does not replay the failed operation,
grant source approval, or increase compute allowance. Source review remains a
separate standalone control and preflight path. Observing tool presentation or
source configuration uses `start=false` and cannot start a source companion.

These source, evidence and display contracts do not establish successful
execution qualification. Production enrollment now applies the explicit source
approval gating above; full-registry API, native, Plan and delegation execution
qualification remain separate required gates.
Actual API final accepted scope under Agent 248 and child accounting under
Agent 249 remain unqualified. Direct-host producer/storage fixtures and
handwritten fresh/saved browser envelopes cannot substitute for these gates.
Actual producer proof association, final SourceGate-accepted events and sourced
replay require their own successful integration evidence. This document records no passing check or test. Enrollment does not change the
source declarations, action policy, allowances, component pins or execution
bounds.
