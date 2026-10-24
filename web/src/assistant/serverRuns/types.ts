import type { ExecutionCheckpoint } from '@judgment-pack/harness/execution/state'

export type RunState = 'admitted' | 'running' | 'awaiting-input' | 'awaiting-client' | 'paused-budget' | 'stopped' | 'interrupted' | 'complete' | 'failed'
export interface RunOwner { readonly kind: 'chat' | 'brief'; readonly id: string; readonly revision: string }
/** Discovery selects the saved request, whose chat revision may have changed. */
export interface RunDiscovery { readonly owner: {readonly kind:'chat';readonly id:string}; readonly requestId: string }
export interface RunBinding { readonly owner: RunOwner; readonly requestId: string }
export interface RunReference extends RunBinding { readonly id: string }
/** Canonical payload retained as data. Domain readers own interpretation. */
export interface RunEvent { readonly id: string; readonly event: Readonly<Record<string, unknown>> & { readonly type: string } }
export interface RunOperation {
  readonly id: string; readonly callId: string; readonly kind: 'model' | 'runtime' | 'host' | 'action' | 'callback'
  readonly name: string; readonly intent: string; readonly state: 'not-started' | 'dispatched' | 'completed' | 'refused' | 'uncertain'; readonly result: string | null
}
export interface RunAnswer { readonly questionId: string; readonly questionDigest: string; readonly selected: readonly string[]; readonly text: string; readonly at: string }
export type RunContinuation = { readonly id: string; readonly digest: string } & (
 { readonly kind: 'answer'; readonly answer: RunAnswer } |
 { readonly kind: 'extend-budget'; readonly grant: { readonly from: number; readonly to: number; readonly at: string } }
)
export interface RunPreview { readonly text: string; readonly taskId?: string }
export interface RunSnapshot extends RunReference {
  readonly packChecks?: readonly import('../../chat/packProposal').PackProposalCheck[]
  readonly admissionId: string
  readonly approvalReference?: {readonly callId:string;readonly argsDigest:string}
  readonly displayPreview?: readonly RunPreview[]
  readonly continuations?: readonly RunContinuation[]
  readonly revision: number; readonly state: RunState; readonly historical?: boolean
  readonly checkpoint?: ExecutionCheckpoint; readonly operations: readonly RunOperation[]; readonly events: readonly RunEvent[]; readonly cursor: string
  readonly chatProjection?: { readonly responseId: string; readonly digest: string; readonly acknowledged: boolean }
  readonly projectionIssue?: 'proposal-target' | 'pending-question' | 'saved-chat-validation' | 'unsupported-unicode'
}
export interface RunStart { readonly admissionId: string; readonly purpose: 'chat' | 'brief'; readonly ownerId: string; readonly revision: string; readonly requestId: string }
export type RunAction = { readonly id: string; readonly revision: number } & (
  { readonly kind: 'cancel' | 'abandon' | 'extend-budget' } |
  { readonly kind: 'answer'; readonly questionId: string; readonly questionDigest: string; readonly answer: { readonly selected: readonly string[]; readonly text: string } } |
  { readonly kind: 'approval'; readonly callId: string; readonly argsDigest: string; readonly approved: boolean; readonly note?: string }
)
/** Ephemeral whole prefix; taskId belongs exclusively to a child view. */
export interface RunProgress extends RunReference { readonly epoch: string; readonly sequence: number; readonly cursor: string; readonly text: string; readonly taskId?: string }
export type RunObservation = { readonly kind: 'progress'; readonly progress: RunProgress } | { readonly kind: 'snapshot'; readonly snapshot: RunSnapshot; readonly cursor: string } | { readonly kind: 'event'; readonly event: RunEvent; readonly cursor: string }
