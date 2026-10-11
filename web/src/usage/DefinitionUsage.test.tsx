import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DefinitionUsage } from './DefinitionUsage'
import { jobsAPI, type Job, type Release } from '../jobs/client'
import { connected, renderConnected, stubClient } from '../testing/harness'
import { readBatch, readJobUsagePage } from './model'

vi.mock('../jobs/client', async original => ({ ...await original<typeof import('../jobs/client')>(), jobsAPI: vi.fn() }))
vi.mock('../chat/ChatProvider', () => ({ useChats: () => ({ store: {}, ready: true, chats: [], drafts: [], error: '' }) }))
const job = (id: string) => ({ id, name: id, releaseId: `release-${id}`, revision: 1, createdAt: '2026-10-10', recentRuns: [{ id: 'r1', state: 'completed', createdAt: '2026-10-09T00:00:00Z' }] }) as Job
const release = (id: string, packId = 'urn:pack:example') => ({ id: `release-${id}`, packId, packVersion: '0.1.0', packDigest: 'sha256:old' }) as Release
function draw({ draft = false, broken = false, incomplete = false } = {}) {
  const runtime = stubClient({
    list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [{ id: 'configured-key', packId: 'urn:pack:example', packVersion: '0.2.0' }] }) }),
    experimental_list_graphs: () => ({ text: JSON.stringify({ status: incomplete ? 'unavailable' : 'resolved', graphs: [{ id: 'launch' }, { id: 'different' }, ...(broken ? [{ id: 'broken' }] : [])] }) }),
    experimental_get_graph: args => ({ structured: { status: args.graph_id === 'broken' ? 'undecodable' : 'valid' }, text: JSON.stringify({ version: '1', nodes: { eligibility: { pack: args.graph_id === 'different' ? 'urn:pack:example' : 'configured-key' } }, edges: [] }) })
  })
  renderConnected(<DefinitionUsage kind="pack" draft={draft} configuredKey={draft ? undefined : 'configured-key'} documentId="urn:pack:example"/>, connected({ client: runtime.client, graphInventorySupported: true, graphDocumentSupported: true }))
  return runtime
}
beforeEach(() => {
  vi.mocked(jobsAPI).mockImplementation(async path => {
    if (path === 'jobs?after=0') return { items: [job('nightly'), job('unrelated')], next: 0 }
    const id = path.split('/')[1]
    return { job: job(id), release: release(id, id === 'unrelated' ? 'configured-key' : undefined) }
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

it('joins graph keys and frozen job identities separately, preserves versions, and opens details below the list', async () => {
  const runtime = draw({ draft: true })
  const graph = await screen.findByRole('button', { name: 'launch' })
  await screen.findByRole('button', { name: 'nightly' })
  expect(screen.queryByRole('button', { name: 'different' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'unrelated' })).toBeNull()
  expect(screen.getByText('References to saved versions of this pack. Graphs and job releases do not use this draft.')).toBeTruthy()
  expect(screen.getByText('0.2.0')).toBeTruthy()
  expect(screen.getByText('0.1.0')).toBeTruthy()
  fireEvent.click(graph)
  const details = screen.getByRole('region', { name: 'Usage details' })
  expect(within(details).getByText('eligibility')).toBeTruthy()
  expect(within(details).getByRole('link', { name: 'Open graph' }).getAttribute('href')).toBe('/graphs/launch?view=overview')
  fireEvent.click(screen.getByRole('button', { name: 'Close details' }))
  expect(document.activeElement).toBe(graph)
  fireEvent.click(screen.getByRole('button', { name: 'nightly' }))
  expect(screen.getByText('sha256:old')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Open job' }).getAttribute('href')).toBe('/jobs/nightly')
  expect(runtime.calls.every(call => ['list_packs', 'experimental_list_graphs', 'experimental_get_graph'].includes(call.name))).toBe(true)
  expect(vi.mocked(jobsAPI).mock.calls.every(([, body]) => body === undefined)).toBe(true)
})

it('retains a truthful partial state and can find a matching job on a later runner page', async () => {
  vi.mocked(jobsAPI).mockImplementation(async path => {
    if (path === 'jobs?after=0') return { items: [job('unrelated')], next: 10 }
    if (path === 'jobs?after=10') return { items: [job('later')], next: 0 }
    const id = path.split('/')[1]
    return { job: job(id), release: release(id, id === 'unrelated' ? 'different' : undefined) }
  })
  draw()
  fireEvent.click(screen.getByRole('radio', { name: 'Jobs' }))
  await screen.findByRole('button', { name: 'Load more jobs' })
  expect(screen.queryByText('No usage found.')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Load more jobs' }))
  await screen.findByRole('button', { name: 'later' })
  expect(screen.queryByRole('button', { name: 'Load more jobs' })).toBeNull()
  expect(screen.getByText('1 item')).toBeTruthy()
})

it('does not turn unreadable graph or job details into a claim of no usage', async () => {
  vi.mocked(jobsAPI).mockImplementation(async path => {
    if (path === 'jobs?after=0') return { items: [job('missing')], next: 0 }
    throw Error('Unavailable')
  })
  draw({ broken: true })
  await screen.findByText('1 job releases could not be read. Usage is incomplete.')
  await screen.findByText('1 graphs could not be read. Usage is incomplete.')
  expect(screen.queryByRole('button', { name: 'broken' })).toBeNull()
  expect(screen.queryByText('No usage found.')).toBeNull()
  expect(screen.getByText('1 loaded item')).toBeTruthy()
})

it('shows graph job capability honestly without executing or fetching jobs', () => {
  renderConnected(<DefinitionUsage kind="graph"/>, connected())
  expect(screen.getByText('Jobs currently run saved pack releases. Running a graph as a job is not supported yet.')).toBeTruthy()
  expect(jobsAPI).not.toHaveBeenCalled()
  expect(screen.queryByText('No usage found.')).toBeNull()
})

it('rejects mismatched release joins and non-advancing cursors', async () => {
  const read = vi.fn().mockResolvedValueOnce({ items: [job('a')], next: 0 }).mockResolvedValueOnce({ job: job('a'), release: release('other') })
  expect(await readJobUsagePage(read, 0, 'urn:pack:example')).toMatchObject({ rows: [], unread: 1, next: 0 })
  await expect(readJobUsagePage(vi.fn().mockResolvedValue({ items: [], next: 10 }), 10, 'urn:pack:example')).rejects.toThrow('invalid cursor')
})

it('limits concurrent reads and stops scheduling after cancellation', async () => {
  const abort = new AbortController()
  let concurrent = 0, peak = 0
  const reads = vi.fn(async (id: number) => {
    peak = Math.max(peak, ++concurrent)
    await new Promise(resolve => setTimeout(resolve, 1))
    concurrent--
    return id
  })
  const results = await readBatch(Array.from({ length: 12 }, (_, i) => i), reads)
  expect(peak).toBe(4)
  expect(results).toHaveLength(12)
  abort.abort(Error('Cancelled'))
  reads.mockClear()
  await expect(readBatch([1, 2], reads, abort.signal)).rejects.toThrow('Cancelled')
  expect(reads).not.toHaveBeenCalled()
})

it('does not read graphs or claim a complete inventory when the runtime did not resolve it', async () => {
  const runtime = draw({ incomplete: true })
  await screen.findByText('Graph inventory is incomplete. Usage cannot be confirmed.')
  await screen.findByRole('button', { name: 'nightly' })
  expect(runtime.calls.some(call => call.name === 'experimental_get_graph')).toBe(false)
  expect(screen.queryByText('Loading usage…')).toBeNull()
  expect(screen.getByText('1 loaded item')).toBeTruthy()
})
