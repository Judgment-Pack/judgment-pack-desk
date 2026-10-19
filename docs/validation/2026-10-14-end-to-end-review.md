# End-to-end implementation review — 2026-10-14

This review covers the current Desk worktree in `/tmp/jps-desk-v013`, including the earlier Dialects, research, pack/test, Job, chat, context, memory and Plan changes. Tests use disposable projects and local scripted providers, with the installed Runtime, Runner and Gateway where applicable. The saved long chat is read only for context replay.

## Findings fixed

| Severity | Finding | Correction and regression evidence |
| --- | --- | --- |
| High | `remember_memory` could not find messages saved by the real chat client. The reader and its old fixture used `checkpoint.turns`; real conversations use `checkpoint.state.turns`. | Read the canonical checkpoint shape and correct the fixture. The browser now remembers from an actual conversation, verifies its provenance, reviews the suggestion and observes subsequent recall through the Gemini adapter. |
| Medium | A pending memory review disappeared after refresh because it depended on transient tool events. | Render pending reviews from the durable memory store. Browser refresh preserves the review; pending memory is excluded from recall until approved. |
| Medium | Plan acceptance was stored as “Work started” before dispatch succeeded. | Persist `accepted` separately, then mark `started` after dispatch. Failed dispatch remains visibly approved after reload and requires another explicit instruction. Server decoding accepts both states. |
| Medium | Revising a plan overwrote unsent composer text. | Preserve existing text and seed a revision prompt only for an empty composer. A regression covers both draft preservation and failed-start recovery. |
| Low | The case-result scroll rule lacked its containing block; several shared styles violated the repository’s scoping/token rules. | Position the case-result scroller, scope collection/chat styles to their owning classes and move forced-color values into shared tokens. Keep reduced-motion and high-contrast behavior. |

The first full frontend sweep also found stale assertions for formatted JSON, bottom-strip notices, the case route, new controls and renamed Dialects. They now check the current interaction, retaining exact original JSON in Copy and accessible warning details. Two contention-only failures passed under bounded test concurrency without weakening their assertions. Older browser fixtures now exercise current labels and selected-record reprocessing rather than the removed run-wide continuation action.

## End-to-end checks

| Journey | Verified behavior |
| --- | --- |
| Generic Dialect creation | Generic default and non-GTM templates, validated local import, rejected executable/approval-bearing imports, origin persistence and refresh. Creation starts no research or Job. |
| Company → people → decision | Actual scheduling and signed source verification, publication/deduplication, selected-company lineage, mapping/conversion, accepted record review, tested pack release and real Runner decision/export. |
| Recipe and linked chat | Propose/review/save, origin linkage in main/pane/list, refresh and history, compact Related work, shared limits, aligned controls and narrow layouts. |
| Research recovery | Correct failed records without replaying successful publication; recover completed source reads; preserve evidence ownership, selected-row scope and user reviews; reject stale writes; retain successful siblings. |
| Budgets and retries | Exact budget approvals, cancel/decline, concurrent decisions once, retry eligibility, request limits, admission waiting and Stop before dispatch. |
| Parallel research | Candidate scheduler saves ten records incrementally; bounded page reads overlap; delegated tasks retain separate budgets, retries and results. Recipe concurrency review and grouped activity pass in the browser. |
| Selected-record reprocessing | Multi-select action uses precisely the selected IDs, requires explicit confirmation, starts no new discovery and exposes no duplicate run-wide continuation action. Keyboard and narrow rendering pass. |
| Packs and tests | Saved sources/tests survive, AI test design reuses the linked session, messages survive refresh, proposals require review, incorrect typed facts are refused, cases use routes/breadcrumbs and the shared collection. |
| Expectations and Runtime | Native Runtime expectation admission/correction and retained snapshot checks pass. Expected decisions are not rewritten to match observed answers. |
| Jobs | Tested-release admission, pause/explicit enable, real decision and pause again; durable outbox, incremental revision eligibility, recovery and duplicate-delivery protections. |
| Chat and providers | Shared main/pane behavior, interactive questions, keyboard/IME, persistence failure, single continuation, explicit retry, Stop and refresh without replay. Gemini progress/spinner/dimming and reduced motion pass. |
| Delegation | Child capability isolation, exclusive-provider handoff, retained results, parent Stop propagation, no restart on reload and narrow/light rendering. |
| Context | Full architecture/conformance checks plus replay of the largest saved chat (142 turns), with no provider calls and exact preservation of prior turns. |
| Memory and Plan | Real-message provenance, review-before-recall, explicit remember, settings/storage lifecycle, forget/clear/staleness, provider coverage, plan persistence/reference checks, read-only capabilities, Auto-admission refusal and explicit Start. |

All completed browser journeys report no browser page errors. Geometry, semantics, navigation, keyboard behavior and screenshots were captured; the local image viewer’s sandbox failure prevented visual image inspection.

## Validation record

Logs are retained under `/tmp/e2e-review-*`. The initial failures and successful corrective reruns are retained, rather than replacing their evidence. The final combined frontend run enables all three native/saved-chat opt-in fixtures.

- Full Go package suite and `go vet -buildvcs=false ./...`.
- Full frontend suite, including context architecture and certified adapter conformance.
- Native Runtime and saved-chat opt-in fixtures.
- Production TypeScript/Vite build, Go binary build and localization checks.
- Browser fixtures listed above, using the built application and disposable storage.

## Limits and follow-up

This review does not repeat paid live model/search calls. Earlier bounded live discovery evidence remains in [the outreach validation](2026-10-14-dialects-outreach.md); current tests establish orchestration, persistence and integration behavior, not search quality or a guarantee of ten suitable prospects.

Exchange catalog integration remains outside the implemented boundary described in [the Dialects design](../design/dialects.md). Built-in/imported templates are local authoring aids, not a live Exchange catalog or a publisher-verification claim.

Context sizing is conservative, not exact provider token accounting. Memory is optional, locally retrieved and bounded; Plan dependencies include linked and explicitly named objects, not an automatically inferred dependency graph. These constraints remain in the shared implementation rules for future AI features.

Existing translation fallbacks, the Vite bundle-size advisory and an AI SDK compatibility-key deprecation remain maintenance items. They did not fail the behavioral checks.

## Reproduction

Use Node 22 and Go with `-buildvcs=false`. The browser tests require an existing production `web/dist`; do not rebuild that directory while a browser fixture is serving it.

The review’s real-companion environment is `/tmp/e2e-review-env.sh`. It pins `JPACK_BIN`, `JPACK_RUNNER_TEST_BIN` and the Gateway bundle to the previously verified installation. Core browser test names are `TestDialectCompanyPeopleTestedJobJourney`, `TestDialectTemplatesBrowser`, `TestPackWorkspaceBrowser`, `TestInputChatBrowser`, `TestInputRecordReviewBrowser`, `TestAIContextGeminiBrowser`, `TestChatQuestionBrowser`, `TestCodexQuestionRecoveryBrowser`, `TestChatRecoveryBrowser`, `TestDelegationBrowser` and `TestAIMemoryPlanBrowser`.

The record-review fixture additionally runs `research-reprocess-browser.mjs` and `research-delegation-browser.mjs` through its `JPS_INPUT_REVIEW_BROWSER` override. Set `JPACK_EXPECTATION_BINARY` for native frontend checks. Saved-chat replay requires explicit `JPS_CHAT_CONTEXT_STORE` and `JPS_CHAT_CONTEXT_ID`; it reads the named conversation without saving or contacting a provider.

## Final result and local installation

- **403 frontend files, 6,308 tests passed; zero failures and zero skips.** This includes native Runtime checks and the saved-chat replay. Final log: `/tmp/e2e-review-complete.log`.
- Full Go package suite passed; 50 focused memory/Plan/research/real-companion checks passed, with additional context, incremental evaluation and durable subscription checks passing after the fixes. `go vet` and `git diff --check` passed.
- All browser journeys in the matrix passed, including corrective reruns of the renamed/updated fixtures. Each reported zero browser page errors.
- Production TypeScript/Vite and Go builds passed. Localization has zero errors and zero unextracted English messages; 11 existing incomplete catalogs retain English fallback.
- Installed and capability-probed manifest: `bd8af748673c4f6e84b702b5ad0bb1665a69d27644f5f890309787f036257f72`. Only the Desk executable changed; matching Runtime, Runner, Gateway and adapters were retained.
- All **1,666** fingerprinted project JSON/JSONL files were unchanged across installation. This broader snapshot includes the **789** private saved files verified unchanged since the previous installation.
- No Desk process was active, and no detached process was started. Launch the usual Desk task and refresh the browser to use the update.


## Follow-up: context reads exhausted the diagnostic turn

The reported 10:10 diagnostic request was saved as failed with 20 successful `read_context` work items and no final answer. The persisted work records show tool names and completion states, not the exact read arguments; they do not establish whether the model repeated a page or traversed different pages. The earlier saved-chat replay checked projection size and returned a scripted answer immediately; it did not exercise repeated retrieval. That coverage was insufficient for this path.

Changes:

- Prioritize the explicit latest message and its current pack/tests input during projection, with a separately retained searchable current-input reference. Preserve recent context and continuation constraints; leave original history untouched.
- Add bounded literal `query` search to retained text so a model can locate a case/rule without reading every history page.
- Reserve the final API request for synthesis within the existing cap, remove tools and enforce the restriction at dispatch. Report an exhausted budget specifically if no answer is produced.
- Preserve the interrupted-conversation phase in checkpoint decoding and interrupted conversational response recovery across reload even when an older draft exists; successful retry/follow-up clears the old conversation error, preserves completed steps and does not duplicate the user message.
- Extend the saved-history replay to 19 real Gemini SDK context calls followed by the reserved final response. Add equivalent API-family tests, subscription context retrieval, final-step mutation refusal, and visible labels for all retained work rows.

Verification: **88 shared chat/research/assistant files, 1,584 tests passed**, including installed Runtime opt-in checks and the saved-chat replay. The smaller context/adapter pass contains 481 passing tests. Production TypeScript/Vite build and whitespace checks pass. Logs are `/tmp/chat-context-fix-*.log`.

The actual pack's latest saved run remains 20/21. `review-no-routing-missing-size` expects `set-aside` with omitted size facts; it observed `unresolved`/`unknown`. The stale-or-undated-size exception has `onUnknown: escalate` and is considered before the no-routing outcome rule. Missing is not false. This is a pack-policy precedence conflict, not a reason to weaken the expected result or change Decimal string encoding. The saved pack, tests and their outcomes have not been edited by this fix.

The extended production-browser fixture passes with the same scoped conversation and existing pack candidate: explicit Retry, 19 context reads, final synthesis, preserved draft bytes and one original user message, plus reload persistence. Gemini main/pane progress, spinner/dimming and reduced-motion checks also pass with zero browser errors. During this extension, the old fixture's missing workspace linkage was corrected so it cannot accidentally test a fresh chat. The reload regression additionally exposed and fixed the checkpoint decoder dropping `phase`.

Browser evidence: `/tmp/chat-context-fix-browser-complete.log`. SDK fixtures exercise OpenAI-compatible, Anthropic and Gemini final-step behavior, and the subscription adapter exercises focused/searchable context. These are scripted providers; no paid live model was used and no claim is made about a model always selecting the ideal reads.


Final follow-up result:

- **403 frontend files / 6,322 tests passed, zero skips**, including installed Runtime fixtures and the saved-chat SDK replay (`/tmp/chat-context-fix-all-final.log`).
- Production browser recovery passed on the final assets (`/tmp/chat-context-fix-browser-release.log`), with zero page errors. TypeScript/Vite, Go binary build, localization checks and `git diff --check` passed.
- The initial broad run found an ownership regression from preserving all saved execution phases. The decoder now preserves only the interrupted-conversation hint; draft/check phases are reconstructed as before. Draft ownership and the full suite pass. A timing-sensitive ResearchProgress assertion also passed unchanged in the bounded final run.
- Installed/capability-probed manifest: `6662ef488a7e7f62e61e2d8f58971594d46d2bee1fb056287f89c52a030b7b7c`. Only Desk changed; companion versions were retained.
- Verified no active chats/research/Jobs before replacement. Restarted the previously running local Desk on port 5173 and verified that it serves the current built assets.
- All **1,666** fingerprinted project JSON/JSONL files remained byte-for-byte unchanged before installation, after installation, and after restart. The reported conversation remains intact with 143 saved turns and its 20 retained work entries.
