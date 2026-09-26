# Desk spacing and action review — 2026-09-24

## Direction and scope

This pass follows Linear's [2026 interface refresh](https://linear.app/now/behind-the-latest-design-refresh):
secondary navigation should recede, actions should occupy predictable locations,
and unnecessary visual treatments should be removed. Its [agent interaction
guidelines](https://linear.app/developers/aig) support native controls and immediate,
unobtrusive feedback. The concrete pixel values below are Desk's existing tokens
and implementation choices, not a claim about Linear's private design system.

Reviewed the shared controls, shell, folder browser, collection, saved pack
Overview and Logic, draft workspace, pack editing, Tests, case details, test
history/results, Assistant, sources, dialogs, Graphs, Admin and Help. Also checked
authentication control styles: the 44px sign-in action is appropriate to that
standalone form and remains deliberately larger than a workspace toolbar.

## Findings and changes

| Finding | Improvement |
| --- | --- |
| Copy/delete, source and focus icons inherited horizontal text padding | Shared square Button size: 32px comfortable, 28px compact; names and tooltips preserved. |
| Adjacent header actions could touch or wrap inconsistently | PageHeader now owns an 8px action gap and wrapping. A single overflow control fits beside a narrow title. |
| Tests displayed three competing boxed actions | New case is quiet, Design with AI secondary, Run tests primary; actions use an 8px gap. |
| A wide More text button duplicated the overflow pattern | Pack details use the existing ellipsis icon with a tooltip on every width. |
| Long test names consumed three or more lines as Details opened | Two-line titles, full-name overflow tooltips, bounded metadata columns, compact row icons. |
| Logic toolbar was taller than the shell's other toolbar rows | Vertical padding reduced from 12px to 8px; the shared gutter and control height remain. |
| Details sections had accumulated blank space around separators | Reduced extra top padding; fields retain the normal density spacing. |
| Test Assistant composer differed from ordinary chat | Reuses chat styling, quiet bounded model selector, square Send/Stop/Add, consistent transcript type and message gaps. |
| Details used a full-width green Ask Assistant button | Content-width secondary action aligned to the detail gutter; Save and Run retain primary emphasis. |
| Idle progress reserved an empty row | Render the status content when present, outside the scrolling transcript. |
| Source picker looked like a stack of full-width submit buttons | Quiet navigation rows with chevrons and a compact Back action. |
| Long Select values could compete with their chevron or escape a pane | Shared ellipsis and overflow tooltip, fixed chevron space, viewport-bounded menu above dialogs. |
| Some new history/source spacing ignored Compact | Replaced fixed padding and gaps with existing density/spacing tokens. |

The folder/navigation icon alignment, primary form controls, neutral palette,
readable type sizes, 44px sign-in action and existing resize behavior remain.
Changes are presentation only: no new model calls, case saves, test runs or pack
version changes occur from opening a page.

## Verification

- Full frontend suite after the initial pass: 207 files passed; 4,205 tests passed,
  one existing skip. Subsequent targeted runs after the final refinements passed
  (703 workspace/style tests and 629 shared UI/reference tests; these overlap).
- Final TypeScript check and production build passed. Existing large-bundle
  warnings remain; this pass does not change bundle splitting.
- Locale check and `git diff --check` passed.
- Live Chrome review: 62 sampled states, including populated/empty screens,
  dark/comfortable and light/compact at 1440px and 390px, plus draft, editing,
  map/details, history/results, source choices and expanded Assistant. No page
  errors or document overflow were reported.
- The final title/overflow alignment and Details footer were additionally
  measured at 14 widths from 390px to 1440px, with zero page errors. The Details
  action is approximately 175px wide instead of filling its available pane.
- Duplicate/delete browser checks passed: hover/focus tooltips, unsaved duplicate,
  Cancel/Escape, failed-delete retry, focus restoration, retained run history,
  and zero native dialogs. Writes were intercepted in memory.
- Broad containment gate: all 480 combinations passed (20 routes, 12 widths,
  right pane open/closed), with the copied fixture project unchanged. The gate
  used the main polish build; the final header, composer and Details refinements
  were subsequently checked through the focused tests and live browser reviews
  above.

Screenshots and raw browser measurements are available locally in
`/tmp/desk-spacing-review`. Browser fixture writes for delete/error-path checks
are intercepted in memory; the user's saved cases and history are unchanged.

## Recommended next features

1. **Design missing tests.** Put one action beside uncovered runtime probes.
   Send the current pack, existing suite and exact missing probes to the existing
   proposal workflow. Add reviewed cases to the suite; never silently replace
   existing cases or rewrite an expected result to make it pass. The uncovered
   `unknown` reason in the user's current suite is the first acceptance example.
   Desk owns interaction/orchestration; Runtime remains the coverage/evaluation
   authority. No new agent framework is required.
2. **Review pack changes and compare runs.** Before accepting an AI revision,
   show changes to rules, evidence, outcomes and sources. Compare test results
   against the saved version and show which revision was actually tested.
   Reuse retained snapshots/digests; add persistent version history deliberately
   rather than treating a version label or chat transcript as a history store.
3. **Global quick switcher.** One keyboard action to find packs, folders, drafts,
   graphs and chats, with recent items and deep links. This becomes more useful
   as the collection grows and reduces navigation effort without more permanent
   controls.
4. **Source refresh review.** Show acquisition time, verification and changed
   sources. Refresh into a new snapshot and review affected pack/test inputs;
   do not silently alter historical evidence. Gateway owns acquisition and
   provider capability; Desk owns review and visibility.
5. **Organization access through OIDC.** Before making folders shared across an
   organization, implement real sign-in, membership and explicit permissions.
   Start with one provider through the existing authentication boundary, then
   reuse that contract for others. Source OAuth is not Desk sign-in. This is a
   larger milestone than the four local-workspace improvements above.

The order favors closing the current test-authoring loop first. These are
recommendations, not additional implementation included in the spacing pass.


## Visual evidence

- [Tests before](desk-spacing-20260924/tests-before.png) and
  [Tests after](desk-spacing-20260924/tests-after.png).
- [Logic and Details](desk-spacing-20260924/details-after.png).
- [Narrow Tests header](desk-spacing-20260924/mobile-after.png).

At phone widths, the test table intentionally scrolls sideways within its own
container to preserve Expected, Last result and row actions; it does not force
horizontal scrolling on the whole page. Long case titles remain available in
the case editor as well as on hover/focus.
