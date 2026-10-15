# Inputs: reviewed conversions and evaluation

Status: approved and implemented locally. See [operator testing](inputs-local-testing.md)
for the implemented behavior, limitations and acceptance checks.

## Direction

Keep Records / Recipe / Activity. Extend the existing mapping controls and
Review inputs dialog instead of adding a wizard page or another tab. Use the
current compact shell, shared menus, table pagination, dialogs and right-aligned
DialogActions. Names remain muted icon + plain link; operational outcomes are
not styled as successful/failed executions.

The first implementation evaluates supplied records against one saved pack.
Graph job execution is not exposed until the runtime/runner contract supports it.
Discovery and AI enrichment remain a subsequent slice.

## Recipe: conversions belong beside mappings

```text
Supplier intake                                  [Prepare inputs]
Records   Recipe   Activity

Decision pack       Supplier readiness · 0.1.0        [Change…]

Input mappings
Source column       Pack input         Use as
Employees           Employee count     [Number ▾]
Active              Active supplier    [Yes / No ▾]
Supplier ID         Supplier ID        [Text ▾]

▸ Conversion rules and examples
  Employee count    "250" → 250
  Active supplier   "yes" → true · "no" → false
  Supplier ID       "00123" → "00123"

Original values are kept. Changes create a new recipe version.
                                        [Cancel] [Save changes]
```

Conversions default to keeping the original text. Numeric and boolean conversion
require an explicit choice; boolean spellings must be declared rather than guessed.
Initial numeric conversion accepts an unambiguous finite number, without rounding,
unit expansion or guessing decimal/thousands separators. Large or ambiguous numbers
require review. Blank/absent values remain unknown, never zero, false or empty evidence.
Dates, currency normalization and custom expressions are outside this first slice.

The pack's inferred field types are hints from its rules, not a new schema or an
excuse to alter the decision logic. Original imported values and older results are
never rewritten. Saving a recipe invalidates any open unsaved evaluation review.
The expanded conversion section previews its effect over the current records and
lists failures, not just a successful sample.

## Records: review the selection

```text
Supplier intake                                  [Prepare inputs]
Records   Recipe   Activity

Search records…   Preparation ▾   Evaluation ▾   [24 selected ▾]

   Record          Preparation      Evaluation       Decision
☑  ◫ Northstar     Ready            Not run          —
☑  ◫ Orchard       Needs review     Not run          —
☑  ◫ Harbor        Ready            Not run          —

24 records                                      ‹ Page 1 of 1 ›

24 selected ▾
  Review inputs
  Evaluate…
  Export
```

The selection menu follows the existing collection pattern and selects across all
matching pages. Evaluate… opens the same input review before the final confirmation.
No row is silently excluded. “Ready” describes successful preparation against the
saved mapping/conversion rules, never the pack's decision or independent evidence
verification. Evaluation describes execution; Decision contains the pack's exact
outcome or unresolved/not-applicable result.

## Review inputs: original and prepared values together

```text
Review inputs                                             ×
24 selected · 22 ready · 2 conversion issues      [Review 2]

Record [Northstar ▾]                  Supplier readiness · 0.1.0

Input              Original         Used for evaluation
Employee count     "250"            250 · Number
Active supplier    "yes"            true · Yes / No
Supplier ID        "00123"          "00123" · Text
Annual revenue     —                Unknown

▸ Evidence availability
▸ Source and preparation details

                         [Close] [Review evaluation →]
```

An unresolved conversion issue in the selection prevents submission. The reader
can fix conversion rules in Recipe or explicitly return and change the selection;
there is no automatic “run the good rows” behavior. Missing values are distinct:
unknown inputs can proceed if the existing mapping and runtime permit them, and
can lead to an unresolved decision. Supplied evidence availability is not upgraded
to verified evidence.

Review inputs is read-only and does not create an operational run. Release checks
may run the existing preview/saved-test flow, which must remain distinguished from
an operational decision and must not label sample outcomes as the selected rows'
results. Technical fields, source identifiers, digests and raw JSON are disclosed
on demand, using the existing expandable viewers.

## Evaluate: one explicit confirmation

After conversion issues are fixed, or the user explicitly changes the selection:

```text
Evaluate 22 records                                       ×

Pack                 Supplier readiness · 0.1.0
Inputs               Recipe 3 · 22 selected records
Saved tests          8 passed
Project safeguards   Ready

Creates one job with one run per record.
Results and input history are saved in Jobs.

▸ Review details

                              [Back] [Evaluate 22 records]
```

The displayed counts and statuses are illustrative. The implementation must derive
them from the exact reviewed selection, recipe, pack bytes, saved tests and actual
project requirements. If the installation requires passing tests and none exist,
show “Saved tests required” and a link to the pack's Tests tab. Honor Runner's
actual release/test policy; do not invent a policy or offer a UI bypass.
If the project, pack, mapping, recipe or selected data changes before confirmation,
stop and ask the reader to refresh the review. Do not silently refresh and run.

“Project safeguards” reflects the configured runtime checks, not the optional
setup reminder's dismissed state. If they are off, say Off; never show Ready or
imply that enabling every optional safeguard is an execution prerequisite.

One reviewed release backs a job; one selected record becomes one operational run.
Initial confirmation may create the job. Later batches may reuse it only after
checking that the release, mapping and conversion policy match exactly. A changed
recipe must not change an existing job's fixed release.

## Results stay in Records

```text
Supplier intake                                  [Prepare inputs]
Records   Recipe   Activity

21 completed · 1 failed                                 [View job]

Record          Preparation      Evaluation       Decision
◫ Northstar     Ready            Completed        Ready for review
◫ Orchard       Needs review     Not run          —
◫ Harbor        Ready            Completed        Needs more evidence
◫ Juniper       Ready            Failed           —

24 records                                      ‹ Page 1 of 1 ›
```

Completion is not a favorable decision. A signing failure must retain the completed
decision and show a separate attention cue. Selecting a result opens the run details
with the original values, prepared values, conversion recipe, pack release, source
references and the existing verification/download actions. Receipt and signature
claims remain limited to the records that actually contain them.

A recipe change labels existing results “Earlier recipe” and preserves them.
Recipe changes never rerun decisions automatically. A failed submission, an accepted
run with a lost response, and a failed execution have different retry semantics:
reconcile accepted runs first; retries of failed executions create explicit attempts
and preserve prior results. No duplicate submissions after reload.

Activity records imports, recipe changes and evaluation batches, linking to Jobs
for execution and signed records. Local preparation activity must not be called a
complete or signed audit trail. The batch has a durable selection and submission
ledger owned by Desk, so closing a tab does not cancel or forget accepted work.
Partial completion is visible; there is no implied all-or-nothing rollback.

## Responsive behavior and placement

- Desktop: a bounded, wide Review inputs dialog with internal horizontal/vertical
  table scroll; a compact Evaluate dialog with the same fixed action footer.
- Narrow screens: the shared dialog fits the viewport, fields stack, table content
  scrolls inside it, and footer actions stay reachable. No nested dialogs.
- Escape/back return to the invoking selection without losing edits or selection;
  focus returns to the opener. No hover-only recovery controls.
- Amber text/icon identify conversion issues and failed operations. Ready/not-run
  states remain quiet. Outcomes preserve the pack's meaning rather than treating
  every completed run as “passed.”
- Desk-wide availability/configuration notices stay in the bottom status strip.
  Conversion errors and release refusals remain beside the action they prevent.

## Implementation boundary and acceptance

The current Runner supports fixed single-pack releases, mapping-v2 case inputs,
input preview and individually idempotent run submission. It does not provide the
Inputs batch workflow, conversion recipe semantics or graph job execution. Desk
must implement deterministic server-side conversions and their retained provenance,
batch submission/reconciliation, exact review binding, and per-record result links.
Do not add conversion members to the released mapping-v2 wire format without a
contract change. Generated/source-backed fields must preserve their classification;
they cannot enter this supplied-assertion conversion path.

After design approval, implement and test these increments:

1. Recipe conversion validation, immutable originals, preview and type edge cases.
2. Exact review binding, stale refusal and existing saved-test/release policy.
3. Durable selected-record handoff, partial failures, retry/reload reconciliation.
4. Record result links, earlier-recipe labeling and retained source/conversion details.
5. End-to-end local operator guide alongside the remaining v0.13 acceptance checks.

The guide must cover blank values, explicit false, leading-zero IDs, ambiguous
numbers, stale reviews, unknown evidence, policy-blocked releases, interrupted
submissions, failed executions and independently reported signing failures.

Reference: Desk's existing design system, Inputs foundation and Jobs contracts;
[Linear's visual hierarchy guidance](https://linear.app/now/behind-the-latest-design-refresh)
informs the restrained surfaces and progressive disclosure, not the execution semantics.
