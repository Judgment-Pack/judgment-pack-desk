# Retry persistence and execution progress

## Findings

The saved failed response performed seven identical saved-pack reads, three identical validations and one repeated evaluation. Its task plan was empty. The last completion reported an output limit (8,188 output tokens, including 7,865 reasoning tokens) after the execution used all 20 model requests. Tool operations, model requests and output tokens are separate limits.

The vanished retry action had a separate cause: reconnecting the runtime refreshed the check for an earlier draft, then replaced the failed conversation's status with the draft's review status. That also hid or overwrote other response-owned controls after refresh. A failure exactly at the request ceiling had no eligible retry action even when its failed status survived.

## Shared changes

- Background artifact checks update readiness while preserving response ownership, pending questions and request-limit recovery. Explicit artifact actions may take ownership.
- Main and pane chat use one compact, right-aligned retry button with the shared icon, typography and density tokens. Exhausted failures offer an explicit ten-request grant; it is saved before dispatch and does not add another user turn.
- Every API request receives bounded, exact references to recent operation arguments and results. The validated working proposal remains retrievable independently of the currently saved workspace object.
- Execution progress detects consecutive exact repeats, warns after two, and closes tools after three. A final response consumes the existing allowance. Changed observations reset the streak. Provider refusal of tool closure cannot continue spending the allowance on rejected calls.
- Context sizing includes execution instructions and message framing. Compaction shares space between reference material and the latest result; retrieval output shows the requested value before navigation metadata.

The repeated-work detector would have intervened after operation 14 in the saved trace. This is deterministic replay evidence, not a claim that a live model will always make the correct edit. It detects exact repetitions rather than every semantic loop. Mutable workspace reads and evaluations remain fresh; no completed or uncertain operation is automatically replayed. Subscription providers own their internal loops; Desk does not infer their hidden request counts.

## Verification

Final verification:

- Full frontend suite: **411 files, 6,438 passed, five optional tests skipped**. The first full run exposed the new icon's missing vocabulary entry and a timing-sensitive ResearchProgress assertion. The icon contract was updated, both focused suites passed (63 tests), and the final full run with eight workers passed without changing the ResearchProgress test.
- Context/execution/adapter regression: 213 passed, two optional replays skipped in the ordinary run. The two saved-session replay suites were then run with the real saved journal: all 47 tests passed. No completed runtime operation was replayed and the source file remained byte-for-byte unchanged.
- Three real-browser suites passed: completion recovery, Gemini execution/explicit continuation, and typed failure recovery. Checks cover main and pane chat, an earlier checked draft, refresh, explicit retry at the request ceiling, durable grants, exact-result continuation, one original user turn, status animation/reduced motion and compact shared button geometry. No browser errors.
- TypeScript/Vite build, Go build, shared Go failure tests, locale extraction/check and changed-file whitespace checks passed. Existing locale fallbacks, SDK deprecation and bundle-size warnings remain.
- Local bundle `b146b3e6d922893c721361c195077d6d6c42c9d97d9eecff31bcc70d939c744a` was verified and installed after confirming no active work. One Desk process restarted on port 5173. HTTP 200 and both entry/adapter assets match the final build.
- All **1,666 managed project JSON/JSONL files** retained their hashes across installation, including saved conversations, packs and cases. No new data files were added.

The saved execution and browser flows use scripted local model responses; no live inference was purchased. These verify orchestration, persistence and provider-protocol behavior, not whether a live model will choose the correct policy edit.
