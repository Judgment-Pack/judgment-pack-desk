# Dialect record storage

Tracking: [Desk #403](https://github.com/Judgment-Pack/judgment-pack-desk/issues/403).
Current collection workflows use the repository boundary described below.
Research acquisition payloads now use the immutable artifact boundary below.
Research and evaluation journal content use storage boundaries as well.
Their production adapters and workflow coordination remain local; there is no
selectable cloud backend yet.

## Portable definitions and deployment settings

A portable dialect describes how retained source material becomes typed facts.
Its requested fields, source semantics and deterministic conversions belong to
the Spec discussion in [#169](https://github.com/Judgment-Pack/judgment-pack-spec/issues/169)
and [#170](https://github.com/Judgment-Pack/judgment-pack-spec/issues/170).
Workflow stages can name logical record collections; the selected-record and
stage relationships belong to [Spec #160](https://github.com/Judgment-Pack/judgment-pack-spec/issues/160).
Physical paths, database connections, bucket credentials and deployment grants
are installation settings. They must not enter published dialect definitions.

The intended Desk experience is **Use workspace default**, with an optional
approved logical destination and collection. Administrators bind those names to
storage profiles. Internal persistence and external publication to a CRM, table
or bucket are separate actions with separate authorization. Do not expose a
selector until the entire workflow supports the chosen backend.

## Implemented boundary

`inputPreparationRepository` owns collection listing, snapshot reads and atomic
revision-checked replacement. The existing preparation names and JSON layout are
retained for compatibility; they are Desk persistence, not the dialect wire format.

The collection HTTP API uses this boundary for creation, recipe updates, imports
and single/bulk record review. Research review, selected-record dependencies,
approval and budget admission, both publication formats, draft promotion and
recovery, evaluation review/confirmation, and incremental subscriptions also use
the same repository. Request parsing and candidate construction happen
outside the storage transaction. The file adapter checks the expected revision
again while holding the existing directory lock, before writing. A slow request
body therefore does not hold the collection lock.

The repository contract is:

- Construct a repository for one authorized workspace. Its methods accept stable
  collection identities, never arbitrary paths or connection strings. HTTP
  authentication and origin checks run before accessing it.
- `List` returns an empty array only for a successfully observed empty or unused
  workspace. Corrupt, inaccessible or disappearing saved documents produce an
  error, not partial rows or a false empty state. Listing creates no directories.
- `Get` returns an independent snapshot and its revision. Missing data is distinct
  from unreadable data. The file adapter retains SHA-256 of the exact saved bytes;
  reading or listing never reformats a document.
- `Read` provides a guarded, consistent view until `Close`; `TryRead` refuses
  contention immediately for background admission. Keep the view through the
  existing approval/admission critical section, and pass its reader to nested
  helpers instead of reopening the repository. Replacing a reviewed collection
  must wait until admission releases it. A database adapter cannot implement this
  with an MVCC snapshot that allows concurrent revocation. Closing twice is safe;
  reads after close are refused. These guards grant no permission to run work.
- `CompareAndSwap` with an empty revision is create-only. A nonempty revision must
  match an existing snapshot. Capacity checking and the replacement are in the
  same transaction; competing edits cannot silently overwrite one another.
- A candidate must pass the existing document validation and size limits before
  replacing saved data. Existing record IDs, source bytes, generated/asserted
  classification, review snapshots and recipe history remain in the document.
- Respect cancellation before work and while waiting for a lock. The existing
  five-second lock bound and `storage-busy` response remain. Do not retry mutations
  inside the repository or HTTP handler. An error after commit may mean the
  completion is uncertain; callers must reload and reconcile before another write.
- A concurrent create of the exact same chat proposal returns the saved snapshot
  after an explicit revision conflict. Different settings under the same proposal
  identity are refused. An arbitrary write failure never takes that replay path.

`fileInputPreparationRepository` retains the owner-only file checks, no-follow
reads, staged replacement, sync, read-back verification and cooperative locks of
the existing store. File-specific paths, encoding and locks are confined to the adapter.
A test-only in-memory adapter exercises both the HTTP API and company/person/job
journey without collection files; it is not an available user destination. An AST
architecture check rejects direct collection-file access in future features.

## Immutable retained research artifacts

`retainedArtifactRepository` is scoped to one authorized workspace. `Put` accepts
exact bytes and returns their SHA-256 and byte count; `Get` checks both before
returning an independent byte slice. Neither accepts paths, URLs or credentials.
Repeated identical content does not rewrite a file, and corruption is refused
rather than silently repaired. The file adapter keeps owner-only/no-follow reads,
staged writes, synchronization and read-back verification, with a 32 MiB per-object
limit. Research retains its tighter per-proof and per-run limits.

New research proof manifests (`storageVersion: 1`, a Desk-private format) refer to
immutable request arguments, provider responses and seals. This fixes JSON
re-encoding that previously discarded original response whitespace. The artifacts
live under `.desk-private/retained-artifacts` in the project and move with it. A
content digest establishes byte identity; Gateway receipt/signature verification
still establishes acquisition integrity, and neither establishes factual truth.

The workflow saves the acquisition before requesting a seal. The only permitted
proof update adds a seal to the unchanged session, source, reviewed Gateway,
arguments and response. A sealed proof cannot be replaced or rolled back. Every
object must be readable and match its digest before publishing its manifest.
Cancellation after a response arrives still allows a bounded persistence step;
it does not authorize another provider request. An uncertain object/manifest write
stops the attempt and never triggers automatic acquisition or seal replay.

Continuation, selected-record reprocessing and reviewed job handoff resolve the
same artifacts. Copying a proof preserves its exact manifest bytes and digest.
Existing inline proofs are read and copied without migration or re-encoding, so
historical job bindings remain valid. Older Desk builds that do not understand the
new manifest version cannot read newly created proofs; keep the new reader and
its referenced objects together when rolling back or migrating. Failed extractions and interrupted seals
count referenced payload bytes, not just the small manifest. Counts are logical
per-run consumption even when an object is shared by several proofs.

Research manifests and run state still belong to the local journal adapter;
this change does not introduce a distributed transaction. A crash before manifest
publication can leave unreferenced objects. They are retained, with no automatic
cleanup or inference that the source request can be retried. A future garbage
collector must trace active and recoverable journals, keep a recovery grace period,
and coordinate with publishers. Cloud migration must move referenced objects and
journals together. The existing chat backup intentionally excludes project
artifacts; it is not a backup of this evidence store.

The file adapter is the only production implementation. A test-only memory adapter
exercises source acquisition and the company/person/job journey without artifact
files. Architecture tests reject direct artifact/proof-file access outside their
storage adapters. Imported attachments and other evidence producers have not yet
been migrated to this interface.

## Research journal boundary

Research journal content now uses `researchJournalProvider` and a run-scoped
`researchJournal` (`Read`, create-only `Create`, and serialized `Update`). Providers
are bound once to a server-authorized workspace. Callers select logical run IDs;
the file adapter receives an already owned directory and verifies the stored ID.
Worker execution, parallel child tasks, retry accounting, HTTP history/Stop,
approvals, budget decisions, publication acknowledgement, continuation and reviewed
job handoff all use this boundary. Direct file helpers exist only in test fixtures;
an architecture test prevents production callers from using them.

`Update` performs one read, at most one callback, validation and a verified commit.
It must not retry the callback on a database conflict or an uncertain acknowledgement.
Callbacks may inspect current local admission state but must not dispatch provider
requests. Reads and successful writes return independent snapshots. Invalid changes
or uncertain writes return errors without a success snapshot. Run identity and prior
operation identity cannot be replaced, and completed operation results cannot be
changed or reset for replay. The existing journal JSON format is unchanged, and a
read never rewrites a historical file.

A provider request starts only after its attempt has been committed successfully.
If read-back fails after an attempt may have been saved, dispatch remains closed;
recovery retains that operation instead of issuing it again. Response/completion
persistence uses a separate bounded context so Stop does not discard a response
already received. Dispatch admission still checks the live operation context and
saved Stop/approval/budget state. Cancellation does not grant additional work.

Three lock roles remain distinct. The file journal adapter uses the shared bounded
short-transaction lock. The worker ownership lease and the research-root admission
lease remain nonblocking and outside that adapter. Stop can update the journal
while a worker waits for a provider. The core execution loop receives a journal and
a proof-size reader; source acquisition is a separate dependency with no journal
transaction around provider IO.

New reviews stage their evidence and journal before the coordinator publishes the
run directory by rename. An uncertain create leaves hidden staging intact and does
not expose a runnable review; a failed directory-publication acknowledgement keeps
all evidence as well. Another create cannot replace either a published or staged
run. Hidden staged runs do not block admission or history for other runs. A visible
directory without a readable journal remains unavailable, not an empty or
restartable run. No automatic orphan deletion or provider replay is introduced.
Recovery/migration tooling must reconcile such staging explicitly, together with
any journal content committed by a non-file backend before directory publication.

This is a content boundary, not a distributed scheduler. Run discovery, worker and
admission leases, proof manifests, and staging remain local. Approval ordering still
relies on the coordinator's admission lease. A cloud adapter needs distributed run
discovery/coordination and durable staged-publication recovery as well as journal
storage. The in-memory adapter exists only in tests and does not enable a cloud
storage setting.

## Evaluation journals and incremental outbox

`evaluationJournalProvider` selects a logical batch in one authorized workspace.
Its `evaluationJournal` exposes independent snapshot reads and revision-checked
`CompareAndSwap`; an empty revision is create-only. Evaluation review, confirmation,
history, worker progress and incremental subscription controls/deliveries all use
this boundary. The file adapter preserves the existing JSON format and exact-byte
revision on read. An architecture check forbids raw journal access in workflows.

The storage contract preserves the reviewed release, recipe/revision, record and
source fingerprints, and typed delivery inputs. Confirmation and accepted job,
trigger, run and occurrence identities cannot be reset. Subscription baseline and
event identity/time cannot be regenerated; completed results and signing status
cannot be replaced. An outbox row drops its payload only after a retained delivery
receipt or a confirmed supersession. Corruption or unreadable subscription history
returns unavailable, never a partial or empty list.

Runner operations are outside journal transactions. Each coordinator lease uses a
checkpoint with the last observed revision; any failed or uncertain write ends that
checkpoint. The next pass must read saved state before reconciling. A bounded
persistence context can retain an acknowledgement after request cancellation; it
does not authorize more Runner requests. No storage callback retries network work.

Recovery uses Runner's existing identities: a frozen release/name/initial-trigger
configuration identifies job creation; batch plus record identifies a selected run;
the retained event ID and exact event body identify an incremental delivery. Desk
verifies the job response's release, name and trigger before retaining it and before
submitting rows. A delivery occurrence remains submitted while its separate run
can complete; Desk recovers the receipt and then the run result without submitting
a new event. These are reconciliation guarantees, not a distributed exactly-once
transaction between Desk and Runner.

Runner's delivery POST can refuse with `429 queue_full` before admitting anything.
That exact refusal keeps the row pending with its retained event ID, timestamp and
input bytes. The outbox persists a separate capacity wait: 2, 4, 8, 16, 32, 64,
128, then at most 256 seconds between attempts, including after restart. It does
not consume the five consecutive-refusal limit, set a failure warning, grant
compute, or replay a provider request. Other 429s, server/transport failures and
errors from other endpoints retain the existing refusal handling. Capacity wait
metadata clears after admission, ordinary errors, pause or supersession. Receipt
recovery, trigger pause checks and current record eligibility remain available
during the wait; pausing never submits the pending envelope.

Review creation writes a hidden staging journal before directory publication. An
uncertain create retains staging, and a repeat cannot replace it. Recovery tools
must reconcile any backend commit and directory publication before exposing it.
The file adapter's bounded `journal.lock` protects only short CAS transactions.
Existing local batch ownership and collection admission leases still serialize
controls and preserve revocation ordering through delivery. Discovery, staging and
those leases remain local. A database adapter also needs a durable distributed
admission/publication protocol; adding CAS alone does not make this a SaaS scheduler.
The in-memory provider is test-only.

## Remaining migration

Record collection access is interchangeable through this interface, but all Desk
state is not yet portable between backends. Draft metadata, research coordination,
proof manifests, evaluation discovery and worker/admission leases still use their
existing local stores. Research journal content (including its approvals) and
evaluation/subscription journal content have adapter boundaries, with only file
adapters enabled in production. Migrate the remaining stores and coordination with
their current operation identities and recovery checks before exposing a SaaS backend.

Research publication now builds a candidate from a saved collection revision and
commits it once with CAS. A conflict retains the new edit and pending research
results; an uncertain write remains unacknowledged. Explicit recovery reconciles
the retained content before acknowledging publication, without repeating model or
source calls or overwriting a newer review. Draft publication likewise reconciles
only an explicit create conflict, and refuses draft edits if it cannot determine
whether publication already happened.

Read guards intentionally preserve existing admission semantics, including the
subscription guard across delivery. The later distributed journal/outbox design
should replace long-lived database locks with a durable admission protocol that
preserves revocation ordering. Do not substitute automatic transaction retries
for that protocol: a callback can already have sent a job to Runner.

Separate storage contracts are needed for:

1. Mutable collection/record metadata, reviews and revision history.
2. Immutable imported bytes, retained source responses and evidence artifacts,
   addressed by verified digests.
3. Operation journals and durable record-change events used by incremental jobs
   ([Desk #392](https://github.com/Judgment-Pack/judgment-pack-desk/issues/392)).

A SaaS deployment will typically bind the first to a database and the second to
object storage. The journal/outbox must coordinate commits and idempotent delivery.
Define staged metadata/blob recovery so a visible record never points to incomplete
evidence, along with orphan cleanup, retention, backup/export/restore and migration.
Server-derived tenant/workspace scope must constrain every access and reference;
file locks cannot provide distributed transaction or tenant isolation guarantees.

Backend migration must preserve identities, exact source bytes/digests, review
bindings and revision checks. Cloud adapters, logical destination settings, tenant
isolation and migration tooling remain follow-up slices under #403.

## Verification

The adapter contract tests cover empty/missing reads, independent snapshots,
creation and update conflicts, cancellation and competing writes. File tests cover
unchanged saved bytes, invalid replacement, atomic collection capacity, failed
listing and cancellation under lock contention. HTTP tests cover operation without
record files, simultaneous chat saves and uncertain completion without replay.
Existing import, record-review, research-publication, evaluation and subscription
tests remain required during migration.

The guarded-read contract is tested against file and in-memory adapters, including
blocked/cancelled writes, release, and immediate refusal of a contended admission.
Research tests cover publication conflicts, uncertain writes, explicit recovery,
manual/Auto/stale approvals, and budget review without collection files. Draft
recovery tests include a failed publication lookup and preserving newer edits.
With installed companions, the company/person/tested-job journey and incremental
Runner outbox run against both record adapters and evaluation journal adapters.
Test servers retain the same in-memory journal instance across restart; this tests
backend independence, not production durability for memory storage.

Artifact tests cover exact raw bytes, concurrent and repeated puts, changed bytes
and sizes, workspace isolation, invalid references, cancellation, failed read-back,
legacy proof compatibility, seal conflicts, unreadable/missing objects, project
rename and acquisition/seal failures without replay. Parallel source acquisition
verifies independent Gateway receipts with both artifact adapters. The real
company/person/tested-job journey exercises both artifact and record adapters.

Journal contract tests cover independent snapshots, create conflicts, validation,
concurrent updates without lost changes or callback replay, cancellation, bounded
lock waits, preserved operation results and uncertain write acknowledgements.
HTTP approval/budget/Stop/history and worker recovery tests remove journal files
and use the in-memory adapter. Parallel acquisition, continuation and the installed
company/person/tested-job journey exercise both journal adapters together with
retained evidence. Neither fixture acquisition nor these tests uses paid AI or
contacts real prospects.

Evaluation journal tests cover stale and competing CAS, invalid transitions,
independent snapshots, workspace scope, preserved fact types and terminal results,
legacy bytes, cancellation, bounded lock contention and uncertain read-back.
Workflow tests refuse a mismatched job binding, stop dispatch after uncertain job
acknowledgement storage, preserve hidden review staging and surface unreadable
history. Installed Runner tests recover lost job/run/delivery acknowledgements,
including delivery persistence failures before and after commit with the run already
completed. Both adapters retain the same job/run/occurrence identities and decision
results. The company/person/tested-job journey runs without evaluation files using
the in-memory adapter.

## Draft storage boundary

`dialectDraftRepository` owns active-draft listing, independent snapshot reads and
revision-checked replacement. Chat retention, recovery after chat deletion,
manual edits, discard and promotion all use it. The default file implementation
retains bounded private-file validation, exact-byte SHA revisions, atomic writes
and read-back verification. A second, test-only in-memory implementation exercises
the same HTTP lifecycle. Neither changes the draft wire format.

An empty revision is create-only. Capacity checks occur in the create transaction;
updates and completion remain possible at capacity. An existing draft's identity,
origin and creation time cannot change. Publication/discard cannot also replace
the reviewed recipe, and completed drafts cannot be edited or resurrected.
Listing excludes completed drafts, creates no storage on an unused workspace,
and reports corrupt/unreadable data as unavailable rather than empty.

Cross-store promotion still uses a local workflow lease, separate from the short
repository transaction. Request bodies are parsed before taking that lease, and
GETs do not acquire it. Promotion records the collection under a deterministic
operation identity before acknowledging completion in the draft. An uncertain
write stops without retry; an explicit later request reconciles the persisted
identity instead of creating a second collection. Edits/discard check for a
completed collection even when the draft receipt is missing. Storage substitution
alone does not provide distributed coordination: a future cloud deployment needs
an equivalent durable admission mechanism across the draft/collection boundary.

Artifact versioning is a separate contract: see
[Spec #180](https://github.com/Judgment-Pack/judgment-pack-spec/issues/180).
The snapshot SHA and internal recipe counter are not semantic release versions.
