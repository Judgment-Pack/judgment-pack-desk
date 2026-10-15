# Shared task progress and request recovery — 2026-10-14

The implementation separates a reviewed Plan (scope and approval) from the AI's working task list. Multi-stage conversational work can use `update_plan`; straightforward answers need no task-list overhead. `request_budget_increase` pauses near the allowance with a concrete reason and next step. The host also handles exhaustion without depending on the model to return an answer.

The shared conversation offers **Continue · +10 requests** or **Keep completed work**. The initial API allowance remains 20, with explicit increments up to 100. Grants and request usage are persisted before dispatch. Refresh restores the original request, task progress, exact operation arguments/results and digest-checked retained-context references. Interrupted operations are not replayed automatically. A new user message starts a new response. Native Codex sessions use the same task tracking, while their internal model loop remains provider-managed.

Only identical successful immutable reads are cached within an active session. Evaluations, writes and failed reads are never cached. Child tasks remain scoped and their request usage is persisted against the shared parent allowance before dispatch. Working plans and budget grants cannot approve artifact writes, change test expectations, leave Plan mode, or expand the existing research Auto permission.

## Validation

- Full frontend suite: **405 files, 6,341 tests passed**, with runtime-dependent fixtures enabled and no skips. Log: `/tmp/desk-execution-full-final.log`.
- Subsequent focused verification of delegated request persistence, native-provider progress, context coverage and localization: **7 files, 99 tests passed**. This includes two added delegation regression cases. Log: `/tmp/desk-execution-post-review-tests.log`.
- Gemini browser fixtures passed in main chat and the right pane. Covered existing-draft recovery, spinner/dimming/reduced motion, preserving the current question, reserving the final request, exhaustion, explicit increase, exact result restoration, task progress and refresh before/after continuation. No browser errors. Log: `/tmp/desk-execution-browser-final.log`.
- The SDK adapter tests exercise budget pause/resume for OpenAI-compatible, Anthropic and Gemini families. Native Codex task tracking does not invent a model-request counter. Tests use scripted providers; these results do not measure live model quality.
- TypeScript and production Vite build passed. Localization validation has no placeholder or extraction errors; 11 translated catalogs retain their English-fallback backlog, including the newly introduced messages. The existing Vite large-chunk advisory remains.
- `git diff --check` passed.

The shared contract and provider comparison are documented in [Plan mode](../design/chat-plan-mode.md) and [context management](../design/chat-context-management.md). Future conversational features are required by `AGENTS.md` and architecture tests to use the same execution boundary. Background Dialect research retains its server journal and separate budget decisions.

The user's saved pack policy and test expectations are not edited by this update. Existing historical runs without execution checkpoints cannot retroactively recover exact tool arguments/results that were never retained; an explicit retry starts the improved path.

## Installed build

- Selected bundle: `78df8cd6d62c899ec80a6dfd39a526514af9d352bb9d86ccc915565d2ca9ef06`. Go binary build, bundle hash verification and capability probe passed. Only the Desk executable was replaced in the selected bundle.
- Final release-asset browser checks passed for Gemini pause/extend/reload and Codex delegated handoff, scoped capabilities, retained results, cancellation and no restart on refresh. Log: `/tmp/desk-execution-release-browser.log`.
- Checked 33 chats, 39 operation files and 10 Job databases immediately before installation; none was active. Stopped the idle launcher gracefully, installed and restarted the usual port 5173 launcher.
- HTTP 200 and served assets match the final build. A single Desk process owns the selected binary.
- Of 1,666 fingerprinted saved JSON/JSONL files, **1,665 were unchanged**. The remaining conversation file was updated while a chat was running after restart and contained the new execution checkpoint. Its live journal was left untouched. No pack, test, recipe or other fingerprinted file changed during this check.
