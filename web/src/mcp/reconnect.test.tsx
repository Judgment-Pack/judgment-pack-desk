import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { McpProvider, useMcp } from './McpProvider'
import { sessionEnded } from './session'

const fixture = vi.hoisted(() => ({ clients: [] as any[] }))
vi.mock('@modelcontextprotocol/sdk/client/index.js', () => ({
  Client: class {
    onclose?: () => void
    resolve!: () => void
    reject!: (error: Error) => void
    connect = vi.fn(() => new Promise<void>((resolve, reject) => { this.resolve = resolve; this.reject = reject }))
    close = vi.fn(async () => this.onclose?.())
    getServerVersion = () => ({name:'fixture',version:'1'})
    listTools = vi.fn(async () => ({tools:[]}))
    constructor() { fixture.clients.push(this) }
  }
}))
function Probe() {
  const connection = useMcp()
  return <><output>{connection.status}:{connection.attempt}:{connection.connectionEpoch}</output><button onClick={connection.retryNow}>Retry</button></>
}
let queries: QueryClient
beforeEach(() => {
  vi.useFakeTimers(); fixture.clients=[]
  vi.stubGlobal('fetch',vi.fn(async () => Response.json({subject:'local user',issuer:null})))
  queries=new QueryClient({defaultOptions:{queries:{retry:false}}})
})
afterEach(() => { cleanup(); queries.clear(); vi.useRealTimers(); vi.unstubAllGlobals() })
async function start() {
  render(<QueryClientProvider client={queries}><McpProvider><Probe/></McpProvider></QueryClientProvider>)
  await act(async () => {})
}
it('classifies a socket closed during initialize and schedules only one retry', async () => {
  await start()
  const client=fixture.clients[0]
  await act(async () => { client.onclose(); client.reject(new Error('closed')) })
  expect(fetch).toHaveBeenCalledOnce()
  expect(screen.getByText('reconnecting:1:0')).toBeTruthy()
  await act(async () => { await vi.advanceTimersByTimeAsync(500) })
  expect(fixture.clients).toHaveLength(2)
})
it('ends an expired session instead of looping after a pre-initialize close', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response('',{status:401,headers:{'X-Jpack-Desk-Refusal':'unauthorized'}}))
  await start()
  await act(async () => { fixture.clients[0].onclose() })
  expect(sessionEnded()).not.toBeNull()
  expect(screen.getByText('failed:0:0')).toBeTruthy()
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(fixture.clients).toHaveLength(1)
})
it('refreshes project queries after a manual reconnect and ignores the old attempt', async () => {
  const invalidate=vi.spyOn(queries,'invalidateQueries')
  await start()
  await act(async () => { fixture.clients[0].resolve() })
  expect(screen.getByText('ready:0:1')).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Retry'}))
  await act(async () => {})
  await act(async () => { fixture.clients[0].onclose(); fixture.clients[1].resolve() })
  expect(screen.getByText('ready:0:2')).toBeTruthy()
  expect(invalidate).toHaveBeenCalledOnce()
})
