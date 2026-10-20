/**
 * The shapes a proposal's findings and progress are kept in, read by the test
 * workspace and the proposal generator. The bounded correction that once ran
 * here (`runProposalWorkflow`) has been the harness's since the swap
 * (`@judgment-pack/harness/execution/correctArtifact`, called from
 * `research/run.ts`), and nothing here runs it.
 */
export interface ProposalFinding {
  code: string
  path: string
  message: string
}
export interface ProposalAttempt {
  document: unknown
  unknowns: string[]
  message: string
  findings: ProposalFinding[]
}
export type ProposalState =
  | 'generating'
  | 'checking'
  | 'correcting'
  | 'ready'
  | 'blocked'
  | 'stopped'
  | 'answered'
export interface ProposalProgress {
  state: ProposalState
  attempts: ProposalAttempt[]
  message: string
}
export interface ProposalReply {
  document?: unknown
  unknowns: string[]
  message: string
  failure?: string
}
