import type { AuthoringRun } from './run'
/** Shared UI contract. Canonical controllers implement transport actions, never the browser engine. */
export type ResearchRunController = Pick<AuthoringRun,
  'subscribe' | 'getSnapshot' | 'running' | 'start' | 'send' | 'stop' | 'appendConversation' |
  'canRetryResponse' | 'canExtendResponse' | 'updateQuestionDraft' | 'prepareQuestionAnswer' |
  'continueQuestion' | 'prepareBudgetContinuation' | 'continueExecution' | 'keepExecution' | 'recheck'> &
  Partial<Pick<AuthoringRun, 'restore' | 'basisNow' | 'recoverDraft' | 'chooseResearch' |
  'establishCases' | 'retryExpectationValidation' | 'proposeExpectationCorrection' | 'approveExpectationCorrection'>> & {
    readonly pendingApproval?: {callId:string;argsDigest:string;name:string;args:Record<string,unknown>}
    decideApproval?: (approved:boolean)=>void
    checkProposal?: (candidate:import('./run').Candidate)=>Promise<import('../chat/packProposal').PackProposalCheck>
    readonly ownership?: 'server'
    disconnect?: () => void
    readonly needsReconnect?:boolean
    reconnect?: () => Promise<void>
    retryResponse: () => void
  }
