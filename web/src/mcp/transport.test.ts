import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DeskWebSocketTransport } from './transport'

class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  onopen?: () => void; onclose?: () => void; onerror?: () => void
  onmessage?: (event: { data: string }) => void
  readyState = 0
  close = vi.fn(() => { this.readyState = 3; this.onclose?.() })
  send = vi.fn()
  constructor() { Socket.instances.push(this) }
}
beforeEach(() => { vi.useFakeTimers(); Socket.instances = []; vi.stubGlobal('WebSocket', Socket) })
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

it('rejects a handshake that closes without an error or open event', async () => {
  const transport = new DeskWebSocketTransport('ws://localhost/ws')
  const result = transport.start().catch(error => error)
  Socket.instances[0]!.onclose?.()
  expect(await result).toBeInstanceOf(Error)
  expect(vi.getTimerCount()).toBe(0)
})
it('bounds a handshake that never answers and closes its socket', async () => {
  const transport = new DeskWebSocketTransport('ws://localhost/ws')
  const result = transport.start().catch(error => error)
  await vi.advanceTimersByTimeAsync(10_000)
  expect((await result).message).toContain('timed out')
  expect(Socket.instances[0]!.close).toHaveBeenCalledOnce()
})
it('settles an abandoned connection immediately and leaves no timeout', async () => {
  const transport = new DeskWebSocketTransport('ws://localhost/ws')
  const result = transport.start().catch(error => error)
  await transport.close()
  expect(await result).toBeInstanceOf(Error)
  expect(vi.getTimerCount()).toBe(0)
})
it('does not time out or close a socket once it is open', async () => {
  const transport = new DeskWebSocketTransport('ws://localhost/ws')
  const started = transport.start()
  const socket = Socket.instances[0]!
  socket.readyState = 1; socket.onopen?.()
  await started
  await vi.advanceTimersByTimeAsync(20_000)
  expect(socket.close).not.toHaveBeenCalled()
  await transport.send({ jsonrpc: '2.0', method: 'ping' })
  expect(socket.send).toHaveBeenCalledOnce()
})
