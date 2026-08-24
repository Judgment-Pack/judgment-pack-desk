import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js'
import type { JSONRPCMessage } from '@modelcontextprotocol/sdk/types.js'

/**
 * A Transport that carries one JSON-RPC message per WebSocket text frame.
 *
 * The SDK ships a WebSocket transport, but it negotiates the `mcp` subprotocol
 * and the desk chassis speaks plain frames: the framing here is exactly the
 * chassis' own contract, so the transport is written against it directly
 * rather than configured around.
 */
export class DeskWebSocketTransport implements Transport {
  onclose?: () => void
  onerror?: (error: Error) => void
  onmessage?: (message: JSONRPCMessage) => void

  private socket?: WebSocket

  constructor(private readonly url: string) {}

  start(): Promise<void> {
    if (this.socket) {
      return Promise.reject(new Error('DeskWebSocketTransport is already started'))
    }
    return new Promise((resolve, reject) => {
      const socket = new WebSocket(this.url)
      this.socket = socket

      socket.onopen = () => resolve()

      socket.onerror = () => {
        // The browser withholds the reason for a failed handshake, so say what
        // the causes actually are rather than reporting an empty Event.
        const error = new Error(
          `cannot reach the desk chassis at ${this.url} — it may not be running, or the session token may be missing or stale`
        )
        this.onerror?.(error)
        reject(error)
      }

      socket.onclose = () => {
        this.onclose?.()
      }

      socket.onmessage = (event: MessageEvent) => {
        if (typeof event.data !== 'string') return
        let parsed: JSONRPCMessage
        try {
          parsed = JSON.parse(event.data) as JSONRPCMessage
        } catch (cause) {
          this.onerror?.(new Error(`the chassis delivered a frame that is not JSON: ${String(cause)}`))
          return
        }
        this.onmessage?.(parsed)
      }
    })
  }

  send(message: JSONRPCMessage): Promise<void> {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('the desk connection is not open'))
    }
    // JSON.stringify escapes newlines inside strings, so one message is always
    // one line — which is what the chassis' stdio side requires.
    this.socket.send(JSON.stringify(message))
    return Promise.resolve()
  }

  close(): Promise<void> {
    this.socket?.close()
    this.socket = undefined
    return Promise.resolve()
  }
}
