import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { deriveWalkShape, parseGraphDocument } from '../mcp/graphDocument'
import type { GraphSuiteEntry, GraphTestRow, MatrixProbe } from '../mcp/types'
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

/**
 * A served graph document, shaped exactly as `experimental_get_graph`'s text
 * half carries one: two nodes, one edge carrying both devices, and a declared
 * result. Its node ids are the ones the coverage fixture above namespaces its
 * probes to, so the two accounts describe one graph.
 */
const SERVED = JSON.stringify({
  formatVersion: '1',
  id: 'onboarding',
  version: '0.1.0',
  nodes: {
    screening: { pack: 'sanctions-screening' },
    decision: { pack: 'vendor-onboarding' }
  },
  edges: [
    {
      from: 'screening',
      to: 'decision',
      fact: '/vendor/sanctionsScreening/status',
      evidence: { id: 'sanctions-screening' },
      description: 'the screening outcome lands where the onboarding rules read it.'
    }
  ],
  result: 'decision'
})

const shape = deriveWalkShape(parseGraphDocument(SERVED)!, entry.coverage)

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

describe('GraphWalkDiagram, drawn from a served document', () => {
  it('draws one arrow per declared edge, which the fallback draws none of', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const arrows = container.querySelectorAll('.diagram-edge-line')
    // One for the declared edge, one for the declared result feeding the
    // composite headline.
    expect(arrows).toHaveLength(2)
    expect(container.querySelector('marker')).not.toBeNull()
    // And the fallback's apology for having none is gone.
    expect(container.textContent).not.toContain('no arrow is drawn between two')
  })

  it('labels each arrow with what that edge carries', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const labels = [...container.querySelectorAll('.diagram-edge-label')].map(
      (label) => label.textContent
    )
    expect(labels.join(' ')).toContain('/vendor/sanctionsScreening/status')
    expect(labels.join(' ')).toContain('declared result')
    // The full text is in the list beside the diagram, whatever the drawing
    // had room for.
    expect(container.textContent).toContain('evidence sanctions-screening')
    expect(container.textContent).toContain('screening → decision')
  })

  it('marks the node the document declares as its result', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const marked = container.querySelectorAll('.diagram-node-result')
    expect(marked).toHaveLength(1)
    expect(marked[0]!.textContent).toContain('decision')
    expect(marked[0]!.textContent).toContain('result')
  })

  it('names each node the pack the document says it evaluates', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    expect(container.textContent).toContain('pack sanctions-screening')
    expect(container.textContent).toContain('pack vendor-onboarding')
  })

  it('never paints the composite with the row status', () => {
    // The same rule as the fallback, and for the same reason: row.status covers
    // the headline AND every reported node comparison, so a node-only mismatch
    // must not colour a byte-identical composite.
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const composite = container.querySelector('.diagram-composite')
    expect(composite!.getAttribute('class')).toBe('diagram-node diagram-composite')
    expect(container.textContent).toContain('covers the composite headline and every node comparison')
  })

  it('colours a node only from the selected row, and says so where the row is silent', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const screening = container.querySelector('.diagram-node-mismatched')
    expect(screening!.textContent).toContain('screening')
    // The row reports no comparison for the result node, so it wears no verdict
    // — not even the passing one its own graph row carries.
    const decision = [...container.querySelectorAll('.diagram-node')].find((node) =>
      node.textContent?.includes('decision')
    )
    expect(decision!.getAttribute('class')).toContain('diagram-node-unreported')
    expect(container.textContent).toContain('selected row reports no comparison')
    expect(container.textContent).not.toContain('no row asserts')
  })

  it('draws a node coverage never named, and says coverage never named it', () => {
    // The case the coverage-only view could not represent at all: a node the
    // run never admitted exists in the document and nowhere else.
    const onlyScreening: MatrixProbe[] = [
      { probe: 'node:screening:outcome:clear', status: 'covered' }
    ]
    const withGap = deriveWalkShape(parseGraphDocument(SERVED)!, onlyScreening)
    const { container } = render(<GraphWalkDiagram entry={entry} shape={withGap} />)
    expect(container.textContent).toContain('not represented in coverage')
    expect(container.textContent).toContain('named by no probe in the coverage report')
  })

  it('claims the document, never coverage representation', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const svg = container.querySelector('svg')
    expect(svg!.getAttribute('aria-label')).toContain('the served graph document declares')
    expect(svg!.getAttribute('aria-label')).not.toContain('represented in coverage')
    expect(container.textContent).toContain('1 declared edge')
  })

  it('keeps the coverage witness on each declared edge, found by its index', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    expect(container.textContent).toContain('resolved: covered')
    expect(container.textContent).toContain('unresolved: missing')
  })
})

describe('GraphWalkDiagram falling back', () => {
  it('renders exactly the coverage view, with no reason, when the runtime has no such tool', () => {
    // jpack 0.18.0 and older: nothing went wrong, so nothing is explained.
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} />)
    expect(container.querySelector('.diagram-edge-line')).toBeNull()
    expect(container.querySelector('.diagram-axis')).not.toBeNull()
    expect(container.textContent).toContain('no arrow is drawn between two')
    expect(container.textContent).toContain('edge index represented in coverage')
    expect(container.textContent).not.toContain('could not decode')
  })

  it('falls back and says why for a document the runtime served undecodable', () => {
    const { container } = render(
      <GraphWalkDiagram
        entry={entry}
        row={row}
        fallbackReason="The runtime served this graph's document and could not decode it — not valid JSON at line 1 — and serving is not validating, so the walk below is the coverage report's evaluation order and no edge is drawn."
      />
    )
    expect(container.querySelector('.diagram-edge-line')).toBeNull()
    expect(container.querySelector('.diagram-axis')).not.toBeNull()
    expect(container.textContent).toContain('could not decode it')
    // The fallback's own account of itself is unchanged underneath.
    expect(container.textContent).toContain('no arrow is drawn between two')
  })

  it('still says why when the fallback has no node to draw either', () => {
    const bare: GraphSuiteEntry = {
      id: 'empty',
      status: 'mismatch',
      summary: { total: 0, passed: 0, mismatched: 0 },
      coverage: []
    }
    const { container } = render(
      <GraphWalkDiagram entry={bare} fallbackReason="the runtime refused to serve it" />
    )
    expect(container.querySelector('svg')).toBeNull()
    expect(container.textContent).toContain('the runtime refused to serve it')
  })

  it('falls back when a served document declares no node at all', () => {
    const empty = deriveWalkShape(parseGraphDocument('{"nodes":{},"edges":[]}')!, entry.coverage)
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={empty} />)
    expect(container.querySelector('.diagram-axis')).not.toBeNull()
    expect(container.textContent).toContain('no arrow is drawn between two')
  })
})
