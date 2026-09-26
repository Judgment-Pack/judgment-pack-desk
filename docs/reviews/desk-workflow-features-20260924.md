# Desk workflow improvements — 2026-09-24

Implemented the first four recommendations from the spacing review. Changes
remain uncommitted. Organization authentication/permissions are not part of this
pass. Desk owns these interactions; Runtime still validates/evaluates tests and
Gateway still acquires and signs source documents. No new repository, provider
adapter, agent framework or dependency was added.

## 1. Design missing tests

Tests shows **Design missing tests** beside the current suite's missing coverage
and in a run's Coverage disclosure. It opens Assistant with an editable, concise
request. Sending supplies the exact runtime probes, run identity, pack snapshot,
existing matrix rows and available case sources to the existing bounded proposal
workflow. It does not make a model request simply by opening the action.

A coverage request must still match the current pack and executable suite when
sent. Older reports explain that a rerun is needed. Runtime contract validation
and proposal correction remain in place. Gap-filling proposals are additions
only: existing IDs are refused during validation and at the final save boundary,
including after reload. The request retains its coverage run ID. Review selected
cases, save, then explicitly run the suite to see updated coverage; a valid
proposal is not a claim that coverage is complete or policy expectations correct.

## 2. Compare pack changes and runs

**Tests → Run history → Compare runs and pack changes** provides two labeled run
selectors. It compares the actual retained pack bytes through the existing
identity-aware JSON diff, including rules, evidence, outcomes and sources. It can
also compare the later tested snapshot with the current pack.

The table shows each runtime-reported status, added/removed cases, changed inputs
or attached sources, and changed expectations. Opening a case shows both runtime
reports. It never decides disposition equality from the runtime's capped display
strings, and it never attributes a status change to pack logic when the test
input or expectation changed. Failed/unreported results are not passes.
Exploratory observations remain separately labeled without pass/fail expectations.

Draft **Review → Review revision changes** compares the current candidate with a
selected retained revision. Saved-pack Assistant already requires its computed
proposal diff before Apply and explicit Save; that boundary is retained. These
are comparisons of snapshots Desk has actually retained, not a fabricated full
version history for file edits that were never recorded.

## 3. Search and switch

The compact search icon beside the user menu opens a keyboard-accessible dialog.
**Ctrl/Cmd+K** opens it when focus is outside a text editor or another modal,
following Desk's existing shortcut policy. Arrow keys select a result, Enter
opens it, and Escape returns focus to the opener.

Search covers pack identifiers/descriptions and available titles, retained drafts,
folder paths, graphs and chat titles, including archived chats. It does not search
private document bodies or fetch every pack on every keystroke. Saved-pack titles
are taken from loaded documents or retained finalized drafts; otherwise the
registered pack ID is the title. Recent destinations are optional project-scoped
local preferences; deleted items are not resurrected from that list.

Folder results use validated `/packs?folder=…` deep links and update the existing
folder browser. Navigation uses the router and preserves its unsaved-edit guards.
Partial loading failures remain visible alongside available results.

## 4. Source refresh review

Open a verified source reader, then **Source refresh review**. Public web sources
can be checked directly. Connected sources use the existing picker for a fresh
authorization grant; those grants expire and cannot safely be reused from a saved
receipt. The selected resource must match the original source identity. Local
files require an explicit replacement upload.

Each acquisition creates a new attachment and signed snapshot. Comparisons ignore
new receipt identities when content is unchanged, distinguish original byte
changes from extraction changes, and show changed pages. Both receipts are
verified against the current configured pin. Acquisition time is shown; receipt
verification is never presented as source accuracy.

The reader lists saved cases and retained packs with direct references to the
original snapshot. This is not a claim to discover every authored URL reference.
**Mark reviewed** persists the review without modifying any pack or case. From a
case, **Use snapshot in this case** updates that case's unsaved source and field
mappings; **Save case** remains necessary. Facts and expectations are preserved
for manual/AI review and an explicit rerun. Partial extraction requires consent.
Old messages, citations, pack bytes and run history retain their old evidence.

`GET/PUT /api/source-reviews` uses the existing authenticated private-record
custody: project binding, owner-only files, 16 MiB bound, atomic writes, read-back,
If-Match conflicts, accounting, relocation and backup/restore. Each project may
retain 2,048 review records, with before/after references and review timestamps.
No source bytes or credentials are copied into the review index. Failed saves
retain the comparison in the reader for retry. Closing the reader cancels or
rejects late source delivery. Previously saved reviews reverify snapshots when
opened.

## Verification and evidence

- Full frontend suite: **214 files, 4,229 passing tests, one existing skip**.
- Final focused workflow/style rechecks: **14 files, 545 passing tests** after
  request-context and comparison refinements.
- TypeScript check, production frontend build and Go binary build passed.
- Desk Go package tests and `go vet ./...` passed. The new endpoint also has an
  isolated authentication, conflict, custody, accounting and backup/restore test.
- Browser coverage: missing-test request, comparison with independently changed
  inputs and expectations, folder deep-link navigation, quick switcher keyboard
  operation, and 1440px/390px containment. No page errors or document overflow.
- A browser fixture used signed test-key receipts for a source refresh, retained
  both snapshots, saved the review, applied an unsaved case edit, explicitly
  saved it, and reloaded. All fixture writes were intercepted in memory. The
  user's saved cases and test history were unchanged; no live model request or
  vendor source acquisition was performed by verification.
- Locale catalogs include the new keys with English fallback values; native
  translations remain follow-up work. Locale completeness/placeholders pass.
- Local Desk backend restarted with its existing configuration; the new API was
  checked through the authenticated local app. The normal `bin/jpack-desk` build
  was also refreshed.

Screenshots use the browser fixtures described above:

- [Design missing tests](desk-workflows-20260924/design-missing-tests.png)
- [Compare runs](desk-workflows-20260924/compare-runs.png)
- [Quick switcher on a phone-sized viewport](desk-workflows-20260924/quick-switcher-mobile.png)
- [Source refresh review](desk-workflows-20260924/source-refresh.png)

Raw verification output remains in `/tmp/desk-features-review` for this session.
