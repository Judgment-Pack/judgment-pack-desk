# Policy governance: design decisions

Status: agreed product design, 2026-10-17. This records the user's decisions
from the design conversation. It does not adopt a specification or implement a
policy, collaboration, approval, or enforcement service.

Background: [architecture assessment](policy-governance-review.md). This decision
record narrows the proposed portable companion and supersedes the conversational
recommendation to require OSCAL as the internal control model.

Implementation progress is tracked separately in the [delivery record](policy-governance-delivery.md). The portable document proposal is [Spec RFC 0023 / PR #212](https://github.com/Judgment-Pack/judgment-pack-spec/pull/212); its non-normative prototype is the current portable-format experiment. The service-interface examples here remain design examples, not a competing standard.

## Agreed product scope

Policies are collaborative artifacts within team and organization workspaces.
The product must support authoring, review, sign-off, publication and ongoing
lifecycle management, rather than only importing sources for pack generation.

The local installation is also a supported single-person mode. Its authenticated
administrator may author, review and publish the same policy when the configured
workflow permits self-review. The audit records that same actor honestly; it
does not claim independent approval. Local policy management does not wait for
team membership/RBAC. Team deployments extend the same lifecycle with real
membership, invitations, scoped access and configurable separation of duties.
The existing OIDC/local identity and session integration is reused; follow-up
[Desk #452](https://github.com/Judgment-Pack/judgment-pack-desk/issues/452) tracks
user management and RBAC, with detailed design deferred by the user.

Each policy explicitly selects its management mode:

- **Desk-managed:** create a policy or import starting material as a draft;
  collaborate, request review, obtain sign-offs and publish through Desk.
- **Externally managed:** retain an externally published policy version and its
  provenance; the external system owns its approval/publication. Desk develops
  and governs its executable interpretations.
- **Import as draft:** external material enters the Desk-managed lifecycle with
  a retained link to its source. Importing a draft does not count as approval.

Approval workflows are configurable, with organization defaults and policy-level
configuration within organization rules. Support sequential and parallel stages,
advisory review and required signatories. Invitation does not automatically make
a participant a required signatory.

External guests may be invited to explicitly shared policies. Permissions to
read, comment, propose changes and sign off are separately assigned. A policy
invitation does not grant general workspace or organization membership.

## Agreed review scopes and revision-bound sign-off

Support required sign-offs on either the whole policy or explicitly assigned
sections. A designated approver remains responsible for the complete published
revision; section approvals alone do not approve the policy as a whole.

Every sign-off identifies the exact revision reviewed and its scope. Subsequent
edits preserve previous comments and sign-offs as history, but those approvals
must not silently authorize new content. Reviewers see the changes from the
revision they reviewed, including changes outside an assigned section that may
affect its interpretation.

Publication requires the configured approvals for the current revision,
including approval of the complete revision. Section review does not bypass the
existing distinction between approval, publication and becoming effective.

## Agreed collaboration phases

Phase 1 supports shared persistent drafts, comments, suggested changes and
explicit conflict resolution. Concurrent saves must not silently overwrite
another contributor's changes, including changes proposed through chat.
Revision-bound review and publication apply throughout.

Simultaneous live co-editing is deferred to Phase 2, to be undertaken when a
concrete user requirement justifies it. It is not a dependency for Phase 1 policy
authoring, collaboration, review or publication.

## Agreed automatic source synchronization

Automatic synchronization of connected external policy sources is required in
Phase 1. Manual import and explicit refresh alone are insufficient. Connector
selection and synchronization latency remain implementation/design decisions.

Build on existing Gateway, Dialect and Exchange capabilities. New provider
connectors can be added as actual requirements arise; SharePoint and general Git
policy connectors are not mandatory first-release commitments. Automatic sync
itself remains required and must work with a supported source, rather than being
only an unimplemented interface.

## Agreed handling of synchronized changes

Keep source revision, source lifecycle, Desk candidate/release lifecycle and
implementation adoption separate. A newly detected source revision creates a
retained snapshot and an update/impact record; it does not overwrite a local
draft, transfer sign-offs, replace a published release, or upgrade a job.
Show source freshness separately from lifecycle, with the last successful sync
time and any unresolved source update.

Treatment of an existing Desk revision:

| Existing state | Behavior when a new source revision arrives |
| --- | --- |
| Draft or changes requested | Preserve edits and comments. Compare the old source, new source and current draft; offer a reviewed update with explicit conflict resolution. |
| In review | Keep review attached to its exact revision and notify reviewers. Incorporating source changes creates a new candidate requiring approvals for that revision. |
| Approved, not published | Preserve the approval as historical evidence for that exact revision. Require resolution of the source update before publication; the designated approver may explicitly retain the older source when organization rules permit. |
| Published, future effective date | Keep the published bytes and scheduled date. Notify the owner to resolve any impact before activation; only an authorized lifecycle action or configured rule may cancel or hold activation. |
| Published and effective | Keep the release and its pinned implementations. Open an impact review and an upgrade candidate; enforce separately any verified withdrawal, expiry or other configured lifecycle restriction. |
| Superseded, expired, withdrawn or archived | Preserve history and restrictions. New source content does not reactivate the old revision; any successor follows the applicable lifecycle. |

Unresolved source updates prevent publication of an older candidate until
the designated approver resolves the update under organization rules. Continuing
with an older source must record the version and rationale; it cannot override
an effective withdrawal or other enforced restriction. Review can continue on
the retained candidate. Publication checks the latest known synchronized state
and candidate revision atomically, with configured freshness requirements.

For externally managed policies, record source approval/publication/effective
status only through a configured, trusted authority mapping. A changed file or
new Git commit alone is not approval or publication. Desk does not control an
external policy's effective date; it governs adoption and the resulting local
implementation restrictions. For imports adopted as Desk-managed drafts, source
changes are proposals to update the draft, not authority to publish it.

Impact review identifies affected clauses, structured requirements when present,
packs, graphs, tests and jobs, including pending reviews. AI may propose updates
through the existing framework, but synchronization does not grant permission to
save, publish or deploy changes. Existing jobs retain exact versions until an
explicit upgrade; applicable lifecycle restrictions can block runs without
silently upgrading them.

Synchronization must handle repeat and out-of-order notifications, retain source
version identifiers and content digests, and capture lifecycle changes even when
content bytes are unchanged. Connection failures, permission loss and source
deletion must not be silently treated as policy withdrawal; show the distinct
condition and apply the organization's configured availability/freshness rules.

## Agreed authoring and execution flow

```text
Raw or authored document
  -> optional structured requirements
  -> pack / graph
  -> testing and review
  -> published release
  -> job execution

Operational records -> Dialect -> prepared facts and evidence -> evaluation
```

Structured requirements aid understanding, editing and review. They are optional,
not a mandatory intermediate representation or a second execution engine.
Document editing and structured authoring are interfaces to the same conversion
and review pipeline, not competing execution-authority modes.

Packs and graphs own executable decision logic. The policy source and governance
records retain organizational intent and provenance. A proposed implementation
change that disagrees with an approved requirement requires explicit resolution;
it must not silently alter the requirement or expected test outcomes.

Skipping structured requirements does not skip source-to-rule traceability,
interpretation review, expected-behavior tests, or exact release identification.

## Agreed portable contract direction

Design a small, domain-neutral **JPS Policy companion**. Its proposed scope is
policy identity, source revisions, governance records and source-to-implementation
links. Structured requirements may be carried optionally. It must not introduce
another executable rule language alongside packs.

OSCAL is an optional control-catalog interoperability adapter, not a mandatory
internal representation. Other standards can be mapped when justified by a use
case. Mapping support and loss must be explicit; do not imply universal or
lossless conversion. Preserve original external artifacts and identities.

The candidate model should be exercised against data access, procurement approval
and refund eligibility before its schema is frozen. Format details, conformance
claims and adoption remain future specification work.

## Agreed independent publication

A policy may be approved and published before every requirement is automated.
Policy publication and executable implementation publication have separate
states and histories.

Completing all required sign-offs leaves the candidate approved but unpublished
by default. An authorized publisher explicitly publishes that approved revision.
An organization may instead configure automatic publication after the required
approvals are satisfied. Either publication path may specify a future effective
date; approval, publication and becoming effective remain separate events.

The policy view must distinguish clauses that are:

- implemented by an approved pack or graph;
- handled through a manual process;
- awaiting implementation or clarification; or
- explicitly outside the implementation's scope.

Policy publication alone does not imply complete automation or authorize a job
upgrade. An implementation's publication does not establish complete policy
coverage. Preserve exact historical source and artifact versions.

## Agreed withdrawal and expiry behavior

Organizations can configure enforcement. The default for an effective policy
withdrawal is to block new job runs governed by the withdrawn policy release.
This admission check must use the job's retained policy dependencies rather than
silently switching the job to another version.

Expiry follows the organization's configured expiry and grace-period rules.
Publishing a new version creates an upgrade notification; existing jobs retain
their approved versions until an explicit upgrade. Historical results remain
available after withdrawal or expiry.

An evaluation already started when withdrawal takes effect may finish and retain
its result. New external actions based on the withdrawn policy are blocked,
including actions proposed by that completed evaluation. This does not imply
that an action already dispatched can be cancelled or reversed.

## Agreed shared-policy model

Shared policies are the default within an organization. One owning workspace
maintains a stable policy identity and immutable published releases. Frameworks
and teams reference specific releases rather than copying or silently following
the latest content.

Each consumer explicitly adopts a release for a particular scope and reviewed
interpretation. Adoption records the policy version, applicable scope and links
to executable implementations. Capture this against the consuming pack/graph
release through its review flow, without requiring a separate framework release
or approval screen for every reference.
Approval of policy content and approval of its use in a context remain distinct.
Different contexts may require different packs; a policy reference alone does
not establish applicability or implementation coverage.

Across organizations, external policies may be referenced, but each organization
retains its own adoption and publication authority. Guest review access does not
merge organizational authority.

Shared-policy lifecycle and access requirements:

- Withdrawal has an explicit scope: one release or the entire policy. Show the
  dependent frameworks and jobs before the authorized withdrawal takes effect.
  Enforce effective withdrawal through actual dependencies, not notifications.
- Policy references do not grant access to restricted content. Access checks
  also cover excerpts, comments, history, exports and AI context/explanations.
  Reviews must identify source material unavailable to the reviewer.
- Ownership transfer preserves policy identity and existing references. Owners
  leaving must not orphan a policy or erase dependent historical releases.
- Independent requirements or governance use an explicit fork or a separately
  authored policy. A fork has a new identity, retained lineage and its own
  approvals; it does not inherit approval of its source text.

Before implementation is accepted, verify that updating one consumer leaves other
pinned consumers unchanged, withdrawal blocks exactly the affected new runs and
actions, and restricted content stays inaccessible through shared views and AI.

## Agreed multi-policy authoring and optional collections

Pack-authoring tools accept a list of policy references from the beginning. Users
may select policies directly or supply a saved, named collection. Resolve and
retain the exact source versions for each build; later collection edits do not
change existing packs, graphs or job releases.

```text
Selected policies or a saved collection
  -> exact policy versions
  -> proposed packs / graphs
  -> review and testing
```

A collection supplies source membership, not executable dependencies or policy
precedence. Authoring uses the requested decision purpose to identify relevant
clauses and propose decisions. Graph edges require actual decision dependencies;
ambiguity or conflicts require reviewed resolution, not an inference from list
order. Shared-policy access and applicability requirements still apply.

Framework is not a required artifact or lifecycle. A framework-like named
collection may organize related policies without introducing another execution
layer. Its authoring input is the same policy-reference list used for direct
selection.

Defer a separate Framework specification and publication lifecycle until a
concrete use case requires an independently approved policy baseline, portable
exchange of that baseline, or governance/coverage across the collection as a
whole. Multi-policy authoring does not depend on introducing that artifact.

## Agreed unified Library navigation

Library is the common home for Policies, Packs, Graphs and Dialects within the
current workspace. This supersedes the proposed separate Policies workspace and
the separate top-level Dialects navigation. Jobs remains the operational area
for execution configuration, schedules, runs and results. Reuse the existing chat
and contextual right pane; Admin continues to hold workspace settings,
connections and members.

Library uses a searchable list with type views: All, Policies, Packs, Graphs and
Dialects. Preserve the selected view and offer Recent, Favorites, Needs my review
and pinned views. The mixed list shows common metadata such as name, type, owner,
relevant status and update time; type-specific views expose their own useful
columns. Policy approval and pack validation retain their distinct meanings.

Folders are optional organization by team, project or domain. Creating an
artifact does not require choosing a folder. Global Library search and type
views find accessible items across folders, with any narrower search scope made
explicit. Saved collections reference existing artifacts, allowing cross-cutting
groups without copies or another publication lifecycle. Access restrictions also
apply to search results and collections.

Artifact pages preserve their working experience. Dialects retain access to
records, recipes, mappings and activity; Policies retain content, reviews,
implementations and history. Direct links expose policy-to-pack and
pack-to-Dialect relationships, so folder traversal is not required to follow the
work. Folder or collection membership does not establish an executable
dependency, applicability or policy precedence.

Moving an artifact between folders preserves its stable identity, versions and
dependent job references. Consolidating navigation must preserve existing saved
work, contextual chats and direct access to artifacts.

The comparison consulted Databricks' [workspace search](https://docs.databricks.com/aws/en/search)
and [object navigation](https://docs.databricks.com/aws/en/workspace/workspace-objects):
folders coexist with search, type filters, Recents and Favorites. These are
reference patterns; the Library design above is the agreed Desk decision.

## Architecture boundaries carried forward from the assessment

These guide the design; they are not claims of implemented capability:

- Spec owns adopted portable semantics; Runtime supplies deterministic document
  operations for adopted contracts.
- Desk and its application service own collaboration, authenticated authority,
  access control and lifecycle transition admission.
- Runner executes retained releases; Gateway owns connected acquisition and
  separately authorized external actions.
- Approval assertions in an exported artifact do not authenticate themselves.
  A content change creates a new candidate; older review history remains intact.
- Reuse Desk's existing chat, context, Plan, action approval, correction and
  artifact persistence boundaries. Chat deletion must not delete policy work.

## Capability comparison for the policy design

Code/document review on 2026-10-17 used Gateway main `4b9fa82`, Spec main
`002ab19`, Exchange main `dbf1ca2`, Exchange release `v0.2.0`, and Desk's
`integration/v0.13` at `e866405`. Desk development capabilities below are not a
claim that they are on Desk main or installed in a user's release. This review
did not run live connectors or rerun acceptance tests.

### Existing capabilities to reuse

- **Gateway:** provider discovery, connection/credential custody, source
  selection, document acquisition/processing and acquisition receipts. The
  current catalog includes Google Drive, Gmail, Notion, Obsidian, AWS S3,
  Databricks and Azure DevOps, alongside public web and web search. Drive reads
  retain the upstream file version and reject a detected change during the
  read. Azure DevOps supports repository item and commit reads. Generic HTTP,
  MCP and Airbyte adapters provide integration mechanisms; their existence does
  not establish support for a particular policy provider or lifecycle.
- **Dialect:** portable parameters, source declarations, typed research fields,
  fact/evidence mapping, reconciliation, canonical identity, local bindings and
  retained-preparation verification. The contract distinguishes implemented
  research/operation paths from proposed standalone `http`, `sql` and `web`
  kinds. Its staged vectors do not establish a released conformance claim.
- **Exchange:** versioned, digest-pinned `preset` and `dialect` entries, catalog
  release history and fixture contracts. Released catalog `v0.2.0` provides three
  research presets: companies, people at a company and procurement opportunities.
  These are not policy-governance templates or verified policy interpretations.
- **Desk development:** reviewed Exchange imports/updates, retained versions,
  supported input-fixture replay, local binding and Runtime pack-fit checks;
  before/after source-review records and reviewed-record subscriptions that
  deliver work to Runner. These are useful foundations, but they do not implement
  policy synchronization, organizational sign-off or policy withdrawal admission.

The current Exchange research acceptance uses authentic Gateway receipts with
deterministic test model answers and all fields `not-found`. Its documented
coverage does not establish live search/page acquisition or found-value
extraction. Imported definitions and existing acquisition primitives must not be
presented as completed end-to-end policy synchronization.

### Agreed additions and ownership

| Area | Required addition for policies | Owner |
| --- | --- | --- |
| Source access | Declare supported revision reads, change enumeration or polling, optional notifications and lifecycle metadata. Add provider implementations when required. Establish recurring read authorization separately from one-time selection. | Gateway |
| Synchronization | Durable subscriptions, cursors/checkpoints, deduplication, bounded retries, catch-up, retained source revisions and visible freshness/failure state. Work must survive browser/chat closure. | Desk application service, reusing shared durable infrastructure where applicable |
| Policy lifecycle | Exact-revision reviews, authority mappings, diff/impact review, publication gates and withdrawal propagation to dependent run/action admission. | Desk governance service, with Runner/Gateway enforcement at their execution boundaries |
| Portable policy identity | The agreed Policy companion: source revisions, governance records and implementation links; deterministic validation/canonical operations after adoption. | Spec contract and Runtime implementation |
| Reusable extraction | Optional repeatable document-to-structured-data mappings where useful; keep approval/publication semantics outside Dialect. | Dialect contract/tools, reusable definitions in Exchange |
| Distribution | Reusable policy extraction presets can use existing entry kinds where they fit. Distribution of the Policy companion itself needs an explicit Exchange contract extension after the Policy contract exists. | Exchange and Spec |

No dedicated SharePoint or GitHub/GitLab policy connector appears in the reviewed
Gateway catalog. Azure DevOps repository reads and Exchange's explicit tagged Git
catalog imports are partial Git-related capabilities, not arbitrary repository
policy synchronization. Likewise, a source reader or connection-setup `poll`
operation is not a durable document-change subscription.

Policy source ingestion can use Gateway document adapters directly. A Dialect is
optional for reusable extraction or operational fact preparation, not a required
extra layer between every raw policy and its pack/graph. Exchange distributes
reusable definitions; private policy content, credentials and team review records
stay in authorized workspace storage. Acquisition integrity does not establish
source truth or organizational approval authority.

Initial agreed delivery order (refined by the accepted adversarial review below):

1. Build the shared synchronization service in Desk's backend, including durable
   subscriptions, checkpoints, retries, retained revisions and visible freshness.
2. Complete one working path using an existing version-aware source, such as
   Drive. The initial provider remains to be selected.
3. Apply the agreed lifecycle and impact behavior to drafts, reviews, published
   releases and dependent jobs.
4. Add provider connectors, webhook/delta support and Exchange templates as
   actual requirements arise.

Scheduled revision checks can satisfy automatic sync where the source supports
them. Exact historical capture beyond observed snapshots must only be claimed
where the provider can enumerate and retrieve that history. Gateway owns provider
access and Desk owns the policy consequences of a detected change; Dialect and
Exchange retain their extraction and distribution responsibilities.

Review evidence:

- [Gateway provider catalog](https://github.com/Judgment-Pack/judgment-pack-gateway/blob/4b9fa82ddeae772654d8f8bcf7b33d47ac65a512/adapters/connections/catalog_v3.go)
  and [capability declarations](https://github.com/Judgment-Pack/judgment-pack-gateway/blob/4b9fa82ddeae772654d8f8bcf7b33d47ac65a512/adapters/connections/catalog_v4.go).
- [Drive version retention](https://github.com/Judgment-Pack/judgment-pack-gateway/blob/4b9fa82ddeae772654d8f8bcf7b33d47ac65a512/adapters/connections/drive.go)
  and [adapter contracts including Azure DevOps](https://github.com/Judgment-Pack/judgment-pack-gateway/blob/4b9fa82ddeae772654d8f8bcf7b33d47ac65a512/adapters/README.md).
- [Dialect contract and maturity](https://github.com/Judgment-Pack/judgment-pack-spec/blob/002ab1908bcef729e768d6ae39a99e3212f36553/spec/dialect.md).
- [Exchange entry scope](https://github.com/Judgment-Pack/judgment-pack-exchange/blob/dbf1ca251bc26d082cad6bfe640d610ca590775e/README.md)
  and [released catalog](https://github.com/Judgment-Pack/judgment-pack-exchange/releases/tag/v0.2.0).
- [Desk executable imports](https://github.com/Judgment-Pack/judgment-pack-desk/blob/e866405daec1efdb380dbb0d37d3c74750771daf/docs/design/exchange-preparation-import.md),
  [source-review persistence](https://github.com/Judgment-Pack/judgment-pack-desk/blob/e866405daec1efdb380dbb0d37d3c74750771daf/internal/desk/source_reviews.go)
  and [research acceptance bounds](https://github.com/Judgment-Pack/judgment-pack-desk/blob/e866405daec1efdb380dbb0d37d3c74750771daf/docs/design/exchange-research-acceptance.md).

## Adversarial review and repository delivery boundaries

Review requested by the user on 2026-10-17. This is a design and code-boundary
review by the current agent, not an independent or cross-vendor approval. The
user accepted its findings, repository responsibilities, revised delivery order
and acceptance requirements. These are agreed design requirements; exact
contracts still require specification review and adoption before public claims.
In addition to the snapshots above, this review inspected Runtime main
`785ea58` and Runner main `7510c5c`. No live connector or new acceptance test was
run. The original Runtime worktree and other agents' work were left untouched.

### Verdict and blocking findings

The unified Library and repository split are viable. They are not yet a complete
team-governance architecture. The largest gaps concern authority, exact governing
dependencies and the moment a run or action is admitted. Adding screens or a
polling loop alone cannot close those gaps.

1. **P0 — Team authority is a prerequisite, not a later UI feature.** The existing
   local-owner/companion model is not authenticated multi-user review. An exported
   `approved` field, current browser identity, or AI Auto permission cannot confer
   an organization's sign-off. Desk needs tenant/workspace principals, membership,
   explicit role grants, guest scopes and server-side transition authorization.
   Review requests bind a workflow revision, required scopes and exact content;
   publication checks current authorization and the applicable workflow. Changes
   to reviewers or quorum must be audited and cannot silently weaken an existing
   request. Preserve historical signatures while separately deciding whether
   they satisfy current publication requirements.

2. **P0 — Immutable execution and current permission are different checks.** An
   occurrence queued before withdrawal could start afterward unless Runner checks
   again at dispatch. A finished evaluation could cause a later external action
   unless that action is checked at its own dispatch boundary. Retain execution
   bytes and dependencies, then obtain a current, scoped admission decision for
   each new start. Define the linearization point: enqueueing is not starting;
   recovery/retry that initiates new work rechecks admission. Desk owns authority
   policy, Runner enforces run admission, and Gateway or the actual action adapter
   enforces action admission. A replayed receipt of already completed work is
   reconciled, not executed again. The agreed default is to hold new governed
   work when current eligibility cannot be established, while preserving history
   and allowing already-started evaluation as agreed. A cached long-lived permit
   cannot promise immediate withdrawal enforcement. Offline leases, if offered,
   must expose their bounded revocation delay rather than claim immediate effect.

3. **P0 — A citation is not a governing dependency.** A referenced background
   document must not automatically acquire power to stop jobs. Conversely, a
   governed release must not omit a policy dependency to bypass withdrawal.
   Define an explicit, reviewed adoption manifest: exact policy identity,
   revision/digest, applicable scope and consuming artifact release. Compute the
   transitive closure for supported compositions. Bind this manifest to the
   checked release and enforce it on direct APIs as well as chat/UI. Informational
   citations remain provenance. An unsupported required dependency or lifecycle
   capability must refuse governed execution, rather than disappear during import.

4. **P1 — Source updates can otherwise cause permanent review churn.** A new
   upstream draft, an upstream publication and an effective withdrawal are
   distinct events. Keep updates tied to the source revision selected as the
   candidate's basis; expose authority and lifecycle metadata separately. Preserve
   the agreed publication hold until an update is resolved, with the reviewed
   older-version option where allowed. A publisher must not compare an external
   opaque revision identifier using JPS artifact version arithmetic. Identical
   bytes with a new withdrawal/approval event still change lifecycle; a new OCR
   rendering of unchanged bytes changes extraction identity, not source identity.

5. **P1 — Whole-revision sign-off and section review need exact scope semantics.**
   Section movement, splitting or changed surrounding definitions can invalidate
   an apparently unchanged excerpt. Bind a review to the complete candidate
   digest plus stable section identity and revision-specific anchors. Keep
   unresolved anchors explicit. Do not carry signatures to a new candidate merely
   because text matches; retain the old review and request the configured new
   approvals. The final approver reviews the complete revision. Whether unchanged
   sections can be reconfirmed together is a UX optimization, not silent approval
   transfer. Separation of duties, delegation and quorum remain organization rules.

6. **P1 — Unified navigation must not collapse different kinds of state.** A
   published Dialect definition, its local connection binding, mutable research
   records and preparation/run history are related but different objects.
   Likewise, source publication, local adoption, pack validity and executable
   readiness have different meanings. Library needs a typed index over existing
   stores, not a new universal file schema or a forced storage migration. A normal
   result represents an artifact with its current draft/publication summary;
   historical releases remain reachable through history and explicit version
   search. Keep record editing on the Dialect page and operational runs in Jobs.

7. **P1 — Folders and search can change access or break references accidentally.**
   Use stable, workspace-scoped artifact identities for navigation and references.
   Moving a folder or changing a title cannot replace identities or rewrite a
   frozen release. Choose explicit permission-inheritance behavior: an
   organizational move should not silently broaden access. Filter search,
   suggestions, snippets, facets/counts, collections and AI retrieval by current
   access; a stale index or retained chat must not bypass that access. A collection
   reference is not a permission grant. Preserve supported artifact URLs and
   contextual chats during navigation migration; do not reintroduce the removed
   `/inputs` route. Changing the sidebar does not require renaming every URL.

8. **P1 — Polling and immutable storage have operational limits.** The service
   must persist a revision snapshot and its processing outcome before advancing a
   cursor, withstand duplicate/out-of-order events, recover invalid cursors and
   disclose gaps in obtainable history. Polling the latest file cannot promise
   capture of every intermediate revision or approval event. Browser-independent
   work still requires a running host/service. Local mode needs honest stopped/
   catch-up states; multi-user hosted operation needs a deployed backend. Retention,
   source-access revocation and backup restoration need explicit policies: a
   restore must not silently resurrect a withdrawn release or an old permission.
   Transactional tenant metadata, protected blob storage and recorded tombstones
   are application concerns, not additions to the portable document format.

9. **P1 — Automated conversion can look complete while missing policy meaning.**
   Keep exact raw bytes, extraction identity and source anchors beside optional
   structured requirements. Record which clauses are implemented, manual,
   excluded or unresolved. AI may suggest links and tests but cannot assert full
   coverage, infer precedence from a collection, approve its own interpretation or
   alter expected outcomes to pass checks. Source text and imported metadata are
   untrusted inputs to the existing AI framework, never permission instructions.
   An impact graph can identify declared affected artifacts; it cannot prove that
   all semantic consequences were discovered. Review multi-policy conflicts and
   coverage explicitly before publishing an implementation.

### Agreed work in each current repository

Names below describe ownership and candidate module boundaries, not adopted
command names or schemas. The portable contracts must be reviewed before public
writers claim support. No new repository is required for the initial service.

| Repository | Work it should own | Work it should not absorb |
| --- | --- | --- |
| **judgment-pack-spec** | Propose the domain-neutral Policy companion with identity, exact source revisions, stable clause references, optional structured requirements and typed provenance/implementation links. Define immutable policy releases, separate format/artifact/draft/external revision identities, explicit governing adoption/dependency semantics and the meaning of exported lifecycle/review assertions. Specify any portable admission evidence only as a separate, explicit contract. Add schemas, refusal cases and independent test vectors, including cross-domain examples. | UI navigation, folder ACLs, live org membership, database layout, connector secrets, sync scheduling or a second decision language. Do not add arbitrary fields to Core to bypass companion review. |
| **judgment-pack-runtime** | Extend the existing shared offline document boundary with Policy starter/schema/validation/canonicalization/digest and deterministic link/manifest checks using supplied exact documents. Expose identical CLI/MCP diagnostics and capabilities. Reuse artifact version arithmetic; keep semantic compatibility separate. Continue pack/graph checks, test evaluation and behavior comparison on explicit snapshots. | Model drafting, live source fetching, choosing reviewers, deciding organizational authority, fetching withdrawal state or reserving published identities. A valid document must never be reported as organizationally approved or safe to run. |
| **judgment-pack-gateway** | Extend versioned provider descriptors for stable resource identity, supported revision reads/change enumeration, bounded polling or notifications and raw source lifecycle metadata. Keep recurring-read grants and secrets in connection custody. Preserve source bytes and extraction identity, scope webhook/notification handling and distinguish deleted/unavailable/unauthorized. Add providers when needed. Enforce scoped current admission at actual connected-action boundaries where those actions are implemented. | Policy review workflows, converting a provider's status into organizational authority by default, the Library index or policy publication decisions. Source integrity is not proof of approval or truth. |
| **judgment-pack-runner** | Extend the release envelope/store to retain exact governing dependencies and any necessary admission references. Enforce current eligibility at every governed run start, including schedules/events/retries/recovered work; retain the checked authority revision and reason in the audit trail. Preserve historical execution and explicit upgrades. Advertise capability support and refuse governed releases an older implementation cannot enforce. Test queue/withdrawal races and migrations. | Policy drafting, source approval semantics, team membership management, a second evaluator, or assuming graph-job execution exists. The current single-pack job contract and experimental workflow work remain distinct. |
| **judgment-pack-desk** | Own the Policy model and application service: authenticated teams/guests, scoped permissions, review requests, sign-off, publication/effective-date/withdrawal transitions, CAS and idempotent audit events. Own sync subscriptions/checkpoints, freshness, lifecycle authority mappings and the admission decision service. Build the unified Library index, views, search, collections, preserved routes and artifact detail pages. Add source/import editing, diffs, clause links, coverage/impact and exact adoption review. Reuse shared chat, context, Plan, permissions, correction, action admission and failure/recovery UI. Implement tenant storage boundaries and local/hosted service lifecycle. | New evaluator/format semantics, provider credentials duplicated in the browser, per-feature AI loops, review authority inferred from model text or an unreviewed permission widening through navigation. |
| **judgment-pack-exchange** | Publish reusable policy extraction presets/mappings and domain examples through supported entry kinds where they fit. Preserve release pins, author/license information, fixture verification and explicit limitations. After adoption of the Policy companion, separately review a catalog entry extension if distribution of Policy artifacts is required. | Private tenant policy storage, review invitations, live source sync, organization approval, or disguising a Policy as a Dialect just to fit the current catalog. |

Dialect is a companion and capability across Spec, Runtime, Runner and Desk, not
a separate repository that should own the policy service. The graph continues
to compose decisions; the review workflow remains Desk application behavior.

The other current repositories have supporting roles only:

- **judgment-pack-evaluator-experiments:** independently designed adversarial
  datasets and conversion/coverage studies, with explicit limitations. It is not
  a production governance dependency or the normative contract owner.
- **judgment-pack-demo:** a disposable, explicitly synthetic end-to-end policy
  example using supported released components. It should not contain a second
  implementation or claim that fixture approval represents a real organization's
  authorization. Update it when the production path is ready.

### Agreed interface boundaries — concrete proposal available for review

The [interface proposal, revision 0.0.1](policy-governance-interfaces.md) now
defines proposed fields, commands, transactional boundaries, withdrawal ordering
and recovery behavior for these four interfaces. Its
[three synthetic domain examples](../examples/policy-governance/README.md)
exercise the same shapes for data access, procurement and refunds. This is a
reviewable design draft, not an adopted schema or implemented service.

1. **Source observation:** stable source/resource identity, opaque source revision,
   original content digest, extraction identity, observed time, raw lifecycle
   assertions and authority provenance; optional cursor/completeness information.
   Gateway supplies observations; Desk retains and interprets them under configured
   authority. Neither content hashes nor timestamps establish total provider order.
2. **Governing adoption:** exact policy releases and scope mapped to exact
   executable releases, with reviewed provenance versus governing dependencies
   distinguished. Spec owns portable semantics; Runtime checks supplied documents;
   Desk creates the approved manifest; Runner retains it.
3. **Admission:** operation-specific, tenant/workspace-scoped current eligibility
   tied to release/dependency digests, operation identity and authority state.
   Desk decides under organization policy; execution hosts enforce. Determine
   freshness, single-use/idempotency and withdrawal ordering explicitly. Signed
   or cached material alone cannot establish that it is the latest state.
4. **Review/publication transaction:** content digest, workflow revision, review
   scopes, authenticated actor and authority basis, expected base revision and
   immutable result receipt. Manual UI and chat use the same service boundary.

### Agreed revised delivery order

Refine the earlier product sequence by establishing identity, authority and the
cross-repository interfaces before enabling synchronized governance. Library
navigation can be implemented independently once its typed index and migration
rules are fixed. A sync-only milestone must not be described as team governance.

1. Freeze the minimal contract/authority decisions and author synthetic examples
   for data access, procurement and refund eligibility. Avoid scope expansion into
   a mandatory framework or control language.
2. Add shared identity/storage/review boundaries in Desk and proposed offline
   Policy tooling in Runtime against the explicitly pinned contract. Implement
   navigation consolidation with migration coverage, without moving stored bytes
   merely to match the new sidebar.
3. Complete one authorized automatic-sync path using an existing provider,
   including restart, gaps, stale state and authority metadata handling. Add
   clause comparison and impact review through the shared AI/manual workflow.
4. Complete Runner release/admission support and enforcement at any implemented
   action boundary before claiming withdrawal protection. Exercise team and guest
   access on the actual multi-user service before claiming team sign-off.
5. Run a complete cross-repository scenario on two isolated installations; publish
   supported examples/templates afterward. Add additional providers as needed.

### Required adversarial acceptance evidence

These are agreed acceptance criteria, not tests run or passed by this review:

- A source update races with sign-off/publication: no content or review basis can
  change between check and commit, and exact completed retries return one receipt.
- A withdrawal arrives after enqueue but before dispatch: no new governed run
  starts; an already-started evaluation may finish, and a subsequent external
  action is independently refused. Reconnect/restart cannot bypass the rule.
- A source emits the same bytes with a changed approval/withdrawal state, events
  arrive twice/out of order, or a cursor expires: lifecycle changes are not lost,
  duplicates have no duplicate effects, and historical gaps are visible.
- A section is moved/split or its definitions change: stale anchors and approvals
  cannot approve another candidate. Changed reviewer/quorum settings are explicit.
- A guest loses access after an index or chat was populated: search, counts,
  snippets, exports and AI retrieval enforce current access; a shared collection
  does not reopen access to the source.
- A pack is valid and tests pass but source coverage is incomplete, or two
  policies conflict: implementation review remains unresolved; expectations are
  preserved, and test success is not treated as full coverage or approval.
- An imported release removes a governing dependency, changes its scope/digest,
  or supplies an unsupported required admission capability: execution refuses
  the unchecked substitution, including through direct APIs.
- An artifact is renamed/moved, a policy is forked, or a collection changes:
  original jobs remain pinned and artifact/chats remain reachable. A fork has new
  identity and approval; merely moving a folder does not grant new access.
- A backup predates withdrawal or role revocation: restoration cannot resume
  governed work using old permissions without reconciling authoritative state.
- Export/import and local/hosted adapters retain canonical identities, source
  provenance, typed facts and historical execution pins. Show unavailable source
  content honestly; do not invent unavailable approval or historical evidence.

Additional boundary evidence: Runtime's
[offline document boundary](https://github.com/Judgment-Pack/judgment-pack-runtime/blob/785ea58927255b775a774e29952c560591429cd3/docs/adr/0052-offline-dialect-document-tools.md)
and [version arithmetic](https://github.com/Judgment-Pack/judgment-pack-runtime/blob/785ea58927255b775a774e29952c560591429cd3/docs/adr/0055-artifact-version-arithmetic.md),
Runner's [job upgrade contract](https://github.com/Judgment-Pack/judgment-pack-runner/blob/7510c5c5e83b97be425bb2c0b123875bc89fe504/docs/design/job-revisions.md)
and [dispatch implementation](https://github.com/Judgment-Pack/judgment-pack-runner/blob/7510c5c5e83b97be425bb2c0b123875bc89fe504/internal/runner/triggers_dispatch.go).
Earlier ADRs and READMEs describe individual delivery stages; they must not be
used alone to infer present end-to-end capability or a new conformance claim.

## Next unresolved decisions

1. Detailed unified Library layouts, folder/collection management and artifact
   page navigation within the agreed structure.
2. Review-template details, reviewer assignment and revision-comparison UX.
3. Initial supported sync source and freshness rules; document editor, clause
   review and change-impact UX. Additional connectors follow actual requirements.
4. Adoption of the proposed portable identity/link/review forms, their trust
   profile and standards adapter mappings; see the concrete interface draft.
5. Collaboration service, storage, concurrency and tenant isolation design.

No implementation or new schema is authorized merely by this decision record.
