# Saved-pack proposal review verification

The reported session contained a new 0.1.1 candidate while the actual saved pack was still 0.1.0. The local PackAssistant comparison baseline was component state, so its apply/review controls disappeared after navigation or refresh. Generic Run checks still used the original draft's one exploratory test despite the saved pack having 21 cases.

Changes retain the proposal basis and saved-suite check results in the existing checkpoint, recover the older host-authored comparison, expose review in both the pane and main chat, and keep saved-pack checks out of the composer. A saved update clears its pending card. Scalar diffs respect their small row count. Exact buffer revision guards remain in place.

Read-only replay on a temporary copy using the installed runtime: the proposed 0.1.1 passes 20/21 unchanged saved cases. `missing-company-size` differs: expected unresolved/unknown with no handoff, actual unresolved/no-match with a requested handoff. The reported no-routing case passes. Neither the user's saved pack nor its cases were modified for this investigation.

Verification artifacts:

- Focused run/store/render/check coverage: `<scratch>/desk-pack-review-tests4.log` (204 passed; three optional runtime fixtures skipped).
- Proposal lifecycle cases, including cancellation before dispatch: `<scratch>/desk-pack-proposal-lifecycle-tests.log` (seven passed).
- Real browser / runtime fixture: `<scratch>/desk-pack-proposal-browser4.log`. Checks legacy recovery, refresh, full saved suite selection, no automatic save, main-chat review, explicit Apply and Save, displayed version update, pending-card removal, no sticky composer checks, compact diff, and no model requests.
- Proposal screenshot: `<scratch>/jps-pack-proposal-review.png`.
- TypeScript / Vite build: `<scratch>/desk-pack-review-build2.log`.
- Native build: `<scratch>/desk-pack-review-go-build.log`.
- Locale check: `<scratch>/desk-pack-review-locales.log`; no placeholder errors; existing untranslated messages fall back to English.

Broader suite and installation results are recorded below when complete.

Final results:

- Full frontend run: 401/412 files passed immediately; 11 files had timing failures under broad concurrency. All 11 passed without code changes on a two-worker rerun (296 passed, two optional fixtures skipped). Together: 6,469 passed, six optional fixtures skipped. Logs: `<scratch>/desk-pack-review-full-tests.log`, `<scratch>/desk-pack-review-rerun.log`.
- Existing Library/Tests browser suite passed: `<scratch>/desk-pack-workspace-regression.log`.
- Dedicated `TestPackProposalBrowser` passed: `<scratch>/desk-pack-proposal-final-browser.log`. Enable with `JPS_PACK_PROPOSAL_BROWSER=.../web/scripts/pack-proposal-browser.mjs` and `JPS_TEST_RUNTIME`.
- Installed manifest: `40b03dd8a862beb6c9a2fd92ccafc62560871e434cdcc292abcc4943cfb15c98`.
- Installation verified all 1,666 managed project JSON/JSONL files unchanged. No saved pack, cases, expectations, or conversation entries were edited to fix the reported policy mismatch.
