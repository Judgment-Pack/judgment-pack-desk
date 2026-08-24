import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { MatrixRow } from '../mcp/types'
import { MatrixRowList } from './MatrixRowList'

afterEach(cleanup)

const disposition = (kind: string, outcomeId?: string) =>
  JSON.stringify(outcomeId ? { kind, outcomeId, reasons: [] } : { kind, reasons: [] })

const baseRow: MatrixRow = {
  id: 'row-1',
  status: 'passed',
  expected: disposition('outcome', 'proceed'),
  actual: disposition('outcome', 'proceed')
}

describe('MatrixRowList', () => {
  it('renders canonical disposition text parsed, unmarked when equal', () => {
    const { container } = render(<MatrixRowList rows={[baseRow]} />)
    expect(container.querySelectorAll('.row-side-differs')).toHaveLength(0)
    expect(container.textContent).toContain('proceed')
  })

  it('marks differing disposition sides, following the payload status', () => {
    const row: MatrixRow = {
      ...baseRow,
      id: 'row-2',
      status: 'mismatched',
      actual: disposition('unresolved')
    }
    const { container } = render(<MatrixRowList rows={[row]} />)
    expect(container.querySelectorAll('.row-side-differs').length).toBeGreaterThan(0)
    expect(container.querySelector('.pill-danger')?.textContent).toBe('mismatched')
  })

  it('never derives a verdict from target renderings: no differs mark, no diagnosis', () => {
    // The runtime compares decoded targets; these display strings differ, and
    // the row still passed. Nothing here may claim otherwise.
    const row: MatrixRow = {
      ...baseRow,
      id: 'row-3',
      expectedHandoffTarget: '{"kind":"human-role","name":"Intake reviewer"}',
      actualHandoffTarget: '{"kind":"human-role","name":"Intake reviewer…sha256:aa"}'
    }
    const { container } = render(<MatrixRowList rows={[row]} />)
    const targets = container.querySelector('.row-targets')
    expect(targets).not.toBeNull()
    expect(targets!.querySelectorAll('.row-side-differs')).toHaveLength(0)
    expect(container.textContent).not.toContain('fails on the handoff target alone')
  })

  it('distinguishes a named target assertion from asserting no target at all', () => {
    const named: MatrixRow = {
      ...baseRow,
      id: 'row-4',
      expectedHandoffTarget: '{"kind":"human-role","name":"Intake reviewer"}',
      actualHandoffTarget: '{"kind":"human-role","name":"Intake reviewer"}'
    }
    const noTarget: MatrixRow = {
      ...baseRow,
      id: 'row-5',
      expectedHandoffTarget: 'null',
      actualHandoffTarget: 'null'
    }
    const { container } = render(<MatrixRowList rows={[named, noTarget]} />)
    expect(container.textContent).toContain('asserts a handoff-target state')
    expect(container.textContent).toContain('asserts no handoff target')
    expect(container.textContent).toContain('Intake reviewer (human-role)')
    expect(container.textContent).toContain('no target')
  })

  it('renders the unavailable member as the report state it is', () => {
    const row: MatrixRow = {
      ...baseRow,
      id: 'row-6',
      status: 'mismatched',
      expectedHandoffTarget: '{"kind":"human-role","name":"Intake reviewer"}',
      actualHandoffTarget: 'unavailable'
    }
    const { container } = render(<MatrixRowList rows={[row]} />)
    expect(container.textContent).toContain('unavailable')
  })

  it('marks a refusal whose class matches but whose expected phase does not', () => {
    const row: MatrixRow = {
      id: 'row-7',
      status: 'mismatched',
      expected: '',
      actual: '',
      expectedErrorClass: 'resource-exhaustion',
      expectedErrorPhase: 'evaluation',
      actualErrorClass: 'resource-exhaustion',
      actualErrorPhase: 'preflight'
    }
    const { container } = render(<MatrixRowList rows={[row]} />)
    expect(container.querySelectorAll('.row-side-differs')).toHaveLength(1)
  })

  it('gives skipped its own tone, never the success accent', () => {
    const row: MatrixRow = { ...baseRow, id: 'row-8', status: 'skipped' }
    const { container } = render(<MatrixRowList rows={[row]} />)
    expect(container.querySelector('.pill-skipped')?.textContent).toBe('skipped')
    expect(container.querySelector('.pill-success')).toBeNull()
  })
})
