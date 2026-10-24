import type { RunState } from './run'
export const INITIAL_STATE: RunState = {
  phase: 'idle',
  status: 'idle',
  detail: '',
  brief: '',
  seedUrls: [],
  turns: [],
  questions: [],
  events: [],
  candidates: [],
  cases: [],
  droppedCases: [],
  heldProposal: null,
  expectationIssues: [],
  unknowns: [],
  citations: [],
  verdicts: {},
  registries: {},
  revisionsUsed: 0,
  sessions: [],
  readiness: ''
}
