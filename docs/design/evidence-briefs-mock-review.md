# Evidence brief static design review

Date: 2026-09-25. Status: isolated mock; no production feature implemented.

Open `/briefs-design.html` on the existing Vite server. The preview uses shared Desk
controls, icons, typography and theme tokens. It is not imported by the production entry.

## User clarification

The brief is a readable one-pager. The dashboard-style mock was replaced by a short
narrative document with context, a compact required-evidence checklist, findings,
uncertainty and the next step. Supporting records open separately. These illustrative
exports contain 224–236 words and each fits on one A4 page with a readable type size.
They are mock documents, not real case or run results.

## Screens

- Test case: missing privacy scan, saved expectation versus observed result, compact
  evidence checklist and limitations. A passing test does not approve the contribution.
- Job setup: purpose, fixed release, manual/API trigger, required inputs and evidence,
  absent artifact bindings, and release-readiness evidence. Requirements are not
  represented as documents already received by future runs.
- Operational run: completed execution versus unresolved decision, missing evidence,
  an open scope objection, source reader, interpretation review and separate human decision.
- Inputs changed: a later privacy scan does not rewrite the original run or its brief.
- Before generation: explicit Generate brief. Saved views show Regenerate and history.
- Revision history, light theme and narrow-screen variants are also captured.

The retained lab-notes.ai Contribution Acceptance Review 0.1.0 pack supplies the title,
three required evidence items and decision semantics. All cases, runs, files, verification
results, dates, review records and narrative are fictional design fixtures. The example
scope-review quotation was written for this mock, not fetched from the reference website.

## Interaction and ownership

The final placement is the right tool rail: Assistant, Brief, Details, Activity. The mock
keeps the existing Tests/Jobs/Run content in the main workspace and selects Brief as the
second tool. There is no separate Brief tab or duplicated page action. The preview's
book glyph follows the existing 16px grid, 1.75px stroke and outline vocabulary while
remaining distinct from Details.

The regular pane is 510px wide on desktop. Expand shows the same document in an inset
900px reading overlay, leaving the main workspace visible at the left. Restore returns
to the split view. Mobile reflows to one contextual pane. A production implementation
should reuse the existing pane/overlay component and its interaction behavior.

Source links and revision history use the same pane with a Back to brief control.
Assistant, Brief and Details retain their example state when switching. Regenerate,
history and sample PDF download are in the Brief header overflow. With no saved brief,
the pane offers Generate brief. Generation controls show a preview notice only.

Primary static images: `rail-brief.png` and `rail-brief-expanded.png`. Additional `rail-`
images show Jobs, runs, first generation, overflow menu, light theme and mobile layouts.
The earlier one-page PDF examples remain available as illustrative downloads.

Only the isolated mock and these notes changed. Durable brief generation, persistence,
sharing and runtime behavior remain proposed in `evidence-briefs-plan.md`.

## Verification

- TypeScript typecheck passed.
- Existing UI convention, palette and containing-block suites: 605 checks passed for the right-rail mock.
- Chrome renders: desktop dark/light, mobile 390px and narrow 320px.
- Rail order and absence of a duplicate Brief tab checked. Normal/expanded geometry,
  Escape/restore, source/back, overflow/history and generation notices exercised.
- Assistant draft retained when switching to Brief and back.
- Browser checks observed no API requests, storage writes, console errors or page overflow.
- All three PDF exports contain exactly one A4 page; text extraction confirms the next
  action and footer are present.
- PNGs and one-page PDFs are stored under `web/mockups/evidence-briefs/` and served directly by Vite.

Nothing committed. The next delivery step is the frozen snapshot and versioned brief
storage contract before AI generation or additional Runtime diagnostics.
