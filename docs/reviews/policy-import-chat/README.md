# Policy import and shared chat verification

Prepared 2026-10-18 for Desk #459, based on `cb4b6971cd1deb3040c71e89f2046211a39bcac1`.

## Scope

Local UTF-8 Markdown/plain-text originals are retained separately from editable
Policy drafts. Policy conversations use the existing pane/main chat, context,
Plan, proposals, action registry and operation-receipt recovery. AI may propose
and save a reviewed draft update; it cannot approve or publish policy content.
The supported deployment has one authenticated local administrator.

The implementation does not claim portable Policy conformance, team RBAC,
connected-source sync, PDF extraction, policy-to-pack derivation or execution
admission. Those remain in Desk #455 and the linked repository issues.

## Validation

- Focused Go race tests cover source retention, legacy no-source identity,
  bounds, original access, stale/body/action/session/phase refusals, explicit
  manual review, one-use automatic grants, receipt recovery, private chat
  history and backup. A standalone-default fixture covers an empty history
  store with separate temporary XDG configuration and data roots.
- Frontend checks cover import and editor behavior, Policy tools and proposal
  actions, retained recovery with no write replay, concurrent proposal mounts,
  abandoned-send admission, child authorization inheritance and affected shared
  engine/conformance/context fixtures. A 128 KiB Policy exercises retained
  retrieval across API provider families and the subscription adapter.
- Typecheck and production build pass. All eleven non-English catalogs contain
  5,998 translated messages with zero placeholder errors in the strict check.
- The browser script uses the built SPA and real Runtime v0.34.0 against a new
  temporary project. It imports a file containing a BOM, CRLF and non-ASCII text;
  changes the editable narrative; saves/reloads; compares the retained download
  byte-for-byte; exercises linked chat history, pane/main navigation and reload;
  and explicitly approves then publishes the candidate as separate commands.
  It seeds only user-authored composer text through the authenticated conversation
  CAS endpoint. No assistant output or approval is forged and no paid model is
  called. Component/adapter tests, rather than this browser smoke, exercise the
  model-to-proposal/action path.

`smoke-result.json` records the browser result and screenshots show desktop
history, narrow chat and narrow editor. Optional companion-service notices are
expected because this fixture does not install those services. All temporary
listeners are stopped after verification.

## Review and dispositions

Implementation workers used OpenAI GPT-6.1 Sol: high for import/UI and xhigh for
shared chat authorization/concurrency. A separate GPT-6.1 Sol xhigh agent did a
read-only review. This is same-vendor review, not independent cross-vendor review.

The reviewer found two P2 issues: delegated children dropped the authorization
callback, and an abandoned overlapping send could overwrite the active turn's
Policy snapshot. The fixes preserve child context controls and commit prepared
snapshots only inside admitted sends. The reviewer accepted both fixes and the
malformed-input normalization fix, with no remaining findings in that scope.
Parent verification also corrected Policy ID length, history authorization versus
content availability, incoming-history/backup admission, unsupported artifact
envelopes, duplicate proposal mounting and the mobile Back label.

The first browser attempts exposed test-harness issues: a label selected both a
section and file input, and a temporary configuration lacked an isolated
`XDG_DATA_HOME`. The existing installation-owner guard correctly refused that
history store. The harness now isolates configuration, cache, data and project
roots and addresses the file input directly. No product authorization check was
weakened for testing.

The maintainer's earlier one-time exception applied only to Desk #453/#458. This
slice still requires its normal independent review before merge.

## Reproduce

Build `web` under Node 22 and build Desk. Launch it with separate fresh
`XDG_CONFIG_HOME`, `XDG_CACHE_HOME` and `XDG_DATA_HOME` directories, a temporary
project containing `{"configVersion":"3","packs":{}}`, local access enabled,
and a compatible Runtime. Keep that listener isolated from the installed Desk.

```sh
node docs/reviews/policy-import-chat/smoke.mjs http://127.0.0.1:PORT /tmp/policy-browser-evidence
```

`CHROMIUM_BIN` may select a local Chrome executable. Stop the temporary Desk when
finished. Full repository validation runs in GitHub Actions rather than repeating
the broad suites locally.
