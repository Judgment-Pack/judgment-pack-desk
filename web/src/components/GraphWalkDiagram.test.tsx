import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { GraphSuiteEntry, GraphTestRow } from '../mcp/types'
import { GraphWalkDiagram } from './GraphWalkDiagram'

afterEach(cleanup)

const entry: GraphSuiteEntry = {
  id: 'onboarding',
  status: 'mismatch',
  summary: { total: 1, passed: 0, mismatched: 1 },
  coverage: [
    { probe: 'node:screening:outcome:clear', status: 'covered' },
    { probe: 'node:screening:outcome:match', status: 'missing', detail: 'no row' },
    { probe: 'node:decision:outcome:proceed', status: 'covered' },
    { probe: 'edge:0:resolved', status: 'covered' },
    { probe: 'edge:0:unresolved', status: 'missing', detail: 'no row' }
  ]
}

// The composite headline matched byte for byte; the row failed on one node
// comparison. The row's verdict covers both together — the composite must not
// wear it.
const row: GraphTestRow = {
  id: 'r1',
  status: 'mismatched',
  expected: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
  actual: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
  nodes: [
    {
      node: 'screening',
      status: 'mismatched',
      expected: '{"kind":"outcome","outcomeId":"clear","reasons":[]}',
      actual: '{"kind":"unresolved","reasons":["unknown"]}'
    }
  ]
}

describe('GraphWalkDiagram', () => {
  it('never paints the composite with the row status', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} />)
    const composite = container.querySelector('.diagram-composite')
    expect(composite).not.toBeNull()
    expect(composite!.getAttribute('class')).toBe('diagram-node diagram-composite')
    // The verdict is reported as the row's own, beside the diagram.
    expect(container.textContent).toContain('mismatched')
    expect(container.textContent).toContain('covers the composite headline and every node comparison')
  })

  it('says a node without a comparison is unreported by the selected row, not unasserted', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} />)
    expect(container.textContent).toContain('selected row reports no comparison')
    expect(container.textContent).not.toContain('no row asserts')
  })

  it('claims coverage representation, never complete structure', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} />)
    expect(container.textContent).toContain('edge index represented in coverage')
    expect(container.textContent).not.toContain('declared edge')
    const svg = container.querySelector('svg')
    expect(svg!.getAttribute('aria-label')).toContain('represented in coverage')
  })

  it('reports nothing about shape when coverage names no node', () => {
    const bare: GraphSuiteEntry = {
      id: 'empty',
      status: 'mismatch',
      summary: { total: 0, passed: 0, mismatched: 0 },
      coverage: []
    }
    const { container } = render(<GraphWalkDiagram entry={bare} />)
    expect(container.querySelector('svg')).toBeNull()
  })
})
