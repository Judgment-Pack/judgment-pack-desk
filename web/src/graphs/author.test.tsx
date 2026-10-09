import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { GraphAuthor, GraphConfirmation } from './GraphAuthor'
import { graphHostTools, type GraphOffer } from './author'
import { deskFetch, readFile } from '../files/client'
import { GRAPH_SYSTEM } from '../assistant/engines/contract'

const state = vi.hoisted(() => ({
  names: ['author_graph'], ready: true,
  events: [] as Array<{ type: string; document?: unknown; unknowns?: string[] }>,
  start: vi.fn(), stop: vi.fn(), getPrompt: vi.fn(), options: vi.fn(), invalidate: vi.fn()
}))
vi.mock('../mcp/prompts', () => ({ AUTHOR_GRAPH_PROMPT: 'author_graph', usePromptNames: () => ({ data: state.names }) }))
vi.mock('../mcp/McpProvider', () => ({ useMcp: () => ({ client: { getPrompt: state.getPrompt } }) }))
vi.mock('../assistant/useAssistantSlot', () => ({
  useAssistantSlot: () => ({ endpoint: { model: 'chosen-model' }, engine: 'vercel', thinking: 'off' }),
  assistantReady: () => state.ready
}))
vi.mock('../assistant/useAssistantRun', () => ({ useAssistantRun: (options: unknown) => {
  state.options(options)
  return { status: 'finished', events: state.events, start: state.start, stop: state.stop }
} }))
vi.mock('../files/queries', () => ({ useFileContent: () => ({ data: { content: '{"configVersion":"2","packs":{"alpha":{"path":"alpha.json"},"beta":{"path":"beta.json"}},"graphs":{"draft":{"path":"draft.graph.json"}}}' } }) }))
vi.mock('@tanstack/react-query', async original => ({ ...await original(), useQueryClient: () => ({ invalidateQueries: state.invalidate }) }))
vi.mock('../files/client', async original => ({ ...await original(), deskFetch: vi.fn(), readFile: vi.fn() }))
const graph = { id: 'graph-document', nodes: [], edges: [] }
const content = JSON.stringify(graph, null, 2) + '\n'
const offer: GraphOffer = {
  id: 'draft', path: 'draft.graph.json', content, description: 'description',
  before: '{"configVersion":"2","packs":{}}\n',
  configContent: '{"configVersion":"2","packs":{},"graphs":{"draft":{"path":"draft.graph.json","description":"description"}}}\n',
  configSha256: 'a'.repeat(64), token: 'b'.repeat(64), nonce: 'nonce', hasLock: true,
  findings: '{ "kind":"non-normative-runtime-convention", "status":"valid", "graphSha256":"digest", "label":"runtime findings sentence" }',
  plan: '{ "kind":"non-normative-runtime-convention", "status":"planned", "label":"runtime plan sentence" }'
}
beforeEach(() => {
  vi.clearAllMocks(); state.names = ['author_graph']; state.ready = true; state.events = []
  state.getPrompt.mockResolvedValue({ messages: [{ content: { type: 'text', text: 'Runtime author_graph prompt. Step 6: write rows.' } }] })
  vi.mocked(readFile).mockImplementation(async path => ({ path, content: path === 'draft.graph.json' ? '{}' : '{ "id":"' + path + '" }', sha256: 'c'.repeat(64), bytes: 2 }))
  vi.mocked(deskFetch).mockImplementation(async route => new Response(JSON.stringify(String(route).endsWith('/proposal') ? offer : { graphWritten: true, declared: true }), { status: 200 }))
})
afterEach(cleanup)

it('offers authoring only with the advertised prompt and a configured assistant', () => {
  state.names = []
  const view = render(<GraphAuthor />)
  expect(screen.queryByRole('button', { name: 'Author graph' })).toBeNull()
  expect(screen.getByText('This runtime advertises no author_graph prompt.')).toBeTruthy()
  state.names = ['author_graph']; state.ready = false; view.rerender(<GraphAuthor />)
  expect(screen.queryByRole('button', { name: 'Author graph' })).toBeNull()
  expect(screen.getByText('Configure an assistant and choose a model to author a graph.')).toBeTruthy()
})

it('fetches author_graph with exactly the chosen packs read through the file API and the relationship', async () => {
  render(<GraphAuthor />)
  fireEvent.click(screen.getByRole('button', { name: 'Author graph' }))
  fireEvent.click(screen.getByLabelText('alpha'))
  fireEvent.change(screen.getByLabelText('Relationship'), { target: { value: 'Alpha feeds the next decision.' } })
  fireEvent.click(screen.getByRole('button', { name: 'Propose graph' }))
  await waitFor(() => expect(state.start).toHaveBeenCalledTimes(1))
  expect(readFile).toHaveBeenCalledTimes(1)
  expect(state.getPrompt).toHaveBeenCalledWith({ name: 'author_graph', arguments: { relationship: 'Alpha feeds the next decision.', packs: '[{ "id":"alpha.json" }]' } })
  expect(state.start).toHaveBeenCalledWith('Runtime author_graph prompt. Step 6: write rows.')
  expect(state.options).toHaveBeenLastCalledWith(expect.objectContaining({ purpose: 'graph', hostTools: expect.arrayContaining([expect.objectContaining({ name: 'graph_validate' }), expect.objectContaining({ name: 'graph_explain' })]) }))
  expect(deskFetch).not.toHaveBeenCalled()
  expect(screen.getByText('Runtime author_graph prompt. Step 6: write rows.').getAttribute('lang')).toBe('en')
})

async function proposed() {
  const view = render(<GraphAuthor />)
  fireEvent.click(screen.getByRole('button', { name: 'Author graph' }))
  state.events = [{ type: 'proposal', document: { graph, id: 'draft', description: 'description' }, unknowns: ['Check the relationship.'] }]
  view.rerender(<GraphAuthor />)
  await waitFor(() => expect(screen.getByLabelText('Graph bytes to write')).toHaveProperty('value', content))
  fireEvent.change(screen.getByLabelText('Graph path'), { target: { value: 'draft.graph.json' } })
  expect(deskFetch).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Review graph write' }))
  await screen.findByRole('button', { name: 'Confirm graph write' })
  return view
}

it('shows the entire declaration, its diff and runtime answers before one confirmation writes exactly that offer', async () => {
  await proposed()
  expect(deskFetch).toHaveBeenCalledTimes(1)
  expect(vi.mocked(deskFetch).mock.calls[0]![0]).toBe('/api/graphs/proposal')
  expect(JSON.parse(vi.mocked(deskFetch).mock.calls[0]![1]!.body as string)).toEqual({ id: 'draft', path: 'draft.graph.json', content, description: 'description', baseSha256: '' })
  expect(screen.getByText(offer.configContent.trim())).toBeTruthy()
  expect(screen.getByText(offer.before.trim())).toBeTruthy()
  expect(screen.getByRole('region', { name: 'The proposal as a diff' })).toBeTruthy()
  expect(screen.getByText(offer.findings)).toHaveProperty('lang', 'en')
  expect(screen.getByText(offer.plan)).toHaveProperty('lang', 'en')
  expect(screen.getByText('runtime findings sentence')).toHaveProperty('lang', 'en')
  expect(screen.getByText(/A change to jpack.json holds every pack/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Confirm graph write' }))
  await screen.findByText('The graph was written. Review and lock updates the reviewed set.')
  expect(deskFetch).toHaveBeenCalledTimes(2)
  const [route, init] = vi.mocked(deskFetch).mock.calls[1]!
  expect(route).toBe('/api/graphs/write')
  expect(JSON.parse(init!.body as string)).toEqual(offer)
  expect(init!.body).not.toContain('override')
  expect(screen.queryByRole('button', { name: 'Confirm graph write' })).toBeNull()
})

it('withdraws confirmation when the owner changes the graph bytes', async () => {
  await proposed()
  fireEvent.change(screen.getByLabelText('Graph bytes to write'), { target: { value: content + ' ' } })
  expect(screen.queryByRole('button', { name: 'Confirm graph write' })).toBeNull()
  expect(deskFetch).toHaveBeenCalledTimes(1)
})

it('shows the graph-only lock sentence on edits and no declaration write', () => {
  render(<GraphConfirmation offer={{ ...offer, baseSha256: 'c'.repeat(64) }} busy={false} onConfirm={vi.fn()} />)
  expect(screen.getByText('Deciding runs of this graph are refused as document-drift until the next Review and lock.')).toBeTruthy()
  expect(screen.queryByText('Whole jpack.json to write')).toBeNull()
  expect(screen.queryByText(/A change to jpack.json holds every pack/)).toBeNull()
})

it('gives the engine only two read-only host tools, passing exact content and retaining runtime bytes', async () => {
  const tools = graphHostTools()
  expect(tools.map(tool => tool.name)).toEqual(['graph_validate', 'graph_explain'])
  const raw = '{"answer":{ "status":"invalid", "kind":"non-normative-runtime-convention", "number":1.0 }}'
  vi.mocked(deskFetch).mockResolvedValue(new Response(raw))
  for (const [index, tool] of tools.entries()) {
    vi.mocked(deskFetch).mockResolvedValueOnce(new Response(raw))
    const result = await tool.execute({ content }, new AbortController().signal)
    expect(result.content?.[0]?.text).toBe(raw)
    const [route, init] = vi.mocked(deskFetch).mock.calls[index]!
    expect(route).toBe(index === 0 ? '/api/graphs/validate' : '/api/graphs/explain')
    expect(JSON.parse(init!.body as string)).toEqual({ content })
  }
  expect(GRAPH_SYSTEM).toContain('proposal is your only output')
  expect(GRAPH_SYSTEM).toContain('never write files or change configuration or its lock')
  expect(GRAPH_SYSTEM).toContain('propose no rows file or rows declaration')
})
