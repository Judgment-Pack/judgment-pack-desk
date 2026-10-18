# Local policy governance delivery boundary

Status: local implementation, 2026-10-18. Library consolidation and the local
Policy service are merged into `integration/v0.13` through Desk #453 and #458.
Desk #459 / [PR #460](https://github.com/Judgment-Pack/judgment-pack-desk/pull/460)
merged retained source import and shared Policy chat at `05795d2`.
Desk #461 / [PR #462](https://github.com/Judgment-Pack/judgment-pack-desk/pull/462)
merged the connected-source path described below at `521f378`.
[Gateway PR #297](https://github.com/Judgment-Pack/judgment-pack-gateway/pull/297)
merged the required host controls at `5b2136c`. Desk's component lock now adopts
published Gateway `v0.20.0`, which includes those controls. An installation using
an older Gateway override must still show unsupported state when those controls
are unavailable. These local, single-administrator slices do not
claim team governance, portable Policy conformance or execution admission.

## Graph interpretations and independent graph custody (#468)

The Policy page and history now offer **Design graph** beside the existing pack
action. Policy chat can propose `propose_policy_graph_interpretation`. Both paths
review the same exact saved revision or selected publication, recheck current
access, and open an ordinary authoring chat with an **unsent** graph request.
Opening the chat calls no model and writes no executable project file. Plan
filters the proposal tool and blocks accepting its handoff. Ordinary chat keeps
the shared execution, managed context, Ask me and allowance controls.

Private handoff records may specify `artifactKind: "pack" | "graph"`; omitted
kind remains the historical pack handoff. Unknown kinds and optional null values
are refused. Exact Policy sources remain private reference data, never governing
adoption, complete coverage, execution eligibility, or Graph Job authority.

Graph drafts now have an independent owner-only workspace record behind
`GET`/`PUT /api/draft-graphs`. The document has version 1, `drafts` and `deleted`
arrays; each draft retains graph proposal bytes, creation time, original chat ID,
and immutable exact Policy sources when present. Identity is the pair
`(originChatId, draftId)`, so equal draft IDs in different chats remain distinct.
Writes use the shared bounded storage lock, exact `If-Match`, current-session
admission, and read-back checks. Reads are read-only and use `privateQuery`.
Library/reopen and saved graph helper conversations use independent retained
records. Current saved graph helpers resolve sources against the exact graph
bytes as well as the declared ID/path; older source associations remain history.
They preserve historical chats with incompatible source sets and refuse an
ambiguous or over-capacity basis before changing a conversation. Finalization
preserves private source history; source narrative is not
automatically copied into graph JSON or another project companion.

A reviewed save persists `writePending: true` on its exact private candidate
before dispatching the public graph write. It records a save intent, not proof
that dispatch or commit occurred. A missing acknowledgement prevents dispatch.
Pending candidates and provenance cannot be edited or deleted. If the public
write succeeds but private finalization fails, the UI states the known public
result separately and offers **Finish saving history**. Reload retains an honest
unresolved state. Reconciliation checks exact graph bytes and the current
ID/path/description registration, then writes only private metadata; an already
completed private result is accepted without replay. Current saved Sources may
show an exactly matching pending association with its pending-history notice.
A proven precommit refusal permits an explicit revised copy while preserving the
attempted candidate. Mismatches and uncertain responses never imply nondispatch,
and no recovery action automatically replays the public graph write.

Legacy chat-only drafts remain visible. The next authenticated conversation write
retains both previous and incoming graph drafts before committing chat changes,
including deletion. The latest incoming edit wins for an identity being migrated
for the first time; a deleted chat instead retains its previous snapshot.
Conversation graph snapshots carry plain proposals only: nested Policy sources
and origin metadata are rejected, and their owning chat's top-level source list
is the sole provenance boundary. A retention failure leaves the conversation
intact. A later
conversation-write failure may leave an extra safely retained draft; these are
two writes, not a claimed atomic transaction. Once retained, chat graph snapshots
are related-work history and never overwrite or block independent graph edits.
Old clients that edit only those snapshots must reopen the current graph editor
to edit the independent draft. Explicit draft deletion retains scoped tombstones.

Folder placements, Library actions and helper routing use the same scoped graph
draft identity. `jpack-folders.json` version 3 adds
`graph-draft:<originChatId>:<draftId>` assignment keys; versions 1 and 2 remain
readable. A bare legacy assignment is projected only when its origin is unique.
Ambiguous assignments remain recoverable metadata with a review notice until
each surviving draft has an explicit placement. Saved graphs inherit an
unambiguous draft placement only when no explicit saved graph placement exists.
Projection is read-only; the next explicit folder action persists the result
through the existing authenticated project-file CAS. Folder metadata remains a
project file, outside private workspace backup/restore. Older builds reject v3
folder metadata and cannot safely organize these scoped drafts; downgrading does
not convert or erase either folder metadata or private graph history.

Graph records participate in private backup, restore and project moves. Launch
credentials and revoked sessions cannot read, write or transfer the store.
Restore validates newly introduced Policy sources through the existing exact
Policy ledger boundary. This private Desk schema is not portable interchange:
older Desk builds may reject the new backup record or graph handoff metadata;
downgrading is not a conversion or permission to discard retained history.

Verification covers scoped identity, exact old publications, failed migration,
chat deletion/reopen, stale CAS, immutable provenance, untrusted restore, Plan,
unsent acceptance and shared API/subscription context. Release/review tracking is
[Desk #468](https://github.com/Judgment-Pack/judgment-pack-desk/issues/468).

## Pack interpretations from retained Policy sources (#464)

The Policy editor and a selected history revision/publication offer **Design
pack**. A Policy conversation can propose the same handoff. Both entry points
resolve one exact saved source, show its identity and retained text for review,
then open an ordinary pack-authoring chat with an **unsent** request. Opening the
chat does not call a model, save a pack, approve the interpretation or adopt a
governing Policy. The normal authoring conversation, Plan/Work controls, checks
and Ask me/Auto write permissions continue to own those later actions.

`GET /api/policies/{id}/authoring-source` requires exactly one `revisionId` or
`publicationId` selector and a current signed-in session. The server resolves the
immutable ledger entry and verifies its retained candidate bytes. A publication
must match that exact revision, digest, content reference and artifact version;
the latest candidate is never a fallback. The content digest and byte count
describe the whole serialized candidate, not just its narrative. The browser
rechecks access to the same selection before opening the chat. Dirty or conflicted
Policy editors must be resolved first.

Private `policySources` records retain the selected identity and narrative on the
chat and independent pack draft. They survive chat reload, deletion and draft
reopening, and remain on the finalized private draft. Managed context exposes
them through the shared retained-document tools alongside existing conversation
documents. These sources are reference data, not instructions or permission.
They are shown separately from traced Gateway citations and are not automatically
exported as narrative attachments or project `.research.json` source files.
This is informational authoring provenance, not portable Policy conformance,
clause coverage, governing adoption or execution eligibility.

New or restored source associations must resolve to the existing Policy ledger
and match its exact retained source. A chat-only backup therefore cannot restore
these associations into an installation without that Policy history: restore
refuses before replacing workspace data. Existing authorized history and exports
remain available when the original source is temporarily unavailable. Existing
associations cannot be removed or replaced on the same retained entry to bypass
its access checks; deleting the entire entry remains supported.

Briefs and test suites can retain private draft content after the originating
chat or draft is deleted. Their entire private stores therefore require the
existing signed-in session on reads, writes and containing backup/restore/move
operations, including indirect HTTP paths that use those snapshots. This does
not depend on a caller-supplied Policy tag or subject query. Existing browser
clients already send that session; scripts using only the launch secret must
use the existing session exchange. Ordinary briefs and suites do not require a
Policy ledger. Previously admitted background work retains its own admission
rules.

Policy-chat handoff proposals are bounded retained metadata, not pack envelopes.
Plan excludes the proposal tool. Accepting a proposal preserves its opened chat
identity; opening it again does not overwrite that chat's composer. Deleting the
opened chat requires a new handoff instead of silently creating a replacement.

## Library foundation

[Desk #451](https://github.com/Judgment-Pack/judgment-pack-desk/issues/451)
tracks the unified Library navigation slice, based on integration/v0.13 commit
`0135ecfbdcbe2fe3e0421fe054ae879f22fc0284`.

The local change merges existing Pack, Graph, Dialect and draft inventories into
the retained Library browser, with All/Packs/Graphs/Dialects filters. Browser
keys are kind-qualified and separate from existing storage/folder keys. The
quick switcher discovers Dialects and their independent drafts. The Library rail
destination covers existing Dialect URLs. Existing detail URLs, artifact bytes,
chat scopes, reviews and execution pins retain their current behavior.

The selected type is an optional workspace-scoped layout preference. The
Library foundation kept the `jpack-folders.json` v1/v2 assignment contract;
the later graph custody slice above adds v3 scoped draft keys. Dialects have no
folder assignment and are discoverable in Home/all views; they are excluded
from folder moves and chat-backed draft deletion. Their existing editors own
their actions. Policy navigation/editor integration uses the new private API;
it is distinct from this completed Library foundation.

## Local policy lifecycle

[Desk #454](https://github.com/Judgment-Pack/judgment-pack-desk/issues/454)
tracks this slice. The implementation is `internal/desk/policy*.go`, with route
registration in `server.go`. These are application-private records, not the
proposed Spec companion.

The local installation has one authenticated administrator. Policy authoring,
review and publication may be performed by that same administrator when the
configured workflow allows it. A requirement for an independent reviewer must
be explicit and configurable. Multi-user authorization is a later dependency
for team/guest workflows, not a prerequisite for useful local policy management.

The private Policy service has a transactional storage interface and a file
adapter at `.desk-private/policies/ledger.json`. It uses existing private-data
containment, bounded locks, atomic writes and the retained-artifact repository.
The unreleased private ledger/record schema is version 2. Each revision, review
and publication persists a per-policy `sequence` allocated from the actual event
count inside that same transaction. History sorts by sequence; whole-second
timestamps remain audit/display data. Validation requires unique contiguous
sequence coverage, ordered event arrays and references consistent with the
committed event order. Older development fixtures without sequences are refused;
this does not infer or migrate historical order from timestamps or event kinds.
Policies do not masquerade as Dialect recipes. Candidate bytes and command-result
snapshots are retained before one atomic metadata write publishes the new head,
history and operation receipt together. Failed preparation can leave unreferenced
immutable blobs; it cannot expose an incomplete policy revision. Metadata errors
never trigger an automatic replay.

The service retains:

- A stable local policy identity independent of title, folder and chat.
- A mutable draft head with an opaque CAS revision and exact retained content
  reference. Each saved candidate retains immutable content and revision history.
- An explicit local workflow configuration and digest forming part of the review
  basis. The default allows the same local owner to author, review and publish.
  `independentReviewRequired: true` can be saved, but local approval/publication
  refuses it with `unsupported-workflow`; another authorized reviewer is required.
- Review decisions pinned to the exact candidate and workflow revision, with
  the actor derived from the authenticated server session. An edit retains old
  decisions as history and requires review of the new candidate.
- A separate immutable publication and receipt binding exact reviewed bytes,
  policy identity and selected version. Duplicate versions refuse overwrite;
  subsequent publications must be greater than the latest published version.
  Draft saved, reviewed and published remain distinct states.

Draft saves check the original service revision. Review/publication check the
exact retained candidate, candidate revision and workflow. The candidate digest
covers title, narrative, artifact version and optional original-source reference. Changed content or workflow clears
the current review while retaining all previous decisions/publications as history.
An identical no-op save preserves its review. Returning to old bytes later does
not transfer an old approval to the new candidate revision.

Approve and publish use the existing Runtime `artifact version` command. Drafts
may retain unfinished version text; request-changes remains available for them.
Runtime owns grammar and arithmetic, including its 256-byte version limit. A
publication compares against the latest publication and requires a greater
version. These checks concern version arithmetic, not Policy conformance or
semantic correctness. Runtime runs before the private write lock; the metadata
transaction then rechecks the exact revision/basis. A concurrent edit refuses the
stale review/publication. Existing file locks provide local coordination only.

Every read and mutation requires a live Desk sign-in session and the existing
Origin guard. A launch secret alone is insufficient. Actors come from the real
local or verified OIDC session; client-supplied actors/roles are refused. The same
administrator's authorship, review and publication are recorded honestly.

## Private HTTP contract and recovery

`internal/desk/policy.go` defines the JSON request/response types.

| Route | Operation |
| --- | --- |
| `GET /api/policies` | Current summaries, separate review/publication state |
| `POST /api/policies` | Create a policy and retained initial candidate |
| `GET /api/policies/{id}` | Current candidate and retained history metadata |
| `PUT /api/policies/{id}` | CAS save of title, narrative, version and optional workflow |
| `GET /api/policies/{id}/source` | Download exact retained original bytes with current authorization |
| `GET /api/policies/{id}/revisions/{revisionId}` | Read exact retained historical narrative |
| `POST /api/policies/{id}/review` | Approve or request changes on an exact current basis |
| `POST /api/policies/{id}/publish` | Publish the exact currently approved candidate |
| `GET /api/policy-operations/{operationId}` | Read a completed operation receipt, without replay |

Mutation bodies include a caller-generated operation ID. A request digest binds
operation kind, policy identity, authenticated actor and complete typed body,
including the CAS base. Reusing an ID with a changed body, route/action or policy
refuses `idempotency-conflict`. Exact completed requests recover the original
snapshot before fresh validation or stale-argument checks, after current access
checks. Clients refetch current policy state before a subsequent edit/action.
Receipts are retained rather than evicted; reaching capacity refuses new work.

Mutations return `{delivery, receipt, policy}`, with `delivery` distinguishing
`committed` from historical `recovered`. Failures use the shared coded JSON and
refusal-header boundary. `outcome: not-committed` denotes a proven refusal before
this request's metadata commit, including failed candidate/result retention.
`outcome: unknown` denotes an uncertain commit/read-back or unavailable completed
receipt. Unmarked transport failures remain uncertain. Read-only recovery returning
404 means no completed receipt was observed, not proof that the original request
cannot still finish. It never authorizes a new operation or repeats the old one.

Limits are 500 UTF-8 bytes for titles, 128 KiB for narrative, 256 bytes for version
text, 8 KiB for review comments, 256 policies, 512 entries per per-policy history
kind, 4,096 retained operation receipts and a 16 MiB metadata ledger. No API removes
immutable history or receipt identities. This slice has no policy deletion,
archive endpoint. Local effective-date scheduling and release withdrawal are
described below; they do not enforce execution admission.

Policy metadata and referenced blobs belong to the project. A project move/backup
must retain `.desk-private/policies` and `.desk-private/retained-artifacts` together.
The existing chat-only backup deliberately excludes project artifacts and is not
a Policy backup/export. There is no standardized portable Policy export here.

Editor integration uses existing fields, contextual pane and `UnsavedChanges`.
Lists use `privateQuery`; background refresh must not replace an editor's frozen
revision. Save conflict recovery preserves the local buffer and shows the saved
revision for explicit resolution. Chat deletion must never delete policy work.
AI proposals inherit `runAssistantSession`, Plan admission, shared approvals,
correction and recovery. A model cannot manufacture a reviewer identity or treat
a successful document check as publication.

## Retained originals and Policy chat

[Desk #459](https://github.com/Judgment-Pack/judgment-pack-desk/issues/459)
adds an optional, create-only `import` containing a simple filename, media type
and base64 bytes. The supported starting material is UTF-8 `.txt` or `.md`, at
most 128 KiB. Import validates the original bytes and retains them independently
of the editable narrative. BOM, line endings and non-ASCII text remain intact.
Selecting a file fills the editor but does not save a policy. Ordinary and AI
draft saves inherit the retained original; neither can replace its identity.
Existing policies without an original retain their previous candidate identity.
Original reads/downloads require current sign-in and use private, no-store
responses. This is manual local import into a Desk-managed draft, not connected
source synchronization or evidence of external approval/publication.

The policy pane uses the existing chat, composer, history and main chat route.
Its saved messages and proposals are linked by policy identity; deleting chat
does not delete the policy or original. Current session authorization is checked
for linked history/backup and before model, child-task and retained-context
access. An unavailable policy file does not make unrelated chat history fail.

Proposal tools prepare draft title, narrative and version changes against the
admitted turn's saved revision. They use shared artifact checks for shape and
freshness; those checks do not verify policy meaning or portable conformance.
Ask me requires explicit review of the diff. Auto grants one exact proposal and
revision through the shared action registry, with a bounded lifetime and a
server-side session, chat, phase and candidate check. Plan cannot propose or
save changes. Unsaved editor changes, stale revisions and invalid sessions block
writes. Pending operations are journaled before dispatch; recovery reads the
immutable receipt without replaying an uncertain write. Policy approval and
publication remain separate manual actions. Pack/graph creation currently uses
its existing artifact conversation rather than adopting a new artifact into the
Policy conversation.

Backend acceptance covers create/save/reload and chat deletion; stale editor
refusal; title/narrative/version/workflow review invalidation; exact-review
publication; same-admin lifecycle and independent-workflow refusal; version reuse;
lost-write acknowledgement recovery; operation body/action/resource conflicts;
unauthenticated, revoked-session and actor-spoof refusal; concurrent publication;
an edit during Runtime validation; precommit retention failure; uncertain ledger
read-back; retained content corruption and ledger symlink refusal; tied-time
event sequence persistence/recovery and rejection of inconsistent event order.
Focused history rendering checks tied timestamps and a backwards wall clock.
Real Runtime
acceptance covers lower/equal/greater versions, unfinished versions and a supported
256-byte version. That test is included in the existing Jobs companion CI
selection. Browser/editor verification exercised the built SPA and real Runtime v0.34.0 on an isolated project: Library → New policy → save → reload → approve → publish → retained history → Library refresh. Approval returned no publication; publication was a separate command. The narrow 420px view had no horizontal overflow. Temporary listeners were stopped after verification. Forced refresh can discard unsaved editor buffers; ordinary navigation and unload use the shared dirty-state guard.

## Try the local workflow

1. Open **Library → Create → New policy**. Enter text or choose **Import file**, then review the title, narrative and artifact version and **Save draft**. Incomplete drafts can be saved before approval.
2. Reload or reopen the policy from Library. Its saved text and history are independent of chat sessions.
3. Edit directly or ask the linked chat for a draft change. Review and save its proposal; this does not approve the policy. Imported originals remain available through **Read original** and **Download original**.
4. Review the saved text. Optionally add a review comment, then **Approve candidate** or **Request changes**. Saving a changed candidate requires a new review.
5. Choose **Publish version** and confirm the reviewed candidate. Publication retains immutable content; later publications require a greater version, checked by Runtime.
6. Inspect **History** to see retained drafts, actor-attributed decisions and publications. Existing publications remain available after later edits.

These controls manage local policy content. They do not create a pack, change a job or authorize execution. The remaining accepted lifecycle is tracked in [Desk #455](https://github.com/Judgment-Pack/judgment-pack-desk/issues/455).

## Inter-repository dependencies and limits

### Local publication lifecycle (#469)

[Desk #469](https://github.com/Judgment-Pack/judgment-pack-desk/issues/469) adds
`GET`/`POST /api/policies/{id}/lifecycle` for the existing authenticated local
administrator. The **Lifecycle** tab explicitly activates a selected immutable
publication now or at a future UTC whole-second time, optionally with a later
expiry. A release-scoped withdrawal requires a rationale and is terminal in this
slice. Optional supersession names one exact older publication of the same policy;
it does not withdraw that release, change eligibility or upgrade a job.

Publication remains separate from effectiveness. Old and new publications with no
activation decision remain **Published**, with **No activation recorded**. No
historical activation event is inferred from publication time. An explicit
activation records the current administrator, server decision time, immutable
publication/candidate identity, effective time, optional expiry and supersession.
Future activation stays Published until its effective time. Effective, expired,
superseded and withdrawn views are observations calculated at the server's
reported `observedAt`, not invented background transition events. Withdrawing a
successor before its scheduled activation cancels its prospective supersession;
withdrawal after activation does not reactivate the older release.

The optional `lifecycles` map extends the unreleased private v2 ledger with a
separately versioned and ordered stream per policy. It leaves existing private
candidate bytes, local IDs, revision/review/publication sequences, publication
content and old operation-result blobs unchanged. Lifecycle CAS is separate from
the draft's revision, so a lifecycle refresh cannot silently rebase an editor.
Every action checks both the current saved policy revision and lifecycle revision
and commits the event and retained body-bound receipt in the same repository
transaction. Lifecycle commands have an explicit `outcomeKind: "lifecycle"`
result with policy ID/revision, lifecycle snapshot and receipt, preserving old
generic Policy outcomes unchanged. The receipt binds the targeted publication's
candidate digest, even when another draft is current. Activation verifies that
target's exact retained bytes. Withdrawal needs the validated release ledger and
current authorization; unavailable or corrupt target/draft blobs do not block
recording withdrawal and no candidate content is fabricated in its result.
A recovered receipt returns its original observation; clients then read current
lifecycle state. A separate draft refresh may fail without trapping a known
lifecycle commit in recovery. Missing recovery is unresolved, not permission to
replay. Failed receipt retention exposes no lifecycle event. The existing Policy
page still needs readable draft content when first opened; the lifecycle API can
record withdrawal independently of that page when content is unavailable.

Both API routes require the current session and Origin guard. Actors and
`admin-configured` provenance come from the service. No model tool or automatic
publication path is added. UI controls share dirty/busy guards, exact confirmation
bases, pending-operation protection and read-only recovery. Source and lifecycle
pending journals share the frozen active Desk and current session boundary, using
a session digest rather than a credential in the key. A different Desk or sign-in
cannot inherit the pending operation of a copied policy. Unscoped development
journals are not automatically imported; authoritative operation receipts remain
retained. The stream is bounded
to 512 events per policy, sharing existing receipt/ledger limits; capacity refuses
new decisions without erasing historical identities. Older strict Desk builds may
refuse the additive ledger field. Project moves must retain the complete private
ledger and referenced blobs together; chat-only backups are not Policy backups.

These records establish local lifecycle history only. They do not supply trusted
release registration, dependency impact completeness, current Runner/action
admission or protection against restoring a pre-withdrawal authority backup. Those
remain prerequisites to an enforcement claim. External source lifecycle and
admin-reviewed status mapping are also separate: missing provider capability may
use an explicitly configured fallback, initially Draft; fetch/permission failures
never activate it. Source-provided status and administrator-configured decisions
must retain distinct provenance and cannot manufacture verified sign-off.

### Remaining dependencies

- Spec owns eventual portable Policy/adoption semantics, reference and canonical
  profiles. Private local draft records are not a standardized Policy export.
- Runtime supplies deterministic checks only for a supported, pinned contract.
  The editor must not advertise interoperable validation until that exists.
- Gateway supplies actual connected source observations. The first Desk path
  under #461 is Google Drive snapshot polling, using the separate recurring host
  controls merged through Gateway #295 / PR #297. It requires a released Gateway
  build advertising those controls, included in the pinned `v0.20.0` release.
  Older Gateway overrides without those capabilities must show unsupported state.
  Drive does not supply policy lifecycle authority. External approval/publication
  mapping remains a separate dependency.
- Runner and actual action executors need retained release/adoption closure and
  authoritative start admission before withdrawal protection can be claimed.
  Policy publication does not upgrade or authorize existing jobs.
- Desk's existing OIDC/password/local session consumer is single-owner
  ([ADR 0007](../adr/0007-authentication-and-identity-boundary.md)). Team and guest
  access require current workspace/resource permissions on every read, search,
  retained chat retrieval, export and mutation, plus appropriate storage and
  Gateway delegation. The local service must not accept client-supplied roles.

Packs/graphs remain executable authority; structured requirements are optional.
No policy-specific evaluator, AI loop, generic workflow builder, remote service
deployment or current-run withdrawal enforcement is part of this Library slice.

## Connected policy sources (#461)

This slice connects a Desk-managed policy to one explicitly selected Google Drive
file. It is a recurring read grant, separate from the one-use file picker grant,
AI permissions and the browser session. Consent explains that checks continue
after ordinary sign-out while Desk runs and resume on restart. The administrator
can pause checks or revoke access. Changing the sign-in authority or disconnecting
Drive ends the old consent; restoring identical sign-in settings does not revive
it. Browser tokens are never retained as background credentials.

The Source page uses the shared connection picker, page controls, table, private
query refresh and loading/error components. It shows check frequency, last attempt,
last success, freshness gaps and retained snapshots. Frequencies are 5, 15 or 60
minutes. This is snapshot polling, not a complete provider event stream: changes
between successful checks may be missed. Missing or inaccessible files and failed
extraction never imply that a policy was withdrawn.

Each admitted observation verifies the original byte identity and the canonical
attachment record using the existing Gateway canonicalization boundary. Original
content and extraction records are retained separately. A semantic projection
ignores acquisition timestamps and duration when deciding whether another source
update is needed. A provider revision, content, processing configuration or other
meaningful metadata change creates a new update. Unchanged checks update freshness
without consuming another source-history entry or retaining duplicate artifacts.
These private host observations are not signed Gateway receipts.

The first snapshot and later meaningful changes require an explicit source-review
decision. **Keep current candidate** binds the decision to the latest source
observation, saved candidate revision/digest and workflow digest. It neither
approves nor publishes the policy. Users may instead edit and save the candidate,
then review the saved text against the source. No check automatically replaces the
editable narrative, original imported file, approvals, publications or job pins.
An open review retains its original comparison and refuses confirmation after a
new source or candidate arrives. Connecting a replacement source requires its own
first snapshot and review. If that replacement is canceled before producing any
snapshot, a prior resolution remains usable only for the exact retained source
and candidate it already covered; unresolved retained updates remain unresolved.

Source metadata and policy metadata share the same repository transaction.
Publication checks the latest unresolved source update inside its commit, closing
the race between a background observation and publication. Source refresh has a
separate CAS revision and does not replace or rebase a dirty editor. Every source
action also checks the exact saved policy basis. Changing a candidate, workflow or
source invalidates the corresponding source-review resolution.

The private source state includes a durable pending bind, Gateway operation ID,
binding generation, scheduling state, observation history and reviewed resolutions.
Gateway binding requests are body-bound and recoverable after a lost response;
revoked identities cannot be recreated by retrying an old operation. Pause,
revocation and authorization changes fence late results. Cleanup reconciles
uncertain grants separately from authority to read the source. No model calls are
involved in checking or retaining a snapshot.
Drive disconnect durably invalidates local source authority before provider
dispatch. A lost disconnect reply or failed per-project status update therefore
cannot permit a previously admitted response to be retained afterward.

The optional `sources` member extends the unreleased private v2 Policy ledger.
Older ledgers without it remain readable. Older Desk builds with strict decoding
may refuse a ledger containing it; this is not a portable interchange upgrade.
Project backups must retain policy metadata and referenced artifacts together.
Background authorization also depends on the installation's private sign-in
authority epoch, so moving project data alone does not transfer recurring consent.

The new private routes are `GET` and `POST /api/policies/{id}/source-sync`, plus
policy-scoped reads/downloads of retained observations. Source mutations use the
existing operation receipt and read-only recovery boundary. Gateway exposes four
Drive host controls: `source-bind`, `source-bindings`, `source-revoke`, and
`source-observe`. They are not available as model-facing tools. The initial Gateway
bounds are 4 MiB original files, 16 MiB observation payloads, 32 active bindings
and 48 lifetime binding-operation records. Capacity refuses new work rather than
evicting recovery identities. Default host extraction has no automatic configured
OCR opt-in; partial extraction remains visible for review.

The implementation and its synthetic-provider acceptance evidence must be reviewed
before release. No production component lock is advanced to an unpublished build.
External lifecycle mapping, source event feeds, governing adoption and withdrawal
admission remain tracked by #455 and the corresponding Spec/Runtime/Runner issues.

Verification includes source admission and restart, unchanged-poll artifact counts,
processor/configuration changes, independent acquisition/processing timestamps,
backwards clocks, retained capacity, canceled replacement sources, lost bind
responses, publication races, and disconnect despite a lost response or failed
project annotation. UI tests cover unavailable capabilities, unchanged dirty
editors, durable operation recovery and a source update during an open review.
The dedicated `policy-source-contract.yml` workflow builds a pinned synthetic
Gateway producer and real canonicalizer, generates an observation, and checks
those unchanged bytes with Desk. Its evidence records actual source revisions,
build identities and fixture hashes separately from official releases.

### Reviewed source ownership and status (#473)

[Desk #473](https://github.com/Judgment-Pack/judgment-pack-desk/issues/473)
adds explicit audited Desk/external ownership and separate reviewed source-status
configuration/application; see [the private API contract](policy-source-status.md).
Current Drive exposes no lifecycle, so only an administrator-configured fallback
is supported, initially Draft. Explicit Published fallback retains an external
version with its exact source/configuration basis; it creates no local approval
or publication. Draft or Published source material can instead be explicitly
imported into a fresh, unreviewed Desk draft without carrying earlier approvals.
Failures never activate fallback or reinterpret reviewed history. These records
claim no provider approval, governing adoption or execution permission.
Workspace ZIP backup/restore still excludes Policy data, tracked by
[Desk #476](https://github.com/Judgment-Pack/judgment-pack-desk/issues/476);
full project backups must retain both `.desk-private/policies` and
`.desk-private/retained-artifacts` together.
