# Admin layout review

Follow-up to the [workspace audit](design-audit.md), against baseline `f20882a`.
The design direction remains the [shared Linear-inspired system](design-system.md).
This pass fixes layout movement and settings state, without introducing another
font, palette, scale or appearance preference store.

## Findings and results

| Finding | Before | Result |
| --- | --- | --- |
| Expanding runtime details moves the page | Section moved 51.5px on desktop; up to 127.4px on a narrow viewport. | Shared portaled `Popover`; measured movement is 0px at all six sampled widths. Escape and outside click dismiss it. Focus returns to its trigger; Copy details reports the clipboard result. |
| Divider stops growing | At 1908px, the pane was 1664px wide but the header only 1104px. | Shared `PageHeader` spans the pane's client width, including the divider. The scrollbar retains its own gutter. |
| Form position changes with available width | Centering changed the left inset from 25px to 225px, including the pane border. Opening Inspector moved it another 166.5px. | Shared `PageBody` starts at a fixed 24px comfortable / 20px compact gutter, 16px narrow, and caps the form at 704px. Inspector opening moves this left edge 0px. |
| Header disappears during long settings forms | Scrolling to Assistant Save hid the header. | Sticky 48px minimum page header with `Admin / current section`. Narrow content can wrap without truncation. |
| Changing settings section loses edits | Organization name was discarded after visiting Storage and returning. | Shared `RetainedPanel` mounts on first visit and retains drafts, revision guards and DOM-only key inputs. Hidden panels leave layout and the accessibility tree. |
| Leaving Admin loses hidden edits | No unsaved-settings protection. | Shared `DraftScope` aggregates dirty flags and reuses the application's route/reload guard. Canceling departure preserves every section's draft. No credential or draft values enter the registry. |
| Assistant Save is active when unchanged | A clean configured form could submit another write. | Save is disabled when values are unchanged; editing and reverting returns it to disabled. Initial endpoint setup remains available. |
| Exceptional sidebar status changes row height | Detailed status text wraps inside navigation rows. | Compact indicator with accessible text; full details remain in the selected section and Inspector. |
| Identity provider is a dead end | Only `Provider: None`. | A short local-session explanation and a working link to local-access guidance. Configured providers remain read-only; the page does not imply sign-in is available. |

## Reusable implementation

- `ui/PageLayout.tsx`: independent page chrome and bounded body, using shared
  typography, gutters, measures and surfaces.
- `ui/Popover.tsx`: Radix positioning, collision handling, accessible naming,
  dismissal and focus restoration, dressed with the common surface tokens.
- `ui/RetainedPanel.tsx`: lazy mounting and retention for section drafts.
- `shell/DraftScope.tsx`: one route guard for independently saved forms; only
  booleans are published by the existing forms.
- `styles.css`: shared layer order joins the existing foundation tokens.
- `/design-system.html`: live page-layout, popover and retained-draft examples.

The existing provider validation, key handling, write preconditions and
section-specific saves remain in their existing owners. Project-file forms keep
their pinned revisions across section visits; retaining only a copied draft
would have lost those protections.

## Browser evidence

| Screen | Capture |
| --- | --- |
| Full-width header and fixed form alignment | [Desktop, dark](design/admin-fixed-layout-dark.png) |
| Shared light theme | [Project, light](design/admin-fixed-layout-light.png) |
| Narrow wrapping and gutter | [320px viewport](design/admin-fixed-layout-narrow.png) |
| Floating runtime details | [Popover](design/admin-runtime-popover.png) |
| Assistant using the same page layout | [Assistant](design/admin-retained-assistant.png) |

Captures use a copied demo project and placeholder credentials. No model request
was made. Chromium measured widths 1908, 1440, 1100, 900, 390 and 320px, plus
Inspector open/closed at 1440px. All had 0px document horizontal overflow and
0px movement when opening the runtime popover. The header stayed at the same
vertical position when scrolling to Assistant Save. Organization and typed-key
drafts survived section navigation; canceling departure retained the form.

Both light/dark themes and comfortable/compact densities rendered Inter
Variable, 20px section headings, 32/28px controls and the documented gutters.
The narrow 320px header wraps its actions; its height stays unchanged when the
popover opens. This is deliberate accommodation of available space.

## Validation and limits

Production build, all 2,922 frontend tests and `go test ./...` pass. The three
mutation cases affected by the header refactor were repaired and each was
caught by the focused Admin suite. The repository's stylesheet convention and
palette checks pass. The final real-browser containment gate is recorded in
the PR before merge.

Runtime diagnostics remain accessible from Admin; the global connection badge
retains its existing status role. Consolidating connection status across the
entire application is separate work. Drafts persist while Admin is mounted,
not after accepting a discard or reloading. The existing browser confirmation
is used consistently with the editors. Browser verification used Chromium on
Linux, not screen readers, physical touch devices or other browser engines.
