# Dialects → companies → people → decisions: verification

Checked 2026-10-14 in isolated projects. Existing user data was not used as a test fixture or rewritten. No outreach was sent.

## Delivered

- New Dialect uses the shared form layout and a generic template chooser: blank, general research, companies, people at selected companies, work-item triage, and table records.
- Validated local template-file import, with example non-GTM presets. Connection credentials, records, approvals, and executable settings are not portable template content.
- General research supports `record` entities. Distinct item URLs on one website do not collapse into a company-level identity.
- Selected company rows can create a separate people Dialect with exact company IDs and the source revision pinned.
- New person rows retain company ID, name, and URL for mappings and exports. Existing published records retain their previous encoding so accepted reviews remain verifiable.
- Manual mapping and chat review share existing/planned columns, including company relationships before research produces rows.
- Fixed the release-review link to Tests and a chat-navigation bug that could redirect an individual test-case page to Overview.

## Verification performed

| Layer | Evidence |
| --- | --- |
| Live providers | Company research completed with 1 record and 3 retained sources; person research completed with 1 record and 2 sources. Actual configured API/search and Gateway acquisition were used. Each stage was bounded to 1 record, 2 searches, 3 reads, and 10 model requests. |
| Full workflow | Controlled model/page fixtures exercised actual research scheduling, signed source proofs, publication/dedup, selected-company lineage, conversion, accepted review, a tested pack release, real Runner execution, and verification export. |
| Workflow browser | A real researched row became a frozen test snapshot; expected outcomes stayed unset. Case page survived chat creation and refresh. Jobs connection saved paused, enabled only after review, returned `proceed`, and paused again. No browser errors. |
| Template browser | Generic default, non-GTM creation, valid import, rejected approval-bearing import, compact shared form, origin persistence, selected-company handoff, refresh. No research or Job requests from creation. No browser errors. |
| Pack browser | Sources/tests retained, one existing chat composer, linked messages survived refresh, proposal review/save, incorrect fact types blocked, routed case editor and shared table geometry. No browser errors. |
| Incremental Jobs | Real Runner durable outbox/recovery checks passed, including eligibility, lost acknowledgement and duplicate delivery behavior. |
| Frontend regressions | Related suites: 225 passing; after the planned-column addition, all 146 Dialect tests passed. Pack-authoring/draft/API-engine suites: 239 passing, including native Runtime expectation validation. These groups overlap and are not a combined count. |
| Backend regressions | Research, recovery, mapping and handoff suites passed with matching companions. New generic entity, template origin and versioned relationship tests passed. |
| Build/localization | TypeScript and production build passed. Localization check passed with zero placeholder errors; existing incomplete translations use English fallback. |

The live smoke is a bounded integration check, not a claim that an arbitrary search will find ten suitable contacts. Pack-authoring orchestration used controlled AI responses; the discovery smoke used live AI. User pack policies and expected test outcomes were not changed to force passing results.

## Exchange boundary

As checked, `Judgment-Pack/judgment-pack-exchange` is not available. Spec #141/#142 and Runner #75 remain proposals. Desk presets are local authoring aids, not an approved Exchange document format or publisher-verification claim.

The future catalog adapter should download pinned entries, verify their exact digests and declared pack/profile compatibility, run their Runner fixtures, and propose reviewed local bindings. It must keep credentials and execution permissions local. See [the workflow and Exchange design](../design/dialects.md).

## Installed locally

Only the Desk executable was replaced; the existing matching Runtime, Runner and Gateway were retained and capability-probed.

Manifest digest: `cfe0dd6bb51b63579f8be9fc649cf14fe1f38cab77d52f10959503e6f6adf1b3`

All 768 fingerprinted saved JSON/JSONL files were unchanged after installation. Desk is stopped and port 5173 is free, ready for the regular launch task. Start Desk and refresh the browser.

Start with **New dialect → Find companies**, review the results, then select companies and choose **Find people at selected companies…**. Connect the relevant saved pack, review mappings and test expectations, run tests, then create a one-off or reviewed incremental Job. Generated records still require an accepted review before decisions run. This flow prepares outreach candidates; it does not send outreach.
