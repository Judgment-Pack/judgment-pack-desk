import type { SavedChatRun } from '../assistant/serverRuns/controller'
/** Shared UI contract. Canonical controllers implement transport actions, never the browser engine. */
export type ResearchRunController = Pick<SavedChatRun,
  'subscribe' | 'getSnapshot' | 'running' | 'start' | 'send' | 'stop' | 'appendConversation' |
  'canRetryResponse' | 'canExtendResponse' | 'updateQuestionDraft' | 'prepareQuestionAnswer' |
  'continueQuestion' | 'prepareBudgetContinuation' | 'continueExecution' | 'keepExecution' | 'recheck'> &
  Partial<Pick<SavedChatRun, 'basisNow' | 'recoverDraft' | 'chooseResearch' |
  'establishCases' | 'retryExpectationValidation' | 'proposeExpectationCorrection' | 'approveExpectationCorrection'>> & {
    readonly pendingApproval?: {callId:string;argsDigest:string;name:string;args:Record<string,unknown>}
    decideApproval?: (approved:boolean)=>void
    checkProposal?: (candidate:import('./run').Candidate)=>Promise<import('../chat/packProposal').PackProposalCheck>
	prepareCreation?: () => Promise<boolean>
    readonly ownership?: 'server'
    disconnect?: () => void
    readonly needsReconnect?:boolean
    reconnect?: () => Promise<void>
    retryResponse: () => void
  }
