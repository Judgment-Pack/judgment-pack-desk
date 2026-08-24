import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import type { Notification } from '@modelcontextprotocol/sdk/types.js'
import { useQueryClient } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { DeskWebSocketTransport } from './transport'

export type ConnectionStatus = 'connecting' | 'ready' | 'reconnecting' | 'failed'

export interface McpConnection {
  client: Client | null
  status: ConnectionStatus
  error: Error | null
  /** The runtime that answered initialize, for the status bar. */
  server: { name: string; version: string } | null
  /** Consecutive failed attempts; 0 while connected. */
  attempt: number
  /**
   * True when the connected runtime's experimental_evaluate advertises the
   * boolean rehearsal argument (ADR-0028, jpack >= 0.18.0). Read from the
   * tool's own declared schema at connect time — a capability is what the
   * server says it accepts, never what a version string implies.
   */
  rehearsalSupported: boolean
  /** True once this page has connected at least once. */
  everConnected: boolean
  /** Abandon the current backoff and try again now. */
  retryNow: () => void
}

const McpContext = createContext<McpConnection>({
  client: null,
  status: 'connecting',
  error: null,
  server: null,
  rehearsalSupported: false,
  attempt: 0,
  everConnected: false,
  retryNow: () => {}
})

export function useMcp(): McpConnection {
  return useContext(McpContext)
}

/**
 * The session token arrives in the URL the chassis prints. Keeping it in
 * sessionStorage lets client-side navigation drop it from the address bar
 * without losing the connection, and scopes it to this tab.
 */
const TOKEN_KEY = 'jpack-desk-token'

function sessionToken(): string {
  const fromUrl = new URLSearchParams(window.location.search).get('token')
  if (fromUrl) {
    window.sessionStorage.setItem(TOKEN_KEY, fromUrl)
    return fromUrl
  }
  return window.sessionStorage.getItem(TOKEN_KEY) ?? ''
}

function socketURL(token: string): string {
  const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${window.location.host}/ws?token=${encodeURIComponent(token)}`
}

/** The backoff schedule: doubling from the base, never longer than the cap. */
const BACKOFF_BASE_MS = 500
const BACKOFF_CAP_MS = 15_000

/**
 * Delay before attempt n (1-based). The cap bounds the wait; the jitter keeps
 * several open tabs from all knocking at the same instant.
 */
function backoffDelay(attempt: number): number {
  const capped = Math.min(BACKOFF_CAP_MS, BACKOFF_BASE_MS * 2 ** (attempt - 1))
  return Math.round(capped / 2 + Math.random() * (capped / 2))
}

/**
 * McpProvider makes the browser the MCP client: it connects the official SDK
 * Client over the chassis relay, runs initialize once, and hands the connected
 * client to the views. There is no desk-specific API in between.
 *
 * A dropped socket is reconnected rather than reported and left. The chassis is
 * a local process a user restarts, and a desk that needs a page reload after
 * every restart is a desk that lies about being live. Each attempt builds a
 * fresh Client and a fresh transport — an SDK Client that has closed already
 * negotiated with a server that is gone — and the delay between attempts
 * doubles up to a cap. A reconnect invalidates every query: the runtime
 * re-reads the project on every call, and whatever the project did while the
 * socket was down arrived as `desk/fileChanged` notifications nobody heard.
 */
export function McpProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [connection, setConnection] = useState<McpConnection>({
    client: null,
    status: 'connecting',
    error: null,
    server: null,
    rehearsalSupported: false,
    attempt: 0,
    everConnected: false,
    retryNow: () => {}
  })
  // Bumping this re-runs the effect, which is what "try again now" means: the
  // effect owns every socket, timer, and Client, so restarting it is the one
  // way to retry that cannot leave a second connection behind.
  const [retryTick, setRetryTick] = useState(0)
  // Survives those restarts, so a manual retry does not tell the views this
  // page has never been connected.
  const everConnected = useRef(false)

  useEffect(() => {
    let disposed = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    // The Client of the attempt in flight or connected. A late event from an
    // attempt this one replaced is ignored by comparing against it.
    let live: Client | null = null

    const retryNow = () => setRetryTick((tick) => tick + 1)

    const token = sessionToken()
    if (!token) {
      // Nothing to retry: no token will appear on its own.
      setConnection({
        client: null,
        status: 'failed',
        error: new Error(
          'No session token. Open the URL that jpack-desk printed at startup — it carries ?token=…'
        ),
        server: null,
        rehearsalSupported: false,
        attempt: 0,
        everConnected: everConnected.current,
        retryNow
      })
      return
    }

    const scheduleRetry = (cause: Error) => {
      if (disposed) return
      attempt += 1
      setConnection({
        client: null,
        status: 'reconnecting',
        error: cause,
        server: null,
        rehearsalSupported: false,
        attempt,
        everConnected: everConnected.current,
        retryNow
      })
      timer = setTimeout(connect, backoffDelay(attempt))
    }

    function connect() {
      if (disposed) return
      const client = new Client({ name: 'judgment-pack-desk', version: '0.1.0' }, { capabilities: {} })
      live = client

      // desk/fileChanged is the chassis' own notification and has no SDK schema.
      // The fallback handler is the SDK's supported way to receive a method it
      // does not know, so no schema has to be invented for it here.
      client.fallbackNotificationHandler = async (notification: Notification) => {
        if (notification.method !== 'desk/fileChanged') return
        // The runtime reads the project tree on every call, so any change under
        // it can make any cached answer stale. Cancel before invalidating:
        // invalidation alone reuses a fetch already in flight, and an answer
        // read from the tree before the change would land as fresh — the abort
        // travels into callTool through each query's own signal.
        await queryClient.cancelQueries()
        await queryClient.invalidateQueries()
      }

      // A socket that closes after a successful initialize is a lost
      // connection, not a shutdown: the subprocess died, or the chassis was
      // restarted under it.
      client.onclose = () => {
        if (disposed || live !== client) return
        live = null
        scheduleRetry(new Error('the desk connection closed — the chassis may have restarted'))
      }

      const reconnecting = attempt > 0
      client
        .connect(new DeskWebSocketTransport(socketURL(token)))
        .then(async () => {
          if (disposed || live !== client) return
          attempt = 0
          everConnected.current = true
          const info = client.getServerVersion()
          // The capability is read off the tool's declared schema, once per
          // connection: what the server advertises is the contract, and a
          // runtime that predates the argument simply never receives it.
          let rehearsalSupported = false
          try {
            const tools = await client.listTools()
            const evaluate = tools.tools.find((tool) => tool.name === 'experimental_evaluate')
            const properties = (evaluate?.inputSchema as { properties?: Record<string, unknown> } | undefined)
              ?.properties
            rehearsalSupported = Boolean(properties && 'rehearsal' in properties)
          } catch {
            // A failed listing leaves the capability off; evaluate still works.
          }
          if (disposed || live !== client) return
          setConnection({
            client,
            status: 'ready',
            error: null,
            server: info ? { name: info.name, version: info.version } : null,
            rehearsalSupported,
            attempt: 0,
            everConnected: true,
            retryNow
          })
          // Whatever the project did while the desk was away, it did unobserved.
          if (reconnecting) await queryClient.invalidateQueries()
        })
        .catch((cause: unknown) => {
          if (disposed || live !== client) return
          // A rejected connect leaves the Client holding a transport it will
          // never use; dropping it here stops its onclose from scheduling a
          // second retry beside this one.
          live = null
          void client.close()
          scheduleRetry(cause instanceof Error ? cause : new Error(String(cause)))
        })
    }

    setConnection((previous) => ({
      ...previous,
      client: null,
      status: everConnected.current ? 'reconnecting' : 'connecting',
      attempt: 0,
      everConnected: everConnected.current,
      retryNow
    }))
    connect()

    return () => {
      disposed = true
      if (timer !== undefined) clearTimeout(timer)
      const closing = live
      live = null
      void closing?.close()
    }
  }, [queryClient, retryTick])

  return <McpContext.Provider value={connection}>{children}</McpContext.Provider>
}
