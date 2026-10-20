# Policy governance: proposed service and document interfaces

Status: design proposal, 2026-10-17. Proposal revision **0.0.1**. These names,
payloads and examples are not adopted JPS schemas, advertised capabilities or
implemented APIs. They make the [agreed design](policy-governance-design.md)
concrete for specification and implementation review. These examples are not the private local Policy API. See the [delivery record](policy-governance-delivery.md) for actual implementation and limits. The newer [Spec RFC 0023 prototype](https://github.com/Judgment-Pack/judgment-pack-spec/pull/212) explores the portable document contract separately.

The executable authority remains the pack/graph. The proposed Policy companion
retains policy content and provenance; governance services determine who may
approve, publish, adopt and start work. Optional structured requirements improve
reading and coverage review without becoming another decision language.

## Boundaries and ownership

| Interface | Producer / authority | Consumer | Portable part |
| --- | --- | --- | --- |
| Source observation | Gateway observes; Desk interprets configured authority | Desk sync service | Retained source and extraction identity/provenance |
| Governing adoption | Desk authorizes an exact interpretation | Runtime checks supplied material; Runner retains closure | Policy references, scope, coverage and exact consuming artifact |
| Run/action admission | Desk governance authority decides; authenticated executor commits the start | Runner at run start; Gateway or another actual action executor at action start | Auditable request/receipt shape; no portable permission token |
| Review/publication | Desk transactional governance service, or configured external lifecycle authority | UI, chat, sync adapters and release consumers | Exact review assertions and publication references; local ACLs stay local |

Spec owns the portable meaning and conformance examples. Runtime owns offline
structural/reference/canonical checks against an explicitly supported contract,
not identity management, source fetching or current eligibility. Desk initially
hosts the authoritative governance service; this does not require a new repo.
Gateway keeps provider credentials and normalizes observations. Runner does not
become another policy editor or evaluator. Exchange carries reusable definitions
after their entry contract exists, not private review or tenant records.

## Shared conventions

All fields below are proposed. `proposalVersion: "0.0.1"` identifies the examples
and protocol drafts; it must not be presented as an adopted `specVersion`.
Unknown required format/capability is a refusal. Optional extensions need an
explicit extension mechanism before adoption; arbitrary unknown fields are not
silently ignored.

| Type | Proposed representation and meaning |
| --- | --- |
| Stable identity | URI string; independent of title, folder, storage location and release version |
| Artifact version | Existing Runtime `x.y.z` parsing/comparison; compatibility and save/publication policy remain separate |
| Content reference | `{bytesDigest, byteLength, mediaType}`; SHA-256 of exact retained bytes, not of a URL or file name |
| Canonical document reference | `{canonicalDigest}` under the explicitly selected canonical profile; never substituted for a byte digest |
| Artifact reference | `{kind, id, version, content}`; exact supplied pack/graph/policy bytes |
| Mutable revision | Opaque service-generated token used for compare-and-swap (CAS); not a public artifact version |
| External revision | Opaque provider value scoped to its resource; not numerically/lexically ordered unless provider contract says so |
| Time | UTC whole seconds, `YYYY-MM-DDThh:mm:ssZ`; audit time is not provider sequence |
| Operation identity | Caller-generated unpredictable ID, scoped by authority/workspace/operation family; repeated body recovers one result |

The proposed JSON canonical profile reuses the existing Spec/Gateway `canon`
domain: UTF-8, Unicode-code-point-sorted object keys, array order preserved,
duplicate keys and invalid Unicode refused, safe integers only, canonical
escaping and `-0` normalized to `0`. This is **not** a claim of JCS or preparation
canonical-format equivalence. The companion's eventual schema/profile must be
pinned before implementation. Existing pack bytes are retained unchanged under
their own contract; they are not reserialized for execution identity.

Version, content digest, draft revision, provider revision and live lifecycle
revision are different values. An unchanged file can acquire a new lifecycle
state; a newer file need not be a published policy. Live status, ACLs and review
receipts are outside immutable policy content. Digests identify material but do
not authenticate its owner or grant access. Resolve every private reference
through current access control, including search, counts, exports and AI context.

Authenticated tenant/workspace/actor context comes from the service boundary.
Client-supplied roles, `approved: true`, model text and fixture receipts are not
authority. For every command, the host checks current authorization before
returning sensitive history, including a recovered receipt.

### Common command result

Commands return either `{delivery: "committed" | "recovered", receipt}` or a
structured refusal `{code, operationId, retryClass, currentRevision?}`. Suggested
stable codes are `access-denied`, `revision-conflict`, `idempotency-conflict`,
`unsupported-contract`, `reference-unavailable`, `review-incomplete`,
`source-update-unresolved`, `dependency-ineligible`, `authority-unavailable`,
`reconciliation-required` and `integrity-failed`.

`retryClass` is `read-retry`, `reconcile`, `revise-request` or `none`; it never
authorizes execution. Provider detail is bounded and protected. Desk maps these
results through the existing failure registry and recovery UI; it must not expose
raw provider errors or create a second retry loop. Cancellation, pending approval
and a blocked admission remain distinct from a transport failure.

For durable commands, persist operation ID, exact request digest, result and
outbox events atomically. Same key/same request returns the retained result;
same key/different request refuses. Receipt recovery does not repeat writes or
dispatch work. Scope includes the operation family so that an ID cannot recover
an unrelated command. Retention or deletion must preserve a non-replay tombstone
or refuse expired keys, rather than silently treating them as new operations.

## 1. Source observations and revisions

### `observeSource` — Gateway to Desk

This is an observation of a source, not a publication command. The local
subscription authenticates the Gateway connection, authorizes recurring reads
and maps a provider resource to the intended policy/source. Credentials and local
connection IDs stay in that installation binding.

| Field | Required meaning |
| --- | --- |
| `observationId` | Stable delivery identity for deduplication, scoped to the authenticated subscription |
| `source` | `{provider, accountId, resourceId}`; provider/account-scoped stable identity, not a display URL |
| `sourceRevision` | Provider revision, or explicit `null` when unavailable |
| `observedAt` | Gateway observation time |
| `availability` | `present`, `deleted`, `inaccessible` or `unavailable`; only `present` carries content |
| `content` | Original bytes reference when present; verified size/digest |
| `extraction` | Optional `{content, processor, processorVersion, configurationDigest}`; separate from original bytes |
| `lifecycle` | `{kind: "not-exposed"}` or `{kind: "observed", assertionRevision, rawState, evidence}`; provider semantics and retained evidence, not normalized approval inferred from text |
| `position` | Optional provider event/cursor/order metadata; interpreted only under that provider's declared semantics |
| `coverage` | `snapshot-only`, `continuous-feed` or `gap`; optional gap reason/range without invented completeness |

Non-present observations cannot claim an empty content snapshot. Their absence
does not mean policy withdrawal. Metadata evidence may itself be a retained
provider response rather than document text. If a provider cannot supply stable
event ordering, Desk must reconcile current metadata before promoting a
lifecycle change; it cannot sort by time or digest.

Source status is capability-driven per supported binding. If the source exposes
genuine lifecycle evidence, an administrator reviews its mapping to status. If
that capability is absent, the administrator may explicitly configure fallback
status, initially **Draft**. Retain `source-provided` versus `admin-configured`
provenance and the mapping/configuration revision. An administrator's explicit
fallback assertion of Published is an administrator decision; it is not evidence
of source approval or an authenticated external sign-off. A changed document,
fetch failure, permission loss, unavailable resource or withdrawal is not a
missing capability and never activates fallback or fabricates approval. These
status dimensions remain separate from Desk candidate review and local adoption.

Desk stores a separate `SourceState` containing `trackingRevision`, last accepted
observation, last successful reconciliation, coverage/gaps and update candidates.
If external lifecycle is authoritative, it also records the authority-mapping
revision and the exact observation/evidence used for its interpretation. Mapping
an unverified document to "published" is not permitted. Changes to that mapping
require authorized review and re-evaluation, not silent reinterpretation.

### Durable synchronization rules

1. Fetch under recurring-read authorization; retain and verify original bytes,
   extraction and metadata evidence before making their references visible.
2. Commit observation, deduplicated effect, resulting tracking revision and
   cursor advancement together. Durable blobs can precede the transaction;
   unreferenced blobs are safe to collect later. A failed transaction must not
   advance the cursor.
3. Deduplicate the complete observation identity, not the content hash alone.
   Same bytes with a new withdrawal assertion is a new relevant observation.
   Same delivery identity with different bytes is an integrity conflict.
4. Preserve out-of-order observations as history. Promote current state only
   using provider ordering guarantees or a fresh reconciliation. Cursor expiry
   initiates reconciliation and records a gap; polling does not prove every
   intermediate version was retained.
5. Re-extraction creates new extraction identity; it does not manufacture a
   source revision or replace reviewed source bytes.
6. An update creates an impact item. It never overwrites a draft, transfers
   signatures, changes a job pin or reactivates a withdrawn policy.

For approved-but-unpublished Desk candidates, an unresolved newer source blocks
publication. An authorized explicit decision to retain the old source may clear
that block if organization rules permit it; record the observed update, rationale
and decision revision. Published future-effective versions retain their schedule
unless an authorized/configured transition holds or cancels it. Current effective
versions retain their pins; verified restrictions are applied through admission.

Source freshness is a per-binding governance requirement, not an invented global
TTL. Record the last successful reconciliation and configured maximum age.
Unavailable or stale required authority evidence holds new governed work.
Routine content-sync failure does not withdraw a Desk-managed policy: its live
authority is Desk, unless an explicit dependency rule requires external freshness.
Providers without a verifiable lifecycle cannot be advertised as authoritative
external publication/withdrawal sources.

## 2. Policy adoption and governing dependencies

Implementation note (2026-10-19): Desk's private adoption slice uses the
explicitly discovered official Policy draft `policy-v1.0.0-draft.1`,
`formatVersion: "1.0.0"`, pinned to Spec
`0799b37b74e35c4a86bfbf70f7bee80bdb7bbe94`, with `draft` maturity.
Pre-publication candidate reports remain exact historical evidence; fresh
preparation and approval require the official contract without converting old
documents, approvals or authority. Legacy prototype 0.0.2 coverage is unchanged. See
[the implemented API and limits](policy-adoption-review.md). The proposal
examples below remain future interface sketches; this slice does not implement
Runner/Gateway dispatch admission from section 3.

### `GoverningAdoption` — proposed portable document

| Field | Required meaning |
| --- | --- |
| `proposalVersion`, `kind` | Draft format and discriminator `governing-adoption` |
| `id`, `version` | Stable adoption identity and immutable version |
| `consumer` | Exact consuming pack/graph artifact reference |
| `governingPolicies` | Nonempty list of `{policy, scope, coverage}` with exact Policy references |
| `scope` | `whole-policy`, or explicit stable section IDs anchored in that exact Policy content |
| `coverage` | Per-section records: `implemented`, `manual`, `awaiting-interpretation`, `awaiting-implementation`, or `out-of-scope`; implemented records reference exact consumer elements and a retained review basis |
| `informationalSources` | Optional source/content references that do not create a governing dependency |

The scope is declarative provenance, not a new conditional rule language.
Operational applicability belongs in the pack/graph. A limited scope must be
explicitly reviewed; silently dropping a policy or clause is not a shortcut to
eligibility. `manual` coverage needs a defined handoff/responsible process;
`out-of-scope` needs rationale. Publication may allow partial automation, while
run eligibility follows the organization's reviewed adoption requirements.

An `AdoptionRecord` in Desk adds owning/consuming workspace, manifest digest,
approval/review reference, live state and revision. It is not embedded into the
portable manifest. Importing a manifest does not import effective authorization.
The destination establishes read rights, trusted authority and local adoption.
An export cannot replace missing private policy bytes or signatures with a claim
that validation succeeded.

### Build and retain the dependency closure

1. Runtime checks supplied references, exact digests, supported formats, section
   anchors and consumer elements. It reports missing/unsupported material. It
   cannot prove that an organization declared every applicable real-world policy
   or that a natural-language interpretation is semantically correct.
2. Desk reviews interpretation, conflicts and coverage against the exact consumer
   candidate and adoption document. Pack validation/test success does not grant
   governance approval or establish complete coverage.
3. A release envelope binds consumer bytes, adoption digest and the supported
   dependency closure. The consumer does not embed its own release-envelope hash:
   hash consumer → adoption → release envelope, avoiding a circular digest.
4. Runner retains that exact envelope and verifies the trusted registration.
   Admission resolves dependencies from it, not from a caller-selected subset.
   Changing a policy reference, scope or consumer requires a newly reviewed
   adoption/release and an explicit job upgrade.

Governed jobs require an approved adoption record. A caller cannot downgrade a
registered governed job to an empty or "ungoverned" request. Legacy ungoverned
jobs must be explicitly classified by authorized configuration; there is no claim
of withdrawal protection for them. Referencing a policy in Core `sources`, a
folder or a collection is not governing adoption.

For supported composition, retain transitive executable/adoption dependencies
and reject unresolved required references or cycles unsupported by that
contract. Do not imply standard Runner graph-job support: it is outside the
current single-pack contract and needs its own accepted implementation boundary.

## 3. Run and external-action admission

### `inspectEligibility` and `commitStart`

`inspectEligibility` is a read-only explanation for UI, planning and impact
review. Its result cannot start work. `commitStart` is called by an authenticated
executor at the actual dispatch boundary, after durable operation preparation and
before evaluation or external dispatch, not when work is queued.

| `commitStart` field | Binding |
| --- | --- |
| `operationId`, `attemptId` | Durable logical operation and unique execution attempt |
| `operationKind` | `run` or `external-action`; independently enforced |
| `resourceId` | Registered job or approved action resource |
| `releaseEnvelopeDigest` | Exact registered executable release and dependency closure |
| `payloadDigest` | Exact frozen run inputs or action arguments, under their specified byte contract |
| `executorId`, `executionEpoch` | Authenticated executor identity and current fenced epoch, validated by service identity |
| `parentRunId` | Required for an action initiated by a governed run; not an inherited action grant |
| `expectedResourceRevision` | Refuse if configuration/registration changed since preparation |

Tenant/workspace and action permissions are derived from authenticated context.
Release dependency status, workflow rules, withdrawal/expiry and freshness are
resolved authoritatively, not accepted as client booleans. Each applicable
existing action approval remains required in addition to governance eligibility.

The result receipt records `requestDigest`, `operationId`, `attemptId`, executor
and epoch, release/payload bindings, `decision` (`start-committed` or `blocked`),
authority revision, checked dependency states, decision time and safe reason
codes. Unavailable authority must return a hold/refusal, never `start-committed`.
A blocked attempt is terminal; an authorized later retry uses a new attempt ID
and a fresh check. A recovered successful receipt is history, not a new permit.

### Withdrawal ordering and the distributed boundary

The governance service serializes withdrawal and start commitment against its
authoritative state. If withdrawal commits first, a new start is blocked. If
start commitment wins first, that attempt is already admitted and may proceed.
An existing evaluation can finish, but each later external action needs its own
start commitment. A queued occurrence is not an already-started run.

This defines the linearization point honestly: it is the committed start
authorization immediately before dispatch, **not** proof that a CPU instruction
or remote side effect happened at that instant. An asynchronous upstream
withdrawal is enforceable once observed and committed locally; the freshness rule
bounds stale observations, not instantaneous upstream revocation. An action
already dispatched may be irreversible. Do not promise remote cancellation or
global atomicity between Desk, Runner, Gateway and an external system.

No offline admission tokens or reusable "allow" cache are proposed for Phase 1.
They would need a separate bounded-staleness contract and could not promise
immediate revocation. A disconnected authority holds new governed work.

### Execution and recovery state machine

`prepared → start-requested → start-committed → dispatched → completed` is an
executor journal, not a UI-only state. Refusal leads to `blocked`; ambiguous
transport/crash state leads to `needs-reconciliation`.

- The executor durably claims one attempt and records its exact payload before
  asking for admission. Fencing prevents another worker or restored epoch from
  taking over an active attempt and dispatching it twice.
- Only the original successful commitment path can advance that claim to
  dispatch. A `delivery: recovered` receipt is never a dispatch instruction.
  Reads of audit history cannot reconstruct executable permission.
- If the commitment response is lost, or a worker restarts before dispatch is
  known, reconcile with both authority and execution journals. If non-dispatch is
  proven and the old worker is fenced, retire that attempt and request a new
  admission for a new attempt. If the external outcome is uncertain, retain the
  uncertainty and use the provider's safe reconciliation facility; do not replay.
- A later action rechecks current governance and action approval even when its
  parent run was admitted. Evaluation outcomes do not authorize external writes.
- Restoring an old database must not restore old authority. Fence restored
  execution epochs and reconcile governance/identity state before accepting new
  starts. If no trustworthy newer state is available, require authorized recovery
  and hold governed work; a local pre-withdrawal backup is insufficient.

The live service must provide strongly consistent start/withdrawal ordering.
Offline document validation and a signed historical receipt cannot provide it.
Runtime therefore checks retained receipt bindings, if standardized, without
claiming current execution eligibility.

## 4. Review and publication transactions

### Candidate and review basis

An immutable `PolicyCandidate` includes policy identity, proposed artifact version,
management mode, retained content/section references and source provenance.
The proposed companion contains no executable rules or mutable lifecycle status.
Its section IDs are stable within the policy lineage; anchors are bound to exact
content. Optional structured requirements reference those sections.

A `ReviewRequest` contains:

- `reviewId`, candidate artifact reference and candidate digest;
- `draftRevision` and exact review `workflow` reference/revision/digest;
- `sourceBasis`: source IDs, observation IDs, content references and Desk tracking
  revisions, including any reviewed decision to retain an older source;
- required/advisory assignments with stable assignment IDs, authenticated subject
  identities, section/whole-policy scopes and stage/quorum rules;
- expected base publication and review-state revision for concurrency checks.

The workflow language is a separate service design; this proposal does not adopt
a general workflow DSL. The minimal examples use explicit stages, assigned
reviewers and quorum. At least one configured whole-policy final approval is
required; section sign-offs cannot substitute for it.

### Commands

| Operation | Client-supplied body | Authoritative checks and receipt |
| --- | --- | --- |
| `saveDraft` | Operation ID, base draft revision, candidate bytes | Current edit authority, CAS, immutable candidate retention and new draft revision; no publication |
| `requestReview` | Operation ID, candidate, draft/workflow/source-basis revisions | Freeze exact review basis and assignments; return review ID/revision |
| `submitSignoff` | Operation ID, review ID/revision, assignment ID, candidate digest, `approve` or `request-changes`, optional comment | Derive actor, check assignment/scope/workflow and current signing authority; append receipt pinned to complete candidate and scoped anchors |
| `publishPolicy` | Operation ID, exact candidate, expected draft/review/workflow/source-tracking revisions, expected base publication, optional effective time | In one transaction verify authority, required sign-offs, known source updates, unique immutable version, configured publication conditions; store release, transition, receipt and outbox |
| `withdrawPolicy` | Operation ID, expected lifecycle revision, release or whole-policy target, rationale | Current withdrawal authority, atomic lifecycle transition/authority revision; block subsequent dependent starts |

A sign-off receipt records the server-derived actor, scope, candidate/workflow
digests, time and the authority decision used at signing. These are historical
assertions, not a promise of perpetual access. Later role revocation is recorded;
whether an earlier valid sign-off still satisfies an unpublished review follows
the explicit organization rule, not silently invented retroactive semantics.
Current publishing authority is always required.

All publication checks and writes use a single governance transaction (with
previously retained blobs verified). A source update committed before publication
is included in that check or causes a revision conflict. A source event arriving
after publication creates impact review; no cross-system transaction claims it
was already known. Immutable version conflicts refuse overwrite. A proposed
version must be selected before final review; changing it changes the reviewed
candidate. Runtime version arithmetic does not reserve that version or approve it.

Published, effective, superseded, expired and withdrawn transitions remain
separate. Future effectiveness follows explicit schedule/hold rules. A later
source update does not automatically cancel a published schedule. Withdrawal
applies to the specified release or policy lineage and does not erase historical
runs. Supersession does not silently upgrade a job or, by itself, declare an older
pin forbidden; eligibility follows the configured lifecycle rules.

For externally managed policies, `publishPolicy` cannot fabricate external
approval. A supported authenticated lifecycle observation plus its reviewed
authority mapping records source-provided lifecycle. A configured fallback, when
the source lacks that capability, records only the explicit administrator's
assertion with admin-configured provenance; it cannot be exported or presented as
source approval. Neither a fetch failure nor revoked access switches that
authority basis. Importing as Desk-managed draft explicitly
starts a separate review path while retaining provenance. Switching ownership of
publication is an authorized, audited transition, not a flag edit that carries
approvals across authorities.

### UI and AI use the same transaction service

The existing chat, proposal review and manual editor call these same commands.

Local single-admin mode is a valid first host implementation: the same
authenticated administrator can author, explicitly review and publish when its
workflow allows self-review. Record that identity at each transition without
calling it independent review. A workflow requiring another reviewer must refuse
self-approval until that capability exists. Team membership/RBAC extends the
existing identity integration and is tracked separately in
[Desk #452](https://github.com/Judgment-Pack/judgment-pack-desk/issues/452).

AI-generated candidates enter the shared correction/check loop; checks preserve
test expectations and expose ambiguous interpretation for review. Candidate
changes never inherit old signatures. Plan mode may inspect and explain but
cannot dispatch governance mutations. Ask me/Auto controls delegated actions,
not a new signing identity: a model cannot sign as a reviewer. Any configured
automated publication is a separately authorized workflow transition with an
auditable service identity and the same required approvals.

The Library displays draft saved, review requested, approved, published and
effective distinctly. Chat must show only persisted receipts as saved/published;
it cannot treat a proposed version or a passed test as publication. Long operations
use existing progress/recovery and compact action components; no second policy
chat, generic workflow composer or dirty-state implementation is introduced.

## Worked examples and acceptance evidence

[Synthetic payloads](../examples/policy-governance/README.md) cover data access,
procurement and refunds using identical interface shapes. Their retained source,
Policy candidate, consuming pack, workflow and adoption have reproducible digests.
They include source observations, review/sign-off/publication commands and modeled
run/action admissions. All identities and receipts are fictional. Sample
consistency is not Runtime conformance, live authorization or end-to-end proof.

| Failure/race to exercise | Required result | Primary owners |
| --- | --- | --- |
| Same source bytes, newer withdrawal metadata | New lifecycle observation retained; no content-only deduplication | Gateway, Desk |
| Duplicate event or expired/out-of-order cursor | No duplicate transition; gap/reconciliation visible | Gateway, Desk |
| Source update commits during review/publication | Exact prior review remains history; publication refuses stale basis | Desk |
| Section split, moved anchor, changed definition/workflow | No silent approval carry to another candidate | Spec, Runtime, Desk |
| Same command key with changed candidate | Idempotency conflict; original result retained | Desk |
| Lost publication response after durable commit | Authorized recovery returns one release; no second publication | Desk |
| Governing dependency omitted/swapped by caller | Registered closure mismatch refuses before execution | Runtime, Desk, Runner |
| Withdrawal commits before queued start | Start blocked | Desk, Runner |
| Start commits before withdrawal | Existing attempt retained; next start/action rechecks | Desk, Runner, action executor |
| Commit response lost or executor restart | Reconcile; recovered receipt never dispatches | Desk, Runner, action executor |
| Action response uncertain | No automatic external replay | Gateway/action executor, Desk |
| Stale authority, network partition or restored backup | Hold new governed work and fence stale executors | Desk, Runner, Gateway |
| Guest revoked after chat/index populated | Current access enforced on retrieval/history/export | Desk, Gateway |
| Valid/tests-passing pack with partial/conflicting coverage | Implementation review unresolved; expectations unchanged | Runtime, Desk |
| Cross-installation import lacks policy access/authority | Preserve references and report unavailable; no invented adoption | Spec, Runtime, Desk, Runner |

These are required future integration tests, not passed tests. Add domain-neutral
fixtures to Spec, offline invalid/reference cases to Runtime, provider lifecycle
fixtures to Gateway, and transaction/concurrency/recovery cases to Desk/Runner.
Use the existing CI and shared agent framework, not a feature-specific runtime.

## Decisions still needed before implementation

1. Adopt the Policy companion and adoption/reference/canonical contracts in Spec;
   determine review-export trust/signature profile without inventing a PKI here.
2. Choose the first real sync provider and its actual metadata guarantees. An
   existing document reader alone is not a lifecycle/change-feed connector.
3. Define organization defaults for review quorum, earlier sign-off after role
   revocation, source freshness, expiry/grace and optional automatic publication.
4. Choose the authenticated Desk collaboration storage/service deployment and
   executor fencing mechanism, including how restore reconciles authority.
5. Review exact Library/editor/diff layouts using the established design system.

These choices do not block reviewing the four boundaries. They do block claiming
interoperable Policy validation, team sign-off or withdrawal enforcement. Follow
the [agreed delivery order](policy-governance-design.md#agreed-revised-delivery-order)
and land contract fixtures before advertising capabilities.
