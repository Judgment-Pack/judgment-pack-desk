/**
 * The decision-record panel inside the real `McpProvider` (ADR-0010, section
 * 4): a change to the project, reported by the chassis, and a reconnect each
 * cancel and invalidate the page's queries, and leave this one alone. A run
 * in flight when a file changes is not cancelled, and neither event runs it
 * again. Only the network layer is stood in: the SDK's Client, the socket
 * transport, the session and the tool listing.
 */
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider, useQuery } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { McpProvider } from '../mcp/McpProvider'
import { testQueryClient } from '../testing/harness'
import { AUDIT_KEY, type AuditReport } from './client'
import { DecisionRecord } from './DecisionRecord'

type StandInClient = { fallbackNotificationHandler?: (notification: unknown) => Promise<void>; onclose?: () => void }
const { clients } = vi.hoisted(() => ({ clients: [] as StandInClient[] }))

vi.mock('@modelcontextprotocol/sdk/client/index.js', () => ({
  Client: class {
    fallbackNotificationHandler?: (notification: unknown) => Promise<void>
    onclose?: () => void
    constructor() { clients.push(this) }
    connect() { return Promise.resolve() }
    getServerVersion() { return { name: 'jpack', version: 'stand-in' } }
    close() { return Promise.resolve() }
  }
}))
vi.mock(import('../mcp/transport'), () => ({ DeskWebSocketTransport: class {} as never }))
vi.mock(import('../mcp/session'), async original => ({ ...(await original()), sessionBearer: async () => 'session', whenSessionEnds: () => () => undefined }))
vi.mock(import('../mcp/capabilities'), async original => ({ ...(await original()), listAllTools: async () => [] }))
vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const report: AuditReport = { status: 'valid', lines: 1, bytes: 10, snapshotBetweenWrites: true,
  coverage: { legacyPrefix: 0, chained: 1, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked' }, signedRecords: 0, unsignedRecords: 0,
    checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 1, stamped: { status: 'not-checked' } },
  segments: [{ firstLine: 1, lastLine: 1 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0,
  establishes: ['The chained lines are consistent with one another.'], doesNotEstablish: ['That the trail is complete.'] }
const answer = () => new Response(JSON.stringify({ state: 'report', runtime: '0.26.0', report }), { status: 200, headers: { 'Content-Type': 'application/json' } })

let asked: number
let probed: number
let release: (() => void) | undefined
beforeEach(() => {
  clients.length = 0
  asked = 0
  probed = 0
  vi.mocked(deskFetch).mockImplementation(async url => {
    if (String(url) !== '/api/audit/verify') return new Response('{}', { status: 404 })
    asked++
    // The first run is held in flight until the test lets it go.
    if (asked === 1) await new Promise<void>(resolve => { release = resolve })
    return answer()
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.useRealTimers() })

/** A query that follows the project, as every other on the page does. */
function Probe() {
  const query = useQuery({ queryKey: ['probe'], queryFn: () => ++probed })
  return <p>probe {query.data}</p>
}

describe('the decision-record panel inside McpProvider', () => {
  it('is neither cancelled nor run again by a change to the project, or by a reconnect', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const client = testQueryClient()
    render(<QueryClientProvider client={client}><McpProvider><DecisionRecord /><Probe /></McpProvider></QueryClientProvider>)
    await waitFor(() => expect(clients[0]?.fallbackNotificationHandler).toBeDefined())
    await waitFor(() => expect(asked).toBe(1))
    await screen.findByText('probe 1')

    // A file changes while the panel's run is in flight.
    await act(async () => { await clients[0]!.fallbackNotificationHandler!({ method: 'desk/fileChanged', params: { path: 'packs/a.json' } }) })
    await screen.findByText('probe 2')
    act(() => release!())
    expect(await screen.findByText('Every check the runtime made passed.')).toBeTruthy()
    expect(asked).toBe(1)
    expect(client.getQueryState(AUDIT_KEY)?.isInvalidated).toBe(false)

    // And again once it has answered.
    await act(async () => { await clients[0]!.fallbackNotificationHandler!({ method: 'desk/fileChanged', params: { path: 'packs/a.json' } }) })
    await screen.findByText('probe 3')
    expect(asked).toBe(1)
    expect(client.getQueryState(AUDIT_KEY)?.isInvalidated).toBe(false)

    // The connection drops and comes back.
    act(() => clients[0]!.onclose!())
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    await waitFor(() => expect(clients).toHaveLength(2))
    await screen.findByText('probe 4')
    expect(asked).toBe(1)
    expect(client.getQueryState(AUDIT_KEY)?.isInvalidated).toBe(false)
    expect(screen.getByText('Every check the runtime made passed.')).toBeTruthy()
  })
})
