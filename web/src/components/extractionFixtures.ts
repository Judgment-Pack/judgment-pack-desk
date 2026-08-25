/**
 * The fixtures the extraction goldens render.
 *
 * They live apart from the golden test because they are rendered twice: once
 * here, and once against the tree as it stood *before* `TracePanel` and
 * `TargetSide` were pulled out of `EvaluationView` and `MatrixRowList`. The
 * golden HTML beside them was produced by that second rendering, so what the
 * goldens pin is the pre-extraction DOM rather than whatever this tree happens
 * to emit today.
 *
 * They are deliberately maximal. A golden is only worth the branches its
 * fixture reaches, and the branches most likely to be lost in a move are the
 * optional ones nobody was looking at: the rehearsal banner, the draft-RFC
 * panel, the handoff-target aside, the bundled-artifact row, an unnamed trace
 * entry, a suppressed one, a repeated stage, a row expecting a refusal, and
 * each of the three target-report states.
 *
 * Every disposition is RFC 8785 canonical: members ordered by name, `handoff`
 * required, and the kind vocabulary the evaluator's own.
 */
import type { Evaluation, MatrixRow } from '../mcp/types'

const PROCEED = '{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"proceed","reasons":[]}'
const REVIEW =
  '{"handoff":{"state":"requested","triggeredBy":["exception-escalation"]},"kind":"outcome","outcomeId":"review","reasons":["exception-escalation"]}'
const UNRESOLVED =
  '{"handoff":{"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}'

/** Every optional member the evaluation view can render, all at once. */
export const FULL_EVALUATION: Evaluation = {
  outputVersion: '2',
  tool: { name: 'jpack', version: '0.19.0' },
  command: 'experimental evaluate',
  status: 'evaluated',
  experimental: true,
  rehearsal: true,
  conformanceClaimReference: 'CONFORMANCE.md',
  specVersion: '0.2.0-draft',
  evaluatorSpecVersion: '0.2.0-draft',
  packId: 'vendor-onboarding',
  packVersion: '0.3.1',
  draftPrototype: {
    rfc: 'RFC 0008',
    status: 'proposed',
    operators: ['allOf', 'noneOf'],
    packValidUnderSpecVersion: false,
    note: 'This evaluation ran under a draft-RFC grammar extension.'
  },
  disposition: {
    kind: 'outcome',
    outcomeId: 'review',
    reasons: ['exception-escalation'],
    handoff: { state: 'requested', triggeredBy: ['exception-escalation'] }
  },
  handoffTarget: { kind: 'queue', name: 'vendor-review' },
  trace: [
    { stage: 'applicability', condition: 'true' },
    { stage: 'exception', id: 'sanctioned-jurisdiction', condition: 'true', effect: 'escalate' },
    { stage: 'exception', id: 'grandfathered', condition: 'false', suppressed: true },
    { stage: 'rule', id: 'screen-clear', condition: 'unknown', onUnknown: 'escalate' },
    { stage: 'rule', id: 'approve', condition: 'true', outcome: 'proceed' },
    { stage: 'rule', id: 'fallback', condition: 'not-evaluated', skipped: true }
  ],
  artifact: {
    specVersion: '0.2.0-draft',
    bundleDigest: 'sha256:9f2c1d',
    provenance: 'embedded'
  }
}

/** The same view with every optional member absent, and an empty trace. */
export const BARE_EVALUATION: Evaluation = {
  outputVersion: '2',
  tool: { name: 'jpack', version: '0.19.0' },
  command: 'experimental evaluate',
  status: 'evaluated',
  experimental: false,
  conformanceClaimReference: 'CONFORMANCE.md',
  specVersion: '0.2.0-draft',
  evaluatorSpecVersion: '0.2.0-draft',
  packId: 'vendor-onboarding',
  packVersion: '0.3.1',
  disposition: {
    kind: 'not-applicable',
    reasons: ['not-applicable'],
    handoff: { state: 'none' }
  },
  trace: []
}

/** Every row shape the pack matrix list can render, including all three
 *  target-report states and both halves of a refusal comparison. */
export const MATRIX_ROWS: MatrixRow[] = [
  {
    id: 'proceeds',
    status: 'passed',
    origin: 'authored',
    specSection: '8.3',
    expected: PROCEED,
    actual: PROCEED
  },
  {
    id: 'dispositions-differ',
    status: 'mismatch',
    expected: PROCEED,
    actual: UNRESOLVED,
    detail: 'the evaluator produced a different disposition'
  },
  {
    id: 'expects-a-refusal',
    status: 'passed',
    expected: '',
    actual: '',
    expectedErrorClass: 'malformed-input',
    expectedErrorPhase: 'admission',
    actualErrorClass: 'malformed-input',
    actualErrorPhase: 'admission'
  },
  {
    id: 'expected-a-refusal-and-got-none',
    status: 'mismatch',
    expected: '',
    actual: PROCEED,
    expectedErrorClass: 'malformed-input',
    expectedErrorPhase: 'admission'
  },
  {
    id: 'asserts-a-named-target',
    status: 'mismatch',
    expected: REVIEW,
    actual: REVIEW,
    expectedHandoffTarget: '{"kind":"queue","name":"vendor-review"}',
    actualHandoffTarget: '{"kind":"queue","name":"vendor-review-emea"}',
    detail: 'the handoff target did not match the assertion'
  },
  {
    id: 'asserts-no-target',
    status: 'passed',
    expected: PROCEED,
    actual: PROCEED,
    expectedHandoffTarget: 'null',
    actualHandoffTarget: 'null'
  },
  {
    id: 'target-unavailable',
    status: 'mismatch',
    expected: PROCEED,
    actual: '',
    actualErrorClass: 'malformed-input',
    expectedHandoffTarget: '{"kind":"queue","name":"vendor-review"}',
    actualHandoffTarget: 'unavailable'
  }
]
