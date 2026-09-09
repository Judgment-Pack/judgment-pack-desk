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

  /**
   * `protocols` is how the session id reaches the chassis on an upgrade.
   *
   * A browser's `WebSocket` constructor takes a URL and a subprotocol list and
   * nothing else — no headers — and the id must not go on the URL, which is the
   * arrangement this desk spent two rounds removing. So the page offers
   * `['jpack-desk', 'jpack-desk-session.<id>']`, the chassis reads the id off
   * the offer, and it answers by selecting the plain `jpack-desk` — so the id
   * is offered and never echoed in a response header.
   */
  constructor(
    private readonly url: string,
    private readonly protocols: string[] = []
  ) {}

  start(): Promise<void> {
    if (this.socket) {
      return Promise.reject(new Error('DeskWebSocketTransport is already started'))
    }
    return new Promise((resolve, reject) => {
      const socket =
        this.protocols.length > 0
          ? new WebSocket(this.url, this.protocols)
          : new WebSocket(this.url)
      this.socket = socket

      socket.onopen = () => resolve()

      socket.onerror = () => {
        // The browser withholds the reason for a failed handshake, so say what
        // the causes actually are rather than reporting an empty Event.
        const error = new Error(
          `cannot reach the desk chassis at ${this.url} — it may not be running, or this page's session may have gone stale`
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
