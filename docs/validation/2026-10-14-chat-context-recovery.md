# Chat context recovery regression

This change fixes shared context navigation and continuation behavior. This report contains no chat identifiers, transcript excerpts, user pack content or saved test data.

## Defects and corrections

- Retained tools expose envelopes, so paths into an underlying document are not necessarily root paths. References now expose available sections. Object queries search keys/text and return exact matching paths, including `/content/0/text`. Invalid paths describe how to inspect the root.
- Previously, compaction always preserved the latest two messages. A completed signed exchange could itself exceed the remaining input budget. Compaction now accepts any complete call/result boundary, including that latest pair. Pending calls and approval state are protected.
- Previous summaries listed tool names without usable outcomes and pointed to nested archives. The replacement carries a flat index with exact argument/result paths, bounded previews and the authoring contract when it fits. The newest outcome has priority over older previews. Generated summaries are not recursively archived.
- Private reasoning and signed transport metadata are omitted from new retained task-memory entries. Exact public calls/results remain available. This does not rewrite original application journals or existing saved context.
- Resume previously copied the entire growing operation array into each continuation snapshot. It now retains individual immutable operations and a compact index. Recent exact argument/result handles are supplied directly on resume. Request increases remain explicit; neither retrieval nor compaction grants permissions or replays completed work.
- A chat's prior candidate is labeled as a draft snapshot, which may predate the current saved workspace. Shared instructions require editing the intended current object, preserving its identity, reusing obtained instructions and completing focused verification.

## Regression coverage

- Unit checks cover envelope navigation, Unicode/escaped text, exact false/null/zero/decimal values, repeated flat compaction, large signed completed exchanges, and protected unresolved calls/approvals.
- A real Gemini SDK fixture with deliberately large signatures completes a focused edit in five scripted requests, with one instruction read and one rehearsal. It requires no allowance increase.
- An opt-in saved-session fixture reads its input file without modification, restores the original scope and approved allowance, retrieves completed arguments/results, and verifies continuation without replay. Providers are local scripted functions; there are no external model calls.
- These checks verify infrastructure and recovery, not live-model reasoning quality or a guaranteed completion count.

Broader verification and installation results follow after completion.

## Canonical behavior audit

The audit removed the initial special case that pinned only pack-authoring instructions. All host features now declare reference priority and optional immutable read behavior through `HostTool.contextPolicy`. Graph instructions remain uncached because they include current workspace content. A deliberately misdeclared workflow tool cannot opt into read caching. Architecture tests require the shared declaration and reject the earlier feature-name branches.

Twelve additional real SDK fixtures cover four unrelated topics across OpenAI-compatible, Anthropic and Gemini. They use arbitrary future tool names, retain usable reference material and recent outcomes through compaction, and complete without a request increase. A separate fixture verifies immutable read reuse, mutable reference freshness and uncached workflow calls. The focused canonical checks pass **45 tests**, including the opt-in read-only continuation fixture.

Before the metadata generalization, the full frontend suite passed **6,372 tests in 408 files**. Browser checks passed for Gemini main/pane status, explicit request extension, exact result restoration, refresh, retry timing and absence of duplicate user turns. TypeScript, Vite and Go builds passed. The final canonical build is validated separately below.

The canonical review also added a shared independent-phase reset. Both adapters clear retained author scope, working summaries and immutable-read cache before an independent critic begins. Execution accounting and original journals remain intact. Regression checks verify that an author-only context reference cannot be read from the reviewer, even if it was cached earlier. Explicit workflow classification overrides a conflicting read hint for caching.

The final affected regression run passed **414 tests in 14 files**, including adapter conformance, context/execution, planning, delegation and feature tool integration. Final browser journeys passed for both main and pane chat, explicit allowance extension, exact-result restoration and refresh. No live provider calls were made. Logs: `<scratch>/desk-canonical-final-regression.log` and `<scratch>/desk-canonical-final-browser.log`.

## Final verification and installation

- Final full run: **6,387 tests passed in 408 files**, with code unchanged throughout that run. Log: `<scratch>/desk-canonical-final-full.log`. This supersedes the intermediate audit run that mixed modules while the isolation and write-classification guards were being finished.
- TypeScript, Vite and Go builds passed. Localization has no extraction or placeholder errors; existing untranslated English fallbacks and bundle-size advisories remain. `git diff --check` passes.
- Installed manifest `b12f41633a4cad56e4b503aa110f1b10fae309ec0ee5aa2cfe46d3709e444e86` after a fresh idle check. Only the Desk executable changed; the selected runtime components stayed intact.
- HTTP 200, final built index/assets match the served files, and one main Desk process owns the selected binary. All 1,666 fingerprinted saved project JSON/JSONL files stayed unchanged.
- Installation verification: `<scratch>/desk-canonical-installed-verification.json`. Browser refresh is required for already-open tabs to load the new frontend. A failed conversation remains saved and resumes only after an explicit user action.
