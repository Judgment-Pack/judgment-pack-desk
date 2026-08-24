import { act, cleanup, screen, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import {
  connected,
  renderConnected,
  stubClient,
  testQueryClient,
  type ToolHandler
} from '../testing/harness'
import type { GraphInventory, GraphSuite } from '../mcp/types'
import { GraphView } from './GraphView'

afterEach(cleanup)

/**
 * One configured graph, run: two nodes, one edge, coverage namespaced to the
 * node ids the document below declares. The two payloads describe one graph and
 * are joined by nothing but those names — which is the whole reason the view
 * insists both come from one settled reading of one connection.
 */
const MATRIX: GraphSuite = {
  status: 'passed',
  summary: { total: 1, passed: 1, mismatched: 0 },
  graphs: [
    {
      id: 'onboarding',
      status: 'passed',
      graphId: 'vendor-onboarding-flow',
      summary: { total: 1, passed: 1, mismatched: 0 },
      coverage: [
        { probe: 'node:screening:outcome:clear', status: 'covered' },
        { probe: 'node:decision:outcome:proceed', status: 'covered' },
        { probe: 'edge:0:resolved', status: 'covered' }
      ],
      rows: [
        {
          id: 'clear-approves',
          status: 'passed',
          expected: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
          actual: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
          nodes: [
            {
              node: 'screening',
              status: 'passed',
              expected: '{"kind":"outcome","outcomeId":"clear","reasons":[]}',
              actual: '{"kind":"outcome","outcomeId":"clear","reasons":[]}'
            }
          ]
        }
      ]
    }
  ]
}

const INVENTORY: GraphInventory = {
  status: 'valid',
  configPath: '/project/jpack.json',
  configVersion: '2',
  graphs: [
    {
      id: 'onboarding',
      graphId: 'vendor-onboarding-flow',
      graphVersion: '0.1.0',
      formatVersion: '1',
      resultNode: 'decision',
      path: 'graphs/vendor-onboarding.graph.json',
      rowsDeclared: true,
      nodeCount: 2,
      edgeCount: 1
    }
  ]
}

const DOCUMENT = JSON.stringify({
  formatVersion: '1',
  id: 'vendor-onboarding-flow',
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
      evidence: { id: 'sanctions-screening' }
    }
  ],
  result: 'decision'
})

const SERVED_META = {
  status: 'valid',
  id: 'onboarding',
  graphId: 'vendor-onboarding-flow',
  graphVersion: '0.1.0',
  formatVersion: '1',
  path: 'graphs/vendor-onboarding.graph.json',
  bytes: DOCUMENT.length,
  sha256: 'a'.repeat(64)
}

function servingDesk(overrides: Record<string, ToolHandler> = {}) {
  return stubClient({
    experimental_test_graphs: () => ({ text: JSON.stringify(MATRIX) }),
    experimental_list_graphs: () => ({ text: JSON.stringify(INVENTORY) }),
    experimental_get_graph: () => ({ text: DOCUMENT, structured: SERVED_META }),
    ...overrides
  })
}

function view() {
  return (
    <Routes>
      <Route path="/graphs" element={<GraphView />} />
      <Route path="/graphs/:graphId" element={<GraphView />} />
    </Routes>
  )
}

function serving(client: ReturnType<typeof stubClient>['client']) {
  return connected({ client, graphDocumentSupported: true, graphInventorySupported: true })
}

const arrows = (container: HTMLElement) => container.querySelectorAll('.diagram-edge-line').length

describe('the graphs page, against a runtime that serves documents', () => {
  it('draws the served document, and says which document it drew', async () => {
    const { client } = servingDesk()
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/declared edge/)
    // One arrow for the declared edge, one for the declared result.
    expect(arrows(container)).toBe(2)
    expect(container.textContent).toContain('drawn from the served document vendor-onboarding-flow')
  })

  it("refuses to draw a document the runtime could not decode, and shows the runtime's reason", async () => {
    // The text parses here — duplicate member names are last-wins in JSON.parse
    // and refused by the runtime's carrier decode — so nothing but the status
    // stops this page from drawing a graph the runtime already refused.
    const parseable =
      '{"formatVersion":"1","nodes":{"a":{"pack":"p"}},"nodes":{"b":{"pack":"q"}},"edges":[]}'
    const { client } = servingDesk({
      experimental_get_graph: () => ({
        text: parseable,
        structured: {
          ...SERVED_META,
          status: 'undecodable',
          graphId: '',
          detail: 'graph document graphs/vendor-onboarding.graph.json: Object member name is duplicated.'
        }
      })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/could not decode it/)
    expect(arrows(container)).toBe(0)
    // Verbatim: the runtime's own sentence, path and all.
    expect(container.textContent).toContain('Object member name is duplicated.')
    expect(container.textContent).toContain('serving is not validating')
    // And the fallback underneath is the coverage walk, saying what it is.
    expect(container.textContent).toContain('no arrow is drawn between two')
  })

  it('falls back and names the member where the document decoded and could not be read', async () => {
    const { client } = servingDesk({
      experimental_get_graph: () => ({
        text: '{"formatVersion":"1","nodes":{"screening":{"pack":"p"}}}',
        structured: SERVED_META
      })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/`edges` member/)
    expect(arrows(container)).toBe(0)
    // The claim a coerced empty array would have produced is nowhere on screen.
    expect(container.textContent).not.toContain('This document declares no edge')
  })

  it('stops drawing when the runtime it reconnects to no longer advertises the tool', async () => {
    // The document stays in the cache. What it may be drawn from is the
    // capability the *current* connection reports, so a reconnect to jpack
    // 0.18.0 withdraws the drawing rather than leaving arrows no live
    // capability accounts for.
    const { client } = servingDesk()
    const { container, setConnection } = renderConnected(view(), serving(client), {
      path: '/graphs'
    })
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)

    setConnection(
      connected({ client, graphDocumentSupported: false, graphInventorySupported: true })
    )
    await waitFor(() => expect(arrows(container)).toBe(0))
    // Nothing went wrong, so nothing is apologised for.
    expect(container.textContent).not.toContain('could not decode')
    expect(container.textContent).toContain('no arrow is drawn between two')
  })

  it('draws nothing from the connection before a reconnect', async () => {
    // The document query is keyed by the connection epoch, so a new connection
    // starts with no document at all rather than with the last one's.
    const { client, calls } = servingDesk()
    const { container, setConnection } = renderConnected(view(), serving(client), {
      path: '/graphs'
    })
    await screen.findByText(/declared edge/)
    const before = calls.filter((call) => call.name === 'experimental_get_graph').length

    setConnection(
      connected({
        client,
        graphDocumentSupported: true,
        graphInventorySupported: true,
        connectionEpoch: 2
      })
    )
    await waitFor(() =>
      expect(calls.filter((call) => call.name === 'experimental_get_graph').length).toBe(before + 1)
    )
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)
  })

  it('withdraws the drawing while the matrix it is joined to is being re-run', async () => {
    // The coverage a node's "not represented in coverage" line rests on comes
    // from the matrix run. While that run is in flight the previous run's
    // coverage is what is in hand, and joining it to a document is a claim
    // about a reading that is no longer current.
    let matrixCalls = 0
    const { client } = servingDesk({
      experimental_test_graphs: () => {
        matrixCalls += 1
        if (matrixCalls === 1) return { text: JSON.stringify(MATRIX) }
        return new Promise<never>(() => {})
      }
    })
    const queryClient = testQueryClient()
    const { container } = renderConnected(view(), serving(client), {
      path: '/graphs',
      queryClient
    })
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)

    await act(async () => {
      void queryClient.invalidateQueries({ queryKey: ['experimental_test_graphs', null] })
      await Promise.resolve()
    })
    await waitFor(() => expect(arrows(container)).toBe(0))
    expect(container.textContent).toContain('the served graph document')
  })

  it('reports an inventory that refused, and shows no configuration it cannot confirm', async () => {
    const { client } = servingDesk({
      experimental_list_graphs: () => ({
        text: 'this project declares no jpack.json under /project',
        isError: true
      })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/could not be listed/)
    expect(container.textContent).toContain('this project declares no jpack.json under /project')
    // The section named "Configured" is absent rather than showing what a
    // failed call could not confirm.
    expect(container.textContent).not.toContain('Configured')
    // The matrix still ran, and still reports what it can.
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)
  })
})
