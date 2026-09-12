# Unveil design system

Unveil follows the compact, neutral workspace direction in Linear's
[2026 design refresh](https://linear.app/now/behind-the-latest-design-refresh)
and [personalized sidebar and settings update](https://linear.app/changelog/2024-12-18-personalized-sidebar).
The shared foundations apply to navigation, Admin, documents, editors, matrices,
graphs, dialogs and the Assistant. The [design audit](design-audit.md) records
the issues found and the resulting changes.

## Reference fidelity

The two references show different iterations of Linear. Use the newer neutral
surfaces and quieter navigation with the dedicated settings layout shown in the
2024 update. Do not introduce a different visual theme on each route.

| Foundation | Evidence and implementation |
| --- | --- |
| Font | Linear's [public app](https://linear.app/login) preloads Inter Variable 4.1. Unveil serves the same upstream release locally. |
| Action text and controls | The public app's initial action styles specify 13px text, weight 500, 32px height and 12px horizontal padding. These are the comfortable Button values. |
| Sidebar width | The public app declares 244px. This is Unveil's default; an explicit project width still wins. |
| Corners | Public app styles expose a 4px control radius and 12px main frame. Unveil uses those, with 8px cards and selected navigation items. |
| Neutral surfaces | The public app's initial light/dark styles and the supplied settings screenshots inform the palette below. |
| Brand accents | Judgment Pack green and gold replace the reference app’s indigo. Keep the neutral surfaces and compact geometry. |
| Layout | Settings replace the app navigation with a dedicated sidebar and Back to app. The header spans the pane; the bounded form starts at a fixed gutter. Row actions align to the right. |

These are documented observations, not a claim to possess Linear's private
design system. Screenshot spacing, card treatment, the type hierarchy, and
Unveil's mobile adaptation are reconstructed. The 2024 and 2026 screenshots
also differ in palette. A screenshot cannot establish hover, focus, disabled,
or responsive behavior. Those states are explicitly defined here and checked
in a browser. Primary hover is darker than Linear's public initial hover color
to retain 4.5:1 text contrast.

## One source of truth

| Concern | Owner |
| --- | --- |
| Color, type, spacing, radius, density | `web/src/styles.css` |
| Header, navigation, shared workspace frame, inspector, console | `web/src/shell.css`, `web/src/shell/AppShell.tsx` |
| Theme/density application and list row arithmetic | `web/src/config/theme.ts` |
| Saved appearance preferences | `web/src/shell/appearanceState.ts` |
| Reusable controls and settings groups | `web/src/ui/` |
| Full-width page header and bounded body | `web/src/ui/PageLayout.tsx` |
| Floating details and retained section panels | `web/src/ui/Popover.tsx`, `web/src/ui/RetainedPanel.tsx` |
| Unsaved form aggregation and route protection | `web/src/shell/DraftScope.tsx`, `web/src/shell/useDirtyGuard.ts` |
| Settings navigation placement | `web/src/shell/SettingsNavigation.tsx` |
| Feature composition | The route or feature CSS module |

Account and desk settings contains **System / Light / Dark** and
**Comfortable / Compact**. A viewer's preference overrides the project setting.
Use this existing preference store. Feature code must not add its own palette,
font stack, density override or competing appearance page.

## Typography and geometry

| Token / role | Comfortable | Compact |
| --- | --- | --- |
| `--text-page` | 20px / 500, page title | Same |
| `--text-heading` | 16px / 600, document sections | Same |
| `--text-body` | 14px, body copy; 600 for group headings | Same |
| `--text-label`, `--text-control` | 13px / 500 | Same |
| `--text-sm` | 13px, secondary text | Same |
| `--text-xs` | 12px, helpers and metadata | Same |
| `--density-control`, `--density-form-control` | 32px | 28px |
| `--density-row` | 40px | 32px |
| `--density-field` | 16px between fields | 12px |
| `--density-field-gap` | 6px label/control gap | 4px |
| `--density-section` | 20px between groups / card padding | 16px |
| `--density-block` | 12px within blocks | 9.6px |
| `--density-gutter` | 24px | 20px |
| Narrow viewport gutter | 16px | 16px |
| `--radius-sm`, `--radius`, `--radius-panel` | 4px, 8px, 12px | Same |
| `--measure-form`, `--measure-wide` | 704px, 1152px | Same |

Type uses rem values at the default 16px root. Body line height is 1.5; headings
use 1.3–1.35; controls use a single centered line. Inter's optical sizing stays
on. Monospace is reserved for identifiers, paths, code and runtime output.
Use sentence case. Dense table captions use the existing density font token;
do not shrink all body copy when Compact is selected.

Fixed spacing steps `--space-1` through `--space-7` are 4, 8, 12, 16, 24, 32
and 48px. Use density tokens for dimensions that tighten with the preference.
The parent owns space between components; a child must not add a second bottom
margin to the same gap. Long model names and translated labels may increase
row height. Do not clip text to meet a screenshot measurement.

## Palette

All literal colors live in `styles.css`. Both dark selectors carry the same
palette: the system preference and explicit Dark must render identically.

| Role | Light | Dark |
| --- | --- | --- |
| Page `--bg` | `#f9f9fa` | `#121213` |
| Sidebar `--sidebar` | `#efeff0` | `#09090a` |
| Card `--surface` | `#fefeff` | `#17181a` |
| Menu `--surface-raised` | `#ffffff` | `#222427` |
| Border `--border` | `#e2e2e2` | `#28282c` |
| Primary text `--ink` | `#23252a` | `#e2e3e5` |
| Secondary text `--ink-soft` | `#5b5b5d` | `#a0a0a4` |
| Supporting text `--ink-faint` | `#68686b` | `#97979a` |
| Inactive navigation `--sidebar-ink` | `#626368` | `#97979a` |
| Action fill `--accent-fill` | `#183b3f` | `#346b63` |
| Action label `--ink-inverse` | `#fefeff` | `#fefeff` |
| Hover / pressed fills | `#0f2b2e` / `#102f32` | `#39786e` / `#30635c` |
| Links / focus `--accent` | `#0c5f68` | `#a7c8bc` |
| Neutral selection background `--accent-soft` | `#ededee` | `#222326` |
| Identity badge / letter | `#183b3f` / `#f4c175` | Same |

Separate action fills from link/focus colors: a readable dark-theme link is too
light to serve as a background under white text. Green and gold are the only
chromatic families. Main backgrounds, panels, ordinary cards, field surfaces,
borders, and body text remain neutral. Selected rows, tabs and toggles use normal
`--ink` text/icons over neutral backgrounds; native checkboxes use green.
Organization initials and the user avatar use gold on deep green. Uploaded organization images retain their supplied artwork.

Success uses green. Errors, warnings and unknown verdicts use gold, with explicit
labels and semantic markup preserving their different meanings. False verdicts
remain neutral. Never communicate status through color alone, and never imply
that an active selection is a successful runtime result.

The palette tests measure text/background pairs at 4.5:1 and focus rings at 3:1.
They also reject chromatic tokens outside the green/gold families. They do not
certify complete WCAG conformance: subtle borders are not all 3:1, and assistive
technology and device testing remain separate work.

### Restrained Judgment Pack accents

Use the [Judgment Pack website](https://judgmentpack.org/) as the source. The
brand badge uses its exact `#183b3f` and `#f4c175`; light actions, links and
success colors also come from the site. Adjust dark-theme shades and contrast
where needed. Use only the shared tokens, never page-specific color literals.
The [color audit](reviews/pack-workspace-color-audit.md) records every role and
source adjustment. Selected-control backgrounds stay neutral, matching the
earlier navigation surfaces. Selected text/icons use `--ink`; weight and
underlines identify the active item. Green remains available for focus and
small indicators; large workspace surfaces must not acquire a green or gold wash.

## Components and action placement

- Use `Button` for actions and `ButtonLink` for navigation styled as an action.
  Both share geometry and states. Use `primary`, `secondary`, `quiet`, `danger`.
  Hover changes paint only. Keyboard focus remains visible.
- Shell icon buttons share one neutral state treatment: quiet at rest, `--bg`
  on hover, and `--accent-soft` with `--ink` when pressed or expanded. Selected
  controls never gain an accent border. Left navigation, Inspector, and both
  console controls use this rule; keyboard focus retains the shared accent ring.
- Each write names its scope: **Save API key**, **Save settings**, or a section's
  **Save**. Group-wide saves sit at the bottom right with feedback before them.
  Cancel/undo belongs beside its save. Toolbars wrap on narrow layouts.
- An immediate row action, such as clearing the default project, occupies a
  separate right-aligned action column. Its explanation sits below the value.
  At narrow container widths the row stacks without collisions.
- Keep key saving next to the key. Test connection belongs in the Connection
  group's footer and stays disabled until there is a saved key for the endpoint.
  A typed key is unsaved state. Saving endpoint/model settings is a separate task.
- Removal belongs below the main form in a distinct section, with an explicit
  confirmation. It uses the shared danger button and never sits beside Save as
  an equally prominent alternative.
- `Field` owns the label/control/helper relationship. Preserve its accessibility
  wiring, errors and names. Input, Select and Button share height and corners.
- `SettingsSection` owns its card, heading, padding and optional action footer.
  Its children own internal spacing. Keep diagnostics in disclosures or the
  inspector while leaving actionable failures visible.
- Tables keep headers and cells aligned. Warnings use a status and border
  without repeatedly filling the entire result area. Interactive relationship
  maps start at readable native size. Opening an inspector must preserve their
  viewport; zoom changes only through an explicit canvas gesture or control.

## Workspace frame

`AppShell` groups main, Inspector and console in a stable `.desk-workspace`.
The wrapper owns the complete 1px `--border` boundary, `--radius-panel` (12px)
on all four corners, `--bg` surface, and clipping. Individual panes have square
corners. Inspector uses a left divider; console uses a top divider. Floating
menus, popovers and narrow-screen drawers remain portaled above the frame.

The frame has a `--workspace-edge` gutter of `--space-3` (12px) at the
browser's right edge, including when the Inspector is closed. With the rail
in a drawer, the same inset applies on the left. The right drawer uses that
inset on its top, right and bottom, with one rounded neutral border.

The main/Inspector divider is draggable when docked. `PaneDivider` supplies
pointer capture and a focusable vertical separator with its controlled pane
and live width bounds. Its invisible 12px hit area paints only one 2px line:
neutral on mouse hover or drag, shared accent for keyboard focus. Pointer
interaction clears keyboard paint, even after focus was in a text field;
release/cancel restores the idle boundary once the mouse leaves. Touch does
not acquire a sticky hover. Never outline the hit area or add parallel rails.
Left/Right move the boundary
8px, Shift moves 32px, Home/End reach the current minimum/maximum, and Enter
closes the Inspector and focuses its header toggle. Double-click or Escape
restores the configured default width.

The default remains 360px (or the project's explicit default). Docked widths
range from 320px to 640px, additionally capped at 45% of the workspace and at
what leaves the route's working-width floor. Main keeps at least 480px, or the
larger width its route requests. A divider never drags itself into a drawer:
its current maximum stops it first. When even 320px cannot fit, the shell uses
the drawer, where resizing is unavailable. Viewport clamps do not overwrite
the saved preference, so a chosen width returns when the window grows again.

Only explicit resize gestures store an Inspector width in the existing
per-project shell record. Version 1 collapse choices remain readable; the
next interaction writes version 2 under the same key. Reset panes includes
the width, while divider reset clears only its width override. Preference
changes write no project configuration and preserve route state and canvas
zoom. The keyboard semantics follow the
[WAI-ARIA window splitter pattern](https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/).

The status strip is outside the frame with a fixed `--space-2` (8px) gap in
both densities. It does not supply the workspace's bottom border. With console
closed, main/Inspector meet the rounded bottom edge; with console open, the
console meets it. The outer frame never changes shape during these toggles.

Console caps and sticky-content height calculations account for the gap and
both frame borders. The current capped console track is calculated on the
workspace, where it can read the shell's pane choices. Existing content gutters,
colors and typography remain owned by their usual shared tokens.

[Browser captures](workspace-frame-review.md) show the frame with both panes
open and closed, plus a narrow light-theme example.

## Navigation and responsive behavior

Admin's section links live in the shell sidebar. The selected section has one
page heading; the small `Admin / section` header provides context. Back to app
returns to Packs and restores the app navigation. Settings retain `/admin#section` links.
The fragment addresses the article in the shell so a hard load keeps its header
visible. Standalone Admin retains its inline section navigation.

Below the existing 900px rail breakpoint, settings links move into the navigation
drawer. Choosing a section, including the current section, closes the drawer.
Opening or closing navigation does not remount the form or discard its draft.
The inspector and console keep their existing responsive behavior.

Pages using `data-layout="page"` opt out of the shell's outer padding. Compose
`PageHeader` and `PageBody` inside the route's full-width article. The header has
a 48px minimum height, spans the pane and stays visible while the form scrolls.
Its divider has no reading-width cap. The body starts 24px from the pane edge
(20px in Compact; 16px on narrow viewports), with the same vertical inset and a
704px maximum form width. Resizing the window or opening the inspector must not
center or shift this left edge. Narrow headings/actions may wrap without clipping.

`Popover` hosts transient runtime details in a portal, with collision handling,
Escape/outside dismissal and focus restoration. Expanding it must move neither
the header nor the form. Copy reports success only after the clipboard answers.
The bottom status strip is the shell's persistent connection indicator; the
header does not duplicate it. The Admin popover adds diagnostics and connection
notices continue to surface failures in the main area.

Use `RetainedPanel` for settings sections: mount on first visit, hide inactive
sections from layout and accessibility, and retain their drafts and stale-write
revision guards. `DraftScope` aggregates only dirty flags and uses the existing
route/reload protection. It never stores field values or API keys. Typed keys
stay only in their existing uncontrolled input until saved, canceled or the
Admin page unmounts. A hidden dirty section must still protect leaving Admin.
Saves are disabled when the configured values are unchanged; undoing an edit
returns the form to clean. Sidebar exceptions use a compact indicator with
accessible status text; the selected section and inspector provide full details.
Explicit pane sizes and the viewer's saved app-rail mode remain respected on return to the app.

## Font distribution and live reference

`web/public/fonts/InterVariable.woff2` is the unmodified
[Inter v4.1 variable font](https://github.com/rsms/inter/tree/v4.1), distributed
under the included [SIL Open Font License](../web/public/fonts/OFL.txt).
It is preloaded in the app and development reference, served locally, and uses
`font-display: swap` with a system fallback. No external font request is needed.

Run `npm --prefix web run dev` and open `/design-system.html`. It renders the
production Button, Field, Input, Select, TextArea and SettingsSection components.
Theme and density switches apply only to that reference page; sample actions
have no side effects. The production entry remains `index.html`.

`main.tsx` imports `styles.css` before `shell.css`; `@layer base, shell` orders
base and shell rules. UI and feature modules stay unlayered. Extend existing
tokens and components instead of increasing descendant specificity.

Build and run the component suite after changing shared components. Run
`scripts/containment-check.sh` before merging stylesheet changes. Inspect
populated pages, empty/loading/error states, both themes, both densities, a
dialog, and narrow layouts. Source checks alone cannot establish visual quality.


## Pack collections

Use the shared compact `PageHeader` for the collection name, subdued count and
Create pack action. The main pane holds the collection at full width; it does
not reserve an empty document preview beside a narrow list. Search and the
explicit Pack ID sort share a wrapping control row. Keep native Tab behavior
for these input controls and reuse `Input`, `Select`, `Button` and `ButtonLink`.

Rows use `--text-control` (13px), `--text-xs` metadata, and the existing 40/32px
density heights. Their headers reserve the same scrollbar gutter as the list.
The collection uses fixed 24/20px gutters and 16px on phones; selection and hover
stay neutral. Available descriptions share the name line and collapse on phones.
Do not add columns for data the runtime has not supplied.

Pack names navigate to Overview. A separate Preview action publishes inventory
metadata into the shared Inspector without fetching documents or evaluating.
Preserve intentional pane preferences and the existing closed default. A modal
preview closes when its Open pack action navigates to the document. Retain
collection controls and scroll across document navigation, and release its
Inspector publication while the document is active. Window all results; no
extra Show all step is needed. Short viewports keep at least three reachable
rows, allowing main to scroll when the console consumes the remaining height.

## Pack workspace

Creation lives at `/create-pack`, outside the pack-ID route so a pack named `new` remains addressable. It uses `PageHeader`, `PageBody`, `FieldGroup`, the shared controls, and the existing byte-preserving document editors. Basics → Build → Review holds one draft. AI is offered only with an available endpoint/key, enabled model, and runtime authoring prompt; accepted suggestions become editable drafts, with declared unknowns retained for review. Neither route navigation nor tab changes silently save a draft.

The reading page starts with Overview. Its compact `PageHeader` title variant
contains the pack name, decision question, primary Test pack action, and the
Overview / Logic / Tests navigation. One divider spans the pane; metadata and
successful validation details live under More. Material validation issues stay
visible. The title uses `--text-page`, the question `--text-sm`, and the shared
control height and gutters. Narrow layouts move Edit into More and wrap the
title/question. The header scrolls with the page so long names and enlarged
text cannot consume the entire work area.

Logic offers Map and List over one `logicModel` projection. The grouped map
uses the lazy-loaded `RelationshipMap` React Flow adapter; nodes express
declared relationships within one pack, not first-match priority or the
runtime composition graph's execution order. Node surfaces and selection stay
neutral. Edges use `--ink-faint` because a meaningful connection needs stronger
contrast than a decorative panel border. Normal node text remains 13px at the
initial 100% zoom; inspector changes never run Fit view.

List keeps rules and exceptions visible, with compact controls for context,
resolution and references. Search spans all declared groups. In Map, typing
does not open a modal: submitting the search opens Outline. The right slot
holds either Outline or selected-item details. Exact conditions reuse
`ConditionTree`, which wraps in every reading context; Inspector copies have
no document selection targets. Author prose and raw JSON are separate disclosures. Full document and editing remain reachable under More
and retain the original pointer address space, ordering and extensions.

Selection, outline/list scroll, and map viewport belong to the route, above
pane remounts. An explicit Map/List choice is remembered; without a preference,
desktop starts with Map and narrow layouts with List. Resizing adapts the
inspector without changing the selected view. The shell's optional
`requestWorkingWidth` contract measures the entire workspace, avoiding a
feedback loop in which opening the inspector changes its own breakpoint input.
Map requests 49rem including content and scrollbar gutters; List requests 34rem. Existing project
pane widths still apply. Drawer dismissal restores focus to the inspection
gesture when it remains mounted, with the header toggle as fallback.

Invalid carriers are shown as original text. A complete map requires a current
valid runtime check; unsupported definitions remain inspectable in List with
the runtime diagnostics. Trace overlays require the same pack and exact
submitted bytes. Unknown, skipped, suppressed and unreported observations
remain distinct from false. A declared outcome and a requested handoff are
separate fields.

The main area holds inputs and results. The right pane holds contextual guidance or inspection. The bottom Activity channel records bounded session milestones without retaining prompts, credentials, facts or evidence. Important errors remain in the main area. Opening a guide is an explicit action, and operations do not open panes automatically.

Testing distinguishes exploratory outcomes from saved-case pass/fail comparisons. A completed request does not overwrite input edits made while it was in flight, and a result from another pack is never attached to the current page.

When the saved document is available, Tests submits that exact loaded snapshot
to evaluation. Explain on map explicitly publishes one result per pack into
the query client's memory, with its submitted bytes. It writes neither facts
nor traces to browser storage. Returning to Tests restores that selected run;
a missing or different revision blocks the overlay instead of presenting an
old observation on new conditions. Runs made without captured pack bytes
remain readable in Tests but cannot offer a bound map explanation.


## Tooltips, truncation and full values

Use `ui/Tooltip.tsx` for brief supplemental hints. `TooltipProvider` is mounted
once by the shell: 300ms hover delay, 300ms skip delay between controls,
immediate keyboard focus, Escape/click dismissal, and navigation cleanup.
These timings are desk choices, not claims about Linear's private tokens.
The content uses the existing neutral raised surface, border, shadow, 12px type,
4px radius, 8px/12px padding and a 20rem maximum width. It portals above dialogs,
collides within the viewport, and remains hoverable. No native HTML `title`
attributes for hover help; component heading props and SVG accessible titles
have different meanings and are retained.

`Tooltip` wraps one existing control with Radix `asChild`. The control retains
its accessible name, ref, action, keyboard behavior and existing
`aria-describedby` references. Labels never depend on the tooltip. Do not put
links, buttons or other interactive content inside a tooltip. Use `Popover`
for selectable/copyable details. For a control automatically focused when a
dialog opens, use `openOnFocus={false}` to avoid an unsolicited hint consuming
Escape; its accessible name still identifies the control.

`OverflowTooltip` measures actual rendered overflow, including descendant text
when a row has multiple cells. Hidden text and text that fits produce no hint.
Resize, content changes, font loading, theme and density changes remeasure it.
Use existing row links/buttons as keyboard targets; do not add a tab stop for
each static label. Full content remains in the DOM and in the destination.

Pack collection rows retain single-line ellipsis and their existing height.
The hint reveals only clipped visible values; names, questions and versions
that fit remain quiet. Beyond 320 characters, a short hint directs the reader
to Preview. Preview wraps the entire description without truncation and works
on touch screens, where collection descriptions may be hidden. Selection and
selected text remain neutral. Full pack-page questions continue to wrap.

Unavailable-action explanations belong in visible help associated with
`aria-describedby`, including Create, Accept, Fix and unavailable editor modes.
Do not attach essential explanations only to a disabled button's hover state.
`Digest` offers full selectable/copyable values in a shared popover, and keeps
an absent file distinct from a real digest. Graph probe explanations use a
native disclosure, so they remain available on touch and keyboard. Already
visible trace values and full digests do not need a redundant tooltip.

Live examples: `/design-system.html`. Browser regression entry points:
`scripts/tooltip-check.mjs` and `scripts/pane-controls-check.mjs`. See
[the tooltip sweep](reviews/tooltip-sweep.md) for scope, references and results.


## Reading, selection and exact values

A selected document block uses the existing neutral `--accent-soft` background
and `--radius-sm`, preserving its text color and dimensions. Selection is not
keyboard focus: `:focus-visible` keeps the shared green outline. Dragging a text
range to copy it must not move the Inspector selection. A displayed copy in the
Inspector has no duplicate document IDs, roving tab stops or selection paint.
Shared tabs mark their active item with a neutral `--ink` underline.

`ConditionTree` wraps by default in Full document, rules, exceptions and the
Inspector. Prefer a break between complete array operands; arrays exceeding
three entries or 80 serialized characters stack vertically. Operators, quoted
strings, numeric/boolean/null values, order and pointer addresses remain exact.
Unbroken values may wrap anywhere. This is a presentation rule, not a rewrite
of the document. The JSON disclosure retains the complete machine representation.

`MemberValue` renders strings as prose and scalars explicitly. Structured values
remain complete JSON. `CodeBlock` owns the shared 12px monospace source size,
soft wrapping, optional horizontal scrolling, and Copy feedback. Its nested
`code` inherits that size. Copy uses the full input text regardless of wrapping;
clipboard failures leave selectable text and explain the fallback. Raw editing
with line numbers still uses `CodeArea` and its existing unwrapped byte buffer.

The Inspector has one mode row, followed by the selected-item heading and
References, Checks and Provenance disclosures. Failed, unavailable, pending,
truncated and stale checks open visibly; dirty/revision warnings remain outside
collapsed provenance. The three-digest binding rule is unchanged. Logic details
reuse this metadata without repeating the selected member or another tab row.

Full document has an explicit active navigation label. Its **On this page**
popover retains counts, omitted-member links, current-section feedback and
pointer-addressed navigation. Ordinary successful check provenance is compact;
warnings and diagnostics keep their existing visible presentation. All document
members remain available in their original order.

Use `Button variant="inline"` for actions occupying a metadata value column:
its text aligns with plain values and can wrap. Keep ordinary quiet buttons in
action groups, where their padding provides separation.

See [the reading and selection sweep](reviews/reading-selection-sweep.md).
