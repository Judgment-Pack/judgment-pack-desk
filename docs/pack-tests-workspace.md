# Pack Tests workspace

Drafts and saved packs share `TestsContent`. `/packs/:packId/evaluate` and the
older `/matrix` link both open the same workspace. The pack stays in the main
area; the existing full-height tool pane alternates between Assistant and
Details. Switching tools retains the selected case, editor buffer and assistant
run. There is no second resize implementation.

## Cases and results

A named case holds nested facts, optional evidence availability, an independently
chosen expected disposition or expected refusal, and optional handoff-target
assertions. Source attachments and per-field source references belong to Desk.
Attaching a document does not make an evidence requirement present.

The form preserves omitted, null, false, zero and empty string values. Fields
with ambiguous types or array-index paths use the exact JSON editor. Runtime
expectation validation runs before a disposition is saved. Saving does not
execute the pack. An input without an expectation is an exploratory case;
its observed result is never converted automatically into an expectation.

Explicit Run actions send exact pack and input snapshots. Saved-suite comparisons
use the runtime's `experimental_test_cases`, which shares matrix admission,
canonical comparison, target budgets and coverage with `experimental_test_packs`.
An exploratory run uses `experimental_evaluate` in rehearsal mode. Neither writes
an audit decision. Runtime failures are separate from mismatches. Coverage is
informational, not evidence that policy requirements are correct.

Every history entry retains its pack digest and bytes, case snapshots, timestamp
and runtime report or failure. A result is current only when both the pack bytes
and the saved case match. Finalization changes the pack identity/version, so a
draft result remains historical until explicitly rerun on the saved pack.

## Storage and recovery

`GET/PUT /api/pack-tests` stores `pack-tests-<project digest>.json` in Desk's
private data directory, alongside conversations and draft artifacts. It uses the
existing authenticated API, owner-only file permissions, 16 MiB limit, atomic
write/read-back and `If-Match` conflict handling. Storage accounting, backup,
restore and relocation include the new files. Each project has at most 512
suites and each suite at most 256 cases. A full store or write conflict is
reported; history is not silently truncated.

Finalization carries the private suite into the registered pack ID. Research
handover files now also retain historical check records and their exact candidate
bytes; their companion matrices and retained reports are imported by Tests. Older saved
packs recover from their retained finalized draft artifacts. Probe identities
are stable, so the conversation copy cannot duplicate the runs. Checkpoint
restore clears computed candidate digests; recovery recomputes them from the
retained bytes before matching historical trials. Old checked reports whose
original case input snapshots were not retained remain historical reports,
without a fabricated binding to today's edited case. Deleted recovered cases
remain deleted.

Configured project matrices are imported once into the Desk suite. Later Desk
edits are private to this installation; Export downloads a standard version-3
matrix containing saved expectations for use by another Desk or the CLI.
Exploratory inputs, source document contents and private history are not in that
matrix export. External edits to an already-imported project matrix are not
silently merged into an edited Desk suite. Import reports duplicate IDs for
review. This is an explicit snapshot/import/export boundary, not file sync.

## Assistant and sources

Test design reuses the configured assistant provider and server credential relay.
The engine's `test-design` purpose changes its system instructions to propose
matrix cases rather than replacement packs, allows prose-only clarifications,
and streams progress through the same engine. The test assistant receives no
runtime or host tools. Observed results are labeled as explanation context;
expectations must be grounded independently in requirements or supplied sources.

Local files, web sources and connected sources use the existing attachment and
connection picker with a `test-case` destination. The destination retains case
identity and rejects/cancels late attachment delivery after a selection change.
It does not create a chat or mutate another chat's attachments. Gateway owns
provider authentication and source acquisition; no provider-specific test adapter
or new repository is introduced.

Proposals carry source references and rationale. They are not saved until Review
and Save. A changed pack or existing case invalidates the proposal. Stop retains unaccepted proposal output as history without admitting cases. The case editor is disabled during its save/run;
unsaved edits require an explicit discard before switching cases or leaving.

## Validation and current limits

The browser fixture covers manual entry without JSON, exact runtime comparison,
reload, source attachment, dedicated AI instructions with no offered tools,
review-before-save, suite execution, narrow layout, and draft finalization with
eight retained trials. Provider completion is deterministic in that fixture;
actual provider availability remains installation-dependent.

New copy has English fallback entries in the existing locale catalogs. Native
translations of those new strings remain follow-up work. Imported project
matrices and Desk suites do not synchronize automatically. Historical reports
can only recover data that was originally retained; chat prose is not parsed to
invent missing test cases or results.

Verified locally on 2026-09-24: 203 frontend test files, 4,172 passing tests and
one pre-existing skipped test; frontend production build and localization checks;
Go tests and vet in Desk and runtime; the isolated browser lifecycle described
above. After restarting the local Desk, the existing lab-notes pack showed its
eight exploratory draft trials once, with exact retained draft bytes, zero
invented saved cases, and the same count after reload. No browser errors were
reported. All implementation changes remain uncommitted.

## Automatic correction and retained proposals

Test design uses the existing Vercel adapter through a reusable Desk proposal
workflow. Before the model request, Desk fetches the runtime-owned matrix
contract. It validates the completed proposal with the read-only
`experimental_validate_test_matrix` tool, then sends precise case/field findings
back for at most two additional correction attempts. Transport failures, storage
failures and runtime resource limits do not cause model retries. Older runtimes
without these tools need an update before AI test design; manual entry remains
available. No evaluator or write tool is offered to the model.

Each request and its exact proposal attempts, findings, sources, model, pack
digest and initial case snapshots are retained in the private test store. Inputs
are checkpointed before validation and findings before the next model request.
Stopped or interrupted proposals remain history, never accepted cases. Reloading
a ready proposal restores its review controls; opening one revalidates against
the connected runtime. Saving selected cases checks both pack and case identity
and revalidates the reviewed matrix. Draft finalization carries this history.

Repairs cannot drop or reorder cases, change factual inputs, evidence, source
associations or existing asserted disposition values. Missing disposition members
may be added; semantic changes require review. A valid schema is not proof of
policy correctness or reachability. The UI reports Designing, Checking and
Correcting progress above the composer; exhausted attempts show unresolved
findings and retain exact JSON under Work details. The conversation reports readiness only after validation succeeds; raw model replies
remain in attempt history for diagnosis. The completion message says nothing was saved.

The tests cover a rejected 12-case suite becoming reviewable after correction,
reload recovery, cancellation during validation, missing/inconsistent runtime
reports, storage errors, bounded exhaustion and preservation of test meaning.
These are deterministic fixtures with no live provider calls. Retention supplies
diagnostic history; automatic cross-run memory/prompt adaptation is not enabled.

## Grouped proposal review and running status

Each assistant response owns one compact proposed-cases reference, its open
questions and collapsed Work details. New conversation messages carry a proposal
ID. Older references recover only from an exact adjacent request/time match;
unmatched proposals stay under Earlier proposals instead of following the latest
message. References reuse the same presentation as pack drafts.

Review cases opens a selectable table in the main Tests workspace. Inspecting a
case reuses the existing editor in that main area while Assistant remains open.
On narrow layouts, Review cases returns from the Assistant takeover to the main
workspace; reopening Assistant preserves both the proposal and review buffer.
Apply to proposal updates the review buffer; Save selected cases is the explicit
persistence action and never runs tests. Runtime validates the selected matrix,
then the private store atomically checks original case snapshots and saves only
those rows with per-proposal receipts. Concurrent edits, changed packs, duplicate
saves and suite limits fail without partially saving. The chat reference shows
Reviewing while open and retains its saved count after reload. Saved receipts
record historical acceptance even if a user subsequently edits or deletes a case.

Ordinary chat and Tests use one RunStatus component with a two-second text
highlight using existing readable ink tokens. The row stays above the composer.
Errors do not animate; reduced-motion and forced-color modes use static text.
Only meaningful label changes reach the live region. No new animation or agent
dependency is introduced.

Verified this presentation with the 4,204-test frontend suite (one existing skip),
production build and locale checks. An isolated browser fixture checked selected
saves, review edits, retained checkboxes, reload receipts, wide/narrow layouts,
and reduced-motion/forced-color fallbacks. Browser fixture writes were intercepted
in memory; no user cases were created and no live model request was sent.

## Coverage proposals, comparisons and refreshed evidence

Tests now offers **Design missing tests** for current runtime coverage, routed
through the existing proposal review. Gap proposals must use new IDs, and stale
coverage is rejected before model dispatch. Run history compares retained pack
snapshots and runtime results, identifying input/source and expectation changes
separately. Draft Review also compares retained candidate revisions.

Verified source readers offer refresh review. Applying a newer source to a case
updates only its unsaved attachment/mappings; explicit Save and Run remain
necessary. Prior evidence and historical reports retain their original snapshots.
See [workflow behavior and verification](reviews/desk-workflow-features-20260924.md)
for storage limits, provenance boundaries and the keyboard quick switcher.
