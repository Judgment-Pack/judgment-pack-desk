# Policy authoring, governance and executable decisions

Status: architecture review and proposed scope, 2026-10-16. This document does not introduce a Policy format, approval service, new Exchange entry kind, or enforcement capability. The client application itself has not been inspected; the review uses the described workflow and current repository contracts.

## Recommendation

Make a Policy a first-class authored and versioned source in Desk. Existing policies can be imported with their original bytes; new policies can be drafted through the existing session chat. Both enter the same review and publication lifecycle. A policy can support several packs and graphs, and a pack can depend on several policies. Preserve the policy even when some clauses cannot become executable decisions.

Keep three explicit approvals: the organization approves policy content; reviewers approve its interpretation into decision artifacts; an operator approves a particular deployment/job release. A source citation, passing tests, a reviewed-set lock, an AI suggestion and permission to perform an action do not substitute for one another.

Do not model the whole policy as a Dialect. A Dialect translates observations into facts. A policy defines the intended requirements from which decision logic and expected cases are authored. A governance framework groups policies/controls and their relationships. A graph composes decisions; it is not the policy collaboration workflow.

## Evidence from the current implementation

- Core §6.3 defines sources, locators and citations; rules and exceptions can reference sources. Core explicitly does not verify source accuracy or organizational authority. §6.4 separates an outcome from authorization to act. `spec/judgment-pack-core.md` and `docs/tooling-architecture.md` are the relevant Spec sources.
- Desk already has retained document/source verification, source-refresh review records, independent drafts, the shared AI execution/Plan/approval framework, tests, definition briefs, and immutable artifact publication. `internal/desk/source_reviews.go` records before/after source snapshots; it is not a clause review or organizational approval ledger.
- `docs/design/briefs-implementation.md` explicitly defines briefs as derived summaries, not approval/readiness authority, and describes the current single local authenticated owner. A generated brief should not become the canonical policy or a collaboration record.
- Runtime validates/evaluates the admitted documents and retains exact decision identity. Its reviewed-set lock is a declaration over bytes, not proof that an organizational reviewer approved the meaning. Its graph prototype composes pack dispositions.
- Runner retains reviewed releases and immutable job revisions. Gateway owns acquisition, credentials, retained responses and separately controlled external actions. Neither should become the policy drafting application.
- RFC 0020 describes stage selection/fan-out workflows, separately from judgment graphs. It remains a draft companion contract. A client review process is a further application workflow and must not be represented as an already-supported execution graph.

## Ownership

| Concern | Owner | Boundary |
| --- | --- | --- |
| Portable Policy/source identity, clause references, derivation links and any exportable review-event meaning | Spec, if standardized through an explicit companion proposal | Schemas, prose and fixtures; no UI, database, users or arbitrary new Core fields. Start with existing Core sources; propose only the missing portable semantics. |
| Shared parsing, validation, canonical identity and diagnostics for an adopted companion | Runtime | Offline deterministic document operations. No source fetching, human approval determination, drafting model or deployment selection. Existing pack/graph validation/evaluation stays unchanged. |
| Policy editing/import, chat drafting, clause organization, diffs, comments, review requests, releases and impact view | Desk and its future hosted application service | Workspace roles, tenancy, access controls, optimistic concurrency and authoritative transition admission belong here. Reuse shared chat/context/approvals rather than a new AI loop. |
| Scheduled execution and exact release selection | Runner | Pin policy-source references and executable artifacts where the release contract supports them; retain each run. A new policy does not mutate existing jobs. |
| Connected document ingestion and authorized external actions | Gateway | Preserve source bytes, extraction provenance and credential boundaries. A valid receipt establishes acquisition integrity, not policy authority or truth. |
| Reusable examples, policy/decision templates and mappings | Exchange | Current supported entry kinds and pinned releases first. A standalone policy/framework entry requires a reviewed catalog contract; do not disguise it as a Dialect. Client-private policies need not enter the public Exchange. |
| Which policy governs, who can approve, and what exceptions are acceptable | Client organization | Recorded and enforced by its application/IAM configuration. These are not answers that Spec or an LLM can confer. |

No new repository is necessary for an initial implementation. A separately deployed collaboration service may eventually merit its own repository; that is a deployment and ownership decision, not a reason to place mutable collaboration state in Runtime.

## Objects and identity

The conceptual objects below are proposals, not a new wire format:

1. Framework/catalog: groups policies and controls; can map one requirement to several frameworks.
2. Policy: stable identity, title, organizational owner, scope, and independently versioned releases.
3. Policy revision: exact source bytes, content digest, editable representation and parent revision. For imports, preserve external identifier/version and original file separately from extracted text and its extraction metadata.
4. Clause/control: stable local identifier and anchors into an exact policy revision; statement, scope, rationale and explicitly reviewed interpretation. Stable IDs survive moves; quote/location verification is revision-specific.
5. Interpretation: many-to-many links from clauses to pack rules, exceptions, required evidence, graph nodes and tests, including rationale and unresolved assumptions.
6. Review event: actor identity, role/authority basis, action, exact candidate digest, timestamp and comment/disposition. Organizational approval requires application authentication/authorization; a serialized claim alone proves none of it.
7. Release record: immutable policy content and approved interpretation set, publication/effective dates, supersession links and later withdrawal events. Keep artifact version, schema version, storage revision and effective state separate.

Comments, approvals and withdrawal events should append history. A content change invalidates approvals bound to old content without deleting those approvals. Publication should reconcile a completed identical request after interruption and reject conflicting identity reuse, through the existing immutable artifact and CAS patterns.

## Two entry paths, one lifecycle

Imported policy: select a file or connected source → retain the original and extraction → resolve policy identity and clause anchors → review extracted requirements and any declared external approval → publish an explicitly reviewed local source release.

New policy: create a Policy draft independently of chat → use shared chat to propose text/clauses and identify unanswered choices → edit and compare → request review → resolve discussions → publish an approved immutable release.

An imported document that says "approved" carries a source assertion until the local trust/authority rules recognize it. AI-generated text remains a draft even if its tone is authoritative. Deleting a chat must not delete the policy, comments or releases.

Suggested lifecycle: Draft → In review → Changes requested or Approved → Published; effective/superseded/withdrawn states are tracked separately. Approval need not activate a release immediately. Begin with explicit review requests and comments, not simultaneous rich-text editing; real-time co-editing adds conflict, presence and authorization requirements that are unnecessary for the first pilot.

## From policy to decisions

1. Select the exact approved policy release and relevant clauses.
2. Identify the individual decisions. Keep explanatory, procedural and non-computable requirements visible.
3. Propose packs and graph connections with clause-level rationale. Record ambiguity, contradictions and missing definitions as open questions.
4. Author expected cases from reviewed policy interpretation: ordinary cases, boundaries, missing/false/zero/null facts, conflicting evidence, exception precedence and unresolved cases.
5. Run Runtime checks and compare expected behavior. The shared correction loop may correct implementation defects; it must not silently rewrite policy or expectations to obtain a pass.
6. Review interpretation and coverage separately from test execution. Use statuses such as implemented, manual control, intentionally out of scope, ambiguous and unmapped. A percentage alone must not imply policy compliance.
7. Publish executable artifacts through existing review/version admission; explicitly review a new job or upgrade a retained job release.

Policy changes need a semantic impact view: changed clauses → affected rules/graph nodes → tests → published Dialects/mappings → deployed jobs. Existing jobs remain reproducible. If a withdrawal or expiry must prevent future execution, define and implement an explicit operational admission rule; a decorative source status or a version bump cannot enforce it.

## Concrete data-governance pilot

Use a synthetic "Restricted dataset access" policy. Its illustrative requirements can include a declared purpose, approved data owner, permitted data classification, a reviewed retention period and an exception route. These are client-defined requirements, not claims about a regulation.

- A catalog/API Dialect maps a dataset and access-request record to typed facts and provenance.
- Separate packs assess classification readiness, purpose/retention suitability and approval evidence.
- A graph combines these determinations into a decision such as eligible, not eligible or needs review.
- A Runner job evaluates the pinned release; a Gateway action grants access only through a separately authorized supported integration. A passing pack does not itself grant database access.
- The policy view shows precisely which clauses are implemented, which need manual review, and which decisions/tests/jobs depend on the release.

A second preset for data-asset inventory or governance-request triage is a useful test of the current generic Dialect work. It must import on another installation with different local connections and pass the same retained fixtures without application code referring to the client's field names.

## Collaboration requirements and limits

The current local owner model does not implement a team governance platform. Before promising collaboration, implement authenticated workspace membership, reader/author/reviewer/publisher roles, scoped sharing, optimistic concurrency, append-only review history, notifications, and policy-specific separation-of-duty rules. Prevent a client-supplied actor/role or AI tool result from authorizing a transition. AI remains subject to Plan and Ask me/Auto; an Auto permission for an artifact edit does not make the model an organizational approver.

Access control must apply equally to source files, extracted text, embeddings/context, comments, diffs, exports and generated explanations. A user allowed to read one public pack must not gain private source text through its chat context. Reviewers need the exact candidate and its source availability; stale or incomplete comparisons must not look approved.

## Standards and systems to reuse

- [NIST OSCAL layers and models](https://pages.nist.gov/OSCAL/learn/concepts/layer/) separate control catalogs/profiles, implementation descriptions and assessment. This supports separating a framework, its implementation and evidence of assessment. Consider an OSCAL adapter when the client already uses its control structure; do not make every free-text policy an OSCAL document by default.
- [W3C ODRL Information Model](https://www.w3.org/TR/odrl-model/) represents permissions, prohibitions and duties concerning assets. It is relevant to data-use policy exchange, but is not a generic document collaboration service or automatic equivalent of JPS rule semantics. Unsupported obligations/conflicts need explicit mapping review.
- [OPA bundles](https://www.openpolicyagent.org/docs/management-bundles) provide a useful reference for distributing versioned executable policy. Treat existing Rego policies as source/external implementations unless a deliberate, tested interoperability adapter exists; do not imply lossless conversion into packs.
- [Cedar terminology](https://docs.cedarpolicy.com/overview/terminology.html) separates authenticated principals, authorization requests and evaluating permit/forbid policies. Existing platform authorization can remain the enforcement mechanism while JPS expresses the broader reviewed decision. A JPS outcome is not automatically a Cedar authorization response.

These are design references, not interoperability claims. Confirm the client's current policy format, system of record, approval authority and desired enforcement boundary against one real example before choosing adapters.

## Delivery sequence and acceptance

1. Complete the current shared artifact/version and portable Dialect work; verify existing behavior instead of rebuilding it. Keep connections, allowances and source acquisition outside imported definitions.
2. Build a thin Desk Policy source with independent persistence, exact import snapshots, clause anchors, edit/diff/history, shared chat and explicit local review. Add a reviewed export contract only for portable requirements that existing sources cannot express.
3. Add policy-to-pack/graph derivation and bidirectional impact/coverage, with expected cases derived independently of runtime outputs. Reuse shared correction and artifact actions.
4. Implement team roles, review assignments and publication authority in the application service, with stale-review and unauthorized-transition tests. Do not advertise team collaboration while using only a local owner.
5. Pilot the governance example on a second installation; add catalog-specific adapters and enforcement only after the policy-to-decision boundary works.

Acceptance must cover import fidelity, source refresh, AI/manual equivalence, revision-bound approval invalidation, partial/non-executable coverage, explicit missing evidence, pinned historical replay, withdrawn policy behavior, authorization on every state transition, and consistent export/import. Unit tests over a JSON schema alone do not establish this experience.
