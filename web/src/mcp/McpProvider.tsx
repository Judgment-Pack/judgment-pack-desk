import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import type { Notification } from '@modelcontextprotocol/sdk/types.js'
import { useQueryClient } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { recordFileChange } from '../shell/consoleLog'
import { deskFetch } from '../files/client'
import { NO_SESSION_MESSAGE, NoSession, sessionBearer, sessionEnded, whenSessionEnds } from './session'
import { UNKNOWN_CAPABILITIES, type RuntimeCapabilities, listAllTools, readCapabilities } from './capabilities'
import { DeskWebSocketTransport } from './transport'

export type ConnectionStatus = 'connecting' | 'ready' | 'reconnecting' | 'failed'

/**
 * What a connection **is**, read off its status and never off what it left
 * behind.
 *
 * `server` is the runtime that answered `initialize`, and it is *retained*
 * across a reconnect — the provider spreads the previous state — because losing
 * the name of the runtime you were talking to would be a worse answer than
 * keeping it. So `server !== null` means "this page has met a runtime", which is
 * not "this page is connected to one": after a reconnect it is true while the
 * socket is down, and every surface that read a verdict off it said `connected`
 * while the banner said the connection was lost.
 *
 * One producer, because four surfaces say this and four sentences about one
 * socket are free to disagree about whether it is up.
 */
export function connectionSays(status: ConnectionStatus): string {
  if (status === 'ready') return 'connected'
  if (status === 'connecting') return 'connecting'
  if (status === 'reconnecting') return 'reconnecting'
  return 'not connected'
}

export interface McpConnection extends RuntimeCapabilities {
  client: Client | null
  status: ConnectionStatus
  error: Error | null
  /** The runtime that answered initialize, for the status bar. */
  server: { name: string; version: string } | null
  /**
   * Which connection this is: 0 before the first, then one more for each
   * socket that completed initialize.
   *
   * It exists so a query whose answer is only meaningful beside another
   * query's can be keyed by the connection both were read over. Two answers
   * read across a reconnect describe a runtime that was restarted, a project
   * that may have changed underneath, and possibly a different binary
   * altogether — joining them would state a relationship nothing observed.
   */
  connectionEpoch: number
  /**
   * Why the tool listing did not answer, where it did not. The connection is
   * still usable — initialize succeeded — but what the runtime can do is
   * unknown rather than absent, and `known` is false to say so.
   */
  capabilitiesError: Error | null
  /** Consecutive failed attempts; 0 while connected. */
  attempt: number
  /** True once this page has connected at least once. */
  everConnected: boolean
  /** Abandon the current backoff and try again now. */
  retryNow: () => void
}

const DISCONNECTED: McpConnection = {
  client: null,
  status: 'connecting',
  error: null,
  server: null,
  ...UNKNOWN_CAPABILITIES,
  connectionEpoch: 0,
  capabilitiesError: null,
  attempt: 0,
  everConnected: false,
  retryNow: () => {}
}

/**
 * Exported so a test can stand one connection up without a socket. Nothing in
 * the application reads it directly: `useMcp` is the door.
 */
export const McpContext = createContext<McpConnection>(DISCONNECTED)

export function useMcp(): McpConnection {
  return useContext(McpContext)
}

/**
 * Take the bare `#` the launch redirect leaves off the address bar.
 *
 * **Why the launch redirects to `/#` at all.** A redirect whose `Location`
 * carries no fragment inherits the *request's* one (RFC 9110 §10.2.2), so
 * `/launch?secret=S#S` would land on `/#S` — the secret still in
 * `location.hash`, readable by every script on the page and kept in history. An
 * explicit empty fragment overrides it, and this removes what that leaves.
 *
 * **`href`, not `hash`.** `location.hash` is the empty string for a URL ending
 * in a bare `#`, so reading it cannot tell that URL from a clean one; the `#` is
 * only visible in `href`.
 *
 * `replaceState` rather than `pushState`: the desk is where the person already
 * is, and a history entry they never asked for is a Back button that does
 * nothing visible.
 */
export function removeTheLaunchHash(): void {
  try {
    if (!window.location.href.endsWith('#')) return
    window.history.replaceState(null, '', window.location.pathname + window.location.search)
  } catch {
    // A browser that refuses history manipulation keeps a bare `#`, which is
    // untidy and carries nothing.
  }
}

if (typeof window !== 'undefined') removeTheLaunchHash()

/**
 * The one address a desk MCP connection is opened at, and it carries **no
 * credential**: the session id travels in the subprotocol offer instead, which
 * is the only place a browser lets a page put anything on a handshake.
 *
 * Exported because the assistant opens a **second** connection over the same
 * relay with its own client and its own gate (`assistant/session.ts`), and two
 * spellings of this address would be two answers about where the chassis is.
 */
export function socketURL(): string {
  const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${window.location.host}/ws`
}

/**
 * The subprotocols a desk upgrade offers: the plain one, and the session id.
 *
 * Exported for the same reason `socketURL` is — the assistant's connection
 * offers the same pair — and because a test can then assert the id is in the
 * offer and nowhere else.
 */
export function socketProtocols(id: string): string[] {
  return ['jpack-desk', `jpack-desk-session.${id}`]
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
 * A dropped socket is reconnected rather than reported and left, and each
 * attempt builds a fresh Client and a fresh transport — an SDK Client that has
 * closed already negotiated with a server that is gone. The delay between
 * attempts doubles up to a cap, and a reconnect invalidates every query: the
 * runtime re-reads the project on every call, and whatever the project did
 * while the socket was down arrived as `desk/fileChanged` notifications nobody
 * heard.
 *
 * **What reconnecting does not survive is a restart**, and this used to say the
 * opposite. A restarted chassis mints a new session store and a new launch
 * secret, so the id this page holds names nothing: the reconnect is refused,
 * `classify` reads the refusal, and the page ends in the terminal no-session
 * state asking for the URL the **new** process printed. What the backoff is for
 * is a socket that dropped while the same process kept running.
 */
/**
 * Thrown to abandon an attempt whose effect was torn down while it awaited.
 * Its own type so the catch can tell it from a real failure and stay silent.
 */
class Disposed extends Error {}

export function McpProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [connection, setConnection] = useState<McpConnection>(DISCONNECTED)
  // Bumping this re-runs the effect, which is what "try again now" means: the
  // effect owns every socket, timer, and Client, so restarting it is the one
  // way to retry that cannot leave a second connection behind.
  const [retryTick, setRetryTick] = useState(0)
  // Survives those restarts, so a manual retry does not tell the views this
  // page has never been connected.
  const everConnected = useRef(false)
  // And so does the epoch: it counts connections this page has completed, so it
  // must not restart when the effect does.
  const epoch = useRef(0)

  useEffect(() => {
    let disposed = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    // The Client of the attempt in flight or connected. A late event from an
    // attempt this one replaced is ignored by comparing against it.
    let live: Client | null = null

    const retryNow = () => setRetryTick((tick) => tick + 1)

    const scheduleRetry = (cause: Error) => {
      if (disposed) return
      attempt += 1
      setConnection({
        ...DISCONNECTED,
        status: 'reconnecting',
        error: cause,
        connectionEpoch: epoch.current,
        attempt,
        everConnected: everConnected.current,
        retryNow
      })
      timer = setTimeout(connect, backoffDelay(attempt))
    }

    // A connection that failed, told apart from one that will never succeed.
    // No session is not a retryable state: only the printed URL mints one, and
    // a page that reconnected for ever would hide the one instruction that
    // fixes it.
    const failed = (cause: Error) => {
      if (disposed) return
      setConnection({
        ...DISCONNECTED,
        status: 'failed',
        error: cause,
        connectionEpoch: epoch.current,
        everConnected: everConnected.current,
        retryNow
      })
    }

    /**
     * The session's end, once, and it takes this connection with it.
     *
     * **A socket that is already open notices nothing.** A `401` is met by the
     * caller that made the request; a connection established before the
     * refusal went on carrying frames for a session the chassis had refused,
     * and every query on the page went on driving it. The one subscription in
     * `session.ts` is what reaches this effect, and the unsubscribe below is
     * what keeps a StrictMode double-mount from announcing the end twice.
     */
    const stopWatching = whenSessionEnds(() => {
      if (timer !== undefined) clearTimeout(timer)
      const closing = live
      // **Dropped before it is closed**, so the client's own `onclose` sees
      // `live !== client` and does not schedule a retry behind this.
      live = null
      void closing?.close()
      failed(new NoSession(sessionEnded() ?? NO_SESSION_MESSAGE))
    })

    /**
     * Why an upgrade was refused, and what to do about it.
     *
     * **The browser withholds the status of a failed handshake**, so a page
     * cannot tell "this desk is down" from "this desk does not know my id". So
     * the id is put to a channel that does answer: `GET /api/session`, through
     * `deskFetch`, which is the same gate every other chassis call goes through.
     * A `401` there forgets the id and ends the connection — that is the
     * terminal no-session state, and only the next page load leaves it.
     * Anything else is a chassis that is simply not answering, which is what
     * the backoff is for.
     */
    const classify = async (cause: Error) => {
      if (disposed) return
      try {
        await deskFetch('/api/session')
      } catch (refusal) {
        if (disposed) return
        if (refusal instanceof NoSession) {
          failed(refusal)
          return
        }
      }
      if (disposed) return
      scheduleRetry(cause)
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
        // The one place the shell reaches into this provider. The chassis
        // sends `{"method":"desk/fileChanged","params":{"path":relPath}}` and
        // this handler is the only thing that ever sees it — deriving
        // "something invalidated" from the query cache instead would name no
        // path, and a channel labelled Files would then say less than its
        // label claims. The path, and nothing else.
        recordFileChange(String((notification.params as { path?: unknown })?.path ?? ''))
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
      // **The bootstrap first, and the socket after it.** The upgrade has to
      // carry the id, and the id comes from the one `POST /api/session` this
      // page makes — memoised, so a reconnect within one page's life resolves
      // from memory without a request.
      //
      // **`sessionBearer()` rather than `bootstrap()`**, and the difference is
      // the terminal state: the bootstrap's promise keeps answering the id it
      // minted, because that is what it minted, while `sessionBearer()` is the
      // one entry point that also refuses once a `401` has forgotten it. A page
      // in the no-session state opens no socket at all, and a provider that
      // read the promise directly would reconnect for ever with a dead id.
      //
      // **Disposal is re-checked after the await.** The effect can be torn down
      // while this promise is pending — StrictMode mounts twice, and a route
      // change unmounts — and connecting afterwards would open a socket with
      // nothing left to close it.
      sessionBearer()
        .then((id) => {
          if (disposed || live !== client) throw new Disposed()
          return client.connect(new DeskWebSocketTransport(socketURL(), socketProtocols(id)))
        })
        .then(async () => {
          if (disposed || live !== client) return
          attempt = 0
          everConnected.current = true
          epoch.current += 1
          const info = client.getServerVersion()
          // The capabilities are read off one listing, once per connection:
          // what the server advertises is the contract, and a runtime that
          // predates a tool or an argument simply never receives it. Every page
          // of the listing is read, because a tool on a second page that was
          // never asked for is a tool this would report as absent.
          let capabilities = UNKNOWN_CAPABILITIES
          let capabilitiesError: Error | null = null
          try {
            capabilities = readCapabilities(await listAllTools(client))
          } catch (cause) {
            // A failed listing leaves every capability off *and says so*. Off
            // alone would be the page impersonating a connection to an older
            // runtime, which is a claim about the runtime this never made.
            capabilitiesError = cause instanceof Error ? cause : new Error(String(cause))
          }
          if (disposed || live !== client) return
          setConnection({
            client,
            status: 'ready',
            error: null,
            server: info ? { name: info.name, version: info.version } : null,
            ...capabilities,
            connectionEpoch: epoch.current,
            capabilitiesError,
            attempt: 0,
            everConnected: true,
            retryNow
          })
          // Whatever the project did while the desk was away, it did unobserved.
          if (reconnecting) await queryClient.invalidateQueries()
        })
        .catch((cause: unknown) => {
          if (cause instanceof Disposed) {
            void client.close()
            return
          }
          if (disposed || live !== client) return
          // A rejected connect leaves the Client holding a transport it will
          // never use; dropping it here stops its onclose from scheduling a
          // second retry beside this one.
          live = null
          void client.close()
          const error = cause instanceof Error ? cause : new Error(String(cause))
          // The one failure that never resolves on its own.
          if (error instanceof NoSession) {
            failed(error)
            return
          }
          void classify(error)
        })
    }

    setConnection((previous) => ({
      ...previous,
      ...UNKNOWN_CAPABILITIES,
      client: null,
      status: everConnected.current ? 'reconnecting' : 'connecting',
      capabilitiesError: null,
      attempt: 0,
      everConnected: everConnected.current,
      retryNow
    }))
    connect()

    return () => {
      disposed = true
      stopWatching()
      if (timer !== undefined) clearTimeout(timer)
      const closing = live
      live = null
      void closing?.close()
    }
  }, [queryClient, retryTick])

  return <McpContext.Provider value={connection}>{children}</McpContext.Provider>
}
