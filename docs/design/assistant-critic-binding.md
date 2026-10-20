# Saved-chat critic inputs

The private server factory binds the published Agent's critic inputs. Agent
v0.5.0 owns proposal detection, critic orchestration, named model selection,
context isolation, comparison, events, checkpoints and request accounting. Desk
adds neither a critic loop nor a provider-specific context policy.

An explicit saved `adversarialReview` wins over the canonical policy default.
Plan disables critic inputs and retains its existing tool restrictions. A normal
explanation produces no critic model request or test. For an enabled review,
Desk discovers and obtains Runtime's `test_pack` prompt with no arguments. It
never inserts the saved request or pack into prompt arguments. Required Runtime
arguments refuse construction. An absent advertised testing prompt leaves
`TestPrompt` empty: Agent reports its canonical no-testing-prompt critique when
there is a proposal, with no checks or positive refutation claim. A failed or
malformed prompt response is a refusal, not substitute guidance.

Declared cases come only from the exact admitted saved conversation's
`checkpoint.state.cases`, using the existing AuthoringCase projection: id,
facts, evidence availability, expected disposition, expected handoff target and
rationale as focus. Historical expectation-source labels, checks and execution
records confer no authority. Saved-test rows come only from the current
project's private test store, selected by the admitted conversation's current
linked pack id. The linked project-relative target is read through Desk's
rooted reader, with symlinks refused, and its document id must match that link.
No browser or model field selects a Runtime path or a storage pathname.

The saved link's digest is historical navigation context; it does not identify
the current file revision. Desk captures the current target's exact byte digest
and the selected suite snapshot once in the run's existing shared input custody.
Absence of a suite differs from an empty suite. The suite snapshot includes
revision, deletions and other saved metadata, so even an otherwise harmless
suite edit conservatively requires a fresh run. Unrelated suites are outside
this snapshot. Reconstruction shares the same held snapshot and never refreshes
it into a newly reviewed target. The shared physical API/native model dispatch
fence and the gated Runtime transport recheck current custody. Short private
store reads and the input mutex are released before Runtime or model work.

A bounded host-authored `deskCriticInputV1` checkpoint extension fingerprints the
run, execution key, exact saved request/source, target, suite and accepted cases.
Its first capture and comparison use the existing private writer transaction
before any Runtime or model request. Canonical checkpoint callbacks reattach
that record; the published open checkpoint reader and snapshots preserve it.
Fresh custody on durable continuation must match the original fingerprint.
Missing evidence after work or an explicit continuation, malformed metadata or
a conflict refuses. Saved-chat fields bearing the same name remain historical
data and never seed this record. The extension grants no permissions and does
not alter allowance, approvals, tools or model context.

The row projection matches `criticCases.ts`: deleted and exploratory rows are
omitted; rationale overrides row focus when nonempty; an AI origin stays AI,
otherwise the row origin wins unless absent or null. Only published critic-case
members travel. Each row is accepted by Agent's own `turn.ReadCriticCases` or
omitted, preserving the existing legacy behavior for malformed facts or
uncomparable expectations. Omission never repairs the case or changes what it
expects. Saved rows precede declared rows, the first accepted id wins, and no
more than 200 accepted unique rows are passed. Numeric strings, numbers, zero,
false, null, omission, expected errors and phases, handoff, origin, focus,
specification references and citations retain their existing representations.
Malformed consumed suite structure and contradictory case identities refuse.

Desk's current connection configuration does not admit
`experimental_test_cases`. This change preserves that restriction. Agent uses
the already granted `experimental_evaluate` rehearsal fallback for declared
comparisons; cases beyond Agent's rehearsal bound remain visibly incomplete.
The critic receives only its published reader subset and the intersection of
its Runtime tools with the current grants. Child and Plan test refusals retain
Agent's shared turn count; a critic tool absent from its offer is refused before
charging. Native invocations retain provider-managed accounting and current
saved effort, including on the named critic model; they do not invent internal
model-request or token counts.

Hand-authored Runtime, API and native protocol peers exercise the actual Desk
bindings. They establish positive comparison, saved-suite precedence, canonical
usage and proposal evidence, missing guidance, incomplete and mismatched cases,
Plan, current grants, retained completed receipts, and snapshot refusal after
real relay/native discovery waits. No test launches an installed provider or
calls a live model. The production executor remains unregistered; this slice
does not migrate domain tools, proposals or actions, change component pins, or
activate browser cutover.
