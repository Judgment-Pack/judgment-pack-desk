# Stalled-loop recovery after the resumed Gemini trace

## Evidence and correction

The retry at 18:33 UTC resumed with an explicit grant from 20 to 30 requests. It repeated three completed reads, then returned another tool call after Desk had closed every tool. The last response used request 24, with six requests still available. Desk reported `invalid-response` and did not retain the refused tool name or arguments. The earlier exact-repeat guard bounded the loop but also removed the useful actions needed to recover.

The shared execution tracker now allows one corrective request after three consecutive unchanged observations, provided a finishing request remains. Previously completed identical requests are refused during that corrective request; different authorized actions remain available. Changed observations resume normal work. Refusal does not count as progress and cannot reopen discovery. A second stall in the same attempt gets no second corrective opportunity. Independent reviewers start a separate observation scope.

Refused calls are journaled with their exact arguments and a typed Desk failure with `outcome: not-started`. SDK refusals for unknown tools and invalid schemas are recorded as well, without dispatch. A model that continues repeating receives a tool-free final request; an unusable answer then reports `repeated-work`, not a generic provider failure. Filtering, transport errors and output truncation preserve their original causes. This path never increases the approved allowance and never starts a second finishing request after its final answer attempt.

Main and pane chat display **Stopped repeating work**, keep one shared Retry button when allowance remains, and preserve the cause and operation details across refresh. At the allowance boundary, repetition does not offer a compute extension. Existing action permissions, approval rules, original journals and test expectations are unchanged.

## Comparison with documented agent behavior

- [Codex harness](https://developers.openai.com/blog/codex-as-a-platform): maintains conversation state, execution events, tools and approval boundaries across turns. The application still owns product context and business rules.
- [Vercel loop control](https://ai-sdk.dev/docs/agents/loop-control): exposes stopping conditions, per-step message/tool selection and custom loop policies. The application must decide what counts as stalled work and what recovery permits.
- [Claude Agent SDK loop](https://code.claude.com/docs/en/agent-sdk/agent-loop) and [sessions](https://code.claude.com/docs/en/agent-sdk/sessions): distinguish normal completion from request/spend limits, with retained context for explicit resumption.

`tool-calls` is normally a continuation signal. Neither Codex nor Claude's cited documentation specifies Desk's three-repeat threshold or one corrective attempt. These are bounded Desk policies, not SDK defaults. A plan estimates the remaining work; it does not predict an exact call count or grant additional compute.

## Verification

Scripted provider fixtures exercise Gemini, Anthropic and OpenAI-compatible protocols. Saved-session replay reads the actual journal without modifying it or sending live model requests. Browser fixtures use temporary projects and local provider responses.


- Full frontend regression: **411 files, 6,460 passed, six optional tests skipped**. TypeScript subsequently required an equivalent boolean check for the finalization guard because the compiler cannot track assignments from the SDK callback; the focused adapter/context regressions and production build were rerun on that final expression.
- Saved-journal replay includes the actual resumed three-read trace, then a different action within the existing allowance. The real journal remains byte-for-byte unchanged. Initial test-fixture errors (a changed request identity and a missing short-request context handle) were corrected without weakening production checks.
- TypeScript/Vite and Go builds, shared Go failure tests and locale checks passed. Existing bundle-size, SDK deprecation and translation fallback warnings remain.
- No live inference was purchased. These tests verify orchestration and persistence, not that a live model will always select the correct policy edit.

- Final focused regression after the type-check adjustment: **14 files, 454 passed, one optional runtime test skipped**, including the saved continuation replay.
- Final real-browser completion, Gemini execution and failure suites all passed, with no browser errors. These cover a different action after repetition, refused-call arguments, shared main/pane recovery, refresh beside an older checked draft, explicit allowance grants and no duplicated user message. The compact recovery panel was visually inspected.
- Bundle **`abb9fad516d83aa4a90cb078f7148a6133e03bd1b704c59ceff7c4ad9a926b46`** passed manifest verification and capability probing and was installed. The immediate installation check found Desk already stopped, so it was left stopped; this build is selected for the next launch.
- All **1,666 managed project JSON/JSONL files** retained their hashes across installation; no files were added. The target conversation hash remains `89f89ffa7e112fd71a82b4266b19a478cb72b43ccb29b2630126572924b51192` with its original failed execution at 24/30 requests. Historical failures were not rewritten.
