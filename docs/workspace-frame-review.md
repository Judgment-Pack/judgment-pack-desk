# Shared workspace frame

The main pane previously rounded only its top corners and delegated its bottom
border to the status strip. This made the bottom edge appear cut off. Opening
the console or Inspector also joined independently framed surfaces.

The shared `AppShell` now owns a stable workspace container around all three
panes. Its complete 1px border and 12px corners remain visible whether either
pane is open or closed. An 8px gap separates the status strip below it. Internal
pane boundaries stay straight and scrolling remains independent.

| State | Capture |
| --- | --- |
| Console and Inspector closed | [Full frame](design/workspace-frame-closed.png) |
| Console and Inspector open | [Joined panes](design/workspace-frame-open.png) |
| Console open on a narrow screen | [Light, 390px](design/workspace-frame-narrow.png) |

All dimensions, surfaces and text use the existing [design system](design-system.md).
The wrapper stays mounted during pane changes. Console height caps reserve the
new gap and border, and sticky route panels receive the actual remaining main
height. Drawers and popovers keep their existing portals and dismissal behavior.

The focused Chromium review checked 40 combinations of viewport (320, 390,
900, 1100, 1440px), light/dark theme and console/Inspector state. It also checked
260, 160 and 109px-high viewports, physical corner clipping, status-strip
visibility, and preservation of the same input node and unsaved draft during
pane toggles. No browser errors or document overflow were observed. Fixtures
were copied and no provider request or user configuration write was made.

The full repository containment gate now checks the shared border, four corners,
clipping, pane bounds, lower pane edge and status gap on every sampled route and
pane state. Final results are recorded in the PR before merge. Screenshot review
used Chromium on Linux; it does not establish cross-browser or screen-reader
conformance.
