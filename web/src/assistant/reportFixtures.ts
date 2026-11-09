import type { DelegationFigures, ExecutionCheckpoint } from '@judgment-pack/harness/execution/state'

/** Execution checkpoints and delegation figures as the harness writes them, for the tests of what the Desk makes of them. */
const A = '11111111-1111-4111-8111-111111111111', B = '22222222-2222-4222-8222-222222222222'
export const DELEGATION: DelegationFigures = {
  batches: [{
    batch: 1,
    children: [
      { task: A, batch: 1, profile: 'review', model: 'a-cheaper-model', state: 'complete', modelRequests: 3, toolCalls: 2, tokens: { inputTokens: 900, outputTokens: 120 }, firstByteMs: 700, durationMs: 8200 },
      { task: B, batch: 1, profile: 'research', model: 'a-cheaper-model', state: 'complete', modelRequests: 5, toolCalls: 4, firstByteMs: 900, durationMs: 11000, partial: true }
    ],
    total: { modelRequests: 8, toolCalls: 6, tokens: { inputTokens: 900, outputTokens: 120 }, durationMs: 11500 }
  }],
  parent: { model: 'a-model', modelRequests: 3, tokens: { inputTokens: 5000, outputTokens: 400 } }
}
export const checkpoint = (more: Partial<ExecutionCheckpoint> = {}): ExecutionCheckpoint => ({
  version: 1, id: 'execution-1', requestKey: 'a'.repeat(64), phase: 'work', status: 'complete', unit: 'model-requests', used: 11, limit: 20, grants: [],
  plan: [], explanation: '', operations: [], context: { scope: 'scope-1', entries: [] }, ...more
})

// Handwritten published report facts: shared verbatim with the Go saved-history
// vectors. Empty and omitted members are intentionally different.
export const REPORT1 = {
  version: 1, checkpointVersion: 2, id: 'report-run', phase: 'work', status: 'stopped',
  unit: 'provider-managed', used: 0, limit: 2,
  grants: [{from: 1, to: 2, at: '2026-10-28T00:00:00Z'}], reason: 'cancelled', explanation: 'Stopped by user',
  prompts: [{request: 41, sha256: 'a'.repeat(64)}, {request: 43, sha256: 'b'.repeat(64)}],
  sources: [{source: 'runtime', kind: 'runtime', calls: 1, refused: 0, proposed: 0, bytes: 0, milliseconds: 0}],
  sourceIdentities: [{source: 'runtime', identityDigest: `sha256:${'a'.repeat(64)}`, withheld: false, serviceAsked: true}],
  skills: [], critic: {mode: 'auto', ran: false, reason: 'no-proposal'},
  delegation: DELEGATION, usage: {rows: [{source: 'agent', connection: 'subscription', role: 'assistant', model: 'reported-model', requests: 0, invocations: 1, milliseconds: 0, bytes: 0, tokens: {outputTokens: 0}}]},
  childUsage: [{id: 'child-one', usage: {rows: []}}],
  failover: [{role: 'assistant', connection: 'second', consumed: 1, history: [{from: 'first', to: 'second', reason: 'timeout'}]}],
  completions: [],
  plan: [], planState: {revision: 'sha256:4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945', sequence: 0, nextId: 1, discarded: 0, history: []}
} as const

export const REPORT2 = {
  ...REPORT1, version: 2, checkpointVersion: 3,
  route: {version: 1, route: 'agent', skills: [], tools: [], operations: ['read'], planning: 'withheld', decidedBy: 'host', reason: 'retained', bindingDigest: `sha256:${'d'.repeat(64)}`, catalogueDigest: `sha256:${'e'.repeat(64)}`, widened: false, refusals: []},
  feedback: [{sourceDigest: `sha256:${'f'.repeat(64)}`, record: {version: 1, id: 'feedback-one', runId: 'report-run', sequence: 1, previous: null, target: {kind: 'run'}, rating: 'good', actor: 'user', recordedAt: '2026-10-28T00:00:00.000Z'}}],
  repeatTools: {executions: [{execution: 'report-run', remindAt: 2, stopAt: 3, count: 0, reminded: false, reminderDelivered: false, stopped: false}], policy: {'repeatTools.mode': {layer: 'run', value: 'on'}, 'repeatTools.remindAt': {layer: 'run', value: 2}, 'repeatTools.stopAt': {layer: 'run', value: 3}}},
  usageTotals: {
    modelRequests: {known: 0, complete: true}, providerInvocations: {known: 1, complete: true},
    inputTokens: {known: 0, complete: false}, outputTokens: {known: 0, complete: true}
  }
} as const

// A fixed cap and completion belong to an API execution, whose used counter
// includes that request; the native used-zero vector intentionally has neither.
export const REPORT_API = {
  version: 1, checkpointVersion: 2, id: 'api-report', phase: 'work', status: 'complete',
  unit: 'model-requests', used: 1, limit: 1, requestCap: 1, grants: [],
  explanation: '', plan: [], completions: [{request: 1, phase: 'work', result: {finishReason: 'stop', outputTokens: 0}}]
} as const
