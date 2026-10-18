# Local policy governance delivery boundary

Status: local implementation, 2026-10-18. Library consolidation and the local
Policy service are merged into `integration/v0.13` through Desk #453 and #458.
Desk #459 adds retained source import and shared Policy chat to the private,
single-administrator service and full-page editor. It does not claim team governance, portable Policy conformance,
automatic source synchronization or execution admission.

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
`jpack-folders.json` v1/v2 assignment contract is unchanged. Dialects have no
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
archive, effective-date scheduling or withdrawal endpoint.

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

- Spec owns eventual portable Policy/adoption semantics, reference and canonical
  profiles. Private local draft records are not a standardized Policy export.
- Runtime supplies deterministic checks only for a supported, pinned contract.
  The editor must not advertise interoperable validation until that exists.
- Gateway supplies actual connected source observations. Automatic sync requires
  one real supported provider, lifecycle authority mapping, reconciliation and
  gap/freshness handling; a file reader alone is not this capability.
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
