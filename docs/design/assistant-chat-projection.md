# Durable terminal response projection

The server can project an admitted terminal chat run into its one saved chat.
This is a private host workflow, with no JSON patch, tool-result, candidate-write
or artifact-write HTTP capability. Production executors and browser engines are
not switched by this change. The canonical run journal remains the original;
its events, checkpoint and exact operation argument/result bytes are retained.

## Supported response data

A completed, failed, stopped or interrupted run can add assistant message turns,
a response work summary and a newly serialized creation candidate. Message IDs
and the response ID are deterministic, browser-compatible identities derived
from the admitted run/event identities. The public projection receipt associates
the response ID with its original run ID. Tool calls/results pair by call ID and
name, including interleaved calls with the same name; ambiguous pairings refuse.
A response without prose uses `afterTurnId` to retain its exact request association.
Canonical error failure metadata survives independently of an older artifact's
`checkRun`. Critique text, available critic figures and charged request/unit
counters remain display facts. Execution snapshots retain Plan and usage history;
approval and handoff capabilities stay in the run journal.

Issue #537 explicitly accepts the Go adapter's sorted-map proposal document.
A canonical proposal on a normal creation chat becomes new candidate text using
that serialization. It does not reconstruct original model bytes. Existing
candidate strings, original turns, input/source strings, missing members, null,
false, zero and numeric spellings remain unchanged. Unrelated chats retain their
exact original JSON slices, including whitespace. Raw values are written directly:
marshaling `json.RawMessage` would compact those bytes and invalidate another run's
pinned source. Duplicate members/IDs and unsupported saved message IDs refuse.

Proposal events on a saved pack, Graph, Dialect/input, Policy or draft-associated
chat lack the producer-bound exact target/basis needed for a reviewed update.
They retain all events and record `projectionIssue: "proposal-target"`; the chat
is left untouched. Terminal runs containing question events similarly retain
`projectionIssue: "pending-question"` because this slice does not reconstruct a
resolved interaction from historical events. Unsupported projection returns an
explicit limitation to the host. Question, budget and client pauses remain only
in the bound journal while pending; their source chat stays pinned.

Other canonical events, exact operation receipts, provenance, source references,
delegated task records and continuation/recovery details remain accessible in the
run journal. The new saved response contains only the supported finite summary;
it does not claim complete domain-workflow presentation parity. In particular,
its new document/website/source reference arrays are empty; existing references
remain unchanged. The next workflow slice must pair finite shared host records
with the real feature producers, exact targets, reviewed bases and original
candidate strings. A generic arbitrary-JSON writer or per-artifact bytes extension
is not a substitute. Production cutover remains blocked on that coverage.

## Prepare, write, read back, acknowledge

`prepareAssistantChatProjection` first saves an immutable private receipt:
version, exact selected source digest, terminal state/checkpoint/event digest,
exact selected-chat output bytes/digest and response identity. Existing recovery
normalizes checkpoint operations still marked `running` to `interrupted`, even
in terminal runs. The identity hash and prepared display copy apply that same
transition, so reacquisition/import does not invalidate a prepared response.
The operation slice is copied first; projection does not change the original
checkpoint, completed/uncertain receipts, approvals, grants or counters. All other
checkpoint/event changes remain identity conflicts. Retries reuse these
bytes and their original timestamp. The journal bounds and retention apply; a
full journal refuses without discarding retained work. Before preparing output,
the existing full conversation validator must also accept it. Otherwise projection
retains `projectionIssue: "saved-chat-validation"` and leaves the chat and originals
untouched. In particular, the current Policy workspace validator case-folds all
nested JSON keys: checkpoint operation arguments containing both `Fact` and `fact`
are valid retained canonical data but cannot be embedded in saved chat. This
slice preserves that refusal; it does not rename keys, discard operations or
weaken Policy validation. Resolving that representational limit requires a
separately reviewed, path-aware conversation validation contract ([Desk #571](https://github.com/Judgment-Pack/judgment-pack-desk/issues/571)), required before full activation.

Before preparing a receipt, the complete selected output must also pass Unicode
scalar validation ([Desk #572](https://github.com/Judgment-Pack/judgment-pack-desk/issues/572)).
The shared scalar helper checks raw checkpoint arguments/results and every other
raw JSON string. Candidate text contains a second serialized JSON document, so
that layer is checked separately for all candidates and property names. Unpaired
surrogate escapes retain `projectionIssue: "unsupported-unicode"`, with no prepared
receipt or conversation change. Original events, checkpoints, exact operation
receipts and already-saved candidate strings remain unchanged. Valid surrogate
pairs, ordinary Unicode, U+FFFD and literal backslash-u text stay supported. Readers
are unchanged; validation never repairs, renames or drops typed values to save.

`writeAssistantChatProjection` takes the existing workspace-write lock before
the private-data lock. It rechecks the current signed-in principal, session,
Policy identity, project namespace, exact owner and exact saved user request.
Restored historical records and pending runs cannot write. The selected chat
must still match its pinned source, or already equal the prepared output. The
writer merges only that selected output into the current document, preserving
concurrent unrelated raw chats. Existing Policy, Graph/Dialect draft retention,
private path custody and workspace guards still run. Authentication is checked
again immediately before the existing atomic private-data write/readback.

`acknowledgeAssistantChatProjection` records success only after observing exact
prepared selected-chat bytes. `reconcileAssistantChatProjection` repeats those
three phases without invoking a provider, tool or execution continuation. A
crash after prepare, a lost write readback, or a crash before ack can retry the
same prepared bytes. Duplicate completion is idempotent. Changed/deleted owners,
contradictory events/output, revoked sessions and namespace moves refuse without
overwriting newer work. This does not claim a multi-file transaction: journal
and conversation writes are separately durable under the existing bounded
filesystem guarantees. A saved response never authorizes applying an artifact.

A concrete terminal executor may call `execution.ProjectChat(ctx)` after saving
its terminal canonical checkpoint and terminal host state. The helper does not
activate an executor or automatically run during restoration. A caller handling
projection failure must retain the terminal run and expose its refusal; it must
not rerun the model or clear the projection receipt to try a different output.

## Read-only discovery

Authenticated `GET /api/assistant-runs/owners/{owner}/requests/{request}` returns
`{"runs": [...]}` with existing public run views. No query/body input, admission
or continuation occurs. Results require the current saved owner and exact user
request bytes, same principal/project and current Policy access. A terminal save
changes the whole-chat digest, so discovery does not require that old digest.
Explicit retries remain distinct and sort lexically by retained run ID.

The current admitted session may see its live runs. A new session for the same
principal may see terminal/paused results as `historical: true`, matching existing
run-read behavior; active results are hidden. Historical discovery does not grant
fresh continuation or projection writes. Foreign principals, changed request
bytes and missing owners reveal no retained run. Views add only
`chatProjection: {responseId, digest, acknowledged}` and finite `projectionIssue`;
private prepared output bytes are not an HTTP write authority.

## Focused verification

Hand-authored source JSON and explicit assertions cover prose, work/failure before
prose, sorted-map creation, unsupported saved updates, question/budget/client
pauses, typed originals and two concurrently pinned chats. Go storage fixtures
exercise prepare and post-write crashes, lost readback, duplicate recovery,
contradictory completion, stale/deleted owner, revoked/new session, restored
historical records, project changes, exact-request discovery and a fake admitted
service executor. Recovery never invokes that executor twice.

The offline `scripts/assistant-chat-projection-reference.mjs` probe reads the Go
fixture outputs with the existing `decodeCheckpoint` and `readResponseHistory`.
Expected behavior is hand-authored; generated outputs are observations, not golden
fixtures recorded as expected data. To bundle the probe with esbuild, supply
`--inject:scripts/assistant-chat-projection-reference-locale.mjs` and
`--define:import.meta.glob=offlineLocaleCatalogues`; this supplies Vite's unrelated
lazy locale hook for the English-only standalone Node probe. No reader is mocked.
Use the existing pinned web dependencies, then pass the output directory to Node.
No dependency installation or browser execution activation is needed.

New `assistant chat projection:` mutation rows name focused Go witnesses for
current owner/session/history/pending checks, request binding, selected-chat CAS,
ack readback, prepared event identity, exact unrelated slices, discovery isolation,
unsupported domain scope and call-ID pairing. Local source-overlay probes require
real named assertion failures; compile/panic/timeout/no-tests are non-verdicts.
Unicode regressions cover nested candidate values/keys, raw checkpoint arguments
and structured results, valid pairs and preserved existing candidate strings. A
deliberate guard omission must recreate the actual candidate reader exception and
the browser-accepted unpaired checkpoint code units that the next server turn
refuses. Separate omissions detect missing raw/output or candidate-text scans.
The normal mutation harness can run these rows from a clean integration candidate.
Full application/CI and independent review remain integration gates.
