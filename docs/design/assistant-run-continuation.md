# Explicit assistant run continuation

The private run protocol accepts two additional finite actions: `answer` and
`extend-budget`. Both require a stable action `id` and the current run `revision`.
There is no generic retry, caller-supplied checkpoint, provider selector, tool
callback result or Plan acceptance action. The production executor remains
unregistered; these admissions are exercised through private fixtures.

## Admission and custody

The original principal, signed-in session, Policy generation, project, exact saved
chat/request/source bytes, connection revision, profile revision, model and phase
remain bound. Each new continuation checks these values under the same storage
transaction that saves its receipt, and checks them again before the executor is
called. All subsequent external dispatch retains its existing authorization check.
The nonblocking process lease and the service's active invocation registry exclude
another executor, including one still draining after a pause.

A continuation requires known completed/refused host operations, no canonical
running/interrupted operation, no unresolved approval and no unresolved delegation
handoff. Refusal retains completed results and uncertainty. It never changes an
uncertain, missing or canceled operation into a completed one.

The receipt, answer or canonical compute grant, new checkpoint when applicable,
and `admitted` host state are one private ledger transaction. The transaction must
succeed before one invocation is scheduled. Re-delivering the same action ID and
exact payload returns retained state, even if its original response was lost or
its invocation has since finished. A changed payload under the same ID refuses.
The payload digest includes the original revision. Receipt lookup follows current
session/Policy authorization but precedes new-admission revision checks.

A crash after persistence leaves an admitted run that recovery marks interrupted.
Neither reconstruction, reads, event streams nor a duplicate action dispatches it.
The answer/grant receipt remains available in optional public `continuations`.
New sessions may inspect historical pauses and use the existing explicit abandon
boundary; they cannot inherit the original session's execution grants.

## Questions and exact answers

`answer` names `questionId`, `questionDigest` and `answer: {selected, text}`. The
host resolves the one unanswered retained canonical `user_question`, reads it with
published `turn.ReadUserQuestion`, and validates selected option IDs, uniqueness,
single/multiple selection, a nonempty selection or trimmed free text, and the
4,000 UTF-16 unit text bound. Free text is always available. Missing/null fields,
unknown members, stale IDs/digests and repeated question IDs refuse. The request
boundary rejects malformed UTF-8 and unpaired surrogate escapes before decoding;
the browser refuses malformed answer scalars before sending. The digest is
the published canonical `RecordDigest` of the question with its `sha256:` prefix
removed; Go and browser readers use the same rule. Answer text is retained exactly,
including whitespace and Unicode; trimming only decides whether it is empty.

A valid question ends its adapter turn unanswered. The service retains its
canonical completed checkpoint in `awaiting-input`, instead of projecting an
unresolved question into a new saved-chat action. An answer continues only with
remaining canonical allowance and that completed checkpoint. Admission copies the full
checkpoint and changes only its status to running, rehashing existing canonical
record metadata and validating before persistence. This mirrors canonical
`Initialize`: host Runtime preflight runs before adapter initialization and needs
the acknowledged running state. It changes no operations or allowance. It grants no requests,
accepts no Plan, and approves no tool or saved-object action.

Published Agent v0.9.0 has no `Input.Answer` member. The host therefore passes each
persisted answer through the existing `Input.Documents` history seam and an
explicitly labeled transcript segment. Both carry the exact question, question ID
and digest, selected IDs, their retained labels, answer text and receipt timestamp.
The original `CurrentRequest` and canonical request key remain unchanged. Answered
material is user history, never host policy, tool consent or compute authority.
Canonical execution restoration retains completed tool results before these history
documents are added; the adapter's actual next provider request consumes the answer.

Terminal chat projection appends the canonical assistant question, a user answer
turn and a canonical saved-question resolution (`answered`, `continued: true`).
The answer turn identity is derived from run/action IDs. It does not launch work.
The original saved request stays unchanged. The projection digest covers the
continuation receipts as well as the checkpoint and semantic events. An unanswered
question remains in the run ledger and blocks terminal projection.

## Compute continuation and supported states

`extend-budget` has no amount or dispatch fields. It requires `paused-budget`, a
canonical paused checkpoint in `model-requests`, no unanswered question and safe
operation custody. The host calls published `runcontext.ExtendExecution` with
`interrupted=false`; the published increment, ceiling and grant chain are the only
allowance rules. The entire returned checkpoint is saved, preserving optional
canonical records, context, facts, completed operations, source/reasoning identity,
test counters and child accounting. Provider-managed units cannot acquire an
invented model-request count or grant. Interrupted/stopped runs are outside this
initial continuation slice.

A question produced on the exact API request ceiling is an explicit unsupported
state in released Agent v0.9.0: `Initialize` refuses `used >= limit`, while
`ExtendExecution` accepts paused checkpoints (or a separately authorized stopped
interruption), not the question's completed checkpoint. Desk refuses the answer
and leaves that question and checkpoint untouched. It does not synthesize paused
status, reset usage, grant on an answer or invoke an unanswered question through a
compute action. This released-contract seam remains open and blocks claiming a
complete production swap; no upstream changes are included here.

## Browser and validation

The typed browser client emits only the two closed action shapes and retains
`RunRequestError`'s not-sent/refused/uncertain delivery distinctions. It never
retries a POST automatically. Historical snapshots remain read-only. Readers
validate answer receipts against the actual retained question and compute receipts
against the checkpoint's canonical grant chain. The existing main chat hooks and
layout are unchanged.

Handwritten action/answer/grant vectors and required-member omission probes cover
the added wire members. Actual Go producers feed the production browser reader.
Focused fixtures cover API and subscription questions, exact provider-visible
answers, canonical API allowance continuation, duplicate/conflicting actions,
malformed input, stale source/session/Policy/phase/profile, uncertain operations,
cancellation, storage failure, restart, and the question-at-ceiling refusal.
