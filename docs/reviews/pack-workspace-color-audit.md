# Pack workspace and color audit

Reference: [Judgment Pack](https://judgmentpack.org/) (live stylesheet checked September 11, 2026), with the existing Linear-inspired desk system retained.

## Palette decision

Neutral page, sidebar, panels, menus, fields, borders and text stay unchanged.
Green and gold are the only chromatic families: no indigo, violet, red or browser
blue. Accent fills are confined to primary actions, native checked controls, identity
badges and existing status indicators. Selected navigation, menu items, toggles
and text-selection backgrounds are neutral.

| Use | Light | Dark |
| --- | --- | --- |
| Primary action | `#183b3f` | `#346b63` |
| Action hover / pressed | `#0f2b2e` / `#102f32` | `#39786e` / `#30635c` |
| Links, focus, selection text | `#0c5f68` | `#a7c8bc` |
| Selected control background | `#ededee` | `#222326` |
| Avatar / organization badge | `#183b3f` with `#f4c175` initials | Same |
| Success / true text | `#236232` | `#93cf9e` |
| Success / true tint | `#e4f3e7` | `#193623` |
| Error, warning, unknown text | `#a25b12` | `#f4c175` |
| Error, warning, unknown tint | `#fff0d8` | `#3e301e` |
| Text-selection background | `#dedee0` | `#36373b` |

The site's brand, brand-strong, links, gold scope-card title, success pair and
warning tint are used directly. Its amber `#a65d13` reads at 4.46:1 on the
warning tint; `#a25b12` reaches 4.63:1. Gold initials on the deep green badge
reach 7.33:1. Dark primary actions are lightened for visibility while keeping
white labels above 4.5:1. Dark adaptations do not imply the website publishes
a dark theme. Uploaded organization artwork is preserved.

Errors, warnings and unknown verdicts keep separate semantic tokens and explicit
labels even when they share gold. False verdicts stay neutral. Green selected
text/icons are not evidence of a successful runtime result.

## Findings and changes

- Removed old indigo/violet/red accents, including native checkbox/radio defaults. Shared selected navigation, tabs, pickers and toggles use green text/icons on neutral backgrounds; identity initials use gold.
- Added a green/gold palette guard alongside the contrast checks.
- Fixed long pack IDs overflowing the shared page header: the context now truncates and exposes its full value on hover.
- Fixed undefined `--ink-quiet` in saved-state text and `--rule` in document borders; both now use existing tokens.
- Removed the native outline on the focusable main scroll region. The shared outer frame retains its border and rounded clipping. Buttons, inputs, links and other interactive controls retain keyboard focus rings.
- Added a cross-source check for undefined CSS custom properties, including properties supplied by the shell. Radix-owned variables remain external dependencies.
- All component backgrounds, separators, hover states, selection fills, shadows and SVG strokes continue to use the shared palette. No feature-specific color literals were added.
- Fixed doubled spacing between creation fields with a reusable `FieldGroup` and limited creation title styles to their intended heading, so nested editor headings keep their own type tokens.

## Where colors are applied

| Area | Tokens and behavior |
| --- | --- |
| Application frame, header, rail, footer | Neutral `bg`, `sidebar`, `border`, `ink` families. One outer rounded frame owns clipping. |
| Reading cards, forms, dialogs and inspectors | Neutral `surface`; raised menus use `surface-raised`; no JP wash. |
| Separators and control boundaries | `border` at rest; `border-strong` for stronger separation/hover; invalid controls use `danger`. |
| Primary buttons, links, focus | Green `accent` family; white `ink-inverse` for filled actions. |
| Selection and toggles | Green `accent` text/icons over neutral `accent-soft`; selected state also carries semantic markup and weight/underline. |
| Identity badges | `brand-fill` deep green and `brand-label` gold. |
| Connection badges, true verdicts | JP-derived green `true` pair. |
| Warnings and stale-result notices | JP-derived amber `warn` pair. |
| Errors / unknown verdicts | Gold semantic pairs with explicit labels; false remains neutral. |
| Code, logs, tooltips and menus | Shared code/surface/text tokens, no hardcoded second palette. |

## Scope and limits

All 45 application stylesheets were inspected for token use; production TS/TSX was checked for separate color definitions. The shared palette tests measure text/background pairs at 4.5:1 and focus rings at 3:1 in both themes, and require identical system/explicit dark values. This is not a full accessibility certification: quiet structural borders and disabled states are not treated as text, and not every control boundary is 3:1.

Browser verification covers the creation, reading, testing and admin pages in both themes, the mobile layout, empty-area focus, and the repository's route/pane containment gate. Verification results are recorded in the PR.

## Stylesheet inventory

Counts below are color-token references, not unique rendered elements.

| Stylesheet | References |
| --- | ---: |
| `web/src/admin/AdminStatusLine.module.css` | 3 |
| `web/src/admin/ConfigPane.module.css` | 7 |
| `web/src/admin/SourceCard.module.css` | 10 |
| `web/src/assistant/AssistantPane.module.css` | 21 |
| `web/src/assistant/EndpointForm.module.css` | 11 |
| `web/src/assistant/ModelChoice.module.css` | 9 |
| `web/src/design/DesignSystem.module.css` | 16 |
| `web/src/packs/CheckStrip.module.css` | 5 |
| `web/src/packs/PackWorkspace.module.css` | 16 |
| `web/src/packs/PacksPane.module.css` | 13 |
| `web/src/packs/document/PackDocument.module.css` | 40 |
| `web/src/packs/edit/CardForm.module.css` | 4 |
| `web/src/packs/edit/ConditionBuilder.module.css` | 7 |
| `web/src/packs/edit/EditToolbar.module.css` | 2 |
| `web/src/packs/edit/LockLine.module.css` | 2 |
| `web/src/packs/edit/PointerField.module.css` | 6 |
| `web/src/packs/edit/RawJsonEditor.module.css` | 3 |
| `web/src/packs/edit/StaleWriteAlert.module.css` | 0 |
| `web/src/packs/edit/TryItPane.module.css` | 11 |
| `web/src/packs/inspector/PackInspector.module.css` | 15 |
| `web/src/routes/AdminView.module.css` | 18 |
| `web/src/routes/PackEvaluate.module.css` | 7 |
| `web/src/routes/PackView.module.css` | 6 |
| `web/src/routes/PacksLayout.module.css` | 5 |
| `web/src/shell/CreatePackFlow.module.css` | 8 |
| `web/src/shell/DescribeIt.module.css` | 8 |
| `web/src/shell.css` | 66 |
| `web/src/styles.css` | 172 |
| `web/src/ui/Alert.module.css` | 4 |
| `web/src/ui/AlertPanel.module.css` | 6 |
| `web/src/ui/Button.module.css` | 23 |
| `web/src/ui/CodeArea.module.css` | 7 |
| `web/src/ui/Dialog.module.css` | 5 |
| `web/src/ui/Field.module.css` | 3 |
| `web/src/ui/Input.module.css` | 8 |
| `web/src/ui/PageLayout.module.css` | 5 |
| `web/src/ui/Popover.module.css` | 4 |
| `web/src/ui/RetainedPanel.module.css` | 0 |
| `web/src/ui/SegmentedControl.module.css` | 8 |
| `web/src/ui/Select.module.css` | 15 |
| `web/src/ui/SettingsSection.module.css` | 6 |
| `web/src/ui/SuggestInput.module.css` | 4 |
| `web/src/ui/Tabs.module.css` | 7 |
| `web/src/ui/TextArea.module.css` | 8 |
| `web/src/ui/Toolbar.module.css` | 1 |
