# Tooltip and truncation sweep

Date: 2026-09-12. Scope: the complete desk frontend; implementation follows the
approved audit in this session.

## Findings and changes

| Surface | Previous issue | Final behavior |
| --- | --- | --- |
| Packs collection | Native title appeared even for a question that fit | Actual-overflow measurement; one-line ellipsis; shared Radix hint only for clipped visible values |
| Very long questions | Unbounded hover text | Short Preview instruction above 320 characters; full description in the touch-accessible Preview |
| Collapsed navigation | Separate tooltip implementation reused 200px menu styling | Shared compact tooltip with the existing accessible link names |
| Pane controls and map zoom | Inconsistent or missing icon hints | Shared neutral hints; existing shortcut definitions in the header |
| Pane divider | Native tooltip and drag/focus interactions | Shared hint suppressed during dragging; existing pointer paint and keyboard resizing retained |
| Breadcrumbs, Admin summaries, file paths | Unconditional native titles | Overflow-only hints, with existing navigation/focus targets preserved |
| Create, Accept, Fix, editor mode | Unavailable reasons hidden in title attributes | Visible help with aria-describedby; existing refusal/validation logic retained |
| Eight shortened digest call sites | Complete values depended on hover | Reusable full-value disclosure, selection, copy and clipboard-failure fallback |
| Evaluation/member digests | Shortened artifact value or redundant title | Full readable values in the details; no redundant hint |
| Graph probe diagnostics | Detail available only by hovering a status | Keyboard/touch disclosure with the runtime's unchanged text |
| Trace conditions | Title repeated the visible value | Redundant native title removed |

All 28 audited native hover-title call sites were removed or replaced. JSX
heading props such as `PageHeader title` and `Dialog title` retain their
meaning. No new dependency or color palette was added. Tooltip/popover layers
are shared tokens, including overlays opened from inside a modal.

## Design references

- [Linear design refresh](https://linear.app/now/behind-the-latest-design-refresh):
  reduced visual weight and a consistent application shell.
- [Linear personalized sidebar](https://linear.app/changelog/2024-12-18-personalized-sidebar):
  restrained, compact navigation.
- [Linear's truncated-label improvement](https://linear.app/changelog/2024-10-10-document-subscriptions):
  full-label tooltips when menu labels are truncated.
- [Linear's tooltip lifecycle fixes](https://linear.app/changelog/2026-01-22-customize-your-navigation-in-linear-mobile):
  avoid stale hints after activation and scrolling.
- [Radix Tooltip](https://www.radix-ui.com/primitives/docs/components/tooltip):
  provider timing, focus/hover activation, Escape/activation dismissal,
  composition and portaled collision handling.
- [WCAG content on hover or focus](https://www.w3.org/WAI/WCAG22/Understanding/content-on-hover-or-focus.html):
  dismissible, hoverable, persistent content.

The styling follows the desk's established tokens. Public Linear references do
not specify exact tooltip timing, dimensions or private implementation tokens.

## Validation

- TypeScript and production bundle build passed.
- 3,022 tests across 123 files passed, including 10 new tooltip/digest cases.
- Existing pane-controls browser gate: 50 checks, zero failures or browser errors.
- Shared tooltip and pack browser gate: 63 checks, zero failures or browser
  errors. Covers hover/focus, existing help, Escape, resize-to-fit, modal layers,
  viewport edges, both themes/densities, long/very long questions, Preview, pane
  drag, touch and a 308-entry virtualized fixture.
- Full responsive containment gate: 435 configurations across 15 routes,
  435 contained, zero failures; the copied project was unchanged by content.

Browser tests use disposable fixture projects; no user project or real assistant
key is needed. The intentionally updated extraction golden changes only native
trace titles and the artifact digest's full-value presentation.

The mutation needle audit retains the baseline's eight stale needles and one
ambiguous needle in unchanged behavior. The stale-write digest needle changed
with the new disclosure, and its test now opens both full values. This sweep
does not claim a clean global mutation harness.


## October 6 follow-up: project editor and remaining native hints

An AST sweep of the frontend found nine native hover-title source sites added
since the original cleanup: fold markers, search actions, condition fields and
operators, pack folders, storage rows/details and the right-pane context label.
These now use shared hints, overflow-only hints, or omit a redundant title when
the complete value is already visible. The generated CodeMirror folded-code
placeholder also uses the shared hint. Heading props, iframe names and document
titles retain their separate meanings. The native-title sweep is now empty.

Editor-owned DOM uses `DOMTooltips` from `ui/Tooltip.tsx`, with the same Radix
provider, timing, portal, typography, collision handling and dismissal behavior
as React controls. Its existing event handlers and focus survive attachment and
cleanup. Fold glyphs are 12px inside 16px hover targets, centered within the
editor line; Comfortable and Compact geometry checks confirm containment.
Escape dismisses a tooltip before a subsequent Escape closes Find.

File switching previously unmounted the current editor immediately and then
seeded the next buffer in an effect, producing an empty/loading render even for
cached reads. Selection now waits for file bytes and the editor module, keeps
the current editor visible and temporarily inert, and commits a keyed editor
initialized from that file's cache. A small header spinner indicates a pending
read. Failed reads retain the current buffer; request ordering discards late
responses from earlier selections. Each editor still owns its file's base
digest and buffer; no previous-file placeholder is reused for a new path.

Validation, in the working copy this follow-up was ported from (the browser
checks were not repeated when it was ported): shared tooltip/focus/lifecycle tests, file switching and retained
buffer tests, folder/condition/storage regressions, design-token checks and the
production build pass. Chromium checks cover hover containment, tooltip
hover/focus/Escape, folding, Find/Replace/undo, read-only behavior, both themes
and densities at 1440/390/320px. A frame-sampled sequence of delayed, failed and
eight cached file switches showed no blank editor or loading-screen frames.
Browser checks intercept file writes and do not invoke an assistant.
