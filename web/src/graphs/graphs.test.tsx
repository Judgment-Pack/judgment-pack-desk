/**
 * A project's graphs: the findings, the plan and the runtime's labels, as the
 * page shows them (ADR-0011, row 1).
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { connected, renderConnected, stubClient, type ToolHandler } from '../testing/harness'
import { GraphView } from '../routes/GraphView'
import { DetailsSlotContext } from '../shell/DetailsSlot'
import { GraphLabels } from './GraphLabels'
import { readGraphFindings, readGraphPlan, shownMessage, shownPath } from './client'
import type { GraphInventory } from '../mcp/types'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const KIND = 'non-normative-runtime-convention'
const SERVED = 'a'.repeat(64)
const OTHER = 'b'.repeat(64)
const HOME = '/home/secret owner/My Project'

const INVENTORY: GraphInventory = {
  status: 'resolved',
  experimental: true,
  kind: KIND,
  configPath: `${HOME}/jpack.json`,
  configVersion: '2',
  graphs: [{
    id: 'onboarding', graphId: 'vendor-onboarding-flow', graphVersion: '0.1.0', formatVersion: '1', resultNode: 'decision',
    path: `${HOME}/onboarding.graph.json`, rowsPath: `${HOME}/onboarding.rows.json`, rowsDeclared: true,
    detail: `The path "${HOME}/onboarding.graph.json" resolves outside the configuration's own directory, which no configured path may.`
  }]
}
const DOCUMENT = JSON.stringify({ formatVersion: '1', id: 'vendor-onboarding-flow', version: '0.1.0', nodes: { screening: { pack: 'sanctions-screening' }, decision: { pack: 'vendor-onboarding' } }, edges: [{ from: 'screening', to: 'decision', fact: '/a/b' }], result: 'decision' })
const META = { status: 'valid', experimental: true, kind: KIND, id: 'onboarding', graphId: 'vendor-onboarding-flow', graphVersion: '0.1.0', formatVersion: '1', path: `${HOME}/onboarding.graph.json`, bytes: DOCUMENT.length, sha256: SERVED }

const findings = (graphSha256: string) => ({
  outputVersion: '2', command: 'experimental graph validate', status: 'invalid', kind: KIND, formatVersion: '1', configPath: 'jpack.json', configVersion: '2',
  summary: { total: 1, passed: 0, failed: 1 },
  graphs: [{ id: 'onboarding', path: 'onboarding.graph.json', graphSha256, status: 'invalid', diagnostics: [{ code: 'JPS-GRAPH-EDGE', severity: 'error', instancePath: '/edges/0', message: 'The edge names a node the graph does not declare.' }] }]
})
const plan = {
  outputVersion: '2', command: 'experimental graph explain', status: 'planned', kind: KIND, formatVersion: '1', configPath: 'jpack.json', graphPath: 'onboarding.graph.json', graphId: 'vendor-onboarding-flow', graphVersion: '0.1.0', resultNode: 'decision',
  steps: [
    { order: 1, node: 'screening', pack: 'sanctions-screening', path: 'sanctions-screening-0.1.0.pack.json', packId: 'https://example.invalid/judgment-packs/sanctions-screening', packVersion: '0.1.0', feeds: [] },
    { order: 2, node: 'decision', pack: 'vendor-onboarding', path: `${HOME}/vendor.pack.json`, packId: 'https://example.invalid/judgment-packs/vendor-onboarding', packVersion: '0.1.0', detail: `The file "${HOME}/vendor.pack.json" could not be read`, feeds: [{ from: 'screening', fact: '/screening/status' }, { from: 'screening', evidence: 'screening-outcome', onUnresolved: 'unknown' }] }
  ]
}

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let validateAnswer: () => Response
let planAnswer: () => Response
beforeEach(() => {
  validateAnswer = () => json(200, { answer: findings(SERVED) })
  planAnswer = () => json(200, { id: 'onboarding', answer: plan })
  vi.mocked(deskFetch).mockImplementation(async url => String(url).startsWith('/api/graphs/plan') ? planAnswer() : validateAnswer())
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function stub(overrides: Record<string, ToolHandler> = {}) {
  return stubClient({
    experimental_list_graphs: () => ({ text: JSON.stringify(INVENTORY) }),
    experimental_get_graph: () => ({ text: DOCUMENT, structured: META }),
    experimental_test_graphs: () => ({ text: JSON.stringify({ status: 'passed', summary: { total: 0, passed: 0, mismatched: 0 }, graphs: [] }) }),
    ...overrides
  }).client
}
const routes = <Routes><Route path="/graphs" element={<GraphView />} /><Route path="/graphs/:graphId" element={<GraphView />} /></Routes>
const open = (path: string) => renderConnected(routes, connected({ client: stub(), graphDocumentSupported: true, graphInventorySupported: true }), { path })
const asked = () => vi.mocked(deskFetch).mock.calls.map(call => String(call[0]))

describe('a path, as the page shows it', () => {
  it('shows a relative path as given and a path from a root as nothing but …', () => {
    expect(shownPath('onboarding.graph.json')).toBe('onboarding.graph.json')
    expect(shownPath('sub dir/a b.graph.json')).toBe('sub dir/a b.graph.json')
    expect(shownPath('../outside.graph.json')).toBe('../outside.graph.json')
    for (const path of ['/home/x/g.json', '/home/x y\t\u2028z/g.json', 'C:\\Users\\x\\g.json', 'D:/x/g.json', '\\\\host\\share\\g.json']) expect(shownPath(path)).toBe('…')
    expect(shownPath('x /home/y/g.json')).toBe('…')
  })
  it('takes a path named in a sentence whole when a path member gave it, spaces and all', () => {
    const path = '/home/secret owner/My\tProject\u2028 X/g.json'
    expect(shownMessage(`The path "${path}" resolves outside.`, [path])).toBe('The path "…" resolves outside.')
    // As the runtime prints a control character or a separator.
    expect(shownMessage(`The path "/home/secret owner/My?Project? X/g.json" resolves outside.`, [path])).toBe('The path "…" resolves outside.')
    expect(shownMessage('read /home/alone/g.json now')).toBe('read … now')
    expect(shownMessage('a fact at /screening/status is not a path here', [])).not.toContain('/home')
    expect(shownMessage('The file "a.pack.json" could not be read')).toBe('The file "a.pack.json" could not be read')
  })
})

describe('the client', () => {
  it('names a graph by its id, encoded, and never by a path', async () => {
    vi.mocked(deskFetch).mockResolvedValueOnce(json(200, { id: 'a b/c', answer: plan }))
    await readGraphPlan('a b/c')
    expect(asked()).toEqual(['/api/graphs/plan?id=a%20b%2Fc'])
  })
  it("passes a refusal's sentence on and refuses an answer that is not the command's", async () => {
    vi.mocked(deskFetch).mockResolvedValueOnce(json(404, { error: 'The project’s configuration declares no graph with that id.', code: 'bad-request' }))
    await expect(readGraphPlan('nope')).rejects.toThrow('declares no graph with that id')
    vi.mocked(deskFetch).mockResolvedValueOnce(json(200, { answer: { command: 'experimental graph list', status: 'resolved' } }))
    await expect(readGraphFindings()).rejects.toThrow('could not be asked')
  })
})

describe('the graphs page: labels, findings and paths', () => {
  it('shows the inventory’s kind and experimental beside Desk’s one word, and the findings in the runtime’s words', async () => {
    const { container } = open('/graphs')
    await screen.findByText('The edge names a node the graph does not declare.')
    expect(screen.getAllByText('Experimental').length).toBeGreaterThan(0)
    const kinds = [...container.querySelectorAll('code[lang="en"]')].map(code => code.textContent)
    expect(kinds).toContain(KIND)
    expect(kinds).toContain('true')
    expect(container.textContent).toContain('JPS-GRAPH-EDGE')
    expect(container.textContent).toContain('/edges/0')
    expect(asked()).toEqual(['/api/graphs/findings'])
    // The status is the runtime’s word, in no verdict colour of Desk’s.
    expect(container.querySelector('.pill-success')).toBeNull()
  })
  it('lets no path from a root reach the page from the inventory', async () => {
    const { container } = open('/graphs')
    await screen.findByText('The edge names a node the graph does not declare.')
    fireEvent.click(await screen.findByText('Technical details'))
    expect(container.textContent).not.toContain('secret owner')
    expect(container.textContent).not.toContain('/home/')
    expect(container.textContent).toContain('resolves outside the configuration')
  })
  it('says nothing was checked when the runtime could not be asked, and keeps the listing', async () => {
    validateAnswer = () => json(500, { error: 'The graphs could not be checked: the runtime did not finish experimental graph validate --config jpack.json --format json within 20s.' })
    open('/graphs')
    await screen.findByText(/did not finish experimental graph validate/)
    expect(screen.getByRole('link', { name: 'onboarding' })).toBeTruthy()
  })
  it('shows a project without graphs as the runtime’s skipped answer', async () => {
    validateAnswer = () => json(200, { answer: { outputVersion: '2', command: 'experimental graph validate', status: 'skipped', kind: KIND, summary: { total: 0, passed: 0, failed: 0 }, graphs: [] } })
    open('/graphs')
    await screen.findByText('skipped')
    expect(screen.getByText(/this project declares no graph/)).toBeTruthy()
  })
})

describe('the diagram’s source details', () => {
  it('shows the document’s path as nothing from a root', async () => {
    const target = document.body.appendChild(document.createElement('div'))
    const slot = { target, open: true, claim: () => () => {}, reveal: () => {} }
    const { container } = renderConnected(<DetailsSlotContext.Provider value={slot}>{routes}</DetailsSlotContext.Provider>, connected({ client: stub(), graphDocumentSupported: true, graphInventorySupported: true }), { path: '/graphs/onboarding' })
    fireEvent.click(await screen.findByText('screening → decision'))
    await screen.findByText('Source details')
    expect(target.textContent).toContain('…')
    expect(target.textContent).not.toContain('secret owner')
    expect(target.textContent).not.toContain('/home/')
    expect(container.textContent).not.toContain('/home/')
    target.remove()
  })
})

describe('a graph’s Plan view', () => {
  it('asks the plan only when the owner opens it, by id', async () => {
    const { container } = open('/graphs/onboarding')
    await waitFor(() => expect(container.querySelector('svg, .diagram, [aria-label="Graph diagram"]')).not.toBeNull())
    expect(asked()).toEqual([])
    fireEvent.click(screen.getByRole('link', { name: 'Plan' }))
    await screen.findByRole('heading', { name: /screening/, level: 3 })
    expect(asked()).toContain('/api/graphs/plan?id=onboarding')
  })
  it('shows the plan as the runtime planned it, with the runtime’s labels and no path from a root', async () => {
    const { container } = open('/graphs/onboarding?view=plan')
    await screen.findByRole('heading', { name: /decision/, level: 3 })
    expect(container.textContent).toContain('planned')
    expect(container.textContent).toContain('/screening/status')
    expect(container.textContent).toContain('screening-outcome')
    expect(container.textContent).toContain('unknown')
    expect([...container.querySelectorAll('code[lang="en"]')].map(code => code.textContent)).toContain(KIND)
    expect(container.textContent).toContain('sanctions-screening-0.1.0.pack.json')
    expect(container.textContent).not.toContain('secret owner')
    expect(container.textContent).not.toContain('/home/')
    expect(container.textContent).toContain('could not be read')
    expect(container.textContent).toContain('names no digest')
  })
  it('compares the findings’ digest with the document’s, and says so when they differ', async () => {
    validateAnswer = () => json(200, { answer: findings(OTHER) })
    open('/graphs/onboarding?view=plan')
    await screen.findByText(/about another revision of the graph file/)
    cleanup()
    validateAnswer = () => json(200, { answer: findings(SERVED) })
    open('/graphs/onboarding?view=plan')
    await screen.findByText('The edge names a node the graph does not declare.')
    expect(screen.queryByText(/about another revision of the graph file/)).toBeNull()
  })
  it('shows the runtime’s own answer for a graph it has no plan for, and points to the findings', async () => {
    planAnswer = () => json(200, { id: 'onboarding', answer: { outputVersion: '2', command: 'experimental graph explain', status: 'error', diagnostics: [{ code: 'JPS-INPUT-READ', message: 'The graph document could not be read as one bounded regular file or standard input stream.' }] } })
    open('/graphs/onboarding?view=plan')
    await screen.findByText('The graph document could not be read as one bounded regular file or standard input stream.')
    expect(screen.getByText(/The runtime has no plan for this graph/)).toBeTruthy()
  })
  it('says why when Desk asked for no plan', async () => {
    planAnswer = () => json(409, { error: 'This graph is declared at a path that is not inside the project’s folder, so Desk asks for no plan.' })
    open('/graphs/onboarding?view=plan')
    await screen.findByText(/asks for no plan/)
  })
})

describe('the runtime’s labels', () => {
  it('shows each member as given, marks it as the runtime’s English, and states the claim reference as a locator', () => {
    const label = 'graph matrix results: rows a project wrote about its own graph, run through this runtime’s experimental composition'
    const { container } = render(<GraphLabels labels={{ kind: KIND, experimental: true, label, rehearsal: true, conformanceClaimReference: 'CONFORMANCE.md' }} />)
    const english = [...container.querySelectorAll('[lang="en"]')].map(node => node.textContent)
    expect(english).toEqual(expect.arrayContaining([KIND, 'true', label, 'CONFORMANCE.md']))
    expect(within(container).getByText('Experimental')).toBeTruthy()
    expect(container.textContent).toContain('a locator for the repository file that makes the claim. This payload makes none')
    for (const word of [/verified/i, /proof/i, /trusted/i]) expect(container.textContent).not.toMatch(word)
  })
  it('shows nothing of a payload that carries none', () => {
    const { container } = render(<GraphLabels labels={{}} />)
    expect(container.textContent).toBe('')
  })
})
