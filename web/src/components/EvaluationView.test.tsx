import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { Evaluation } from '../mcp/types'
import { EvaluationView } from './EvaluationView'

afterEach(cleanup)

/**
 * One evaluation whose trace exercises what the renderer distinguishes: two
 * consecutive stages, an entry with no id, an unknown resolution with the
 * `onUnknown` it was resolved by, and a skipped entry.
 */
const PAYLOAD: Evaluation = {
  outputVersion: '1',
  tool: { name: 'jpack', version: 'test' },
  command: 'experimental evaluate',
  status: 'ok',
  experimental: true,
  conformanceClaimReference: 'CONFORMANCE.md',
  specVersion: '0.2.0-draft',
  evaluatorSpecVersion: '0.2.0-draft',
  packId: 'vendor-onboarding',
  packVersion: '0.1.0',
  disposition: {
    kind: 'outcome',
    outcomeId: 'proceed',
    reasons: [],
    handoff: { state: 'none' }
  },
  trace: [
    { stage: 'applicability', condition: 'true' },
    { stage: 'exception', id: 'sanctioned-jurisdiction', condition: 'false' },
    { stage: 'rule', id: 'screen-clear', condition: 'unknown', onUnknown: 'escalate' },
    { stage: 'rule', id: 'fallback', condition: 'false', skipped: true }
  ]
}

describe('EvaluationView', () => {
  it('renders the trace through the shared renderer, badges and all', () => {
    // This is the evaluate side of the one trace renderer the graph matrix now
    // also uses (ADR-0031). It is asserted here so that breaking the shared
    // component fails on *both* surfaces: a test on only one would let the
    // renderer drift for the other, which is exactly what sharing it prevents.
    const { container } = render(<EvaluationView payload={PAYLOAD} />)

    expect(screen.getByText(/^Trace/)).toBeTruthy()
    expect(container.textContent).toContain('It decides nothing')
    // The stages, in the evaluator's own walk order and never regrouped: two
    // `rule` entries follow one `exception` entry, and the two stage headings
    // stay two.
    const stages = [...container.querySelectorAll('.trace-stage h4')].map((h) => h.textContent)
    expect(stages).toEqual(['applicability', 'exception', 'rule'])
    // The numbering is the trace's, not each stage's.
    const lists = [...container.querySelectorAll('.trace-list')].map((ol) =>
      (ol as HTMLOListElement).getAttribute('start')
    )
    expect(lists).toEqual(['1', '2', '3'])

    expect(container.textContent).toContain('sanctioned-jurisdiction')
    expect(container.textContent).toContain('on unknown: escalate')
    expect(container.textContent).toContain('skipped')
    // An entry with no id is said to be unnamed rather than shown blank.
    expect(container.textContent).toContain('unnamed applicability condition')
  })

  it('says a payload carries no trace entries rather than showing nothing', () => {
    const { container } = render(<EvaluationView payload={{ ...PAYLOAD, trace: [] }} />)
    expect(container.textContent).toContain('This payload carries no trace entries')
  })
})
