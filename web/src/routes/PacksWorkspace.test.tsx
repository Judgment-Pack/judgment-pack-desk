import { act, cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { connected, renderConnected, stubClient, testQueryClient } from '../testing/harness'
import { GraphView } from './GraphView'
import { MatrixView } from './MatrixView'

afterEach(cleanup)
const graph = JSON.stringify({ formatVersion: '1', nodes: { first: { pack: 'a' }, second: { pack: 'b' } }, edges: [{ from: 'first', to: 'second', fact: '/input/value' }], result: 'second' })
const inventory = { status: 'valid', graphs: [{ id: 'flow', graphId: 'flow', graphVersion: '1', formatVersion: '1', path: 'flow.json', rowsDeclared: true, nodeCount: 2, edgeCount: 1 }] }
const suite = { status: 'passed', summary: { total: 1, passed: 1, mismatched: 0 }, graphs: [] }
function desk() {
  return stubClient({
    list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [{ id: 'a', matrix: true }] }) }),
    experimental_list_graphs: () => ({ text: JSON.stringify(inventory) }),
    experimental_get_graph: () => ({ text: graph, structured: { status: 'valid', id: 'flow', graphId: 'flow', sha256: 'a'.repeat(64) } }),
    experimental_test_graphs: () => ({ text: JSON.stringify(suite) }),
    experimental_test_packs: () => ({ text: JSON.stringify({ ...suite, packs: [] }) })
  })
}
function views() {
  return <Routes>
    <Route path="/graphs" element={<GraphView />} />
    <Route path="/graphs/:graphId" element={<GraphView />} />
    <Route path="/matrix" element={<MatrixView />} />
    <Route path="/packs/:packId/matrix" element={<MatrixView />} />
  </Routes>
}
const testCalls = (stub: ReturnType<typeof desk>) => stub.calls.filter(call => call.name.startsWith('experimental_test_'))

describe('Packs workspace commands', () => {
  it.each(['/graphs', '/graphs?view=tests', '/graphs/flow?view=tests', '/matrix', '/packs/a/matrix'])('does not run tests while browsing %s or refreshing files', async path => {
    const stub = desk(), queryClient = testQueryClient()
    const rendered = renderConnected(views(), connected({ client: stub.client, graphInventorySupported: true }), { path, queryClient })
    await waitFor(() => expect(stub.calls.length).toBeGreaterThan(0))
    await act(async () => { await queryClient.invalidateQueries() })
    rendered.setConnection(connected({ client: stub.client, graphInventorySupported: true, connectionEpoch: 2 }))
    await act(async () => { await Promise.resolve() })
    expect(testCalls(stub)).toHaveLength(0)
  })

  it('does not run suites as an inventory fallback on older runtimes', async () => {
    const stub = desk()
    renderConnected(views(), connected({ client: stub.client }), { path: '/graphs' })
    expect(screen.getByText(/cannot list pack flows without running/)).toBeTruthy()
    expect(stub.calls).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Run all flow tests' }))
    await screen.findByText(/1 of 1 cases passed/)
    expect(testCalls(stub)).toHaveLength(1)
  })

  it('reads a diagram directly without testing the flow', async () => {
    const stub = desk()
    renderConnected(views(), connected({ client: stub.client, graphDocumentSupported: true, graphInventorySupported: true }), { path: '/graphs/flow' })
    await screen.findByRole('button', { name: /first → second/ })
    expect(testCalls(stub)).toHaveLength(0)
    expect(stub.calls.find(call => call.name === 'experimental_get_graph')?.args).toEqual({ graph_id: 'flow' })
    fireEvent.click(screen.getByRole('link', { name: /^Tests$/ }))
    expect(testCalls(stub)).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Run tests' }))
    await screen.findByText(/1 of 1 cases passed/)
    expect(testCalls(stub)[0]?.args).toEqual({ graph_id: 'flow' })
  })

  it.each([['/matrix', 'Run all tests', {}], ['/packs/a/matrix', 'Run tests', { pack_id: 'a' }]] as const)('runs the requested pack test scope at %s once', async (path, name, args) => {
    const stub = desk(), queryClient = testQueryClient()
    renderConnected(views(), connected({ client: stub.client }), { path, queryClient })
    fireEvent.click(screen.getByRole('button', { name }))
    await screen.findByText('passed')
    await act(async () => { await queryClient.invalidateQueries() })
    expect(testCalls(stub)).toHaveLength(1)
    expect(testCalls(stub)[0]?.args).toEqual(args)
  })
})
