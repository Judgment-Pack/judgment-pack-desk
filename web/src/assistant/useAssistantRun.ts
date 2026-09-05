/**
 * One run of the assistant, as the pane holds it.
 *
 * A run owns three things and releases all three: an MCP connection of its own,
 * an `AbortController`, and the growing list of events. It is a hook rather
 * than a component's state because *when* those are released matters — a stop,
 * a navigation and an unmount all have to close the connection, and the chassis
 * spawns one `jpack mcp` per socket.
 *
 * **Nothing is persisted.** A session is a conversation with a model, not a
 * document; there is no draft of it to restore, and a page that reopened one
 * would be re-showing a proposal nobody accepted as though it were still on
 * offer.
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { loadEngine, resolveEngine } from './engines'
import { openAssistantConnection, relayBaseUrl, runAssistantSession } from './session'
import type { AssistantEvent } from './engine'
import type { AssistantEndpointConfig, AssistantEngine, ThinkingTier } from '../config/deskConfig'

export type RunStatus = 'idle' | 'running' | 'finished'

export interface AssistantRun {
  status: RunStatus
  events: AssistantEvent[]
  /** Which engine actually ran, and why it is not the configured one. */
  engineId: AssistantEngine
  substituted: string | undefined
  /** Start one run with the prompt text the desk already fetched. */
  start: (prompt: string) => void
  stop: () => void
}

export function useAssistantRun(options: {
  endpoint: AssistantEndpointConfig
  engine: AssistantEngine
  thinking: ThinkingTier
}): AssistantRun {
  const [status, setStatus] = useState<RunStatus>('idle')
  const [events, setEvents] = useState<AssistantEvent[]>([])
  const controller = useRef<AbortController | null>(null)
  const connection = useRef<{ close: () => Promise<void> } | null>(null)
  // Read at call time rather than captured, so a run started with one
  // configuration is not carried on with another.
  const settings = useRef(options)
  settings.current = options

  const release = useCallback(() => {
    controller.current?.abort()
    controller.current = null
    const open = connection.current
    connection.current = null
    // The socket is going away either way; a rejection here is not the
    // viewer's business.
    void open?.close().catch(() => undefined)
  }, [])

  const stop = useCallback(() => {
    release()
    setStatus((current) => (current === 'running' ? 'finished' : current))
  }, [release])

  // A run that is still open when this unmounts is a `jpack mcp` nobody is
  // watching. The route change that unmounts the pane is the same event.
  useEffect(() => release, [release])

  const start = useCallback(
    (prompt: string) => {
      if (controller.current !== null) return
      const { endpoint, engine, thinking } = settings.current
      const own = new AbortController()
      controller.current = own
      setEvents([])
      setStatus('running')

      void (async () => {
        const push = (event: AssistantEvent) => {
          // Only while this run is the current one: a stop, then a second
          // start, must not have the first run's tail land in the second's
          // list.
          if (controller.current !== own) return
          setEvents((previous) => [...previous, event])
        }
        try {
          const opened = await openAssistantConnection({
            allowed: endpoint.tools,
            onEvent: push
          })
          if (controller.current !== own) {
            void opened.close().catch(() => undefined)
            return
          }
          connection.current = opened
          const { id } = resolveEngine(engine)
          const loaded = await loadEngine(id)
          await runAssistantSession(
            loaded,
            {
              prompt,
              tools: opened.tools,
              callTool: opened.callTool,
              model: { family: endpoint.kind, baseUrl: relayBaseUrl(), model: endpoint.model },
              thinking: { tier: thinking },
              signal: own.signal
            },
            push
          )
        } catch (cause) {
          // Everything before the engine's own `try` — the socket, the tool
          // listing, the engine's chunk. The engine reports its own failures
          // and always ends; this reports the ones it never got to see.
          push({ type: 'error', message: `${(cause as Error).name}: ${(cause as Error).message}` })
          push({ type: 'end' })
        } finally {
          if (controller.current === own) {
            release()
            setStatus('finished')
          }
        }
      })()
    },
    [release]
  )

  const { id, substituted } = resolveEngine(options.engine)
  return { status, events, engineId: id, substituted, start, stop }
}
