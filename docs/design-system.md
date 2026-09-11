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
| Primary action | The public app uses indigo `#5e6ad2`. Use it for action fills in both themes. |
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
| Action fill `--accent-fill` | `#5e6ad2` | `#5e6ad2` |
| Action label `--ink-inverse` | `#fefeff` | `#fefeff` |
| Hover / pressed fills | `#5964c7` / `#4f5bbf` | Same |
| Links / focus `--accent` | `#545fbf` | `#a0a8ff` |

Separate action fills from link/focus colors: a readable dark-theme link is too
light to serve as a background under white text. Success stays green; warnings,
errors and condition verdicts keep their existing semantic pairs. Indigo does
not mean a successful runtime result. Selected sidebar rows use a quiet neutral
fill; inactive entries are muted. Borders separate surfaces without enclosing
every level of hierarchy.

The palette tests measure the named text/background pairs and focus rings. They
do not certify complete WCAG conformance: subtle borders are not all 3:1, and
assistive technology and device testing remain separate work.

## Components and action placement

- Use `Button` for actions and `ButtonLink` for navigation styled as an action.
  Both share geometry and states. Use `primary`, `secondary`, `quiet`, `danger`.
  Hover changes paint only. Keyboard focus remains visible.
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
  without repeatedly filling the entire result area. Diagrams may shrink to
  fit but must not be enlarged beyond their natural SVG coordinate size.

## Workspace frame

`AppShell` groups main, Inspector and console in a stable `.desk-workspace`.
The wrapper owns the complete 1px `--border` boundary, `--radius-panel` (12px)
on all four corners, `--bg` surface, and clipping. Individual panes have square
corners. Inspector uses a left divider; console uses a top divider. Floating
menus, popovers and narrow-screen drawers remain portaled above the frame.

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
Runtime's global connection badge remains a status indicator; the Admin popover
adds diagnostics without changing the badge's behavior on other routes.

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
