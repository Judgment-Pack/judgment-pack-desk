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

## Controls withheld until their owners migrate

These are explicit remaining activation gates, not delivered replacements:

| Existing control | Saved-chat behavior in this slice | Required canonical replacement/owner |
| --- | --- | --- |
| Recover draft from assistant prose | Hidden for canonical binding | Reviewed generic proposal recovery with exact source/candidate custody; standalone authoring owner |
| Generic unlinked Run checks | Hidden for canonical binding; linked saved-pack Check proposal remains | Generic creation-candidate check/readiness owner, including source/case evidence |
| Retry expectation validation | Hidden without a controller capability | Canonical generic expectation validation/recovery owner |
| Establish cases | No callback for canonical binding | Canonical generic creation-case owner and explicit independent check path |
| Propose expectation correction | No callback for canonical binding | Canonical expectation correction with retained findings and explicit reviewed replacement |
| Approve expectation correction | No callback for canonical binding | Canonical exact expectation approval/CAS owner; historical text cannot authorize it |
| Legacy research-choice continuation | Hidden for canonical binding | Explicit fresh read-only canonical research-choice request owner; composer choice on new Send remains |

These omissions keep full factory activation and full v0.14.0 migration pending.
Existing standalone surfaces retain their own implementation until their separate
owner cutovers; this change introduces no compatibility fallback for saved chat.

## Focused evidence

The bounded checks cover actual Go-produced API/native observations, exact
admission recovery, question/allowance/approval controls, explicit Stop,
route/project/session isolation, acknowledged terminal reconciliation, unrelated
local edits, numeric lexical custody, current/stale saved-pack checks and Save
veto/readback. Scratch-only semantic mutants check save-before-start, lost-ack
replay, terminal acknowledgement, required metadata, dirty conflict, numeric edit
custody and latest-check replacement. Full CI and the final interaction/release
review remain coordinator gates.
