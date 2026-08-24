import { cleanup, render } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { deriveWalkLayout, readGraphDocument, type GraphWalkShape } from '../mcp/graphDocument'
import type { GraphSuiteEntry, GraphTestRow, MatrixProbe } from '../mcp/types'
import { GraphWalkDiagram } from './GraphWalkDiagram'

afterEach(cleanup)

/**
 * Two edges whose coverage is the inverse of each other's, so a witness read
 * from the wrong edge's index is a failure and not a coincidence: edge 0 has
 * its resolved branch witnessed and its unresolved branch not, and edge 1 the
 * other way round.
 */
const entry: GraphSuiteEntry = {
  id: 'onboarding',
  status: 'mismatch',
  summary: { total: 1, passed: 0, mismatched: 1 },
  coverage: [
    { probe: 'node:screening:outcome:clear', status: 'covered' },
    { probe: 'node:screening:outcome:match', status: 'missing', detail: 'no row' },
    { probe: 'node:decision:outcome:proceed', status: 'covered' },
    { probe: 'edge:0:resolved', status: 'covered' },
    { probe: 'edge:0:unresolved', status: 'missing', detail: 'no row' },
    { probe: 'edge:1:resolved', status: 'missing', detail: 'no row' },
    { probe: 'edge:1:unresolved', status: 'covered' }
  ]
}

/**
 * A served graph document, shaped exactly as `experimental_get_graph`'s text
 * half carries one: three nodes, two edges — one carrying both devices — and a
 * declared result. Its node ids are the ones the coverage fixture above
 * namespaces its probes to, so the two accounts describe one graph.
 */
const SERVED = JSON.stringify({
  formatVersion: '1',
  id: 'onboarding',
  version: '0.1.0',
  nodes: {
    screening: { pack: 'sanctions-screening' },
    references: { pack: 'reference-check' },
    decision: { pack: 'vendor-onboarding' }
  },
  edges: [
    {
      from: 'screening',
      to: 'decision',
      fact: '/vendor/sanctionsScreening/status',
      evidence: { id: 'sanctions-screening' },
      description: 'the screening outcome lands where the onboarding rules read it.'
    },
    {
      from: 'references',
      to: 'decision',
      evidence: { id: 'reference-check', onUnresolved: 'absent' }
    }
  ],
  result: 'decision'
})

function shapeOf(text: string, coverage: MatrixProbe[] | undefined): GraphWalkShape {
  const read = readGraphDocument(text)
  if (!read.ok) throw new Error(`the fixture is not readable: ${read.reason}`)
  const layout = deriveWalkLayout(read.document, coverage)
  if (!layout.drawn) throw new Error(`the fixture is not drawable: ${layout.reason}`)
  return layout.shape
}

const shape = shapeOf(SERVED, entry.coverage)

/**
 * The composite headline matched byte for byte; the row failed on one node
 * comparison. The row's verdict covers both together — the composite must not
 * wear it.
 *
 * `mismatch` is the runtime's own word for it (internal/graph/rows.go), and it
 * is the word the stylesheet colours. A fixture using any other spelling would
 * assert a class nothing styles, which is a test that passes while the colour
 * is gone.
 */
const row: GraphTestRow = {
  id: 'r1',
  status: 'mismatch',
  expected: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
  actual: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
  nodes: [
    {
      node: 'screening',
      status: 'mismatch',
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
    expect(container.textContent).toContain('mismatch')
    expect(container.textContent).toContain('covers the composite headline and every node comparison')
  })

  it('says a node without a comparison is unreported by the selected row, not unasserted', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} />)
    expect(container.textContent).toContain('selected row reports no comparison')
    expect(container.textContent).not.toContain('no row asserts')
  })

  it('claims coverage representation, never complete structure', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} />)
    expect(container.textContent).toContain('edge indices represented in coverage')
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
    // Two for the declared edges, one for the declared result feeding the
    // composite headline.
    expect(arrows).toHaveLength(3)
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
    expect(container.textContent).toContain('evidence reference-check (absent if unresolved)')
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
    const screening = container.querySelector('.diagram-node-mismatch')
    expect(screening).not.toBeNull()
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

  it('colours that node with a class the stylesheet actually paints', () => {
    // The other half of the previous test: the class name has to be one the
    // sheet has a rule for, or the colouring can be deleted with every
    // assertion still green.
    const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')
    expect(css).toMatch(/\.diagram-node-mismatch\s+\.diagram-box\s*\{/)
    expect(css).toMatch(/\.diagram-node-unreported\s+\.diagram-box\s*\{/)
    expect(css).toMatch(/\.diagram-node-passed\s+\.diagram-box\s*\{/)
  })

  it('draws a node coverage never named, and says coverage named no probe for it', () => {
    // The case the coverage-only view could not represent at all: the document
    // declares a node the coverage report names no probe for. Why it names
    // none is not something either payload states, so nothing here says why.
    const onlyScreening: MatrixProbe[] = [
      { probe: 'node:screening:outcome:clear', status: 'covered' }
    ]
    const withGap = shapeOf(SERVED, onlyScreening)
    const { container } = render(<GraphWalkDiagram entry={entry} shape={withGap} />)
    expect(container.textContent).toContain('not represented in coverage')
    expect(container.textContent).toContain('named by no probe in the coverage report')
    // The absence is reported; the reason for it is not invented.
    expect(container.textContent).not.toContain('never admitted')
  })

  it('claims the document, never coverage representation', () => {
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const svg = container.querySelector('svg')
    expect(svg!.getAttribute('aria-label')).toContain('the served graph document declares')
    expect(svg!.getAttribute('aria-label')).not.toContain('represented in coverage')
    expect(container.textContent).toContain('2 declared edges')
  })

  it('keeps each declared edge beside its own witnesses, found by that edge index', () => {
    // The two edges' coverage is inverted, and each edge's witnesses are read
    // out of that edge's own list item: an off-by-one would put edge 1's
    // witnesses under edge 0 and pass any assertion made over the whole page.
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} shape={shape} />)
    const slots = [...container.querySelectorAll('.diagram-edges .edge-slot')]
    expect(slots).toHaveLength(2)
    expect(slots[0]!.textContent).toContain('screening → decision')
    expect(slots[0]!.textContent).toContain('resolved: covered')
    expect(slots[0]!.textContent).toContain('unresolved: missing')
    expect(slots[1]!.textContent).toContain('references → decision')
    expect(slots[1]!.textContent).toContain('resolved: missing')
    expect(slots[1]!.textContent).toContain('unresolved: covered')
  })
})

describe('GraphWalkDiagram falling back', () => {
  it('renders exactly the coverage view, with no reason, when the runtime has no such tool', () => {
    // jpack 0.18.0 and older: nothing went wrong, so nothing is explained.
    const { container } = render(<GraphWalkDiagram entry={entry} row={row} />)
    expect(container.querySelector('.diagram-edge-line')).toBeNull()
    expect(container.querySelector('.diagram-axis')).not.toBeNull()
    expect(container.textContent).toContain('no arrow is drawn between two')
    expect(container.textContent).toContain('edge indices represented in coverage')
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
})
