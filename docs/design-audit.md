# Workspace design audit

Reviewed against the current desktop and narrow layouts, then implemented on
`design/full-workspace-sweep`. The baseline is the previous design foundation
at `853ffc6`. This review covers the entire workspace, including the Admin
Project screen supplied in the request.

The main issue was inconsistent density: large titles and controls appeared
beside tiny uppercase labels, duplicated spacing, and actions without a stable
position. Changing the accent alone could not resolve that.

## Chosen reference

The reference is Linear's [2026 design refresh](https://linear.app/now/behind-the-latest-design-refresh)
with the dedicated settings navigation shown in its
[personalized sidebar update](https://linear.app/changelog/2024-12-18-personalized-sidebar).
The [design system](design-system.md) separates values verified in Linear's
public app from spacing and states reconstructed for Unveil.

## Findings and changes

Priority describes the original impact. All rows below are addressed by this
sweep; the limits of verification are listed separately.

| Priority | Finding | Result |
| --- | --- | --- |
| High | Typography was not consistent across routes. Page titles were 24px, Admin sections 20px, body 15px and controls 14px, with many independent values in feature styles. | Locally served Inter Variable 4.1 and shared 20/16/14/13/12px roles. Page titles use weight 500. Feature styles consume the same tokens. |
| High | The pack question was 24px while section headings were about 11.5px and uppercase, producing a dramatic jump in hierarchy. | The question is 16px; section labels are 14px and sentence case. Metadata remains subordinate. |
| High | A second Admin navigation column consumed space and made the content begin far from the app rail. Its heading competed with the selected section. | Admin uses a dedicated 244px default sidebar, Back to app, a small context label, and one centered form column. Section summaries no longer truncate visibly in every nav item; full summaries remain in link titles and exceptional status remains visible. |
| High | Controls ranged from a bare browser Edit button to 36px standard and 40px Admin controls. Segmented controls had another height. | Button, input, select, action links and segmented controls use 32px comfortable / 28px compact geometry. The pack's Edit/Try it/Test matrix now align. |
| High | Default project placed an action immediately after the value, with long explanatory paths below. | A dedicated action column aligns the button to the right. The shared 120px label column aligns values; narrow containers stack the row. |
| High | Save, feedback, removal and editor actions used inconsistent placement and grouping. | Group-wide saves and feedback align at the end of the form. Editor save/undo/discard stay together. Destructive removal has its own section and confirmation. Hover changes color without moving controls. |
| High | Field spacing accumulated: the document's 24px gap and the Field's own bottom margin both separated adjacent editable fields. | One owner per gap. Shared 16px field spacing and 6px label/control spacing replace the double margin; adjacent read-only metadata uses a smaller gap. |
| Medium | Navigation, surfaces, text, selection and status used an accent that did not match the selected reference. | Dark sidebar, neutral frame/cards, muted inactive navigation and indigo actions. Success remains green. Link/focus and action-fill tokens are separate so white labels remain readable. |
| Medium | Model names, IDs and default controls lacked a compact, consistent rhythm. | Two aligned columns, 13px names, 12px IDs, smaller internal gaps, and content-aware row height. The checkbox/default distinction and search remain intact. |
| Medium | Small graph diagrams were forced to a minimum 420px display width, enlarging their labels beyond the SVG's intended size. | The diagram is capped at its natural width or 760px, whichever is smaller. Narrow containers can shrink it. |
| Medium | Coverage results repeated large warning fills and uppercase headings, overwhelming successful run status. | Neutral result surfaces with warning accents and sentence-case section hierarchy. Runtime verdicts and coverage details remain distinct. |
| Medium | Toolbars did not consistently wrap; long file names pushed byte counts and actions out of alignment. | Wrapping toolbar groups, ellipsized file names with full-path titles, and non-wrapping byte counts. |
| Medium | Author's implementation explanation and Help's expanded connection JSON occupied prime screen space. | Explanations and connection capabilities move into keyboard-accessible disclosures; the editor and help content are easier to reach. |
| High | Browser fragment scrolling could cut the Assistant heading off on a direct `/admin#assistant` load. | The shell's fragment target is the article; panel IDs stay distinct. Direct loads and section navigation keep the header visible. Trailing-slash routes retain their settings sidebar. |
| High | Moving Admin navigation into the shell could leave a mobile drawer covering the destination or destroy an unsaved draft. | Portal-based section navigation changes location without moving the form. Choosing the current or a different section closes the drawer; opening/dismissing it preserves the draft. |

## Previously reported Assistant behavior

The earlier key-saving fix remains in place and was exercised again:

- No key: Save API key and Test connection are disabled.
- Typing a key: Save API key becomes available; Test connection remains disabled.
- Successful key save: the stored-key state appears and Test connection becomes available.
- Testing loads model choices; selecting a default also enables that model.
- Save settings persists model/endpoint configuration separately from the key.
- Remove endpoint presents confirmation; Keep it cancels removal.

## Screens reviewed

The browser sweep used a copied enterprise demo with four packs and a configured
graph. It covered Home, Packs, pack reading, pack form editing, Evaluate, pack
matrix, project matrix, Graphs, graph detail, Author, all five Admin sections,
Help, the create dialog and mobile Admin. Additional workflow checks covered
saved/unsaved keys, mocked model discovery, save feedback, hovered removal,
light/dark appearance and compact controls.

| Example | Capture |
| --- | --- |
| Supplied screen's baseline, with two navigation columns | [Project before](design/admin-project-before.png) |
| Dedicated settings sidebar and row action | [Project, dark](design/admin-project-dark.png) |
| Light palette | [Project, light](design/admin-project-light.png) |
| Assistant setup and key state | [Assistant](design/admin-assistant-dark.png) |
| Save and hovered removal | [Actions](design/admin-actions-dark.png) |
| Narrow settings | [Assistant, narrow](design/admin-narrow-dark.png) |
| Navigation drawer | [Settings navigation, narrow](design/settings-navigation-narrow.png) |
| Workspace navigation | [Packs](design/packs-dark.png) |
| Document typography | [Pack, light](design/pack-light.png) |
| Dense editor fields and aligned toolbar | [Pack editor](design/pack-edit-dark.png) |
| Coverage hierarchy | [Project matrix](design/matrix-dark.png) |
| Shared primitives | [Component reference](design/design-system-dark.png) |

Screenshots are of the implemented app, not generated mockups. They use local
copied fixtures and a placeholder credential. Provider probe/listing replies
were mocked; no real model request was made. Temporary paths visible in examples
belong to the isolated review environment.

## Verification and limits

Browser checks confirmed the custom Inter font actually rendered, the default
sidebar measured 244px, and controls measured 32px comfortable / 28px compact.
At 390px and 320px, section selection dismissed the drawer, same-section clicks
also dismissed it, and an unsaved organization draft survived opening and closing
navigation. Document/body dimensions stayed within those viewports. Organization
changes and the project-default action were exercised against the copied backend.
No browser page errors were observed in these runs.

The production build and all 2,900 component tests passed. The pane-containment
gate checked 319 route/viewport/pane combinations: all 319 were contained, with
no project fixture changes. It covers 11 routes and derived breakpoint widths;
it checks geometry, not visual taste. The palette suite
checks its named contrast pairs, not complete accessibility conformance.

Remaining limits: the references do not expose Linear's entire private token
library, and exact raster equivalence across operating systems is not promised.
The live review used Chromium on Linux. Screen-reader, physical touch-device,
Safari/Firefox and broad localization testing were not performed. Synthetic
data cannot cover every production document length or provider failure. None
of the sidebar customization, favorites or drag/reorder features shown by Linear
is implied by this visual alignment.
