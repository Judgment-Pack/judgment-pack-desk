# Case-sensitive conversation data

Conversation validation checks decoded member names against an explicit schema
boundary before the existing readers validate types and workflow rules. Declared
struct fields require their exact JSON spelling, including workspace ownership,
Policy sources, approvals and the published Agent checkpoint controls. Maps and
`json.RawMessage` payloads retain case-sensitive data keys. Published open Agent
objects retain unknown additional members as data after rejecting aliases of
known fields. Unknown conversation branches keep the older conservative check.
The validation shapes do not serialize or replace saved records.

Every object rejects duplicate decoded names, including equivalent JSON escapes.
The member walker preserves the nesting limit of 64 and refuses malformed UTF-8
or unpaired surrogate escapes. Existing conversation byte bounds, closed-schema
checks, source custody, current authorization, atomic writes and exact readback
continue to apply. Opaque data cannot introduce another reserved control member.

For example, operation arguments and structured results can retain
`{"Fact":false,"fact":null,"zero":0,"number":50,"literal":"50"}`.
Nested arrays and maps preserve those distinctions. The same explicit case
schema covers `state.cases`, `expectationIssues[].original` and
`expectationIssues[].resolved.replacement`. Issue, resolution and case control
names stay exact; only their `facts` values become opaque data. Actual workspace
PUT and next-turn input compilation cover both issue variants, and the actual
browser reader checks their typed facts after persistence. The actual terminal projection
fixture reacquires an interrupted operation, prepares/writes/readbacks/acknowledges
its response, reloads the checkpoint, compiles the next-turn saved context and
exports/restores the backup. Both data keys and exact retained values survive;
restored runs remain historical. Workspace writes using `Execution`, `Args`,
`StructuredContent` or `Approval` fail without changing the saved conversation.

## Shared-consumer audit

The original `rejectDuplicateMembers` remains conservative. Changing that helper
globally would also change unrelated authority decoders.

| Consumer | Decision |
| --- | --- |
| `policyWorkspaceEntries`, used by conversation save, source custody, storage validation and archive restore | Use the explicit workspace/checkpoint schema. Sources and Policy controls retain exact names. |
| `gatewayCanonicalJSON` | Use exact decoded duplicates. Canonicalization reads JSON maps, keeps exact keys, and still enforces its integer/Unicode profile. It grants no authority. |
| `sameDialectJSON` | Use exact decoded duplicates. Mapping comparison keeps key case significant. Callers retain their own admission and authority checks. |
| Input requests, execution intents, retained pack qualification/exchange targets | Use typed request controls, exact intent-envelope names and a bounded retained-pack member shape. Declared maps and opaque values preserve case; controls and decoded duplicates stay strict. |
| Policy/coverage/adoption/archive authority records and other closed decoders | Leave strict; no global decoder relaxation. |

Desk #576 extends the distinction to the supplied-input and Policy admission
boundaries. `inputRequest` checks the actual typed request before its closed
decoder runs, so preparation values and conversion-map keys can differ by case.
Control aliases, unknown request fields and exact decoded duplicates remain
refused. Policy intent envelopes permit only exact `facts` and optional
`evidence` members. Their opaque payloads retain both case-distinct keys and
exact bytes, including omitted/null evidence and false/zero/numeric/string
values. Malformed input is refused before authorization or artifact writes.

Retained packs use a finite name-validation shape for the identity, decision,
title, description and evidence-requirement controls Desk reads. Other pack
branches remain Runtime-owned data; this shape does not claim to validate the
pack grammar. Exact decoded duplicates, Unicode errors, excessive nesting and
aliases of Desk-read controls are refused. Qualification and Exchange retain
the original pack bytes and digest. Exchange validates those controls before
Runtime dispatch and still requires the retained template identity, current
coverage check and declared pack digest or version range. This grants no new
execution capability and does not relax unrelated authority decoders.

This change fixes Desk #571's conversation/projection/recovery/import mismatch.
It does not add production execution registration, Graph hosts or other pending
Agent cutover domains. Projection reader CI wiring is tracked separately.
