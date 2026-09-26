import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { bindAgentRun } from './agentTransport'
import type { AgentCallbacks, AgentRequest } from './agent'

const auth = vi.hoisted(() => ({ token: 'a'.repeat(48), listeners: new Set<() => void>() }))
vi.mock('../mcp/session', async importOriginal => ({
  ...await importOriginal<typeof import('../mcp/session')>(),
  sessionBearer: async () => auth.token,
  whenSessionEnds: (fn: () => void) => { auth.listeners.add(fn); return () => auth.listeners.delete(fn) }
}))
vi.mock('../mcp/McpProvider', () => ({ socketProtocols: (id: string) => ['jpack-desk', `jpack-desk-session.${id}`] }))

class Socket {
  static opened: Socket[] = []
  static sent: Record<string, unknown>[] = []
  static respond: (value: Record<string, unknown>, socket: Socket) => void = () => {}
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  bufferedAmount = 0
  closed = false
  constructor(readonly url: URL, readonly protocols: string[]) {
    Socket.opened.push(this)
    queueMicrotask(() => this.onopen?.())
  }
  send(text: string) { const value = JSON.parse(text); Socket.sent.push(value); Socket.respond(value, this) }
  close() { if (!this.closed) { this.closed = true; this.onclose?.() } }
  receive(value: unknown) { this.onmessage?.({ data: JSON.stringify(value) }) }
}
const runID = 'b'.repeat(48)
const callID = 'c'.repeat(48)
const request: AgentRequest = { prompt: 'Prompt', instructions: 'Instructions', phase: 'author', tools: [{ name: 'desk_runtime_0', description: '', inputSchema: { type: 'object' } }] }
const callbacks = (): AgentCallbacks => ({ signal: new AbortController().signal, event: vi.fn(async () => {}), tool: vi.fn(async () => ({ text: 'runtime result', isError: false })) })

beforeEach(() => {
  Socket.opened = []
  Socket.sent = []
  Socket.respond = () => {}
  auth.listeners.clear()
  vi.stubGlobal('WebSocket', Socket)
})
afterEach(() => vi.unstubAllGlobals())

it('binds the selected model and session protocol, round-trips a tool, and drains terminal frames before closing', async () => {
  Socket.respond = (value, socket) => {
    if (value.type === 'start') {
      socket.receive({ type: 'started', runId: runID })
      socket.receive({ type: 'tool-call', runId: runID, call: { id: callID, name: 'desk_runtime_0', arguments: { rehearsal: false } } })
    } else {
      socket.receive({ type: 'event', runId: runID, event: { type: 'message', id: 'd'.repeat(48), text: 'Complete', phase: 'final' } })
      socket.receive({ type: 'end', runId: runID, error: '' })
      socket.close()
    }
  }
  const c = callbacks()
  await bindAgentRun('selected-model')(request, c)
  expect(Socket.opened).toHaveLength(1)
  expect(Socket.opened[0]!.url.pathname).toBe('/api/agent/run')
  expect(String(Socket.opened[0]!.url)).not.toContain(auth.token)
  expect(Socket.opened[0]!.protocols).toEqual(['jpack-desk', `jpack-desk-session.${auth.token}`])
  expect(Socket.sent[0]).toMatchObject({ type: 'start', request: { model: 'selected-model' } })
  expect(Socket.sent[1]).toMatchObject({ type: 'tool-result', runId: runID, callId: callID, answer: { text: 'runtime result' } })
  expect(c.event).toHaveBeenCalledOnce()
  expect(auth.listeners.size).toBe(0)
})

it.each(['cross-run', 'unoffered', 'duplicate', 'malformed', 'native-error'])('refuses %s without retrying or exposing upstream data', async mode => {
  Socket.respond = (value, socket) => {
    if (value.type !== 'start') return
    socket.receive({ type: 'started', runId: runID })
    if (mode === 'native-error') { socket.receive({ type: 'end', runId: runID, error: 'PRIVATE_SENTINEL' }); return }
    if (mode === 'malformed') { socket.onmessage?.({ data: '{' }); return }
    const message = { type: 'tool-call', runId: mode === 'cross-run' ? 'f'.repeat(48) : runID,
      call: { id: callID, name: mode === 'unoffered' ? 'exec_command' : 'desk_runtime_0', arguments: {} } }
    socket.receive(message)
    if (mode === 'duplicate') socket.receive(message)
  }
  const c = callbacks()
  await expect(bindAgentRun('model')(request, c)).rejects.not.toThrow('PRIVATE_SENTINEL')
  expect(Socket.opened).toHaveLength(1)
  expect(c.tool).toHaveBeenCalledTimes(mode === 'duplicate' ? 1 : 0)
  expect(Socket.opened[0]!.closed).toBe(true)
})

it('socket loss aborts an in-flight callback promptly', async () => {
  let called!: () => void
  const entered = new Promise<void>(resolve => { called = resolve })
  Socket.respond = (_value, socket) => {
    socket.receive({ type: 'started', runId: runID })
    socket.receive({ type: 'tool-call', runId: runID, call: { id: callID, name: 'desk_runtime_0', arguments: {} } })
  }
  const c = callbacks()
  c.tool = async (_call, signal) => { called(); return new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Stopped', 'AbortError')), { once: true })) }
  const pending = bindAgentRun('model')(request, c)
  const result = expect(pending).rejects.toThrow('connection ended')
  await entered
  Socket.opened[0]!.close()
  await result
  expect(auth.listeners.size).toBe(0)
})

it('Desk session expiry closes the socket and prevents late callbacks', async () => {
  let ready!: () => void
  const opened = new Promise<void>(resolve => { ready = resolve })
  Socket.respond = () => ready()
  const c = callbacks()
  const pending = bindAgentRun('model')(request, c)
  const ended = expect(pending).rejects.toMatchObject({ name: 'RunCancelled' })
  await opened
  for (const cancel of auth.listeners) cancel()
  await ended
  Socket.opened[0]!.receive({ type: 'tool-call', runId: runID, call: { id: callID, name: 'desk_runtime_0', arguments: {} } })
  expect(c.tool).not.toHaveBeenCalled()
})

it('an already cancelled run never opens a socket', async () => {
  const controller = new AbortController(); controller.abort()
  await expect(bindAgentRun('model')(request, { ...callbacks(), signal: controller.signal })).rejects.toMatchObject({ name: 'RunCancelled' })
  expect(Socket.opened).toHaveLength(0)
})
