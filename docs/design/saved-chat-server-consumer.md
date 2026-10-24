# Saved-chat canonical consumer (#642)

Saved conversations use `SavedChatRun` / `useSavedChatRun` and the authenticated
`ServerRunClient`. The worker does not create browser tools, context, a provider
connection or an `AuthoringRun`. Standalone authoring, Brief, background and memory
owners remain separate integration work. This slice does not register the final
production executor or claim that the complete browser bundle has no legacy engine.
Main chat layout, navigation and saved connection/model/Plan/research controls stay
with their existing owners.

## Source and admission

A fresh explicit Send/Retry retains the original user turn: display, prepared
input, statement, selected attachments, per-message research choice and exact pack
context. The normal conversation CAS saves it with a `serverRequest` data marker
containing `requestId` and `admissionId`, before any start POST. Neither the marker
nor a historical answer grants approval, compute or execution. A Retry creates a
new turn/admission and keeps the earlier request and output.

The existing authenticated conversation response adds `chatRevisions`, computed
from each selected raw stored chat before the response encoder can compact it.
The browser never substitutes a digest of normalized `Chat`. The authenticated run
snapshot adds required `admissionId`; lost acknowledgement discovery must match
that admission and saved owner/request (and original revision for start recovery).
A proven pre-POST failure/Stop clears only its matching unadmitted marker through
normal CAS, retains the original stopped turn, and permits a fresh explicit Retry.
Once a start is attempted or acknowledged, stream/read refusal cannot clear that
marker as not-sent. Zero or ambiguous matches remain uncertain; discovery never issues a replacement
POST. An acknowledged run whose saved projection cannot be read also stays
uncertain. Refresh, reconnect and route changes only read; they never resume work.

## Document CAS and rendering

Server-owned observations are display-only: `ChatStore.report` never checkpoints
them into a pinned chat. The store blocks edits and test-design append to that
owner until its acknowledged terminal projection is reconciled. Route cleanup
aborts subscriptions; explicit Stop requests cancel. Session end invalidates the
controller and clears private displayed turns. Late observations cannot update a
replacement owner/project.

Conversation saves read current private data and three-way merge local field
edits with the current server document. A same-field conflict keeps the local
unsaved draft and reports failure without a write. A terminal server response and
an unrelated local composer edit can therefore coexist. Reads and failed refreshes
never blindly reload over dirty local work.

The browser retains a strict JSON AST of each read and explicit edited-field
provenance. Only untouched fields reuse retained wire literals. Numeric spelling
(`1.0`, `1e0`, `-0`, integers beyond JavaScript's exact range) is never reconstructed
from parsed numeric equality. Same-field lexical changes conflict. After existing
CAS and authorization, the server preserves structurally identical rows byte for
byte using a number-preserving comparison; real numeric edits remain edits.
This prevents an unrelated whole-document save from invalidating a pinned source.

Current semantic observations use the shared work, question, execution, response
history and failure components. Parent/child prefixes and retained previews stay
outside the replay cursor and saved original journal. Existing historical decode
is pure; it does not start a run or restore approval authority.

## Explicit controls and check evidence

Answer, allowance extension, abandon, cancel and approval use the authenticated
current run revision. An asked approval carries optional closed
`approvalReference: {callId,argsDigest}`, emitted only for the exact current
canonical asked approval. The browser uses the server-authored argument digest;
it never reserializes arguments to invent approval identity. Ask me/Auto write
admission, Plan and compute remain distinct.

Optional `packChecks` contains the latest completed check per exact candidate
digest, including invalid and mismatching findings. Full receipt history stays
server-side. At most 64 checks are exposed, under the existing 16 MiB response
limit. Strict readers reject malformed/duplicate entries; display overlay requires
an exact retained candidate and preserves its original `packReview.basis` and
`suiteKey`. New failed findings never fall back to an older pass. Reconnect
restores this evidence without a browser checkpoint write. Display evidence is
not fresh Save authority.

Manual/Auto saved-pack Save calls the canonical model-free recheck for a canonical
binding and retains the existing current-suite/current-file checks, explicit write
admission, editor dirty/buffer/save/reload veto, CAS and exact readback. A successful
check does not grant a write. Stale source, changed suite and mismatch findings
refuse Save; uncertain delivery is not replayed.

## Generic creation controls (#648)

The private creation executor and current browser binding now provide these controls
through the same persisted chat admission and terminal projection boundary. The
[creation workflow](creation-server-runs.md) owns their evidence and readiness.

| Existing control | Canonical behavior | Boundary |
| --- | --- | --- |
| Recover draft from assistant prose | Explicit exact retained-message recovery | Strict complete proposal and model-free Runtime validation |
| Generic unlinked Run checks | Fresh current creation check | Independent cases and current source evidence; Save remains separate |
| Retry expectation validation | Explicit retry of the retained full proposal | No paid reviewer, changed outcomes or reduced suite |
| Establish cases | Explicit independent reviewer | Original person statements and verified evidence, without draft rules or observed results |
| Propose expectation correction | Explicit independently grounded replacement | Current typed finding and unchanged handoff target |
| Approve expectation correction | Exact visible person approval | Current token/source/candidate/session/project/generation, full retest and projection CAS |
| Research-choice continuation | Fresh read-only canonical admission | Current phase, action permissions and compute; no restored grant |

NewPackView and DraftPackView use this binding. Historical fallback is pure display;
it constructs no browser execution owner and restores no approval. Global factory
activation and engine removal still wait for the remaining release gates.

## Focused evidence

The bounded checks cover actual Go-produced API/native observations, exact
admission recovery, question/allowance/approval controls, explicit Stop,
route/project/session isolation, acknowledged terminal reconciliation, unrelated
local edits, numeric lexical custody, current/stale saved-pack checks and Save
veto/readback. Scratch-only semantic mutants check save-before-start, lost-ack
replay, terminal acknowledgement, required metadata, dirty conflict, numeric edit
custody and latest-check replacement. Full CI and the final interaction/release
review remain coordinator gates.
