import type { ExecutionCheckpoint } from '@judgment-pack/harness/execution/state'

export type RunState = 'admitted' | 'running' | 'awaiting-input' | 'awaiting-client' | 'paused-budget' | 'stopped' | 'interrupted' | 'complete' | 'failed'
export interface RunOwner { readonly kind: 'chat' | 'brief' | 'author-pack' | 'describe-pack' | 'author-graph' | 'connection-diagnostic' | 'settings-observation'; readonly id: string; readonly revision: string }
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
 { readonly kind: 'answer'; readonly answer: RunAnswer; readonly successorRunId?: string } |
 { readonly kind: 'extend-budget'; readonly grant: { readonly from: number; readonly to: number; readonly at: string } }
)
export interface PlanDecisionReference { readonly callId:string; readonly planRevision:string; readonly candidateRevision:string }
export interface RunPreview { readonly text: string; readonly taskId?: string }
export interface ExternalSourceCall {
 readonly callId:string
 readonly hostOperationId:string
 readonly ownTool:'search_sources'|'read_link'|'explore_website'
 readonly wireName:string
 readonly control?:import('../../search/step').ExternalControl
 readonly outcome?:'not-started'|'unknown'
}
export interface ApprovalResultView { readonly callId:string; readonly name:string; readonly isError:boolean; readonly result:import("../../chat/responseHistory").ApprovalResultDisplay }
export interface RunAnswerOrigin { readonly parentRunId:string; readonly admissionId:string; readonly actionId:string; readonly question:{readonly type:'user_question'; readonly version?:1; readonly id:string; readonly question:import('@judgment-pack/harness/questions').UserQuestion}; readonly answer:RunAnswer }
export interface RunSnapshot extends RunReference {
 readonly answerHistory?: readonly RunAnswerOrigin[]
 readonly approvalResults?:readonly ApprovalResultView[]
 readonly externalCalls?:readonly ExternalSourceCall[]
	readonly creation?: import('./creation').CreationEvidence
  readonly packChecks?: readonly import('../../chat/packProposal').PackProposalCheck[]
  readonly admissionId: string
  readonly planDecisionReference?: PlanDecisionReference
  readonly approvalReference?: {readonly callId:string;readonly argsDigest:string}
  readonly displayPreview?: readonly RunPreview[]
  readonly continuations?: readonly RunContinuation[]
  readonly revision: number; readonly state: RunState; readonly historical?: boolean
  readonly checkpoint?: ExecutionCheckpoint; readonly operations: readonly RunOperation[]; readonly events: readonly RunEvent[]; readonly cursor: string
  readonly chatProjection?: { readonly responseId: string; readonly digest: string; readonly acknowledged: boolean }
  readonly projectionIssue?: 'proposal-target' | 'pending-question' | 'saved-chat-validation' | 'unsupported-unicode'
}
export interface RunContextSelector { readonly document:string; readonly pane?:string }
export interface RunStart { readonly context?:RunContextSelector; readonly admissionId: string; readonly purpose: 'chat' | 'brief' | 'author-pack' | 'describe-pack' | 'author-graph' | 'connection-diagnostic' | 'settings-observation'; readonly ownerId: string; readonly revision: string; readonly requestId: string; readonly observation?: SettingsObservationSelector }
export type RunAction = { readonly id: string; readonly revision: number } & (
  { readonly kind: 'cancel' | 'abandon' | 'extend-budget' } |
  { readonly kind: 'answer'; readonly questionId: string; readonly questionDigest: string; readonly answer: { readonly selected: readonly string[]; readonly text: string } } |
  ({ readonly kind: 'plan-decision'; readonly approved: boolean; readonly note?: string } & PlanDecisionReference) |
  { readonly kind: 'approval'; readonly callId: string; readonly argsDigest: string; readonly shownDigest?: string; readonly approved: boolean; readonly note?: string }
)
/** Ephemeral whole prefix; taskId belongs exclusively to a child view. */
export interface RunProgress extends RunReference { readonly epoch: string; readonly sequence: number; readonly cursor: string; readonly text: string; readonly taskId?: string }
export type RunObservation = { readonly kind: 'progress'; readonly progress: RunProgress } | { readonly kind: 'snapshot'; readonly snapshot: RunSnapshot; readonly cursor: string } | { readonly kind: 'event'; readonly event: RunEvent; readonly cursor: string }

export interface SettingsObservationSelector { readonly kind: 'capability' | 'ping'; readonly model: string }

/** Separate saved display data. Decimal CAS strings are lossless through uint64. */
export interface DialectReadRecoveryView {
 readonly runId:string
 readonly runRevision:string
 readonly recoveryRevision:string
 readonly reads:readonly DialectReadRecoveryRow[]
}
export interface DialectReadRecoveryRow {
 readonly operationId:string
 readonly originalOutcome:'completed'|'failed'|'interrupted'|'unknown'
 readonly backingState:'not-observed'|'present-valid'|'absent'|'corrupt'|'unavailable'
 readonly reason?:'owner-unavailable'|'provider-unavailable'|'ownership-mismatch'|'storage-io'|'close-uncertain'|'cancelled'|'budget-unresolved'
 readonly ordinal:string
 readonly storageState:'unresolved'|'accounted-retained'|'accounted-absent'
 readonly canInspect:boolean
}
