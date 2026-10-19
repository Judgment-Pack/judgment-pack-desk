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
| Sidebar width | The public app declares 244px. Unveil uses a more compact 220px default; an explicit project width still wins. |
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
| `--radius-pill` | 999px, circular indicators and slider ends | Same |
| `--measure-form`, `--measure-wide` | 704px, 1152px | Same |

Type uses rem values at the default 16px root. Body line height is 1.5; headings
use 1.3–1.35. Action buttons use a single centered line; inspection rows and
disclosure labels align left and wrap as needed. Inter's optical sizing stays
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
  Use `InspectionRow` to open details of displayed information and `Disclosure`
  to expand content in place; neither is styled as a command to execute the value.
  Hover changes paint only. Keyboard focus remains visible.
- Shell icon buttons share one neutral state treatment: quiet at rest, `--bg`
  on hover, and `--accent-soft` with `--ink` when pressed or expanded. Selected
  controls never gain an accent border. Use `PaneToggle` for main navigation,
  folders and contextual right panes; mirror its panel icon for the right side.
  Keep navigation before the brand, folders at the workspace's upper-left corner,
  and right-pane controls together at the header's end. Keyboard focus retains
  the shared accent ring. Diagnostics belongs in Help & About and a contextual
  right pane; do not restore global Inspector or bottom Console toggles.
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
closes the Inspector and focuses its contextual opener. Double-click or Escape
restores the configured default width; Escape during an active pointer drag
restores the width at the beginning of that gesture.

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
both densities. It does not supply the workspace's bottom border. Main and the
contextual right pane meet the rounded bottom edge. The outer frame never changes
shape when the pane opens. The bottom Console has been retired; diagnostics use
the right pane. Content gutters, colors and typography remain owned by their
usual shared tokens.

[Browser captures](workspace-frame-review.md) show the frame with both panes
open and closed, plus a narrow light-theme example.

## Pack workspace spacing

Test-design messages belong to the existing linked chat session. Tests pages must
not mount a second assistant, transcript, composer, or a “Back to chat” switch.
Design with AI and Design missing tests reveal that session and add the request
to its composer, preserving unsent text and attachments. The same chat tools read
runtime test guidance and retain validated case proposals; questions stay normal
conversation. A compact Review cases action opens the shared case review while
keeping the session visible. Saved proposals remain in Related work.

Test suites keep proposals, cases and run history. Migrate legacy conversation
rows only after the chat write is acknowledged. Refresh restores the same session.
Main chat and page panes use the same chat model, attachment, question, send/stop,
and history controls, with no separate test-generation mode.
Pack action menu commands use the shared icon column; filters retain selection
indicators. Test results inherit the bottom panel's gutter and scrolling, with
one title and compact metadata. Expected and actual results share a row when
space permits and stack in narrow panels.

Library and saved pack pages use `LibraryLocation` for their folder toggle and
breadcrumbs: one 24px toggle slot, a 4px gap, and one parent-owned gutter. The expanded
folder header uses that same gutter and height so the toggle never shifts.

Saved and draft pack views use `--density-gutter` for the title, section tabs,
toolbar controls and reading content. A canvas can fill the available pane;
its controls and status text still keep that gutter. The single-line folder
location remains above the saved pack title across Overview, Logic and Tests.

Logic keeps one toolbar beneath its section tabs in both List and Map. Draft
Logic uses this same row instead of placing controls beside the tab list.
The search field wraps below the other controls when the pane is narrow.
Only the list content scrolls, and its scroll position is retained separately
from canvas pan, zoom and node positions. The decision question is shown in
Overview; Logic uses the space for its controls and content.

Document tabs use the shared Tabs page variant. The tab row owns its gutter;
individual panels own their body padding. Pack and graph Overview share the bounded reading measure, in drafts and saved definitions. Details content uses the same density
gutter as the test editor, without an additional top margin on its first heading.
Draft header actions wrap beneath the title when they cannot fit alongside it.

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

Project and account dropdowns share a 22rem preferred width, clamped to Radix's
available viewport width with 16px collision padding. Long paths and prose wrap;
menu height is capped to the available space and scrolls vertically when needed.
The local account trigger shows the avatar, display name and chevron without a
redundant local badge. Identity information remains available inside the menu.

Admin sidebar group headings use 13px, weight 600, and `--ink` to distinguish
them from the regular navigation links. Notes beneath settings rows start after
`--density-block`; a row divider must not touch the explanatory copy.

Admin groups pages by task. **Workspace** contains General, Assistant, Research,
Storage & backups, and Decision safeguards. **Connections & access** contains
Connections, Document processing, and Sign-in & access. Each section states
whether changes apply to the current desk or are shared on this computer.
Connections uses AI, Files & apps, and Web search tabs; shared credentials and
provider defaults live there. Assistant and Research hold per-desk preferences.
Technical configuration and runtime details open from the header's Details menu.
Admin page titles come from `SettingsSection` in its standalone form (20px,
weight 500); nested groups use 14px, weight 600. `SettingsStack` owns the gap and
single separator between groups. `FieldGroup` owns field spacing, including
composite logo/favicon fields. Section content starts 12px below group headers
and 20px below page headers at Comfortable density; Compact uses the matching
density tokens. Labels and controls stay 13px, and field hints stay 12px.
All advanced settings use `Disclosure`. Settings tabs add no second horizontal
inset. Save actions align to the right, with the primary save last.

Connections tabs use manual keyboard activation so restoring focus after a
canceled unsaved-changes prompt does not attempt the navigation again.

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


## Menus

Use `ui/Menu` for dropdown commands, radio/checkbox choices, nested menus,
labels and separators. It wraps Radix behavior; callers keep their existing
triggers, links, selection handlers and dialog focus handoffs. Use `icon`,
`description` and `shortcut` props instead of styling SVGs or checkmarks in
individual features. `asChild` preserves navigation links and their semantics.

`MenuChoice` is the shared toolbar picker for type/status filters and Sort;
form fields retain `Select`. The compact View menu uses the same radio rows.

Every row reserves a 16px icon/check slot and an 8px gap, including commands
without an icon. Rows follow the density control height, 13px text and shared
surface/focus tokens. Descriptions wrap below their label. Radio checkmarks
and submenu chevrons come from the component. Menu surfaces scroll within the
available viewport. Desk/account, collection, folder, attachment, history,
Admin and editor menus use the same foundation; feature CSS controls only
content-specific widths and scrolling. Use native `disabled` for unavailable
commands; an explained `aria-disabled` command can retain focus for a tooltip,
but the shared item prevents its activation. Destructive actions retain their
existing confirmation flow.

## Pack collections

Use `PageHeader`'s collection variant: one sticky 48px row with the folder
toggle, location breadcrumb and visible item count on the left, and collection
options and Create on the right. The Library heading remains available
to assistive technology without another visible row. The breadcrumb is the
navigation; do not repeat a Browse tab or another location row below it.
Counts observe existing inventory without decorative runtime requests.
Filtered counts appear in the header as “1 of 2”. Keep native Tab behavior.

Rows use `--text-control` (13px), `--text-xs` metadata, and the existing 40/32px
density heights. Names, descriptions and versions align in columns; headers and
rows reserve the same scrollbar gutter. The collection uses fixed 24/20px gutters
and 16px on phones; selection and hover stay neutral. Container queries respond
to actual pane width, including divider changes: descriptions move to preview
below 44rem. The compact Version column remains visible for draft state. A runtime refusal retains a visible warning
marker and an accessible explanation even when its description column is hidden.
Do not add fields the inventory has not supplied.

Rows navigate to Overview. Their trailing 32px preview control has an accessible
name and a Radix tooltip. Space toggles preview for a focused row (held repeats
are ignored); Up/Down, Home and End move through the complete virtualized list
and update an already-open preview. Escape closes preview and restores the latest
inspection trigger; menus and the focused splitter retain their own Escape
behavior. Splitter keyboard focus suppresses its visual tooltip so Escape can
reset width; the control keeps its accessible instructions.

The collection owns a temporary `InspectorPresentation`, initially closed, with
a 360px default, 320px minimum and 420px maximum. Main retains 720px; if these
cannot fit, the existing drawer is used. This does not rewrite the document
Inspector's open state or saved width. The collection's own preview selection,
open state and width survive a visit to a pack; releasing the presentation
restores the document's Inspector. No preview state is persisted across reloads
or shared between projects. A manually opened empty preview uses one muted hint.

Preview reads only inventory metadata. Its content starts with the identity and
full supplied description, then version and saved-case availability; paths and
technical IDs live under Technical details. There is one Open pack action. It
fetches no document, runs no test and evaluates nothing. Retain list controls
and scroll across document navigation; filtering out a selected pack or losing
the inventory clears its preview. Window all results, with no Show all step.
Short viewports retain at least three reachable rows, allowing main to scroll
when the console consumes the remaining height.

## Pack workspace

Creation lives at `/create-pack`, outside the pack-ID route so a pack named `new` remains addressable. It uses `PageHeader`, `PageBody`, `FieldGroup`, the shared controls, and the existing byte-preserving document editors. Basics → Build → Review holds one draft. AI is offered only with an available endpoint/key, enabled model, and runtime authoring prompt; accepted suggestions become editable drafts, with declared unknowns retained for review. Neither route navigation nor tab changes silently save a draft.

The reading page starts with Overview. Its compact `PageHeader` title variant
contains the pack name and Overview / Logic / Sources / Tests / Review / Usage
navigation. Tests owns Run tests; finalization remains an explicit reviewed
action. One divider spans the pane; metadata and successful validation
details live in Review. Material validation issues stay visible. The title
uses the same 13px/600 styling and 48px row as Folders and Assistant; the
decision question uses `--text-sm` in the scrolling body. Long titles truncate
with the shared overflow tooltip. Below 32rem, actions wrap onto a separate row
while the title and pane toggle retain their position. The header stays outside
that scroll region. Narrow layouts move Edit into More.

Logic offers Diagram and List over one `logicModel` projection. List uses
compact rows; selecting an item opens its complete conditions, source references
and exact definition in the bottom panel. Logic display options controls inline
conditions and diagram grouping. Scope, evidence requirements, contributed
outcomes, unknown behavior, fallback and handoff remain available without
opening or replacing chat. The existing Diagram/List preference still wins.
Rules use full-width rows at every width.
Long conditions wrap without clipping or horizontal scrolling. `LogicDetails`
shares reading content between representations; `ConditionTree` preserves
operators, operand types, order, nested logic and unrecognized values. Structured reading separates the field, comparison and exact operands. Arrays
show one complete quoted/typed value per row, with no surrounding JSON punctuation.
Readable field labels lead each condition, followed by its comparison and exact value.
Equality uses a compact = with an accessible operator name. Exact paths and the
complete selected definition are in a collapsed Technical details disclosure.
Changing selection closes this disclosure; switching tools preserves it.
Evidence uses one full-width row per item with requirement/type below its name.
The scope/evidence columns and rule cards stack when the Logic container is under
54rem; the evidence list never nests a second two-column grid.

The lazy-loaded `RelationshipMap` React Flow adapter shows individual rules,
special cases and outcomes. `logicGraph` derives edges from exact declared
outcome IDs and exclusion targets. Documentary evidence/source references are
not execution edges. Source references, scope/evidence and fallback/handoff
remain in the reading area around the canvas. Rules contribute independently;
map placement does not set first-match priority or imply runtime composition.
Node surfaces and selection stay neutral; edges use `--ink-faint`. Node heights
are measured and columns reflow without overlaps. Inspector changes never run
Fit view or reduce the reader's chosen zoom.

One shared Display popover remembers condition visibility and optional grouping
of map rules by outcome. Group cards name their members, show accurate counts,
and expand directly on the canvas. Rules targeted by an exclusion stay
individual so an edge cannot suggest the whole group is excluded. Detailed
conditions are the default; compact mode is an explicit reader preference.

Search spans all declared groups. List filters matching items; Map highlights
matches without moving the viewport on each keystroke. Enter / Next match
reveals and focuses the next matching item without opening the Inspector.
Matching conditions are readable even when compact display is selected.

The Logic toolbar sticks within the main PageBody scroll region. List uses
that one scroll owner; Map has a bounded pan/zoom canvas. Selection, List scroll,
display preferences and map viewport belong to the route. Switching representations
preserves the Inspector's current tab and open/closed preference. A searchable
`PackJumpTo` popover navigates to items in the main view, clears a conflicting
filter and closes with focus on the destination. It expands a grouped map target
before focusing the measured node. Escape returns focus to the trigger.

Map node clicks/Enter/Space select without opening the Inspector or changing
its Assistant tab. A separate native View details button opens supporting details;
group actions expand in the main canvas. `InspectionRow` remains the explicit
detail action for reading rows. Deep links still open their addressed Inspector.

Alongside detailed Logic, the Inspector omits already-visible conditions, outcome
contributions, unknown handling and primary definitions. It adds author reasoning,
references, checks, provenance and exact JSON. Compact reading and Overview
inspection retain the condition definition where the main view does not show it.
With no selection it shows pack metadata, without a duplicate Outline.

Pack and graph Overview share `DefinitionOverview`: authored purpose, a compact
result section and one collapsed Details disclosure. The same reading width,
top gutter, typography and 24px section spacing apply to drafts and saved
versions. Do not add rule lists, source cards, raw JSON or findings beneath the
summary. Use whitespace instead of a border between every section.

Pack Overview shows the decision question, distinct authored context and at
most four outcome labels. Keep the fallback visible and mark it once; additional
outcomes are available in Logic. Scope, evidence requirements and a declared
human handoff live under Details. Graph Overview names the declared result step
and counts steps separately from distinct referenced packs. Referenced packs
and connection counts live under Details. It does not infer execution order.

Logic retains the React Flow diagram, list and source references. Sources owns
source browsing; Review owns check findings and the optional Brief; Usage owns
references to graphs and jobs. Keep concise check status in the document header.
`View JSON` in the shared options menu opens the same wide, read-only dialog for
all four views. It displays exact retained bytes with wrapping and both scroll
axes available; it never formats, writes or runs the document. Escape restores
focus to the menu trigger. Graph's Source editor remains available for editing.

Missing, unreadable and loading declarations must not become invented zero
counts or a successful result. The overview does not run checks or models.

The shell's `requestWorkingWidth` contract measures the entire workspace,
avoiding breakpoint feedback loops. Map requests 51rem and List 34rem, while
existing project pane widths still apply. Drawer dismissal restores focus to
the inspection gesture when mounted, with the header toggle as fallback.

These choices apply Linear's [content hierarchy](https://linear.app/now/behind-the-latest-design-refresh),
[persistent display options](https://linear.app/docs/display-options), and
[overview/detail sidebar](https://linear.app/docs/project-overview) principles
to pack reading; they are not claims about a private Linear component library.

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

For DOM controls owned by an embedded editor, use `DOMTooltips` from the same
module and mark the existing element with `data-tooltip`. It binds the shared
Radix trigger to that element, preserving its listeners, focus, accessible
help and keyboard behavior. It follows added/removed controls and cleans up
when the editor closes. Do not create a React root per gutter marker or add
native `title` attributes. Editor shortcuts must respect `defaultPrevented`,
so Escape dismisses an open hint before closing Find.

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
selected text remain neutral. Pack questions live in the scrolling body. A two-line preview offers Read full
question when measured text overflows; expansion keeps the header compact.

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
remain complete JSON. `CodeBlock` presents valid JSON objects and arrays with two-space indentation
using the shared lossless formatter. Wrap only breaks long lines; turning it off
keeps JSON indentation. Copy preserves the original source, including numeric
precision, duplicate keys and escape spelling. Invalid, incomplete or oversized
JSON stays as received. Formatting never changes saved files or chat context.
`CodeBlock` owns the shared 12px monospace source size,
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

Use `InspectionRow` for conditions, evidence, outcomes, source references and
Inspector member lists. Its trailing chevron is visible without hover. The
entire row is one native button; the accessible name starts with View and
includes the visible subject. Outcome labels are author content, never commands
to execute a decision. Current items use `aria-current` and the shared neutral
selection fill, with unchanged text color and geometry. This is not an on/off
toggle. Long labels and secondary values wrap within the row.

Use `Disclosure` for author descriptions, technical details, file information,
references and checks in the Inspector. Its leading chevron rotates when open;
its native summary owns keyboard/touch interaction. All instances share padding,
hover and focus states, including controlled open state for attention messages.

Keep the neutral active underline on view navigation and ordinary links inside
prose. Reserve `Button variant="inline"` for controls embedded in prose, such
as expanding a long question. Secondary navigation, such as View logic, uses a
quiet link beside the section heading. Commands such as Save and Retry connection
use the shared Button; legacy global button/link-button classes are retired.
Do not use tooltips as the only clue that information can be inspected.

See [the reading and selection sweep](reviews/reading-selection-sweep.md).


## Relationship-map interaction

`RelationshipMap` owns the React Flow integration. Fixed nodes use the documented
`nopan` utility class, a pointer cursor and a route-controlled current state.
Focusable node groups select on click, tap, Enter or Space. A separate native
button opens supporting details or expands an aggregate group.
Dragging the empty canvas pans; inspecting a visible node does not reset the
viewport. The attribution badge is disabled through `proOptions.hideAttribution`.

Single-item nodes select their real document pointer. Multi-item and empty nodes
select a view group via `?group=`, replacing `?at=`. Their Inspector shows only
that group’s entries. Choosing an entry clears the group and selects its real
`?at=` pointer. This keeps aggregate nodes such as Resolution separate from the
JPS document address space. Group selection survives pane/drawer changes and
reloads, and uses the existing neutral selected-node treatment.

The integration follows React Flow’s
[interaction props](https://reactflow.dev/api-reference/react-flow) and
[attribution option](https://reactflow.dev/api-reference/types/pro-options).


## Plain-language pack labels and contextual help

`packs/terminology.ts` owns display labels, concept descriptions and explicit
value mappings. Use these consistently across Overview, Logic, document reading,
Inspector, forms and trace observations. Stored JSON, pointers, IDs, authored
labels and operand types remain exact. Unknown keys and values retain their
spelling. Never infer a friendly alias for arbitrary author data or merge
unknown, false, skipped and unreported states.

Use a clear heading and one visible sentence for an essential concept. Graph
nodes use the same names and open their group's content in Inspector. Complex
settings such as unknown-condition policy and fallback use `InfoHelp`: a neutral
info button beside the label, backed by the shared Radix `Popover`. Explanations
with examples support click/tap, keyboard focus, Escape and focus restoration.
They do not belong inside a tooltip or require hovering over ordinary prose.
Existing `Tooltip` remains for optional action hints and shortcuts.

`ExpandableText` displays a measured two-line preview of long questions, with
an explicit keyboard/touch control for the full text. It changes only the body;
pack titles and actions stay in the header. Full titles remain in More details.

## Pinned pane headers

`data-layout="page"` declares the shared column layout: `PageHeader` does not
scroll and `PageBody` (`data-page-scroll`) owns the remaining scrollable space.
Page wrappers must propagate the available height and allow children to shrink.
The document observer watches this body, so navigation and deep links use the
same visible region. Scrolling content never passes behind the header.

The shared `desk-pane-head` is a 48px row with 12px horizontal insets in both
densities. Document titles reuse it; route-specific padding must not enlarge
that row or shift the folder toggle. Descriptions and navigation sit below it.

`RightPane` holds the pane header above its scrollable slot. `Tabs scrollable`
keeps the tab list above the active panel's scroll region in both docked and
drawer forms. Its `resetScrollKey` starts new selections at their heading;
Jump to navigation is transient and has no persistent duplicate outline. Pane scrolling never moves the
other pane, shell header or bottom strip. Keep existing neutral surfaces,
borders, corner clipping and keyboard focus treatment.

See [the terminology and pane review](reviews/plain-language-panes.md) and run
`scripts/pane-help-check.mjs` for actual-browser regression coverage.


## Pack editing and return navigation

`PackEditHeader` composes the shared `PageHeader`, `Button` and `EditToolbar`.
Save is the only primary header command. Back to pack is always visible, including
narrow panes; neither belongs in a popover or the scrolling document. Secondary
controls use the shared toolbar and neutral Form/JSON selection. The reading
header's Test pack and section links are replaced while editing, so saved-pack
navigation is not mistaken for a test of unsaved changes.

Save status sits beside Save as small muted text; do not add a full-width “Editing · No changes to save” banner. Unfinished-field guidance stays near these actions. An explicit return
with unsaved work uses the shared Radix Dialog, with Keep editing, Discard and
return, and Save and return. Focus returns to the editor on cancellation and to
the reading action after exiting. The dialog never dismisses unsaved work merely
because a write was attempted; verified read-back and the current buffer determine
whether Save and return can finish.

## Packs and Judgment Graphs

The primary sidebar carries Library, Jobs, Admin and Help & About. Library
combines packs and graphs in the existing folder browser. Graphs retain the
`/graphs` and `/graphs/:graphId` routes. Only the current destination is active,
including in the collapsed rail and navigation drawer. Drawing navigation never
fetches graph inventory or executes tests.

New chat and Chat history belong in the chat header. Library retains its folder
browser and collection controls. Saved cases and their Run tests action belong
inside each pack; there is no collection Tests page or Run all tests action.
The legacy `/matrix` bookmark redirects to `/library` without executing tests;
`/packs/:packId/matrix` still opens that pack's saved cases. `/` opens the chat
landing page. Existing graph and author URLs remain reachable without reserving
pack IDs.

Project files belongs in the header's project menu. That trigger carries the
shared unsaved-change indicator so advanced file editing remains visible while
dirty. Source editors retain their existing write and navigation guards.

Graph diagrams reuse `RelationshipMap`, measured spacing, responsive node widths,
`ExpandableText`, and `InspectionRow`. A node opens contextual details and an
Open pack link; a connection opens its destinations and source details. The
Inspector publishes only the selected item. Pointer, keyboard and drawer
navigation must all work. Diagram browsing never depends on test results.

Tests are explicit commands, including detailed traces. Opening a page, changing
trace options, reconnecting or invalidating files must not execute a suite.
Completed results are snapshots within one connection. A revision mismatch
withdraws the joined result diagram and refreshes the document; running tests
again remains the user's action. Diagnostics records command progress
without retaining facts or evidence payloads.

### Full-height workspace tools

The right tool rail switches Assistant, Details and Activity into one resizable
pane. Never stack a long conversation and long details vertically. Header actions
sit together at the right. Expand lifts the same pane over the pack with a soft
shadow, rounded border and slight backdrop. The underlying pack keeps its normal
split-view layout and scroll position. Reserve at least 240px of actual pack
content, in addition to any docked folder navigation, so the context is visible.
The pane header retains the pack title and a visible Return to split view action.

On desktop, the overlay starts at 75% of the workspace and is bounded between
640px and the width that leaves the visible pack context. The shared left-edge
`PaneDivider` supplies the same hover, pointer and keyboard behavior as split
view. Pane width offers Narrower, Wider and Reset width as pointer alternatives
to dragging. Resize never docks or dismisses a pane. Enter on the divider returns
to split view. Escape cancels an active drag; otherwise the focused divider resets
its width, and Escape outside menus, dialogs and the divider returns to the pack.

All pane dividers preview size at most once per animation frame. Commit the
width preference on release; cancellation restores the starting layout without
saving. Keep drag updates local to the divider and CSS layout, rather than
re-rendering the conversation, pack or folder tree for each pixel. Pane actions
and portal targets use `useInspectorControls`; only width-dependent readers
subscribe to measured size through `useInspectorSlot`.

Overlay width is an independent optional preference in the existing per-project
shell record. Viewport clamping never overwrites it. Reset panes clears it; Reset
width clears only the overlay override. Return restores the previous split width.

The dimmed main content is inert while covered. Clicking the exposed background
only restores split view; it cannot activate an underlying pack control. The
application navigation and tool rail stay usable, so this is a workspace overlay,
not a global modal dialog. Keep the same mounted pane and portal targets throughout
expansion, resizing and restoration. Retain each tool's DOM, scroll and unsent
composer while hidden, including while an assistant response streams.

If the minimum readable width plus visible pack context cannot fit, use the full
workspace with Back to pack and no resize affordance. Keep a visible return action
at browser zoom and narrow widths; do not force an unusably small overlay.

The Details footer offers Ask Assistant about this. It attaches an explicit,
removable reference chip without replacing or sending the user's text. Clicking
the chip opens the referenced selection. Sending includes the exact reference;
removing it excludes the reference from that request. Keep references scoped to
the active conversation. Node title dragging adjusts visual positions only;
Reset layout is separate from viewport reset.


## Workspace density and action hierarchy (2026-09-24)

Use the normal 32px control height (28px in Compact). Icon-only `Button` and
`ButtonLink` actions use `size="icon"`: one square target with no text-button
side padding. Give each an accessible name and the shared tooltip. Pane toggles
continue to use `PaneToggle`; do not resize navigation independently.

Toolbars use an 8px action gap; adjacent row icons use 4px. Keep primary writes
such as Run tests and Save visible, secondary navigation quiet, and infrequent
operations in the existing overflow menu. The shared PageHeader spaces actions
and allows wrapping. A lone overflow icon stays beside a narrow document title.

Use `Popover.triggerTooltip` or wrap Radix's trigger with `Tooltip`; do not put
the custom Tooltip component inside an `asChild` trigger, where trigger events
would not reach the underlying button.

The Assistant header puts conversation-wide Chat sources in the shared ⋯
Chat options menu. Source selection opens Details and returns focus to the
menu trigger when dismissed. Keep message-owned sources beside their response.
Omit the redundant Open status when a draft is already visible; offer Open draft
only when it provides navigation.

The Tests assistant reuses chat's composer and message styles: quiet bounded
model selection, square Add/Send/Stop actions, and progress immediately above
the composer. No empty status row reserves space. Retain readable body text;
compactness comes from consistent controls and removing redundant gaps.

Pack test indexes use `CollectionLayout`, `CollectionToolbar` and
`SelectionTable variant="index"`: density-aware single-line rows, complete names
in overflow tooltips, fixed search/filter controls and a pagination footer.
Select matched cases to run or export only that selection. Coverage is a compact
footer action with details on demand; load and connection notices use the bottom
status strip, while case validation stays beside the field or save action.

Case rows are real links to `/packs/:packId/matrix/cases/:caseId`. The case page
keeps the pack header and existing chat, adds a Tests → case breadcrumb, and
protects unsaved edits. Refresh restores saved cases; list search, filters and
chat context survive the round trip. New and duplicate cases stay unsaved until
Save case. Results may use the bottom panel; case editors do not. Draft cases
use the same layout with a `case` query parameter. Narrow tables scroll inside
the collection, never the entire workspace. Source choices use quiet navigation
rows rather than stretched boxed buttons.

See [the spacing review](reviews/desk-spacing-review-20260924.md) for scope,
verification and the proposed next features.

Project Files uses the shared JPS chevron geometry for folding. The editor search
panel shares Button and Input styles, keeps navigation and close actions together,
and reveals replacement controls separately. Search options remain keyboard
operable and selected toggles use the neutral selection background. Read-only
files retain search, folding and copy; mutation controls are absent, and a neutral
badge and explanation identify the owner of the generated output. The file API
reports read-only reasons and rejects replacing runtime lockfiles, known audit
records in their configured or legacy locations, and files with no write bits.
Authored config, pack and input files remain editable; job draft saves retain
the existing Jobs workflow.


## Project file location

The editor uses one compact header: a quiet parent-folder path, a separator,
the filename and save state, then file actions. Root-level files show only the
filename. Folder context is static text: no pointer cursor, hover background,
button role or extra tab stop. Long paths truncate with the shared overflow
hint; the full selectable path and Copy path remain in File actions. Browse
with the file tree or its existing Files/expand controls on narrow or collapsed
layouts. Avoid an additional breadcrumb strip that repeats the sidebar and
only refocuses an already-visible folder.

This applies the hierarchy and reduced visual structure described in Linear's
[2026 design refresh](https://linear.app/now/behind-the-latest-design-refresh)
to this file editor; it is not a claim that Linear prescribes this exact layout.


AI connections are named records under Connections > AI, with one Add connection
button. Rows show connection name, provider and readiness; Manage is the
primary row action. Rename, Set default and Disable/Enable use the shared overflow
menu. Adding an API provider never replaces another connection or changes an
existing shared default. Connection method is fixed after creation.

Assistant is scoped to the current desk. “Use shared assistant defaults” inherits
both connection selection and connection model defaults. A custom desk chooses
its enabled connections and a default, then optionally its own model list and
reasoning per connection. Connection model lists are defaults, not a hidden
machine-wide restriction on desk-specific model lists. The effective desk list
is checked at execution. Connection selection uses the shared searchable popover
with Select all and Remove all; no second settings sidebar is introduced.

The composer keeps one compact model/reasoning menu. Model choices are grouped by
connection name and selected by connection ID plus model ID. The same model ID on
two connections remains two choices. Failed/disabled/removed selections remain
explicitly unavailable, and the menu stays available to choose another target.
Older chats with no connection identity require a choice before their next send;
old replies are never assigned today's provider. New reply details show the actual
bound connection and model.

## Decision composition workspace

Packs and graphs share one Library collection, its existing folders, search,
preview and resizable pane. Use type filters and shared type icons; preserve
the same selection, hover, density, keyboard and empty/loading/error treatment
for both. Graphs do not need a separate theme or global navigation entry.

Graph composition uses the shared relationship map and a bottom selection editor.
The main window's existing right pane hosts Assistant, Details and Activity;
never nest a second chat pane inside the builder. Pack editing uses the same
bottom editor and existing structured fields. Both dividers support keyboard
and pointer resizing; expansion keeps the conversation and buffer mounted.
A searchable Add pack picker, labelled Connect form and List view provide
alternatives to canvas interaction. Selection preserves the viewport. Overview,
Logic, Tests, Review and Usage are the graph tabs. Graph settings and Edit JSON
are dialogs opened from Graph options; closing them retains the draft buffer. Graph tests read saved files;
pack draft rehearsals retain their existing runtime path. Edits invalidate an
outstanding write offer; Check is distinct from Review & save and confirmation.
See [the builder review](reviews/decision-builders-20261010.md).

The design follows the information hierarchy of [Linear's workspace refresh](https://linear.app/now/behind-the-latest-design-refresh),
[GoRules' graph workspace](https://docs.gorules.io/brms/build/graphs),
[DecisionRules' flow designer](https://docs.decisionrules.io/doc/rules/flow/flow-designer),
and [Camunda's decision requirements graphs](https://docs.camunda.io/docs/components/modeler/dmn/decision-requirements-graph/).
These inform discoverability and inspection, not execution semantics. JPS
retains its own tokens and runtime contract. The non-drag form follows
[WCAG's dragging-movements guidance](https://www.w3.org/WAI/WCAG22/Understanding/dragging-movements.html).

## Library browser actions

The collection is named **Library**, uses `/library` and `IconLibrary`. Pack and
graph document URLs retain `/packs/:id` and `/graphs/:id`. It holds reusable
packs and graphs; Desk names the whole workspace, while List names a view. The folder
pane lists real folders with an 8px inset below its header; the header breadcrumb
returns to the full collection. Folder creation and renaming use the shared modal dialog, preserving
entered text through pane resizing and returning focus to the initiating control.

Move dialogs use one Destination folder selector, with full paths and the shared
selector's keyboard type-ahead. Do not put a separate search input above it.
New folder is a secondary action at the start of the footer; it reveals Folder
name and Parent folder within the same dialog. Back restores the existing
folder choice. Create and move commits the new folder and the entire move in
one revision-checked write, with no empty folder left behind on failure. The
name and parent stay editable after validation or conflict errors. This applies
[progressive disclosure in forms](https://www.nngroup.com/articles/4-principles-reduce-cognitive-load/)
to the existing folder workflow.

Keep **Create** visible as the primary command, with Pack and Graph choices.
Collection **⋯** holds New folder. Review and lock belongs in Admin → Decision
safeguards. A row opens through its name and keeps its Preview button visible;
its **⋯** holds Move to folder and Delete draft when eligible.
Folder hierarchy actions remain in each folder's existing context menu.
Command rows opt into the shared `desk-menu-command` geometry: a 16px icon
column, 8px gap, centered labels and the density control height. Iconless rows
reserve the same column. Disabled actions retain an accessible explanation.

Row highlights and separators span the collection width up to an actual scrollbar.
Put the shared horizontal gutter inside each row and its column header, not
around the list. The header compensates for the measured scrollbar width; short
lists have no empty scrollbar gutter.

Every row reserves a leading checkbox column. On mouse devices, row checkboxes
appear on hover or keyboard focus; Select all appears when the column header is
hovered or focused. While anything is selected, all checkboxes stay visible.
Touch devices always show them. The checkbox is followed by the shared cube (`IconPack`) or
connected-node (`IconGraph`) glyph and name. Type glyphs have accessible names and
shared tooltips. Preview toggles the item inspector, with Spacebar shown through
the shared tooltip's separate shortcut slot, not embedded in the action label.
The shortcut works when the item link or Preview button has focus. Draft occupies the Version column; saved objects show their
actual version without an added `v` prefix. There is no duplicate type badge beside the name.

One toolbar holds search, search scope, type, status, sort and selection actions.
Search scope can include all folders while retaining the current folder location;
choosing another folder resets that scope. Type, status and sorting collapse into
View when the pane is narrow. The toolbar stays on one line, including while an
Inspector is open. Controls use the shared height and an 8px gap.

Search fields use `ui/SearchInput` across collections, chat history, source and
model pickers, and navigation. The leading search glyph, border, native input
height, padding and focus ring match Library. Labels, native input refs, filtering
and keyboard handlers remain on the input; the decorative icon adds no tab stop.
Library passes its existing scope menu as a trailing control. Set layout sizing
on the wrapper (`className`); use `[data-search-field]` in parent layout selectors.
Ordinary search inputs expose the native searchbox role; command navigation keeps
its explicit combobox semantics. A folder-prefix field retains text semantics.

Library, Inputs, Jobs and the all-runs index share collection geometry. Use
`ui/CollectionToolbar` for the control row and `SelectionTable variant="index"`
for native collection tables. Library keeps its virtualized list, using the same
geometry tokens rather than a second set of values.

| Detail | Shared treatment |
| --- | --- |
| Location header | 48px, 13px / 500 labels; plain Inputs/Jobs titles, Library’s functional collapse control; primary command at the right |
| Search | `--collection-search-width` (23rem / 368px) including any scope control; `Search…` placeholder and a specific accessible label such as Search inputs |
| Controls | Density height (32px / 28px), 8px gaps; filters and sort collapse into View below 800px of available toolbar space |
| Selection | Right aligned; compact count and in-menu Clear selection on narrow panes |
| Name column | `--collection-name-width` (`clamp(10rem, 26cqi, 18rem)`); the reserved checkbox slot precedes the 16px glyph and 13px / 400 name |
| Column labels / metadata | 12px, neutral secondary ink; column labels remain aligned with values |
| Table headings / rows | Headings 32px / 28px; rows 40px / 32px; one line with full values available on overflow |
| Numeric counts | Right aligned, tabular digits; labels align with their column values |
| Scrolling / footer | Full-width row highlights; vertical scrolling inside the collection; horizontal scrolling where needed; pagination stays at the bottom |
| Empty results | The Library heading/copy hierarchy and spacing; filtered-empty copy points back to search and filters |

Keep native table semantics, named searchboxes, labeled checkboxes and real links
or buttons as keyboard targets. Column widths follow content roles; do not make a
count or action column as wide as a name. Library may omit its description column
on narrow panes while the denser Jobs and Inputs tables scroll horizontally.
These choices apply [Linear's predictable placement and restrained hierarchy](https://linear.app/now/behind-the-latest-design-refresh),
[Carbon's table guidance](https://www.carbondesignsystem.com/building-blocks/core/components/data-table/guidelines),
and [WAI's native table guidance](https://www.w3.org/WAI/ARIA/apg/patterns/table/)
to Desk's existing density and palette; they do not copy another system's pixel values.

Jobs collection rows use the standard density row height (40px comfortable,
32px compact). Keep the pack version beside its title, summarize triggers inline,
and keep recent-run controls in one row. Long names and trigger summaries truncate
with `OverflowTooltip`; full values remain in the DOM and job details. On narrow
panes, scroll the table horizontally inside `CollectionLayout` rather than
wrapping rows. Run (play) and Resume draft (pencil) use shared icon-only controls
with accessible names and tooltips. Keep these actions visible for keyboard and
touch use. Run still opens the input form; row navigation does not execute a job.
This follows [Linear's restrained visual hierarchy](https://linear.app/now/behind-the-latest-design-refresh)
and [Carbon's guidance for a small number of inline row actions](https://www.carbondesignsystem.com/building-blocks/core/components/data-table/guidelines).

Select all toggles between selecting and clearing the filtered collection, including
all pages and virtualized rows. A partial selection shows an indeterminate checkbox. Filtering
removes hidden items from selection. A “3 selected” menu holds Move to folder,
Compose graph and Delete drafts. Clear selection is an adjacent icon on wide panes,
and a menu item on narrow panes. Selection adds no extra toolbar row. Composition requires
only saved packs; it never silently drops a selected draft or nests a graph.
Deletion requires only unsaved drafts and explicit confirmation, preserves chats
and saved files, and rechecks the entire selection before changing it. Failed saves
remain visible and retryable. Folder moves update all assignments in one write
against the original digest.

Collection totals belong in the bottom footer, not the location header. Use the
shared `ui/Pagination` with `ui/usePagination` so tables and lists share controls
without replacing their row semantics. Three rows show “3 items”; more than one
page shows the displayed range and matching total on the left, with page position
and previous/next controls on the right. Use 50 items by default and the shared
menu for 25/50/100 per page. Omit navigation when there is only one page and omit
the size picker for 25 or fewer matches. Keep the footer visible below the scrolling
rows; narrow panes can wrap controls instead of clipping or shrinking hit targets.
For page and tab collections, use `ui/CollectionLayout` to keep the toolbar,
scrollable rows and bottom footer in one bounded pane, following Library. Its
native table headings stay visible while the body scrolls vertically or
horizontally. Parent panels must fill the available height (`PageBody` fill or
`Tabs` fillPanel); do not put the entire collection inside a page scroller.
The footer stays at the pane bottom even with one item. Do not reserve an empty
scrollbar gutter: rows fill the pane edge to edge, with scrollbar space used only
when vertical overflow exists, matching Library. Page/filter changes reset
the row scroll position; background refreshes do not. Cursor-based lists keep
honest loaded counts and their existing Load more/Load older actions. Small tables
inside forms, details or dialogs remain embedded in that content.

Rows with a destination or inspection view use `ui/CollectionRow` and mark their
existing primary link/button with `data-row-primary`. Clicking any non-control
cell opens that action; hover and focus use Library's shared highlight token.
Keep the native link/button as the keyboard target and retain table row semantics,
without adding a second tab stop to the row. Checkboxes, their selection cells,
menus and secondary links remain independent (`data-row-ignore` marks custom
control areas). Selecting text must not open a record. Ctrl/Cmd-click and
middle-click on linked rows open another tab. Rows with no detail action remain
non-clickable; do not make a destructive or execution action the row default.

Paginated native tables use `ui/SelectionTable` with `useCollectionSelection`,
`SelectionHeader`, `SelectionCell` and `SelectionActions`. This applies Library's
reserved checkbox column, hover/focus reveal, persistent visibility while selected,
and always-visible touch targets. Keep row opening on the primary link/button;
checkboxes and their whole cell must not navigate. Use native indeterminate state
for partial selection. Shift-click selects a range; Ctrl/Cmd+A selects matching
loaded rows when focus is in the table, and Escape clears them. Text inputs keep
their native text-selection behavior.

Selection covers matching loaded rows across every local page and survives page
changes. Filtered-out, removed or changed-owner rows are discarded. New rows loaded
after selection are not silently added. Incomplete inventories label their header
control “Select all loaded matches”; toggling a fully selected header clears the
selection. Row counts and pagination retain their independent loaded/total scope.
The selected-count menu and clear icon align to the right of the existing toolbar.
Use the shared `SelectionCountButton` in Library and native tables: its neutral
fill and accent label make selection mode visible without implying success.
Selected rows use the stronger neutral `--selection` background, distinct from
hover and preview, and native checkboxes use `--accent-fill`. Inputs Records
retains Review inputs, Evaluate and its existing input export; other collection
tables offer Export selected rows and Clear selection. Table exports include exactly
the selected loaded rows with a `desk-table-selection` format marker and do not
claim to be signed verification records. Bulk selection never submits jobs or
modifies sources. Tests and small embedded tables retain their task-specific controls.
These interactions follow [Linear's selection patterns](https://linear.app/docs/select-issues)
and [Carbon's conditional batch-action guidance](https://www.carbondesignsystem.com/building-blocks/core/components/data-table/guidelines).

Counts describe the current folder/search/type/status, and incomplete inventories
say “loaded items”. Filtering, sorting, scope or page-size changes start at page
one; removing the last row of a page clamps to the last remaining page. Opening
an item and returning preserves the current page. Home/End and arrow keys navigate
within the displayed page. Select all is explicitly “Select all matches” and keeps
its collection-wide meaning across pages. This applies
[Carbon's table pagination guidance](https://www.carbondesignsystem.com/building-blocks/core/components/pagination/guidelines)
within Desk's existing controls and density tokens; it is not a claim that Linear
uses this pagination layout.

These choices apply [Linear's selection patterns](https://linear.app/docs/select-issues)
and [restrained action hierarchy](https://linear.app/now/behind-the-latest-design-refresh)
to JPS's distinct pack, graph, and draft lifecycles.


### Assistant ownership across document tabs

Only the active document tab publishes an Inspector presentation. Draft Overview,
Logic, Sources, Tests and Review share the conversation attached to that pack
and its draft. Case selection supplies additional context without creating an
unrelated conversation. Inactive presentations and header controls are hidden. Share the open/closed state across the
handoff, so visiting Tests cannot remove Assistant from the other tabs or undo
an explicit collapse. Saved pack reading and Tests use the same open preference.

Chat welcomes use a regular-weight 14px title, 13px secondary text and a 4px
text gap in both main chat and the right pane. Embedded chat keeps left alignment
and uses `--density-block` content insets (12px comfortable, 9.6px compact); the
home landing keeps its centered placement. Pane messages and composer text use
`--text-sm` (13px) with the shared 1.5 body line height. Messages are separated by
`--density-field` (16px comfortable, 12px compact); user bubbles use
`--density-gap` vertical padding. Metadata stays 12px and chat commands use the shared small control size
described in Chat controls below. Pane Markdown headings use 14px for h1/h2
and 13px for lower levels, with weight 600 preserving hierarchy. The pane
composer starts at two lines and grows with its text. Full-page message type and
spacing retain their existing reading layout. Prompts
refer to the current pack, graph or input. Welcome copy appears only in an empty,
idle conversation, without proposals or work. Inputs also checks its research
status before showing the welcome: queued, running and retained finished runs or
status failures suppress it, including research started outside chat. The Inputs
page owns its research progress, recovery and budget controls. A docked chat beside
that page does not duplicate them. When chat covers the workspace or opens on its
own, show a compact status, Open input link and Stop while active; the link reveals
the Inputs page. Keep detailed source explanations and continuation controls there. Draft commands already in the document header are
not repeated above the composer. Empty chat footnotes reserve no space.

Graph reading and authoring use the Library folder frame, shared document title
bar, page tabs and main right-hand Assistant. Overview describes purpose and the declared result without executing the graph; secondary composition details stay collapsed. Build keeps
selection editing in the bottom pane; saved Logic and Tests keep Assistant in
place. Logic opens View runtime plan in a wide dialog. Tests groups Saved cases
and Try inputs, preserving inputs when switching. Graph options holds settings
and editable JSON dialogs; Review & save remains the builder header action. Read-mode proposals
remain conversation artifacts until opened and reviewed in the builder.


### Document header options

Pack and graph headers use the shared `OptionsMenu` trigger and `Menu.Item` rows.
Keep menus as short, labeled command lists; editing forms and metadata belong
in dialogs. A menu that opens a dialog hands focus to it, and dismissal restores
focus to the original trigger. Use the same 16px icons, 8px gaps, density-aware
row height, keyboard navigation and collision handling as other Desk menus.

- Draft packs: Rename…, Move to folder…, Run checks when restored, then
  Delete draft… below a separator. Rename opens a Name form; deletion opens a
  confirmation. Run checks repeats runtime validation, source verification
  and saved case checks without a model call. Review and finalize remains
  visible but unavailable until the checks pass; its restored-state hint points
  to Pack options. Do not repeat Recheck as a prominent header button.
- Saved packs: Move to folder…, then Create job…, Saved cases
  when available, Full document, and Pack details… in its own group. Details
  shows identity, version and existing checks in a dialog.
- Graph builders: Move to folder… for an existing item, Keep
  draft, and Delete draft… for an unsaved retained draft. Settings and Source
  already have tabs and are not repeated in the menu.
- Saved graphs: Move to folder… and Graph details…. Keep Edit
  composition and Run tests visible. Identity and path changes are handled by
  graph settings and the reviewed write flow, not a label-only Rename command.

The persistent right rail already opens Assistant; document menus do not
repeat that control. A chat link opens Assistant once; returning to the document must not
reopen it merely because the shell republishes its pane callbacks. Primary Test, Save and Review actions remain visible in the header.
Destructive draft actions use the shared danger treatment and confirmation;
the dialog stays mounted through save failures so users can retry. Navigate
away after deletion only when it has been saved. Saved project files are not
exposed to draft deletion, because graphs and jobs may reference them.

This applies Linear's [consistent header actions and reduced visual structure](https://linear.app/now/behind-the-latest-design-refresh)
and the [menu design guidance on clear labels and predictable placement](https://www.nngroup.com/articles/menu-design/)
to Desk's existing document lifecycles, rather than copying an undocumented
Linear component specification.


**Run checks** remains available in Pack options after success and after reload.
The header shows the check state using the shared tooltip: not checked, verifying,
checking, passed, failed, outdated, unavailable, or valid with no tests. Running
checks validates the unchanged draft and its saved tests without a model call.
Review and finalize remains the primary action and is gated by current checks,
source verification and independent test cases.

Complete retained reports can become current again after their identity and
source proofs are verified. Legacy summaries require one explicit run to obtain
this record. No pending finalization approval survives a reload. See
[conversation recovery](adr/0002-project-conversations-and-chat-workspace.md#persistence)
and [retained test reports](pack-tests-workspace.md).


### Pack and graph tab density

Pack tabs are Overview / Logic / Sources / Tests / Review / Usage. Graph tabs
are Overview / Logic / Tests / Review / Usage. Share toolbars, reading measures,
menus, disclosure styling, table density and bottom selection panels. Settings
and raw JSON are document commands, not primary browsing tabs. No navigation,
filter, selection or disclosure performs a test, rehearsal or project write.

Sources uses one compact browser for declared references, attached documents
and retrieval records. Search and kind filter share the toolbar; selecting a
source opens its details below. Declared references never imply retrieval.
Receipt status, citations and page quotes preserve their exact recorded
meaning; a verified receipt is byte lineage, not a truth or origin claim.

Pack Tests groups Add case (manual or AI), search, result filter and Run tests
in one toolbar. Case and run details use the bottom panel. Independent
expectations belong here, with unresolved expectations expanded. Draft
exploratory runs remain available under a disclosure. Graph Tests lists case,
expected result, observed result and status; selecting a case reveals its exact
runtime comparison and optional execution diagram. Runtime labels, coverage
and exact output remain inspectable; digest mismatches are never overlaid on a
newer diagram. Try inputs is an explicit rehearsal, not a decision audit event.

Review starts with readiness and actionable findings. Link to Tests and Sources
instead of repeating their complete content. Brief is optional and collapsed;
once opened its state survives collapse. Revision history is collapsed.
Usage lists only confirmed graph/job references and presents incomplete
availability once with detailed causes on expansion.

Provenance placement: use Review for document revision/check history and links
to decision or job records; Sources owns acquisition/receipt details and Tests
owns run inputs and outputs. A future unified “Provenance & audit trail” section
must identify each record's scope and retained coverage, and must not label
rehearsals, chat events or draft revisions as signed decision audit records.
Trail verification, signing, checkpoint and repair controls remain under Admin
→ Decision safeguards. This placement does not imply that a per-definition
unified audit timeline already exists.

### Reprocessing selected research

Use the shared selected-count menu for **Reprocess from saved sources…**. Do not
repeat this action in research status rows or above the chat composer. The review
uses the existing compact dialog spacing, selected-record disclosure, saved recipe,
AI connection/model and request ceiling. Name the confirmation **Reprocess N records**.
Explain that only the selection is updated, no new sources are acquired, and changed
records need review again. Preserve the frozen selection while background reads occur.

Progress reports publication outcomes (added / updated / unchanged / needs correction),
not a misleading total of model-emitted records. Details and per-record results stay
in Activity; background read failures use the bottom strip. Use **Needs correction**
for record validation failures and **Recovered** only with a linked published correction.
An evidence link is separate from an official website; unconfirmed websites stay explicit.

### Status and warning messages

Use one severity and placement pattern across the shell, chat, and forms:

- Temporary background state (connecting, reconnecting) belongs in the existing
  status strip with a polite announcement. It never inserts a row above document
  headers. The status control opens a focused, dismissible popover with Retry and
  diagnostics. Attempt counts stay inside Technical details and do not repeatedly
  announce themselves.
- Background read failures across Inputs, Library, Jobs, chat and Admin belong in
  the bottom status strip alongside configuration, local processing, connections,
  folders and optional safeguards setup. Register them through `DeskStatusNotice`; `DeskNotices` deduplicates by
  service and shows one bounded summary with a count. Recovery actions and details
  stay available in its popover. Workspace content remains mounted during runtime
  outages; authentication recovery still uses the access screen.
- Use `QueryStatusNotice` for failed background reads. Keep the same resource ID
  when a page and its Assistant pane observe the same query. Include the document
  name in details where available. The notice clears after a successful read or
  when its last observing page unmounts; an open details popover does not retain
  stale actions. Retry refetches that resource and never starts research, repeats
  an evaluation, or replays a write. Preserve a journal's continuation cursor.
- Keep cached rows and edits visible after refresh failures. If no data loaded,
  show a quiet unavailable state, not a false empty-list or “Not run” result.
  Research progress and the result of a run stay in their compact contextual row;
  failures to refresh that progress belong in the strip. Do not repeat the same
  warning in Records, Activity, and Assistant. Chat storage failures register once
  for the whole Desk; unsaved changes take priority and retain Retry saving.
- Dialog read failures stay inside their modal with recovery controls reachable
  in its focus scope. Field validation, rejected writes, review blockers and
  saved run outcomes stay beside their affected action or result. Incomplete
  history/usage coverage may retain a quiet contextual explanation; background
  request diagnostics and retries belong in the strip.
- Use the same amber `--unknown` token as failed chat steps for the icon and text
  of attention notices, including “Project safeguards off.” Connecting states and
  a healthy connection stay neutral. Use words and icons as well as color.
- Status popovers have one header/body inset, a 13px title and message, and shared
  buttons. Do not nest an outlined notice inside another padded panel. Optional
  safeguards setup can be dismissed per project; service faults cannot. Setup
  remains available in Admin → Decision safeguards and follows service faults in
  the notice list. Never insert a setup banner above Library filters.
- Limited capabilities use an explicit Limited connection status. Do not call a
  connection healthy merely because initialization answered.
- Action failures and important warnings use the shared Notice next to the
  affected action. Alert, AlertPanel, and ErrorBox reuse it. A neutral surface,
  small severity icon, restrained leading border, and 13px text replace filled
  warning panels. Meaning is communicated by text and icon as well as color.
- Admin action feedback uses `SettingsFeedback` and `SettingsActions`: one compact
  13px message before the right-aligned action group, wrapping within narrow panes.
  Confirmed saves use a small green check with neutral text; attention uses the
  existing gold token. No filled banner, automatic dismissal or focus change.
  Clear confirmation when its draft changes. Keep saved, tested and service-ready
  states distinct; a successful write does not prove a connection works.
  Show a reconnect consequence while reviewing a changed setting, beside Apply.
  Context-specific retry instructions belong in that workflow, not generic Admin
  confirmations. An unconfirmed write says the outcome is unknown and offers a
  read-only reload; never silently repeat the write. Recovery actions sit outside
  the live region. Detailed audit/test reports keep their existing structure.
- Web-access recovery uses a standard secondary button, never the muted inline
  text variant. Keep the control available for a supported editable policy even
  after enabling it; the saved failure describes an earlier read. Research and
  chat share `WebAccessDialog` with one title and one fixed action footer.
  Explanations and the reconnect warning stay in the body; the warning appears
  only for an edited setting. Apply never retries work. Keep the dialog open
  during the write and restore focus to its opener when finished. Admin retains
  its section layout and uses the same form state and write logic.
- State changes use role=status; failures requiring attention use role=alert.
  Ordinary notices never steal focus, auto-dismiss, or hide recovery controls.
  Destructive confirmations retain their existing dialogs.
- State the result and next action first. Put long errors, digests, and connection
  attempts behind a keyboard-accessible Technical details disclosure. Details are
  bounded and scrollable. No transient toast is the sole copy of a blocking error.

The placement follows [Linear's visual hierarchy](https://linear.app/now/behind-the-latest-design-refresh)
and [Carbon's inline-notification guidance](https://v10.carbondesignsystem.com/components/notification/usage/).
These are Desk adaptations, not a claim about Linear's private components.

Connection attempts have bounded WebSocket and initialization waits. All close
paths check the session before retrying; an expired session stops retrying and
uses the existing sign-in/local-Desk recovery screen. Manual reconnection refreshes
project queries. Authentication policy and the one-session-per-page boundary are
unchanged.

Inputs collection names follow the Library item pattern: a muted 16px Inputs icon,
a regular-weight `--ink` name and no underline or accent color. Preserve the link,
row hover treatment, and visible keyboard focus.

### Recent runs and collection exports

Jobs' recent-run links use unframed shared 16px glyphs in stable 24px targets:
a neutral check for completed (`--ink-soft`), gold cross for failed (`--danger`),
gold stop for interrupted (`--warn`), accent activity line for running (`--accent`)
and muted clock for queued (`--ink-faint`). Completion is an operational state,
not a successful qualification or verified signature; reserve green success
verdicts for explicit successful results. Do not wrap these glyphs in colored
circles. Use neutral square hover surfaces, no text-link underline and the shared
focus ring. Run and Resume row actions use `--bg` with a neutral strong border
on hover, so they remain visible against highlighted or selected rows. Status and explicitly labelled submission time appear
in the portal tooltip on hover or focus; the link also names its View run action.
Missing or invalid submission times read Not recorded.

Jobs and Runs keep search at the top, including empty lists. Their header's
Collection options menu opens Download run history, with the shared download
glyph in the menu and download action. Show the export scope and
unverified-file explanation inside that dialog beside its download action; opening
it does not download or verify anything. The runner's complete original file is
independent of filters or selected-row exports.

## Local sign-in controls and password settings

Entry-header language and appearance selectors size to their label with an 8px
chevron gap and wrap within the header. Scope this to entry preferences; form
Select controls retain full width. Admin → Sign-in & access uses SettingRow for
the password action and the shared Dialog, Field and SettingsActions components
for setup/change. One title, scrollable form, fixed right-aligned footer. Current
password failures belong beside the field; unconfirmed saves offer return to
sign-in without another write. Reset instructions remain in Recovery.

### Password entry

`ui/PasswordInput` composes Input, a quiet icon Button and Tooltip. It places
Show/Hide at the trailing edge within the field, using shared density and focus
styles. Each field owns visibility independently and starts hidden. Field keeps
label and error associations; validation replaces a duplicate hint. Password
length is a feature policy, not imposed by the shared control, so existing
credentials and provider secrets are not silently truncated.

### Loading states

Use `ui/PageLoading` for an initial main-pane load. It fills the available body
and centers one quiet status below the existing header and toolbar. Collections
use `CollectionLayout loading={label}`; detail pages use `PageBody loading={label}`
or place `PageLoading` directly in their flex page body. Labels name the resource
(e.g. Loading library…, Loading dialects…). Do not add page-specific padding,
large spinners, blocking overlays or loading messages inside collection toolbars.
Keep cached content (including an empty result) visible during background refresh;
reserve this state for a first read with no content. Failures stop the spinner and
use the existing unavailable state and bottom status strip.

Use `ui/LoadingState` for local pane, dialog, editor, inline and settings loads. It composes `RunStatus` and the shared `Spinner`: quiet small text, an
8px icon gap, readable shimmer and reduced-motion support. Containers own their
usual spacing; use `inset` only for an otherwise unpadded surface. Inline states
inherit their control or cell typography. Icon-only loading controls use the
same `Spinner` and retain an accessible label.

Use `SettingsFeedback pending` for a settings action that switches between
progress and a result. Never keep an error or an empty result animated. Separate
loading from not-found states, and keep recovery actions in their existing
status strip or local error surface. One loading state owns one live region.

### Chat activity and recovery

Chat panes use `ChatPlaceholder` while history loads: a 16px content gutter,
shared `RunStatus` text and spinner, and reduced-motion support. A failed load
stops the animation and shows a quiet unavailable state; error details and Retry
remain in the bottom status strip. Saved work retains historical tool names, so
renaming a workspace cannot make existing conversation history unreadable.

Use the shared 12px `Spinner` in place of a working step's trailing chevron, and
in place of the Work disclosure's leading chevron while any step is active. Keep source/provider icons recognizable.
Completed, failed and interrupted steps have no spinner. `RunStatus` pairs the
same spinner with the subtle text shimmer; reduced-motion mode leaves a static
indicator and plain, readable text. The spinner and chevron share a 16px slot,
so switching states does not move the labels.
Do not add a second live region per step; the current status owns announcements.

Action buttons, including inline actions, use a neutral hover surface and the
shared focus ring. Underlining remains for prose links and active navigation.
A failed chat response places its saved reason and Retry response together in the
conversation, with no duplicate error above the composer. Label this deterministic
Desk feedback as Response interrupted rather than manufacturing model prose.

Input recipe proposal validation uses closed, actionable failure reasons. These
are returned to the assistant for correction and retained under the failed work
step; raw exception messages and tool arguments are not saved as diagnostics.
An unused qualification target is omitted, matching the declared tool schema.


### Compact Inputs references in chat

Input recipe and action artifacts use the shared reference component's compact
variant: 8px vertical padding, a single-line 13px title, one 12px metadata line
and the existing review action. Long titles use the shared overflow tooltip.
Fields and full instructions belong in the review, not a repeated field list.
A saved recipe or applied action for the input already open in the pane is
omitted; the context header identifies it. Pending proposals still appear, and
Related work retains every reference in grouped history. Hiding the
latest applied item never revives an older pending item.


### Related work rows

Related work follows [Linear's restrained hierarchy and lighter separators](https://linear.app/now/behind-the-latest-design-refresh).
Use a single 13px, weight-500 title with 12px secondary type/version text and a
16px neutral entity icon. Keep the standard 32px control height, keyboard focus
and background hover feedback. A pending review is a flat action row or the
creation proposal's direct action, never a bordered card inside another row.
Rows share a trailing action edge; below 19rem of available content width,
actions wrap below the identity. Long names truncate with the shared overflow
tooltip, while status and recovery text wrap. The existing reader owns scrolling.

Completed and earlier proposals recede into native disclosures. An older
unsaved creation proposal is labelled Earlier proposal · Not saved and does not
resurface as Needs review after a later recipe is saved. Saved entity IDs, graph
paths and explicit draft identities govern grouping, never display-name matches.
The same presentation serves main chat and Inputs/Library panes and is reusable
for Jobs. Related work remains a labeled control, without an ellipsis menu.


### Inputs status and record controls

Use one compact research status beneath the tabs: current state, saved count
explicitly scoped to this run, and the relevant actions. Paused runs say
“Approval needed”; retain View steps, Keep results and Review increase. Running
work retains the spinner, current operation, elapsed time and Stop. Retries keep
the actual countdown. Do not repeat source counts, long provider/budget messages,
or general review instructions above the records. View steps owns retained
sources, source gaps and the full recorded explanation. Actionable web-access
recovery remains a bordered button in the status row. Real refresh/save failures
remain visible beside their recovery; never suppress them as decorative noise.

Records uses CollectionToolbar with the same 23rem search width and responsive
View menu as Library, Inputs and Jobs. Preparation and Evaluation filters sit
beside search on wide panes; selection stays at the right. The table's Preparation
cells carry review state. Explain a disabled evaluation in its selection-menu
item, and keep evidence/verification guidance in record review. Routine guidance
is not another persistent warning panel.

This applies [Linear's restrained hierarchy](https://linear.app/now/behind-the-latest-design-refresh)
and [Carbon's concise contextual feedback](https://www.carbondesignsystem.com/building-blocks/core/components/notification/guidelines)
to Desk's existing controls rather than introducing a new banner or toolbar.

Research execution settings use the existing Execution disclosure, FieldGroup
and Select controls. Offer Standard/Delegated and, only for Delegated, Auto or a
supported numeric maximum. Show unsupported choices disabled with an explicit
connection explanation; retain an existing requested maximum when connections
change, and show effective concurrency in the start review. Auto here governs
scheduling, never permission. Keep internal task caps and queue mechanics out of
the recipe form.

Research task groups reuse compact operation disclosures, shared status tokens,
and Spinner in place of the disclosure chevron while running. Keep actual
operations and their times inside the owning task instead of presenting an
ungrouped list that implies all work is sequential.

The keyboard hint beneath the chat composer is aligned to its trailing edge in
both full-page and pane chat, including Tests' shared composer. Its existing
secondary text size and spacing stay unchanged. Both idle and working notices
wrap at narrow widths without horizontal overflow.

Chat composer help, including the active-window notice, aligns to the trailing edge in the main page and right pane. Chat history uses the shared inline RunStatus for Working, with the same spinner, text shimmer, and reduced-motion behavior as the composer.

Test proposal review keeps static fact-type findings separate from runtime results. An intentional wrong-type case carries a specific fact pointer and explanation, shown in a collapsed shared Disclosure beside the case. Changes to an existing case expose the actual saved and proposed rows through the shared CodeBlock. No type conversion, expectation change or save is inferred from a passing check.

## Chat controls

Use `ChatAction`/`ChatActionLink` for commands in the transcript, recovery notices, questions, artifact cards and composer. `ChatActions` provides wrapping rows and feedback. These adapters share the small `Button` geometry; `size="icon"` maps to a matching square target. Secondary commands use neutral borders or quiet presentation; only the current primary action uses a fill. Retain native keyboard behavior, accessible names, tooltips and disabled states. Select triggers in the composer use `size="small"` from the same density scale. Menus and full settings/approval dialogs retain their shared menu/form geometry. Message navigation rows and in-text citations remain rows and inline links.

Retry response uses the same small secondary button in the transcript and recovery header. Never size it independently. Narrow layouts wrap labels/actions without horizontal overflow. Coarse pointers retain standard control targets. New chat features must use these adapters rather than a new button class or local pixel dimensions.

## Chat proposal review

Use `ChatActions`, `ChatAction` and `ChatActionLink` for artifact commands. They use the shared `Button` small size: 28px tall (24px in Compact), 12px labels, 8px side padding and 4px icon/action gaps. Small icon actions are square and retain accessible names and tooltips. Coarse pointers retain the standard control target. Use the same small geometry in embedded diff toolbars and proposal disclosures; page toolbars keep their standard size. Keep one primary save action, secondary checks/corrections, and visible reasons for disabled actions. Related-work links use the same shared adapter. Ask me exposes explicit Save proposal. Auto requires a separately reviewed scoped allowance and the same fresh checks. Proposal checks and diff expansion never grant permission. Applying to an unsaved editor buffer remains a secondary editor action. Group Save, Check and optional editor/correction commands in one wrapping action row after the review details, with explanations 4px below. Proposal title and status use a 4px rhythm; summary, disclosures and actions are separated by 8px. Do not stack generic notice rows or place buttons flush against an expanded diff.

`CodeDiff` is the shared read-only JSON comparison. It uses the existing CodeMirror editor theme with `@codemirror/merge` (MIT), line numbers, deletion/insertion marks and collapsed unchanged ranges. Narrow panes use unified diff; Expand diff opens the shared dialog with an optional split comparison. No per-hunk mutation controls appear. Never substitute model-authored change descriptions for this exact host-computed comparison.

## Desk changes and persistence

The desk dot and tab asterisk share `UnsavedChanges` state. They indicate manual edits awaiting Save or an automatic save that failed. Normal chat checkpoint writes, streaming, successful autosaves, background work and busy-only operations do not raise an unsaved-edit marker. A failed automatic save stays marked during retry until the owner confirms persistence; starting a retry is not success. The desk tooltip includes the names of registered changes when available.

Register real edits with `useDirtyGuard`/`useRegisteredChanges` (default `kind: 'edit'`). Use `kind: 'autosave'` and `saveFailed` for retained state that saves automatically, and `kind: 'activity'` for temporary operations whose navigation needs protection. Pass write admission as `busy`, never combine it with an edit's dirty boolean. Owners clear only their own registration. Header and tab consume the same aggregate; path-specific author guards must not supply a separate global dirty indicator.

Visual attention and exit protection are distinct. Pending autosaves and active writes still protect tab closing, sign-out, project moves and desk switches. Destructive flows use `useHasPendingChanges` or the shared exit guard; `useHasUnsavedChanges` describes the visible unsaved-edit state. Ordinary chat navigation retains the session and does not require a save prompt.

## Record review

Dialect research review opens in the main pane and keeps chat available. Single
and selected-record review share the same workspace, decision controls and
`RecordDetailsTable`. Values use aligned Field / Value / Evidence columns, with
row-level expansion for quotations, notes and technical field names. Long values
use an explicit Show more action. Narrow containers stack the same fields without
horizontal scrolling. Preserve false, zero, null and missing distinctions.

Pack-mapped fields come first; search and All / Needs attention / Changed filters
use `CollectionToolbar`. Changed compares against the latest saved review. It
does not infer correctness from a prior review or a source quotation.

The selected queue becomes Previous / Next and a selector in narrow panes. Save
and next saves only the current record; navigation preserves each unsaved review
in memory. The selection and active record are retained in the URL. Shared dirty
guards protect leaving/reloading, and stale revisions require explicit reload.
Decision controls and save feedback stay in the fixed footer. Batch decisions
remain a small dialog naming the exact scope; they reuse `RecordReviewDecision`.

### Dialect recipe review and editing

The Recipe page and independent draft workspace share `RecipeSection`,
`RecipePurpose`, `RecipeFields` and `RecipePackSection`. All section controls are
open and edit the same recipe buffer. Do not add section-level Edit, Done or
confirmation actions. Purpose, criteria, source settings, execution limits and
pack mappings are available immediately within their page tab. The page save
action validates and saves the complete recipe.

Every dialect researches records. Domain examples belong in instructions and
requested fields, not a Find selector. AI recipe tools and presets follow the
same contract. Historical target labels remain readable for saved research
identities and journals; they must not reintroduce a mode selector. Explicit
selected-company references retain their revision and relationship checks.

Requested fields use an always-visible shared search/filter toolbar and an
editable table. Name, type and complete instructions are available in each row;
filtering does not commit or discard edits. On narrow panes, stack descriptions
under name/type controls. Preserve typed values, dirty buffers, revision checks,
approvals and the existing right-pane conversation.

### Dialect draft workspace

New dialects and chat recipe proposals open a page at `/dialects/drafts/:id`.
Use the same `routes/DialectsView` page frame as saved dialects, with `PageHeader`, page tabs and `PageBody`; do not place the recipe in a dialog. The frame owns the full-width layout, so the shell must not add another gutter.
Overview, Requested fields, Sources, Execution and Pack separate the editing
concerns. Requested fields use the full available width, the shared collection
search/filter toolbar and a compact table. Keep all field controls open. Keep name/type columns bounded so descriptions receive the remaining width.
The existing assistant pane remains available, with the draft as its context.

Drafts are project artifacts stored in `.desk-private/dialect-drafts`, independently
of conversations. Autosave incomplete settings with revision checks. Show quiet
save progress in the page footer; failed persistence uses the shared bottom notice
and retains local edits. Register pending writes as autosave, not manual dirty
state. Final Save to Dialects validates the complete recipe and creates one
preparation idempotently; it never starts research. A chat reference opens the
same draft or its saved dialect. Discarded drafts retain a tombstone so restored
conversation checkpoints cannot recreate them.
