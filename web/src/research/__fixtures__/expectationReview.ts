import { INITIAL_STATE, type RunState } from '../run'

const row = {
  id: 'missing-required-fact-unresolved', facts: {}, expectationSource: 'src-1#e1',
  rationale: 'Missing facts must leave the decision unresolved.',
  expectedDisposition: { kind: 'unresolved', reasons: [], handoff: { state: 'none' } }
}
export const blockedExpectation: RunState = {
  ...INITIAL_STATE,
  phase: 'review', status: 'needs-input', detail: '1 invalid expectation must be corrected and approved before testing or creating the pack.',
  cases: [{ ...row, id: 'valid-case', expectedDisposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } } }],
  candidates: [{ revision: 1, producedBy: 'research', document: null, text: '{}', digest: 'fixture-digest' }],
  expectationIssues: [{ id: row.id, original: row, message: '§8.3: reasons is empty if and only if kind is "outcome"' }]
}
export const proposedExpectation: RunState = {
  ...blockedExpectation,
  expectationIssues: [{ ...blockedExpectation.expectationIssues[0]!, proposal: {
    expectedDisposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } },
    rationale: 'The missing fact retains the unknown reason. This pack has no handoff configured.',
    token: 'visible-proposal-token', candidateDigest: 'fixture-digest'
  } }]
}
