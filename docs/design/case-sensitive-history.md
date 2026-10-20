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
| Input requests, execution intents, retained pack qualification/exchange targets | Leave strict. Their mixed control/data schemas need separate owning changes. |
| Policy/coverage/adoption/archive authority records and other closed decoders | Leave strict; no global decoder relaxation. |

Concrete remaining cases are separate from the conversation boundary fixed here:
`inputRequest` rejects case-distinct keys in prepared-input `values` and recipe
`conversions` maps before its closed decoder runs. `policyExecutionService.prepareIntent`
rejects `{"facts":{"Fact":false,"fact":null}}` at the initial request guard.
`validateInputQualification` and `checkExchangeTarget` apply the conservative
helper to retained pack bytes while checking identity. Pure Dialect comparison
now respects case, but it does not bypass these earlier gates or certify all
Dialect/Policy snapshot formats. Each mixed schema needs its own reserved-name
and typed-data regression before changing that authority boundary.

This change fixes Desk #571's conversation/projection/recovery/import mismatch.
It does not add production execution registration, Graph hosts or other pending
Agent cutover domains. Projection reader CI wiring is tracked separately.
