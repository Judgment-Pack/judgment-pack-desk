# Project memory and Plan mode — 2026-10-14

## Implemented

- Project memory uses an owner-only bounded JSON store, separate Use and Suggest controls, review for inferred entries, verified explicit remember commands, source attribution, freshness checks, edit/forget/clear, deletion tombstones, backup and project-move support.
- All API families and Codex receive relevant approved memory through the shared server context layer. Recall is local keyword matching with five-entry/4,000-byte limits. No paid summarization or vector database was introduced.
- Plan uses the existing shared chat/composer, task menu and `/plan`. Source policy remains independent. Plans retain revisions and referenced object digests. Start work rechecks the references and records acceptance; existing approvals and budgets remain required.
- Shared adapter capability filtering blocks artifact/workflow/memory writes while planning, including direct runtime dispatch and emitted proposal events. Children inherit the phase. Server admission prevents a planning chat from spending a research Auto allowance.
- The repository AI implementation rules cover these boundaries for future features.

## Verification

- Broad frontend suite: **89 files, 1,613 tests passed**, three optional fixtures skipped. Includes adapter conformance, shared chat/research, context architecture and memory/Plan regressions.
- Full `internal/desk` and `internal/aicontext` Go checks passed. Final targeted memory, Plan and research-approval tests passed after the final changes. Memory-only project moves, backup restore, source staleness, credentials, CAS, symlinks, deletion protection and pending-versus-active recall are covered.
- Browser journey uses the real Gemini adapter with a local scripted provider: create a plan in main chat; reload it as the linked Dialects pane chat; explicitly Start work; include approved memory in the request; persist acceptance and refresh without replay. No browser errors and no paid calls. Screenshots: `/tmp/jps-plan-main.png`, `/tmp/jps-plan-pane.png`.
- TypeScript, localization validation and production frontend build passed. Existing untranslated catalog fallbacks remain; new untranslated messages also fall back to English. The existing bundle-size advisory remains.
- Local screenshot inspection through the image tool was unavailable because its filesystem sandbox failed. Browser layout bounds, semantics and navigation were checked programmatically.

## Behavioral limits

This does not introduce autonomous background memory mining. Memory changes come from a tool request or the management UI. Dependencies are the linked object plus explicitly named plan references, not a full automatically discovered dependency graph. Start work preserves ordinary scoped permissions. Execution is not resumed automatically after refresh or uncertain dispatch.

Installed verified manifest `b81d876ddc237f4a92b4769ef30ed0c8cbb6e0b5ed0f356bd66e9ec27032a709`. Capability probe passed; only the Desk binary changed. All **789** fingerprinted saved JSON/JSONL files are unchanged. No Desk process was active and none was started. Start the usual Desk task and refresh to use the update.

Final focused frontend checks passed (45 tests), as did `internal/codexbridge`. The browser journey also passed against the rebuilt UI. Memory settings remain opt-in; no user history was mined or imported.
