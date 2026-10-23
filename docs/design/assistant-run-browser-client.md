# Durable Assistant run browser transport

The internal browser API in `web/src/assistant/serverRuns` reads the existing
private durable run routes. It is not registered in chat or research hooks.
No executor is activated, and no provider or tool loop runs in this module.

`serverRunClient()` captures the document's current authenticated session. All
requests use `deskFetch`, its frozen document desk, omitted cookies, bearer
header and marked unauthorized handling. Session end aborts each applicable
request and cancels its reader. A replacement session cannot inherit this
client. Relative route paths are fixed; IDs are validated and request path
segments encoded. Tokens and cursors never occur in a URL.

`start` accepts only the current Go start DTO: `admissionId`, `purpose: chat`,
`ownerId`, saved chat `revision`, and `requestId`. It does not accept prompts,
tools, checkpoints, grants or provider settings. `read` requires an exact run,
owner kind/ID/source revision and request ID. `discover` selects saved owner
kind/ID and request ID, independently of the current whole-chat revision, and
returns every matching run;
it never chooses a replacement run. Discovery uses the server's narrower
alphanumeric/hyphen request ID rule (100 bytes), while start uses its current
bounded text rule (128 bytes). Discovery retains each returned run's validated
original source revision. A refresh knowing only the current saved journal can discover the unchanged saved
request after projection or an authorized journal edit, then use that retained
original binding for exact read/action/event requests. No current revision is
substituted into a returned run.

`action` accepts only explicit cancel, approval or abandon DTOs. Every body
contains the caller's exact action ID and observed numeric run revision.
Approval also carries the exact call ID, argument digest, boolean decision and
optional note; false and an empty note survive serialization. Historical
observations cannot be acted on. Local stale revision detection does not
replace server validation. This transport does not infer approval eligibility,
recompute grants, select actions, continue pauses or create idempotency IDs.

The Desk-owned envelope is closed and bounded: run ID, positive safe numeric
revision, owner, request ID, state, optional historical/checkpoint/projection
metadata, operations, events and cursor. It retains operation intent/result
base64 bytes and distinguishes null results from completed empty bytes. Go nil
operation/event slices are explicitly accepted as JSON null and normalized to
empty arrays; missing members fail. Counts follow the host limits (512
operations, 1024 events, 128 discovered runs), and JSON/frame bytes are limited
to the host's 16 MiB ledger bound. Unsafe numeric revisions are refused rather
than rounded. Duplicate JSON members, malformed UTF-8 and truncated frames fail
with safe catalogue failures.

Checkpoints pass through released Agent 0.9 `readExecution`. The released
browser package has no whole RunEvent reader. Semantic payloads therefore stay
explicitly opaque data, with a known canonical discriminator, exactly as the
Go durable sink checks its union leg. A compile-time exhaustive map follows
released `AssistantEvent` types; the actual Go producer proof checks parity for
all 31 durable discriminators. Progress and checkpoint event variants are
excluded, matching the sink. This does not claim domain payload/schema
validation. A feature consumer must use canonical per-member readers before
interpreting payloads. Event prose and saved payloads never authorize actions.

`RunObservations` retains bounded in-memory observations for one exact run,
separately from its authoritative snapshot. `events` performs one authenticated
fetch subscription and sends its cursor in `Last-Event-ID`. The parser handles
fragmented UTF-8, LF/CRLF/CR newlines, heartbeat comments, bounded frames and
complete JSON messages. Semantic IDs are deduplicated, with conflicting payload
or journal position refused. Every valid snapshot is accepted independently
of cursor equality, including the snapshot following an event with the same
cursor. Snapshots cannot move back in revision or erase observed completed
event history. Abort and early iterator return release the reader and retain
completed observations. An active stream EOF is a connection failure; a paused
or terminal snapshot EOF schedules nothing.

Reconnection is an explicit new call with the retained observations; there is
no timer, retry scheduler or local durable run ledger. Mutation delivery is
`not-sent`, `refused` for a definite non-timeout 4xx, or `uncertain` after an
interrupted/malformed success or other unknown delivery. Typed errors use the
shared safe failure catalogue and retain status/delivery knowledge; no response
body or caught error prose is displayed. Uncertainty requires explicit read or
discovery. It never replays start, approval, cancellation, abandonment or
provider work. A read cannot prove an action receipt that the current public
view does not expose; the transport does not invent that proof.

Handwritten shared fixtures cover pending nil arrays, completed exact bytes,
canonical checkpoints and UTF-8 text. Focused browser tests cover malformed and
oversized data, required omissions, ownership, shared cursors, duplicate events,
session loss, historical reads/actions, stale actions and lost POST receipts.
`scripts/dev/assistant-run-client.sh` feeds actual Go HTTP envelopes and SSE
through the browser decoder and stream reader, without saving captured golden
fixtures. Hook migration, continuation actions, background/diagnostic dispatch,
legacy engine removal and complete activation remain separate work.
