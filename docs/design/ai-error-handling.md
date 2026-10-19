# Shared AI error handling

Desk owns one failure vocabulary and recovery contract across API chat, subscription chat, delegated tasks, context management and background research. Provider adapters translate errors; workflows decide whether another operation is permitted; views render the shared result. Pack policy and expected test outcomes never change in response to a transport error.

## Contract and storage

`internal/aifailure/catalog.json` defines stable codes, messages, recovery categories and provider aliases. `web/src/assistant/failures.ts` and `internal/aifailure` implement the browser/server boundary. Version 1 records a code, source and outcome certainty, with optional HTTP status, bounded request ID and absolute retry time. Explicit host validation may also supply an explicitly authored, bounded correction message. Arbitrary caught messages cannot become that field.

Failure metadata is stored with the existing conversation checkpoint or research attempt. Delegated tasks retain their failure beside findings. Refresh restores the same user request and failure; it does not retry. Legacy journals without metadata remain readable. There is no new global error log or raw provider-payload store.

Only allowlisted metadata crosses the model response facade. The handler parses bounded provider envelopes for stable codes, never their prose. Authentication headers, provider bodies, URLs and stacks are excluded. Tool results that the runtime deliberately reports in band remain results, including their existing evidence and refusal semantics; an exception thrown during tool dispatch is normalized separately.

## Ownership and recovery

| Condition | Desk behavior |
| --- | --- |
| User stopped | Cancellation, without a second failure or retry |
| Local request allowance reached | Persisted budget pause; explicit allowance increase uses existing review controls |
| Provider limit reached | Provider-managed limit; never claim that a local +10 grant increases it |
| Context limit | Keep original material; ask for a smaller current request/context section |
| Known refusal before admission | Existing bounded admission retry may run; no prior operation is replayed |
| Rate limit | Preserve `Retry-After`; disable explicit response retry until the deadline |
| Authentication, access or configuration | Explain the required correction; no automatic retry |
| Timeout, stream loss or uncertain provider failure | Preserve partial output and completed operations; require review before explicit continuation |
| Invalid tool arguments | Return a recoverable tool result with authored correction guidance |
| Checkpoint could not be saved | Stop before the next model dispatch; keep the current window's work |
| Saving or context revision changed | Retain original journals; reread current state before continuation |

Background artifact verification owns artifact readiness and check diagnostics. It must not replace the current response's failure, stop, question or budget-pause controls. `AuthoringRun.setReviewResult` separates these state updates for retained-check verification and citation refresh; explicit draft actions can still take ownership. Main chat and the right pane share this rule, including after checkpoint reload and runtime reconnection.

`outcome` describes the failed operation, not the entire response. An HTTP refusal can occur after earlier tools succeeded. Even a retryable HTTP status does not authorize replaying a workflow.

The Vercel adapter keeps `maxRetries: 0` and omits `streamRetries` (installed SDK disables stream retry when omitted). Existing bounded provider-capability negotiation is retained only before any output or tool result. Native Codex owns its internal transport recovery; Desk reports native retry progress and does not invent internal request counts. Research retains its narrower, journaled acquisition retry rules. The central catalogue grants neither retries nor extra budget.

## Rendering and future features

Main chat and the right pane use `ResponseRecovery`; other assistant surfaces use `EventList`; all use `FailureDetails` for bounded diagnostics. Research activity includes the same fields in its existing technical disclosure. Do not add a second banner for the same failure. Legacy after-terminal errors still invalidate a proposal without adding a second terminal event.

New AI features must enter the certified adapters and shared context/execution boundary, persist typed failures, and use the existing controls. Architecture tests enforce model call placement and failure boundaries. Behavioral tests cover all three API protocols, native bridge reasons, admission retry, post-HTTP-200 stream errors, storage refusal, cancellation and explicit recovery after reload. Provider fixtures do not demonstrate live model quality.

## External patterns adopted

- [Vercel AI SDK error handling](https://ai-sdk.dev/docs/ai-sdk-core/error-handling): distinguish thrown errors from stream errors, tool failures and aborts. Its retry mechanisms are configurable; Desk keeps replay decisions in its workflow layer.
- [Codex App Server](https://learn.chatgpt.com/docs/app-server): completed, interrupted and failed turns are separate; warnings and fatal errors have different effects. [OpenAI recovery guidance](https://developers.openai.com/api/docs/guides/agents-api/errors) reinforces inspecting saved work before retrying.
- [Claude API errors](https://platform.claude.com/docs/en/api/errors): stable error types, request IDs and retry timing; a stream can fail after HTTP 200. Billing/quota errors require configuration rather than indefinite retries.
- [Claude Agent SDK loop](https://code.claude.com/docs/en/agent-sdk/agent-loop): explicit result subtypes distinguish limits and execution failures from success. Desk follows that distinction without introducing a second Claude agent execution path; Anthropic API calls use the shared Vercel adapter.

The installed `@ai-sdk/google` 4.0.89 chat schema omits error-only Gemini chunks. The adapter inspects the raw error envelope for stable codes and immediately projects it to the shared failure contract. Raw chunks are never emitted into the UI or retained context. This covers error-only and partial-answer failures after HTTP 200 without claiming success.

### Empty and incomplete model responses

The API adapter reads the SDK's normalized finish reason instead of treating every empty reply as a generic error. `length` becomes `output-limit`; `content-filter` remains a distinct provider refusal. A length-truncated proposal is never accepted as complete, even when its partial text happens to parse as JSON. Independent reviews follow the same completion classification.

For a known empty `stop` response or a `length` response with no tool calls, the owning adapter may make **one** finishing request from the existing projected history. All tools are disabled at both the SDK and dispatch boundary. The request counts against the existing allowance, uses unchanged model/output settings, and passes through managed context and durable request accounting. It cannot replay completed actions, increase limits, retry transport errors, bypass filtering, or continue an uncertain tool call. A second empty or truncated answer stops with typed diagnostics. Explicit Retry response continues from retained operations rather than restarting them.

Execution checkpoints retain normalized finish reasons and reported token counts by request and phase; the failure includes the last completion metadata. Raw response bodies, private reasoning, provider messages and arbitrary finish strings are excluded. Missing provider telemetry stays missing. Shared fixtures cover Gemini, Anthropic and OpenAI-compatible transports; browser fixtures cover both chat placements and reload.


An interrupted conversational response whose model-request allowance is exhausted can offer an explicit **Retry · +10 requests** action, subject to the shared ceiling and existing retry eligibility. The grant is persisted before dispatch; it does not add a duplicate user turn or replay prior operations. Remaining-allowance failures retain ordinary **Retry response**. Both controls use the shared compact secondary button, retry icon, density tokens and right-aligned action placement. Technical diagnostics stay in the shared disclosure. A changed artifact's background validation updates readiness without stealing ownership from failed responses, pending questions or budget pauses.

Background feature calls use the same normalized completion contract as chat.
`backgroundCompletion` rejects explicit truncation, filtering and unexpected API
tool calls before returning structured output to a feature. Valid JSON does not
make an explicitly truncated answer complete. Failure metadata keeps sanitized
usage totals (including Gemini reasoning and Anthropic cache input) and normalized
stop reasons; raw provider prose is excluded. This guard performs no second model
request. The owning workflow retains the failed attempt and decides continuation.
Older compatible endpoints that omit stop metadata still undergo the feature's
bounded response validation.
