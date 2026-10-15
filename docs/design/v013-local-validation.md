# Local v0.13 validation

The combined release scope is recorded in [v0.13 release scope](v013-release-scope.md).
As of 2026-10-15, Codex and Claude contribute to that one release. Dated results
below describe the build checked at that time; they do not establish readiness
of later changes or completion of the combined release gates.

This worktree preserves the pre-existing local chat/research changes and uses
main at ad9296f with Runtime 0.30.0, Gateway 0.11.0 and Runner 0.8.0. No PR,
merge or release is authorized by this local test cycle.

Implemented locally: web-access opt-in and contextual recovery; repair
preconditions; expected public keys for Runner boot and managed project
Runtime invocations; per-run signing states; explicit saved-record signature
verification. The retained key list, never a re-derived replacement seed,
defines the managed project expectation. An explicit project expectation or
startup environment retains its configured semantics. Configuration is not
silently migrated. Successful reviewed rotation reconnects runtime relays.

The explicit saved-record action fetches one export and checks its original
audit bytes and sidecar with Runtime against Desk's retained Runner public-key
history. It adds JSONL framing only, never re-encodes the payload.

Desk now saves the release digest from the local release-check response before
it reaches the browser, plus the installation's trusted input profiles at
that time. Runner `verify-run` checks the same export against those saved
references. A verified signature also supplies the retained historical public
key and `--require-signed`. The digest, key and profiles embedded in an export
are never used as independent trust. Existing references cannot be replaced
by replaying a release response with different contents.

The approved UI reports record signature, release/inputs and decision outcome
separately. The decision is not re-run. Older jobs with no independently saved
references keep the explicit unavailable state; opening, verifying or
exporting them never creates references retrospectively. These are local
installation checks, not an independent witness, and do not establish source
accuracy, policy correctness or completeness of the Jobs chain. The private
project references are not part of chat backups and must be preserved with
the Runner state and signing-key history for a full move or restore. Issue
#345 still needs its independent review before closure.

The early checks below included complete translations for their then-current
messages. The 2026-10-15 translation pass completes the current Dialect and shared
AI message backlog; see [localization status](../localization.md). Dialect
discovery and reviewed evaluation (#369 / #371), together with portable Dialect
integration (#398),
are now part of v0.13. Before release: integrate both contributors’ work, translate
any new messages, complete independent review and full release validation, and
obtain user acceptance. See the [Dialect foundation](input-preparations.md)
and the combined release scope for implementation boundaries.

The Activity tab now uses Runner's advertised `journal-backward` capability:
it opens the newest page, fetches earlier entries through **Load older**, and
refreshes from the newest forward cursor. Sequence-based row identities stay
stable while earlier pages are added. Older Runners retain their prior forward
reader and coverage wording. Cursor combinations are validated by Desk before
proxying, and loaded data is bounded with explicit coverage/limit states.

Validation completed on 2026-10-11:

- Full Go suite using the pinned published companions; Go vet.
- Focused race checks for Web access, managed signing, historical reference
  storage, private Inputs storage, concurrent edits and real Runner verification.
- Production frontend build; latest full frontend sweep: 348 test files,
  5,819 tests passed (two pre-existing skipped tests). Subsequent focused Inputs
  checks passed, including two additional background-refresh/edit-conflict tests.
- Strict translation checks: 4,760 messages in each of 11 non-English locales.
- Newest-first Activity tests use a 10,000-entry stand-in and the released
  Runner. A real-browser check confirms the first request is `tail=true`, not
  `after=0`. Older-page retries preserve the newest refresh cursor; navigation
  cancels pending requests and malformed sequence pages are refused.
- Isolated browser checks with a real completed Runtime/Runner job: explicit
  signature and reference verification, unchanged export download, missing
  historical references, and verification after restarting Desk.
- Desktop and narrow verification layouts have no page-level horizontal
  overflow. The shared details disclosure keeps the three results separate.
- Main, pack and graph chats open the same Web access settings dialog even
  without an AI connection selected. Cancellation restores focus. Keyboard
  changes persist across a Desk restart; applying settings never retries a
  source read. These recovery checks use synthetic blocked-source history,
  not a live Google search/provider request. No browser JavaScript errors.
- Real Runtime/Runner tests reject changed repair tails, substituted seeds,
  tampered audit records, modified signatures, unrelated public keys, changed
  release contents and invalid independent input profiles. Missing references
  remain unavailable; saved references survive reopening the project.
- Final focused UI regressions cover cancelled requests, navigation between
  runs and the distinction between a lost connection and missing references.

The original checkout's working diff is unchanged. The implementation is in
`/tmp/jps-desk-v013` on `codex/v0.13-local`; its local executable and verified
companions are in `/tmp/jps-v013-components`.

Local Desk was restarted at `http://localhost:5173/`. Browser verification
confirmed the app loads, Runtime connects, the new Web access control is
present and remains Off, and the Inputs workspace loads with no JavaScript
errors. The executable is `/tmp/jps-v013-components/jpack-desk-input-binding`; the earlier local executable
remains available for rollback.


Additional local validation on 2026-10-11:

- The optional safeguards setup offer moved from Library to the bottom status
  strip. A shared status trigger and neutral popover preserve project-scoped
  dismissal, Admin access and connection/configuration priority. Real-browser
  checks passed at 1440, 390 and 320 pixels, including keyboard focus restoration
  and confirmation that no project configuration was written.
- The local Inputs slice now retains a chosen pack snapshot and shared mapping-v2
  column assignments with each recipe version. Released Runner preview checks
  map selected supplied records without creating a release, job or decision.
- A full frontend sweep passed 349 files; one configuration request allow-list
  assertion needed the newly expected `/api/upgrade` read. After that correction,
  all 68 tests in the nine affected UI suites passed. The additional review-state
  test passed, covering unchanged strings, mismatched types and unknown evidence.
- Input storage, recipe-binding and concurrency tests passed under the race
  detector. Go vet, production frontend build and TypeScript checks passed.
- Strict locale validation passes for all 4,772 messages in each of the 11
  non-English catalogs. The original Desk checkout diff remains unchanged.

AI discovery, reviewed type conversion, batch evaluation and graph job handoff
are not implemented by this Inputs slice. This remains a combined local test
build, not a released v0.13 artifact.

The final local restart and browser smoke check succeeded. The app and Inputs
load, Runtime reports connected, dialog actions align correctly, and the Web
access setting retains its previous Off value. No JavaScript errors were seen.
The complete current Inputs browser flow passed after the final dialog changes;
its request trace confirmed no external reads, jobs or decisions were created.

## Desk-wide notices and Inputs styling — local follow-up

The status strip now collects configuration problems, local-processing availability,
shared AI-connection read failures, folder availability and safeguards setup through
`DeskStatusNotice`. Repeated reports from mounted panes appear once. One bounded
trigger lists all current notices, with service faults before optional setup. Runtime
connection recovery stays at the right edge, and an outage no longer replaces the
workspace. Authentication recovery still uses the existing access screen.

Attention labels and icons use the same amber token as failed chat steps. The
safeguards popover uses a single compact header/body inset, shared buttons and no
nested notice border. Inputs names use the Library pattern: a muted icon and a
plain, regular-weight link with visible keyboard focus.

The notice audit intentionally retains form validation, failed writes, stale-edit
conflicts, requested-operation errors, run/signature results, and chat source setup
at the relevant control or record. They describe the current task rather than Desk
health. Admin's detailed settings and diagnostics remain available for resolution.

Validation:

- The full frontend run passed 349 of 352 suites; four assertions in three suites
  identified the moved connection warning, a newly shared live region, and two
  CSS border declarations. All were corrected. The subsequent affected run passed
  8 suites / 693 tests, including the style conventions; the application outage
  suite then passed all 4 tests. Earlier connection/session/folder checks passed
  171 tests, including terminal authentication refusal handling.
- Production frontend and embedded Go builds passed. Strict locale validation
  passes for 4,774 messages across all 11 non-English catalogs.
- An isolated real-browser test covered Inputs creation/link styling, amber status
  color, dark/light themes, 1440/390/320px layouts, grouped configuration/gateway/AI
  failures, absence of duplicate announcement text, keyboard focus restoration,
  safeguards dismissal across reload, and continued access from Admin. No browser
  errors or configuration writes occurred.
- Local artifact: `/tmp/jps-v013-components/jpack-desk-status-notices`.
  Browser evidence: `/tmp/jps-desk-status-acceptance-result.json`.
  Screenshots: `/tmp/jps-desk-status-{desktop,light,320}.png` and
  `/tmp/jps-desk-status-grouped-{1440,390,320}.png`.

Manual check on the local build: open Inputs and confirm the icon/name treatment;
open the amber safeguards cue, close it with Escape and verify focus returns; resize
or zoom the window and confirm the footer remains usable. Review setup must only
navigate to the existing review flow; dismissing the reminder must not enable gates.

The replacement is now running at `http://localhost:5173/`. The live smoke check
confirmed the app and Inputs load, Runtime is connected, dialog actions align, and
Web access remains Off. No JavaScript errors were observed.


## Inputs conversion and durable pack evaluation — local follow-up

Implemented the approved Inputs → Review inputs → Evaluate flow using shared
Desk controls, neutral surfaces, compact mapping rows and right-aligned dialog
actions. The hierarchy follows the reviewed Linear design guidance; errors stay
next to the record or action they affect.

- Recipes retain explicit Text, Number or Yes/No conversions separately from
  mapping-v2. Original supplied values remain unchanged. Blank values remain
  unknown; invalid or lossy conversions block evaluation of the selection.
- Review shows original and prepared values, source lineage, type differences
  and evidence. An explicit confirmation binds the selected records, recipe,
  pack snapshot and saved checks before any job starts. Changed references
  invalidate an unconfirmed review.
- Confirmed batches are stored privately and reconciled by Desk after reload or
  restart. Stable submission keys recover accepted work without repeating it.
  Preparation, execution, decision and signing states remain separate. Results
  link to Jobs and remain available when a newer recipe is saved.
- Full traces remain in Runner; the preparation history retains compact outcomes
  and run references. Interrupted terminal runs require a new review instead of
  an automatic repeat.

Validation:

- Released Runtime v0.30.0 and Runner v0.8.0 integration checks passed, including
  saved-test policy, stale confirmation, lost-response recovery, restart,
  idempotent confirmation, partial results and independent release references.
  The broader focused backend race run passed, followed by the final affected
  race checks.
- The targeted frontend run passed 1,212 of 1,214 tests; two CSS convention
  failures were corrected. All 751 tests in the seven affected suites passed
  afterward, and the final four review-dialog tests passed after plural wording
  was added. TypeScript, production frontend and embedded Go builds passed.
- Strict locale checks pass for 4,822 messages in all 11 non-English catalogs.
- A disposable-project browser test covered all-record conversion previews,
  refusing invalid selections without silently skipping records, explicit
  confirmation, reload during execution, three independent decision outcomes,
  saved-record signature verification, earlier-recipe results, keyboard focus
  restoration, dark/light themes and 1440/390/320px review layouts. No browser
  JavaScript errors occurred. Evidence:
  `/tmp/jps-input-evaluation-browser-result.json`.
- The original Desk checkout's working diff is unchanged.

The local upgrade succeeded at `http://localhost:5173/` using
`/tmp/jps-v013-components/jpack-desk-input-evaluation`. The live smoke check
confirmed the app and Inputs load, Runtime is connected, dialog actions align,
Web access retains its previous Off value, and no JavaScript errors occur.
The previous local executable is preserved for rollback.

See [Inputs local end-to-end testing](inputs-local-testing.md) for the disposable
fixture, expected outcomes, restart checks and accessibility checks. This slice
supports supplied records evaluated with packs. AI discovery/enrichment and graph
job handoff remain subsequent work. No release, merge or publication was made.


## Inputs Activity styling and pagination — local correction

Activity now uses the same `Pagination` and `usePagination` components as Library
and the other Inputs collections. Preparation and confirmed evaluation events
share one newest-first table, retaining links to Jobs. The total and page controls
stay below the scrolling rows; navigation is omitted for a single page.

The activity coverage explanation is a spaced, neutral description above the
table, using the existing secondary text token. It is associated with the table
for assistive technology and does not announce an actionable warning. The table
scroll region supports keyboard focus and horizontal scrolling on narrow screens.

Validation: 619 focused UI/design tests passed, with 612 affected checks passing
after the keyboard change. Production frontend and embedded Go builds, strict
translation checks for all 11 catalogs, and `git diff --check` passed. The isolated
browser check caught and resolved a panel-sizing issue before installation, then
passed at 1440/390/320px with dark/light themes, keyboard pagination, page-size
changes and single-page navigation suppression. No JavaScript errors occurred.
Evidence: `/tmp/jps-input-activity-browser-result.json`.

Local artifact: `/tmp/jps-v013-components/jpack-desk-input-activity`. The restart
and live smoke check passed at `http://localhost:5173/`, preserving Web access Off,
working Runtime connectivity, and aligned Inputs dialog actions. No release made.


## Collection layout and Library URL — local correction

Page and tab collections now follow Library's bounded layout: fixed controls,
scrolling rows with visible column headings, and the item count/page controls at
the pane bottom, including single-item lists. The shared `CollectionLayout`
works with the existing `Pagination` and `usePagination`; native table semantics,
selection, filtering and server cursor actions remain with their features.

Applied to Inputs, Records, Inputs Activity, Jobs, Runs, job Run history and
Activity, draft/saved pack Sources, and pack/graph History. Usage now keeps its
footer below its optional details panel and scrolls its table as one region.
Embedded form, detail and dialog tables retain their content layouts. Incomplete
server inventories show loaded counts rather than claiming a complete total.

Library's collection URL is `/library`, used directly by the sidebar, folders,
quick search and return actions. There is no `/packs` collection alias or
migration redirect. Pack and graph document URLs retain their existing paths.
The shared layout keeps collection filters, pagination and previews mounted
while browsing documents.

Validation:

- Focused collection tests exposed duplicate accessible region names and a CSS
  convention violation; both were corrected. The affected Activity/design run
  passed 639 checks, with another 15 journal-bounds and Usage checks passing.
  Other suites in the initial collection run passed.
- Library route/folder/preview checks passed 43 tests; the navigation, selection,
  retained pagination and design-convention run passed another 683 tests.
- TypeScript and the production frontend build passed. The embedded development
  executable built with VCS stamping disabled after Go could not obtain VCS
  status for the temporary worktree. No release identity was assigned.
- Strict translation checks passed: 4,822 messages in each of 11 catalogs.
  `git diff --check` passed.
- Disposable-project browser checks passed for desktop, 390px and 320px layouts,
  light/dark themes, single-item footer placement, internal vertical scrolling,
  page-scroll reset, details opening/closing, and Library/document/back navigation.
  No document overflow or JavaScript errors occurred. Evidence:
  `/tmp/jps-collections-browser-result.json`.

Local Desk at `http://localhost:5173/` now uses
`/tmp/jps-v013-components/jpack-desk-collections`. The live smoke check passed for
Library, Inputs, Runtime connectivity and dialog alignment. Web access remains
Off. Existing project data/settings and the previous executable were preserved.
No merge, release or publication was made.


## Clickable collection rows — local correction

The shared `CollectionRow` now extends each table's existing primary link or
details button across its non-control cells. Applied to Inputs, Records, Jobs,
Runs, job Run history and Activity, Sources, pack/graph History, and Usage.
Inputs Activity evaluation rows open their linked job; preparation events with
no details stay non-clickable. No new execution or destructive row action was
introduced.

Rows preserve native table semantics and a single primary keyboard target.
Checkboxes and their whole selection cell, menus, secondary links, editable
content and text selections remain independent. Detail dialogs restore focus to
the original primary button. Linked rows support Ctrl/Cmd-click and middle-click;
middle-click explicitly opens a tab without an opener because browsers do not
follow synthetic auxiliary-click events.

Validation: the existing affected feature suites passed; the new interaction
suite passed after correcting event construction. The final interaction,
Activity and design-convention run passed 629 tests. TypeScript, production web
and embedded local Go builds passed, as did `git diff --check`. Disposable-project
Chrome checks covered all migrated detail destinations, independent controls,
keyboard/focus behavior, modified and middle-click navigation, and narrow-screen
row activation, with no JavaScript errors. Evidence:
`/tmp/jps-collection-rows-browser-result.json`.

Local Desk was upgraded to
`/tmp/jps-v013-components/jpack-desk-collection-rows`. The live smoke check passed
for the app, Library, Inputs and Runtime connectivity; Web access remains Off.
Project data/settings and the prior binary were preserved. No release was made.

## Compact Jobs and shared search fields — local correction

Jobs now use a single density-scaled row: pack title/version and trigger summaries
stay inline, with overflow tooltips and horizontal scrolling inside the collection
pane. Recent-run links remain independent. Run uses a play icon; Resume draft uses
a pencil. Both retain accessible names and tooltips, and Run still opens its input
form. The Actions column heading remains available to assistive technology.

Library and the other production search fields now share `ui/SearchInput`, including
Inputs/Records, Jobs/Runs, pack/graph Sources/Tests/History/Usage, chat history and
message navigation, model/connection pickers, file search and quick navigation.
Library retains its trailing folder-scope menu. Native labels, refs, keyboard and
filter behavior are preserved; search fields use the native searchbox role, while
quick navigation retains combobox semantics and prefix browsing stays text input.

Validation: 31 affected test files passed after updating search-role and icon
vocabulary expectations (904 tests); the final source-search sweep passed 162 tests
across 17 files. TypeScript, production frontend and embedded Go builds passed.
Strict locales report 4,822/4,822 translated messages for all 11 languages with no
placeholder errors. `git diff --check` passed.

Disposable Chromium checks confirmed 40px comfortable / 32px compact Jobs rows,
overflow tooltips, independent Run navigation, leading icons and focus rings across
11 collection contexts, filtering/clearing, horizontal and vertical table scrolling,
fixed footers at 390px and 320px widths, and light/dark rendering. No JavaScript
errors occurred. Evidence: `/tmp/jps-compact-search-browser-result.json`; screenshots
`/tmp/jps-compact-jobs-dark.png` and `/tmp/jps-compact-jobs-light.png`.

Local Desk at http://localhost:5173/ now runs
`/tmp/jps-v013-components/jpack-desk-compact-search`. The live smoke check passed for
app loading, canonical Library navigation, Inputs/dialog layout and Runtime
connectivity. Web access remains Off; project data/settings and the previous binary
were preserved. This is a local development build; no release was made.

## Shared table selection — local correction

Paginated collection tables now use `SelectionTable`, `SelectionHeader`,
`SelectionCell`, `SelectionActions` and `useCollectionSelection` to follow Library's
selection behavior. Applied to Inputs, Records, input Activity, Jobs, Runs, job Run
history/Activity, pack Sources, pack/graph History and pack Usage. Graph Sources is
a disclosure list rather than a paginated table. Existing Library controls and
small task-specific tables retain their current actions.

Checkboxes reserve their column, reveal on hover or keyboard focus, remain visible
while selecting, and are always visible on touch. Select-all covers the matching
loaded inventory across pages and toggles off; incomplete lists explicitly say
“Select all loaded matches.” Partial selection is indeterminate. Filtering prunes
hidden rows; owner changes clear selection; later arrivals are never silently
selected. Shift-click ranges, scoped Ctrl/Cmd+A and Escape are supported. Checkbox
cells remain independent from row navigation and existing Run/Resume links.

Selected-count menus use the shared controls in the existing toolbar. Records
retains Review inputs, Evaluate and its existing input export. Other collection
tables export only the selected rows, marked as a `desk-table-selection` extract.
Selections do not trigger job execution, source changes or deletion. New labels
are translated in all 11 supported languages; strict locale validation passed
with 4,823/4,823 messages and no placeholder or stale-key errors.

Validation: affected feature suites and the six new selection interaction tests
passed after table assertions were updated for the new leading checkbox column.
The final shared-control, design-convention and journal/bounds suite passed 663
tests. TypeScript, production frontend and embedded local Go builds passed, as did
`git diff --check`. Browser checks caught and corrected the selected-count button's
accessible name and covered exact-row exports across 11 table contexts,
cross-page/filtered selection, loaded-data scope, independent checkboxes,
keyboard/focus, touch visibility, 390px/320px layouts, fixed footers, both densities
and both themes. No job mutation requests or JavaScript errors occurred. Evidence:
`/tmp/jps-selection-browser-result.json`, `/tmp/jps-selection-menu.png`,
`/tmp/jps-selection-dark.png`, `/tmp/jps-selection-light.png`.

Local Desk at http://localhost:5173/ now runs
`/tmp/jps-v013-components/jpack-desk-selection`. The first live smoke check assumed
there would be an Inputs row and automatically restored the previous binary when
it encountered the user's empty list. After correcting that assumption, the
upgrade and live smoke passed: app, canonical Library route, Runtime connection,
Inputs empty state, dialog alignment and no browser errors. Web access remains Off;
project data/settings and the prior binary were preserved. No release was made.

## Collection controls and recent-run hover — local correction

- Shared selection controls now align to the toolbar's right edge, matching Library. Library and native tables share the accented selected-count button, green native checkbox treatment and stronger neutral selected-row background; hover and preview remain distinct.
- Recent runs use shared glyphs and semantic tokens: completed green, failed/interrupted gold, running accent and queued neutral. Glyph shapes and explicit accessible labels distinguish the states. Hover removes the inherited text underline; the shared tooltip names the submission timestamp and handles missing/invalid dates.
- Fixed adjacent-tooltip handoff when Radix's hover corridor overlaps a neighboring run icon. First-hover delay, hoverable content, keyboard focus, Escape dismissal and navigation remain intact. Added a focused regression for switching hints without activating the link or moving focus.
- Jobs and Runs search now lead the collection, including empty lists. Header Collection options → Download run history opens a dialog with the whole-file scope and unverified-file explanation. Opening the dialog performs no download; the explicit Download action preserves the runner's original bytes and filename. Selected-row exports remain independent.
- Shared collections no longer reserve an unused scrollbar gutter. One-row Inputs and Jobs tables were measured at zero left/right gaps to their pane; overflowing lists retain scrollbars and the fixed footer.
- Relevant tests passed: 747 tests across Jobs index/export, History, palette and conventions; then 648 tooltip/convention tests after the tooltip fix. Shared selection, Library actions and tooltip regressions also passed during the correction. TypeScript/frontend and Go builds pass. All 4,823 messages are translated in every supported non-English locale; strict checks and whitespace checks pass.
- Browser fixture checked Library/Inputs/Records/Jobs selection alignment, five status shapes, both themes, both densities, keyboard and adjacent pointer tooltips, direct run navigation, exact export bytes, focus restoration, 320px/390px layouts and full-width one-row collections. No browser JavaScript errors. Evidence: `/tmp/jps-table-polish-browser-result.json`, screenshots `/tmp/jps-table-polish-{dark,light,library,inputs,export}.png`.

References: [Linear interface hierarchy](https://linear.app/now/behind-the-latest-design-refresh), [Linear selection](https://linear.app/docs/select-issues), [Carbon tooltips](https://www.carbondesignsystem.com/building-blocks/core/components/tooltip/guidelines). Status colors follow Desk's own palette.

## Recent-run controls follow-up

The user reproduced overlapping tooltips after the previous correction. A browser fixture with five completed runs confirmed rendered-tooltip counts of `1, 2, 2, 2, 2` across adjacent icons. Controlled handoff opens bypassed Radix's internal open notification. Desk now explicitly dismisses the previous owner before opening another tooltip. Pointer activation also dismisses hints before a menu trigger can prevent the event's default behavior.

The user approved neutral completed icons. Recent runs now use unframed 16px glyphs in 24px targets: completed neutral, running brand accent, queued muted, and failed/interrupted gold with different shapes. Run and Resume have a neutral hover surface and border that remain visible against hovered or selected rows. Download run history and its dialog action use the shared download glyph.

Validation: 706 affected tooltip, glyph, Jobs index/export and convention tests passed; 650 tooltip/convention tests passed after adding the menu-activation regression. Browser verification checks every rendered tooltip, including slow and rapid movement in both directions across identical completed statuses, hoverable content, keyboard/Escape behavior, dark/light token values, Run hover contrast, menu/dialog icons and narrow viewport positioning. Evidence: `/tmp/jps-run-controls-browser-result.json` and `/tmp/jps-run-controls-{dark,light,tooltip,menu}.png`. Local-only change; no release or merge.

Final local build: `/tmp/jps-v013-components/jpack-desk-run-controls`. The browser check recorded exactly one tooltip for 18 slow/rapid transitions and no overlap mutations. Opening the collection menu leaves no tooltip behind.

## Optional Library folder metadata

A fresh project previously requested `jpack-folders.json` as a required file, producing a browser-console 404 even though the folder loader correctly used the default home folder. Library now uses an explicit optional file read: an absent file returns 204 with `Cache-Control: no-store`; an existing file keeps the original content and digest. Ordinary reads retain their 404 behavior, and authentication, containment, symlink, permission and decoding failures remain errors. Visiting Library never creates metadata.

Validation: 34 frontend tests passed for the file client, folder persistence and FolderBrowser. Targeted Go tests passed for optional reads, file API guards, refusal-code contracts and unreadable files. TypeScript and production frontend/Go builds passed. A disposable browser project reproduced the original 404, then verified 204 before folder creation and 200 after creation/reload, with no page errors or writes on the initial visit. Browser fixture: `/tmp/jps-optional-folders-browser.mjs`.

## Library, Inputs and Jobs consistency review — 2026-10-12

Reviewed the live pages against Library, the shared design system, Linear’s 2026 interface refresh and selection guidance, Carbon’s table guidance, and WAI’s table guidance. Found three different search widths (368/320/360px), inconsistent placeholders and control gaps, a 49px Jobs header versus 48px elsewhere, automatic versus percentage-based name columns, and different heading/metadata spacing.

The top-level collections now use the shared CollectionToolbar. Inputs and Jobs/Runs use SelectionTable’s index variant, while Library’s virtualized list consumes the same geometry tokens. The pages retain their contextual actions, folder scope, job tabs, and selection/export behavior. Empty-state hierarchy, neutral type colors, table labels, keyboard focus, numeric count alignment, truncation, and narrow-pane filter menus were reviewed alongside the requested header/search/name-column work.

Validation: 684 final affected component/convention tests passed; production TypeScript/frontend and Go builds passed. The isolated browser fixture uses 61 collection items, long names and all recent-run states, without running jobs or changing the real project. It passed 16 desktop combinations (four views × two themes × two densities) and 12 narrow combinations (four views × 640/390/320px). At the same 1206px pane width all four views measured 48px headers, 368px search fields, 288px name columns, 13px normal-weight names, 32px column headings, 40px comfortable rows and identical footer positions. Compact controls/headings were 28px and rows 32px. Selection stayed visible and its menu actions remained reachable at every narrow width. No browser page errors or body overflow were observed.

Fixture and evidence: `/tmp/jps-collection-consistency-browser.mjs`, `/tmp/jps-collection-consistency-result.json`, `/tmp/jps-collection-after-{library,inputs,jobs,jobs-runs}.png`, and `/tmp/jps-collection-narrow-{library,inputs,jobs,jobs-runs}.png`.

The final localization pass removed two retired search-placeholder keys (no missing translations) and aligned Inputs timestamps with Jobs’ shared, locale-aware medium-date/short-time formatter. An additional UI regression verifies that switching Desk to French updates input dates without losing rows, and that invalid timestamps display Not recorded. The final affected Inputs/convention checks passed (644 tests, including that added regression).

Final follow-up: Inputs and Jobs now have plain-text main headers; Library retains its functional collapse control. Shared selection menus show the existing download icon for Export selected rows and close icon for Clear selection. The final targeted collection suite passed (15 tests), the frontend and Go builds passed, and the final browser fixture passed eight desktop and four narrow cases, including both menu icons and plain collection headings. The local Desk at localhost:5173 was updated and its live smoke check passed with the runtime connected and no browser errors. Evidence: `/tmp/jps-collection-final-smoke-result.json`.

## Combined v0.13 checkpoint — 2026-10-15

Local commit `6e14150` retains the accumulated Desk implementation and the
[combined v0.13 scope](v013-release-scope.md). Claude's ongoing portable-Dialect
work has not yet been integrated into this checkpoint. No push, PR merge or
release was performed. The machine-specific `.vscode/tasks.json` remains outside
the commit.

Checkpoint verification:

- Full Go suite, Go vet, TypeScript check, seven local-launcher tests and four
  UI-message-audit tests passed.
- The full frontend sweep ran 421 files: 6,571 passed, six skipped and nine
  failed. Five failures were old editor-status assertions, corrected without
  changing their save/navigation behavior checks; all 26 affected editor tests
  then passed. Four failures identified scroll containers missing a positioned
  containing block; those CSS rules were corrected, and all 147 affected layout
  and record-review tests then passed. The full sweep was not repeated after
  these focused fixes.
- Final production frontend build passed. All 56 localization tests passed after
  extracting the catalog and pruning obsolete translation keys.
- Normal catalog/source checks passed with zero placeholder errors in all 11
  translated catalogs. Strict checks fail solely on the deferred translation
  backlog; see [localization status](../localization.md) for exact counts.

These checks establish this local checkpoint only. The integrated revision still
needs the shared release gates, translations and final acceptance recorded in the
release scope.

## Current v0.13 translations — 2026-10-15

The user requested translation immediately, superseding the extraction-only
sequence above. All 11 non-English catalogs now cover the current 5,696 English
messages. The pass adds 10,922 entries, including required plural forms, without
changing any previously translated value. Technical identifiers, interpolation
values and numeric limits were checked separately from wording.

The browser audit now uses the current Library route and includes Dialects,
the new-dialect form, Jobs and job creation. Screenshot review found a shared
chat-context button using the browser's default font, leaving Korean text as
missing glyphs. The button now inherits Desk's existing font stack; a browser
assertion checks the Korean fallback on that control.

Validation for this translation pass:

- Strict catalog/source checks passed for all 11 locales: 5,696/5,696 messages,
  zero placeholder errors, and no untranslated source literals.
- All 56 localization tests and all 846 palette/convention tests passed.
- Production frontend build and Go build with embedded frontend passed. The Go
  build used `-buildvcs=false` because VCS stamping was unavailable in this local
  environment. The existing frontend chunk-size advisory remains.
- The isolated browser sweep passed for English plus all 11 locales, including
  live language switching, unsent-draft preservation and reload persistence.
  German, Cantonese and Japanese also passed both themes at 1440, 768 and 390
  pixels. The sweep reported no browser errors, untranslated checked labels,
  horizontal page overflow or saved conversations; it made no AI-provider calls.
- After the font and final wording corrections, a focused browser rerun passed
  for Korean, European Portuguese and German, including the new font assertion
  and German’s narrow layouts.
- Original-catalog comparisons and `git diff --check` passed.

Claude's portable-Dialect integration remains separate. This completes the
current translation backlog; re-extract and translate any new messages after
integration before final release acceptance.
