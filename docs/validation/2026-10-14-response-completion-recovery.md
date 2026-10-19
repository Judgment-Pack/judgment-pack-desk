# Response completion recovery

The shared API adapter previously discarded normalized finish reasons and immediately failed on an empty final response, even with requests remaining. It could also accept text from a length-truncated generation as though generation had completed. Prior saved failures lack the telemetry needed to distinguish those provider outcomes retrospectively.

The adapter now records normalized finish reasons and reported input/output/reasoning token counts. Shared failures distinguish output-token exhaustion from the approved request count and from provider filtering. Truncated proposals are not admitted as complete. The independently scoped reviewer also checks completion status.

A known empty stop or length-truncated response without tool calls can use one additional, tool-free synthesis request. It receives the existing projected history and completed results, stays inside the approved allowance, and uses unchanged model/output settings. Both SDK tool availability and host dispatch are closed. Transport failures, unknown finish states, malformed function calls and filtering do not automatically retry. Repeated empty output stops with specific diagnostics.

Validation on the final functional implementation:

- Full frontend suite: 410 files, 6,410 tests passed; five optional integration tests skipped.
- Focused regression: 518 tests passed across context, execution, API and Codex conformance, conversation, failure and recovery paths.
- Additional opt-in replay: the saved failed execution continued through a local scripted model, without replaying completed operations or changing the source journal.
- Three real-browser scenarios passed: Gemini completion recovery, execution/budget continuation, and typed provider failure recovery. Main chat and right pane, reload, finishing status, token diagnostics, explicit retry and user-turn deduplication passed with no browser errors.
- TypeScript/Vite build, Go build, shared Go failure tests, locale extraction/check and diff whitespace checks passed. Existing locale fallbacks and bundle-size warnings remain.

Provider responses in these tests are local fixtures; no live inference was purchased. Fixtures cover Gemini, Anthropic and OpenAI-compatible responses, including reported output exhaustion after reasoning, empty stops, truncated JSON, filtered output, transport failures, malformed calls, forbidden tools during synthesis, exact request accounting and safe persistence.

References: [AI SDK streamText](https://ai-sdk.dev/docs/reference/ai-sdk-core/stream-text) exposes finish reasons and usage separately from text; [Gemini thinking](https://ai.google.dev/gemini-api/docs/generate-content/thinking) documents that an output ceiling reached during reasoning can leave no answer text. These explain the tested failure modes, not the cause of an older failure whose finish telemetry was not retained.
