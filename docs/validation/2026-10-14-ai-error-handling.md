# Shared AI error handling — 2026-10-14

Desk now uses one versioned failure catalogue across browser API/subscription adapters, context preparation, delegated tasks and server research. The contract retains a stable code, source, outcome certainty and bounded HTTP status/request ID/retry time. Host-authored validation remains useful corrective feedback; caught provider prose, bodies, URLs and stacks are excluded from displayed/persisted failures.

Main chat and the right pane restore the same typed recovery state after refresh. Explicit retry respects `Retry-After` and does not duplicate the user turn. Existing request-limit approval and retained-operation recovery remain separate from provider limits, cancellation and recoverable tool failures. Failed or interrupted execution cannot silently leave an acceptable proposal.

Two integration gaps were repaired: the model response facade previously stripped retry timing/request IDs, and the installed Google SDK's chat stream schema omitted error-only chunks. Gemini stream errors are now classified even after a partial answer. Raw chunks are consumed only by the adapter and never enter the saved context or UI.

## Verification

- Broad frontend run: **407 files, 6,366 tests**; 6,365 passed. Its one failure used the normalizer loaded before the final prototype-key/payload-spoof hardening. The final-code rerun passed all **13** tests in that file, and the subsequent boundary run passed **77** tests. Logs: `/tmp/desk-failure-full-final.log`, `/tmp/desk-failure-final-rerun.log`, `/tmp/desk-failure-boundary-final.log`. This is combined verification, not a claim that one final full-suite invocation was entirely green.

- OpenAI-compatible, Anthropic and Gemini SDK fixtures cover HTTP failures, in-stream failures, one terminal error/end, secret-payload exclusion and no automatic replay. A Gemini partial-answer fixture verifies the later error remains a failure.
- Native bridge tests preserve typed failure codes and cancellation. Delegated tasks retain their failure metadata; admission retry remains restricted to known refusal before execution.
- Main-chat/right-pane browser fixtures verify retry timing, safe diagnostics, explicit retry, no duplicate user message and failure restoration after refresh. Existing Gemini budget pause/extension, exact-operation recovery, task progress and reduced-motion fixtures also pass with no browser errors. Log: `/tmp/desk-failure-release-browser.log`.
- Backend AI/research/agent tests pass, including provider quota classification, request IDs, retry timing and no automatic model replay. Log: `/tmp/desk-failure-go-final2.log`.
- Subsequent boundary checks: 77 tests passed, covering provider payload spoofing, malformed code names, local budget/context errors, delegated tasks and native Codex behavior. Log: `/tmp/desk-failure-boundary-final.log`.
- TypeScript, production Vite and Go builds pass. Localization validation reports no extraction/placeholder errors; existing English fallback translations and the large-bundle advisory remain. `git diff --check` passes.

Tests use scripted local providers; they do not measure live provider availability or model quality. The [design contract](../design/ai-error-handling.md), `AGENTS.md` and architecture coverage tests define the path for future AI features. Saved pack policy, test expectations, recipes and user records are outside this change.

## Local installation

- Installed selected manifest `4a928aeaebbd8ce1f4c789ed01e447d3f7c67bb28deb6940958a85fca96e63d4`; only the Desk executable was replaced. Bundle hash verification and capability probe passed.
- Checked 33 chats, 39 operation files, 10 Jobs databases, subscriptions and test-generation state before gracefully restarting the idle launcher on port 5173.
- HTTP 200; the served index and both referenced assets match the final build. One main Desk process owns the selected binary.
- All **1,666** fingerprinted saved JSON/JSONL files remained unchanged. Selection backup: `/tmp/jps-ai-failures-final-previous-selection.json`.
- Installation and verification logs: `/tmp/desk-failure-install.log`, `/tmp/desk-failure-installed-verification.json`.
