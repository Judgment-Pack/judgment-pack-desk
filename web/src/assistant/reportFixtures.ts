import type { DelegationFigures, ExecutionCheckpoint } from '@judgment-pack/harness/execution/state'

/** Execution checkpoints and delegation figures as the harness writes them, for the tests of what the Desk makes of them. */
const A = crypto.randomUUID(), B = crypto.randomUUID()
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
