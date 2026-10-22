# Chat context and recovery validation — 2026-10-14

## Shared context update and Gemini progress — installed

The shared context implementation supersedes the earlier one-request projection fix below. Installed manifest: `65c30e6c3a12abb47b5998a17311d683f8a88421605aa829ecce2dec13aedaec`.

- 88 frontend test files passed: **1,634 tests**, including adapter conformance, architecture coverage, typed retained-context retrieval, active approvals, compaction, cancellation, profile validation and the saved-chat replay.
- `go test -buildvcs=false ./internal/aicontext ./internal/codexbridge ./internal/desk` passed. Tests cover fresh project guidance, request guards, provider bodies, server research retrieval/isolation and existing Desk behavior.
- The 3 MiB undeclared relay-body regression is retained and now asserts rejection before provider dispatch; a separate accepted-size case verifies exact forwarding.
- The Gemini browser fixture passed for main chat and the right pane with a restored saved draft. Spinner, dimming and reduced motion passed, with no browser errors. Scripted provider responses only; no paid inference. Screenshots: `<scratch>/jps-context-gemini-main.png`, `<scratch>/jps-context-gemini-pane.png`.
- TypeScript, production Vite and Go binary builds passed. Localization validation reported only the existing fallback backlog in 11 catalogs; both new context tool labels exist in every catalog. The existing bundle-size advisory remains.
- Bundle hash verification and capability probe passed. Rechecked 33 chats, 39 operation files and 10 Job databases; no active work was recorded. The idle launcher was stopped gracefully before installation.
- All **789** fingerprinted saved JSON/JSONL files remained unchanged. Only the Desk executable changed in the selected bundle. No detached server was started; port 5173 was free after installation.

Start the normal Desk task and refresh the browser to use this update. No configured project brief is required. Active projections and retrieval ledgers are transient; saved messages and journals remain authoritative. Context sizing is conservative and estimated, not exact provider token accounting. Codex owns its internal opaque context; the application controls its initial request and callback boundaries. No cross-chat semantic memory was introduced.

The [Plan-mode review](../design/chat-plan-mode.md) documents the next integration and control semantics. Plan mode itself is not enabled by this update.

## Earlier projection-only update

Installed the Desk context projection and explicit retry fix into the existing local bundle. Selected manifest digest: `4f67f295f75216add058c981f5cc2b9c86c33e93a9bbc9409dc782c7bb9ee650`. The bundle was verified and capability-probed before installation. Only its Desk binary was replaced; provider/runtime components were retained.

The saved mapping chat had 138 turns and approximately 35,000 visible characters. Repeated historical workspace context inflated its prompt to 236,595 characters. The revised projection is 85,732 characters. A replay of the real saved request used a scripted model response, made no provider calls, and preserved all existing turns. This proves request construction/recovery; it does not claim a live mapping response or a saved mapping.

## Checks

- 37 shared-chat and adapter test files: 534 tests passed, including the native runtime expectation fixture and the saved-chat replay.
- Final focused run after retry-work association was corrected: 116 passed, 1 opt-in native-runtime test skipped (that fixture passed in the broader run).
- Browser recovery test passed: persisted interruption, explicit retry, tool progress and a single tool call, shared spinner/reduced motion, narrow/light/dark rendering, no browser errors. Uses a scripted provider.
- TypeScript, Vite production build and Go binary build passed. Vite retains its existing large-chunk advisory.
- Localization check completed with existing English-fallback warnings in 11 locale catalogs. No new message strings were introduced by this fix.
- Rechecked 33 chats, 38 operation files and 10 Job databases before stopping the idle Desk launcher.
- SHA-256 comparison confirmed all 773 fingerprinted saved JSON/JSONL files unchanged after installation.
- No detached Desk process was started; port 5173 was free after installation.

## Use

Start the usual Desk task, refresh the browser and select **Retry response** on the failed mapping request. The original user message is reused without appending it again. Genuine requests above the existing guard are still rejected; automatic summarization is not implemented by this fix.

See [the context and interaction audit](../design/chat-context-management.md) for the now-implemented shared context layer and its limits.
