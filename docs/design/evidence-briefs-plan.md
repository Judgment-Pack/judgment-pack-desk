# Persistent evidence briefs for tests and Jobs

Date: 2026-09-25. Status: proposed requirements and implementation plan. No brief
feature, Runtime behavior, or JPS schema is changed by this review.

## User requirement

A potential user needs a concise account of a test case or job, including all required
evidence, so another person can understand and review it without finding its chat.
Generation/regeneration is explicit and on demand. Once generated, the brief persists
and is the same artifact for everyone authorized to view it. Opening a page, switching
users, rerunning a test, or loading a job must not silently invoke an LLM.

This requirement moves evidence briefs and their frozen input contract ahead of recurring
Jobs. It complements release readiness: a release brief can show exact test evidence,
while a case/run brief explains the particular question, inputs and resulting decision.

## Reference reviewed

The user's [GAIA policy-evidence demonstration](https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/gaia/policy-evidence#brief-human-review)
was fetched successfully on 2026-09-25. The review covered its decision brief, human
review, potential dissent, human decision record, frozen evidence and change notice.
Also read: [method](https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/method),
[contribution guidance](https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/contribute)
and the [policy-evidence playbook](https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/playbooks/policy-evidence).
It is a read-only hosted demonstration using explicitly synthetic records, not evidence
that a production multi-user workflow or the real-world analysis is validated.

The demonstration separates a frozen evidence-derived brief, stored human interpretation
review, a separate human decision/rationale and a later evidence-change comparison.
Model interpretations and potential counter-evidence retain their status after mechanical
checks or human confirmation. The change comparison reports artifact identity at a stated
check time; it neither rewrites the old record nor establishes the decision still holds.
Its domain playbook also distinguishes source-supported context from synthetic examples.

These observations inform the design below. The specific API, fields and lifecycle are
our proposals, not claims that the reference website implements them.

## Clarified presentation: a one-page brief

The user clarified that “brief” means a concise, readable one-pager. The first
mock exposed too much of the evidence machinery as a dashboard. The revised
mock uses a short document (roughly 200–400 words for these examples): context,
required evidence, key findings or expected behavior, uncertainty, and the next
decision or action. It can be read independently and exported as a single page.

Source verification, raw inputs/results, the complete evidence register and
revision history remain linked supporting records. Important missing evidence
and unresolved objections must still be visible in the brief itself. Do not
achieve one-page length through tiny text or by silently omitting limitations.
For large packs, use named evidence groups and explicit missing-item counts with
a link to the complete requirement checklist; never imply completeness without
that link. On mobile the same concise document naturally scrolls.

This changes presentation, not the explicit Generate/Regenerate lifecycle or the
requirement to persist and share the same authorized saved revision.

## One concept, three contexts

| Context | Required content | Snapshot identity |
| --- | --- | --- |
| Test case | Purpose and scenario; facts and required evidence; expected disposition/handoff and rationale; linked actual result if a run is selected; coverage and gaps | Pack digest + case revision + selected test-run ID, or explicit not-run |
| Job / release | Business question and scope; fixed policy; required inputs/evidence and bindings; configured trigger/owner; exact validation/test evidence; escalation expectations | Release + job revision + mapping and test-evidence digests |
| Operational run | Particular item; supplied inputs and acquired evidence; actual Runtime disposition, reasons, trace and handoff; limitations; subsequent human review/decision | Run ID + exact release/input/evidence/result artifacts |

A job brief describes the setup. It cannot claim a future item already has evidence.
A passing test is a match with its saved expectations, not proof of operational fitness.
A Runtime handoff is not a performed human review. A policy's authored metadata review
is not a review of the particular case, evidence bundle, generated prose or decision.

## Brief contents and evidence status

Use a common section order with context-appropriate omission of inapplicable sections:

1. **Question and context**: what is being tested/decided, for whom, bounded scope.
2. **Inputs and evidence**: every declared requirement, including missing requirements;
   optional items remain visible with their optional label.
3. **Result or expectation**: expected and actual kept separate; not-run never acquires
   a result from prose. For Jobs show the fixed release and its test-readiness record.
4. **Explanation and counter-evidence**: contributing rules, exceptions, limitations,
   contradictions and unresolved interpretations, with retained source links.
5. **Human review / next decision**: who reviewed what snapshot, their explicit rationale,
   requested follow-up and any later recorded decision. AI cannot supply a human event.
6. **Provenance and history**: snapshot, brief revision, generation method/model/template,
   actor/time, source locations and evidence-change check time.

Each evidence row needs separate dimensions; one green “verified” flag is insufficient:

- Requirement ID, description, required/optional and related rule/exception references.
- Availability supplied to Runtime: present, absent or unknown, preserving omission.
- Artifact binding: none, attached or unavailable to this viewer; immutable ID/digest,
  originating source, version/acquisition time and citations/locators where available.
- Checks actually performed: receipt signature, digest, quote/span or declared schema
  check, with checker/version and result. Unperformed checks say not checked.
- Human assessment: unreviewed, accepted for the stated purpose, questioned or rejected;
  actor, scope and rationale are separate from mechanical integrity.
- Declared freshness/adequacy policy if applicable; do not invent a generic expiry rule.

Keep input availability separate from whether resolution actually inspected a requirement.
For example, false applicability can end the evaluator's resolution path before required
availability inspection. An input register may still show all requirements, but it must
not label them all consulted by Runtime.

A counter-evidence source can be byte-verified while its bearing on a conclusion remains
unresolved. Human confirmation of wording does not erase it. Preserve supporting,
contradicting, qualifying and unassessed evidence rather than inventing agreement.

## Generation and persistence

The evidence register, IDs, checks, Runtime result and recorded review are deterministic
views of retained data. They need no model. **Generate brief** creates a frozen snapshot
and optionally asks AI to write explanatory prose against that snapshot only. A template
brief remains available without an AI provider. Both methods produce the same saved
artifact format and disclose their generation method.

No LLM is called on page load, polling, navigation, viewing history or comparing hashes.
No live sources are fetched by brief generation: refresh/acquire is a distinct authorized
step, followed by a new snapshot. AI analysis of new evidence or a dissent challenge is a
separate stored activity, not a side effect of summarizing existing records.

Proposed minimal stored record:

- brief ID, workspace ID, subject kind/ID, revision and parent revision;
- frozen manifest: exact pack/case/job/run versions, facts, availability, result,
  evidence/source artifacts, test evidence, mappings and review-ledger revision;
- deterministic evidence register plus sections with resolvable citation bindings;
- template/schema version; generation kind; for AI, actual model/provider, prompt/output
  fingerprints, bounded input manifest and declared omissions/limits;
- generating actor/time, output digest and immutable revision history;
- a separate latest-revision pointer, updated using optimistic concurrency.

Save validated output atomically. Regenerate produces a new revision; never overwrite a
brief that a reviewer or decision record already referenced. The previous revision stays
readable if generation fails. Concurrent requests for the same generation operation share
one durable operation/idempotency key, rather than issuing duplicate billed requests.
Retries after an uncertain provider response must not silently claim no extra cost.
If subject inputs change during generation, keep the result bound to its original snapshot
and mark it changed relative to the new subject. Never attach old prose to new inputs.

Input change status and review status are different fields:

- Current inputs match / inputs changed / comparison incomplete or unavailable, each
  with scope and checked-at time. Template/model changes are separately identified.
- Unreviewed / reviewed at this revision / changes requested, with explicit scope.

A failed comparison is not a match. New sources do not retroactively modify a historical
run or its original brief. The newest input snapshot can be reviewed separately. An
annotation or correction is a new attributable revision/event, not an edit to old evidence.

## Shared access and user experience

Save server-side; neither chat history nor localStorage is the source of truth. Place **Brief** as the second tool on the right rail: Assistant, Brief, Details, Activity.
It follows the selected test case, job or run and opens the saved one-pager in the existing
full-height contextual pane. Do not add a separate Brief tab or duplicate page buttons.
Without a saved brief, the pane offers **Generate brief**. Use the existing expanded
reading overlay, leaving some main workspace visible, when more width is needed.
**Regenerate**, history and PDF export live in the brief header overflow menu. Source
links temporarily open the reader in the same pane with Back to brief. Switching tools
preserves scroll and chat drafts. A changed-input notice never auto-regenerates.

Use a one-page document with short headings, paragraphs and a compact evidence
checklist. Keep technical details in linked supporting records. Chat can link to the saved brief rather than duplicate its contents as custom
cards. A concise first screen must still expose unresolved issues and missing required
evidence; folding technical JSON must not conceal the substantive limitations.

“Everyone” means authorized members of that workspace. Desk is currently a local single-
owner installation, so persistence works across sessions today but is not yet organization
collaboration. Add subject-level read/generate/review permissions when shared identity
lands. A brief contains derived source information: its access policy must be no broader
than the source material it includes. Do not publish prose derived from a person's private
connector simply because the job itself is visible. Revocation, redacted views, export,
retention, backup and deletion must apply to stored briefs and source artifacts too.

For the local pilot, new brief records must be included in the appropriate storage backup
and restore validation from the start. Operational artifacts currently live in Runner and
are excluded from Desk chat backups; do not silently claim those backups cover a brief's
external references. A portable brief export needs its authorized frozen evidence bundle
or explicit missing-reference notices, not just Markdown with expired URLs.

## Repository responsibilities and actual gaps

**Desk owns the common brief schema/renderer, explicit generation UX, AI orchestration,
workspace test-case brief persistence and citation presentation.** Reuse the existing
model relay and bounded agent facilities. A new agent framework/repository is unnecessary.
Generation receives data-only evidence, not executable instructions from source documents.
Validate the output and citations; prose cannot modify facts, evidence flags, expectations,
Runtime results or human review records.

**Runner owns operational release/run snapshots and durable job/run brief/review records.**
Expose bounded authenticated artifact APIs through Desk. It does not import Desk UI or
Runtime internals. Share a versioned brief wire contract/conformance fixtures with Desk;
avoid a separate service merely for displaying prose. Define generation ownership and
idempotency explicitly so Desk and Runner do not both retry the model request.

**Gateway owns source acquisition/provenance receipts.** Desk/Runner bind those receipts
and frozen acquired artifacts to the brief and identify the verification actually performed.
A valid receipt proves its stated acquisition/integrity contract, not source truth or
semantic adequacy. The current Jobs pilot accepts only facts/evidence declarations and
has no acquired source artifacts or receipt bindings; that is a real implementation gap.

**Runtime owns deterministic evaluation and machine-readable explanation.** Its current
output already supplies disposition, rule/exception trace and handoff target; its audit
retains pack/input binding and caller-supplied receipt references. The trace intentionally
has no separate required-evidence inspection stage (ADR-0027), and an aggregate `unknown`
reason does not identify every unresolved fact/evidence leaf. Receipt references are
recorded as supplied and not verified by Runtime (ADR-0033).

First use retained pack/input/result data for the brief's full evidence register, without
reimplementing resolution. Where that cannot explain which inputs were actually consulted,
propose bounded deterministic diagnostic output from the actual evaluator: per-requirement
inspection status and unknown-condition locations, with explicit skipped/not-evaluated
states. Review this as a Runtime output-contract/ADR change, preserve default result and
canonical disposition bytes, and test short-circuit/precedence/limits. It is not permission
to turn Runtime into an LLM, source verifier, workflow database or human decision maker.

**JPS core already represents evidence requirements, located sources, rule/source references,
unknown semantics and escalation.** It does not standardize this brief or per-case review
ledger. Do not inject an undeclared `brief` member into the closed core schema. An external
brief contract is sufficient for the product first. If interchange later requires a portable
brief/evidence profile, develop it through the spec's RFC process. RFC 0003 Evidence reference
is still Draft, not an implemented or adopted interoperability guarantee.

The demo's policy-specific rules (e.g. review recorded, unresolved challenge disclosed,
required source relationships retained) can be represented as authored pack inputs/rules.
Their factual truth and reviewer authority must come from retained records/integrations.
Do not hard-code this one domain's review outcomes or thresholds into universal JPS semantics.

## Acceptance cases

1. Two authorized viewers see identical brief revision/content without a model call.
2. Reload, reopen, run a test, poll Jobs and inspect history generate zero model requests.
3. Every required evidence item appears, including unavailable and omitted items; presence,
   artifact verification and human adequacy are never conflated.
4. A test's expected outcome and an observed result remain different fields; a not-run case
   cannot claim success. An interrupted operational run cannot acquire a decision from AI.
5. Each factual citation resolves to a retained artifact/version and validated locator;
   invalid references cannot be published as verified. AI-added unsupported claims are
   refused or visibly unresolved, never treated as deterministic evidence.
6. A model draft that omits known counter-evidence cannot remove it from deterministic
   brief sections. A confirmed interpretation keeps unresolved dissent visible.
7. Pack/case/evidence/mapping/review changes mark the relevant brief changed without an
   automatic LLM call; unchanged display preferences do not change snapshot identity.
8. Regeneration and edits preserve prior revisions, human decisions and source identities.
   A review event applies only to the snapshot and scope the human actually reviewed.
9. Concurrency, cancellation, restart and failed generation retain the old valid revision;
   stale completion cannot become the latest brief for a different input snapshot.
10. Unauthorized viewers cannot obtain evidence indirectly through prose/export. Backup,
    restore and deletion preserve or explicitly report referenced artifacts.
11. A real Runtime regression suite proves added diagnostics preserve disposition equality,
    unknown/absent distinctions, trace reachability, resource limits and audit behavior.

## Delivery order

1. Agree this requirement and inspect a static case/job/run brief using one real retained
   pack and synthetic run. Include a required missing item and counter-evidence.
2. Ship the common contract, frozen snapshot, deterministic register and durable versioned
   storage/read/history with backup participation. This is useful without AI.
3. Add explicit bounded Generate/Regenerate narrative and citation validation; same content
   is shared on subsequent reads. No background generation setting by default.
4. Add attributable human review/decision records and explicit evidence-change comparison.
5. Add the targeted Runtime diagnostics only where the first implementation proves a gap;
   then bind connected-source snapshots and release test evidence before recurring jobs.

## Static design preview

The isolated [mock review](evidence-briefs-mock-review.md) covers test cases, job setup,
operational runs, new evidence, revision history and first generation. Open
`/briefs-design.html` on the local Vite server. All case/run content is fictional; only
the retained contribution pack title and requirements are reused. No brief feature or
Runtime behavior has been implemented by the preview.

Implementation: see [saved brief behavior and verification](briefs-implementation.md). The rail reader, explicit generation, frozen revisions and operational storage are implemented locally. Organization-wide identity/permissions and automatic external-source freshness remain outside this release.
