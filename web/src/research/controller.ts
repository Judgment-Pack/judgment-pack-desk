import type { SavedChatRun } from '../assistant/serverRuns/controller'
/** Shared UI contract. Canonical controllers implement transport actions, never the browser engine. */
export type ResearchRunController = Pick<SavedChatRun,
  'subscribe' | 'getSnapshot' | 'running' | 'start' | 'send' | 'stop' | 'appendConversation' |
  'canRetryResponse' | 'canExtendResponse' | 'updateQuestionDraft' | 'prepareQuestionAnswer' |
  'continueQuestion' | 'prepareBudgetContinuation' | 'continueExecution' | 'keepExecution' | 'recheck'> &
  Partial<Pick<SavedChatRun, 'basisNow' | 'recoverDraft' | 'chooseResearch' |
  'establishCases' | 'retryExpectationValidation' | 'proposeExpectationCorrection' | 'approveExpectationCorrection'>> & {
    readonly pendingPlanDecision?: SavedChatRun['pendingPlanDecision']
    decidePlan?: (approved:boolean)=>void
    readonly pendingApproval?: SavedChatRun['pendingApproval']
    decideApproval?: SavedChatRun['decideApproval']
    canCheckProposal?: (candidate:import('./run').Candidate)=>boolean
    checkProposal?: (candidate:import('./run').Candidate)=>Promise<import('../chat/packProposal').PackProposalCheck>
	prepareCreation?: () => Promise<boolean>
    readonly dialectReadRecovery?: SavedChatRun['dialectReadRecovery']
    readonly dialectReadRecoveryBusy?: boolean
    readonly dialectReadRecoveryFailed?: boolean
    inspectDialectRead?: (operationId:string)=>Promise<void>
    refreshDialectReadRecovery?: ()=>Promise<void>
    readonly ownership?: 'server'
    disconnect?: () => void
    readonly inputBlocked?:boolean
    readonly needsReconnect?:boolean
    reconnect?: () => Promise<void>
    retryResponse: () => void
  }
