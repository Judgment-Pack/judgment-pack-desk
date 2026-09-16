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
/**
 * A settled, complete, passing check on the current candidate, with the run
 * withheld for a reason of its own. Create must still be refused: the pre-PR
 * gate looked only at the latest check and would offer it.
 */
export const withheldButPassing: RunState = {
  ...INITIAL_STATE,
  phase: 'review', status: 'needs-input', detail: 'The draft cites no source.',
  cases: [{ ...row, id: 'valid-case', expectedDisposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } } }],
  candidates: [{
    revision: 1, producedBy: 'research', document: null, text: '{}', digest: 'fixture-digest',
    check: { documentDigest: 'fixture-digest', valid: true, diagnostics: [], cases: [{ id: 'valid-case', passed: true, expected: {}, actual: {} }] }
  }]
}
/**
 * A checked draft whose one disagreeing case differs only in members the table
 * does not draw: both sides render `unresolved`, and the whole of the
 * difference lives in `reasons`, in `handoff` and in the target §8.3 keeps
 * outside the disposition.
 */
const expectedSide = {
  disposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } },
  handoffTarget: { kind: 'human-role', name: 'Screening officer' }
}
const actualSide = {
  disposition: { kind: 'unresolved', reasons: ['no-match'], handoff: { state: 'requested', triggeredBy: ['no-match'] } },
  handoffTarget: null
}
const agreedSide = { disposition: { kind: 'outcome', outcomeId: 'approve', reasons: [], handoff: { state: 'none' } } }
/**
 * What `checkCandidate` stores for its second refusal branch: the runtime
 * answered a disposition but no completed rehearsal, so `actual` is the bare
 * disposition and not the `{ disposition, handoffTarget }` pair. A disclosure
 * over it would read `Runtime disposition: null` beside a real expectation.
 */
const refusedSide = { kind: 'unresolved', reasons: ['no-match'], handoff: { state: 'none' } }
export const disagreeingCase: RunState = {
  ...INITIAL_STATE,
  phase: 'review', status: 'needs-input', detail: 'The unchanged draft disagrees with the reviewed expectations.',
  cases: [
    { ...row, id: 'blocked-unresolved', expectedDisposition: expectedSide.disposition, expectedHandoffTarget: expectedSide.handoffTarget },
    { ...row, id: 'agreeing-case', expectedDisposition: agreedSide.disposition },
    { ...row, id: 'refused-case', expectedDisposition: expectedSide.disposition, expectedHandoffTarget: expectedSide.handoffTarget }
  ],
  candidates: [{
    revision: 1, producedBy: 'research', document: null, text: '{}', digest: 'fixture-digest',
    check: {
      documentDigest: 'fixture-digest', valid: true, diagnostics: [], cases: [
        { id: 'blocked-unresolved', passed: false, expected: expectedSide, actual: actualSide },
        { id: 'agreeing-case', passed: true, expected: agreedSide, actual: agreedSide },
        { id: 'refused-case', passed: false, expected: expectedSide, actual: refusedSide, refused: 'no completed rehearsal: status refused' }
      ]
    }
  }]
}
export const proposedExpectation: RunState = {
  ...blockedExpectation,
  expectationIssues: [{ ...blockedExpectation.expectationIssues[0]!, proposal: {
    expectedDisposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } },
    rationale: 'The missing fact retains the unknown reason. This pack has no handoff configured.',
    token: 'visible-proposal-token', candidateDigest: 'fixture-digest'
  } }]
}
