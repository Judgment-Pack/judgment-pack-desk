/**
 * One run of the assistant, as the pane holds it.
 *
 * A run owns three things and releases all three: an MCP connection of its own,
 * an `AbortController`, and the growing list of events. It is a hook rather
 * than a component's state because *when* those are released matters — a stop,
 * a navigation and an unmount all have to close the connection, and the chassis
 * spawns one `jpack mcp` per socket.
 *
 * **The connection is recorded the instant it exists**, not when its setup
 * finishes. `openAssistantConnection` returns its handle synchronously for
 * exactly this reason: a `tools/list` that hangs or refuses used to leave a
 * socket open with nothing anywhere holding a reference to it, so Stop and
 * unmount had nothing to close.
 *
 * **Exactly one `end`, on every path.** The engine emits its own, and this
 * normalizes: the first `end` is the run's terminal event, a second is dropped,
 * and a run that ends any other way — Stop, an abort, a failure before the
 * engine was ever reached — gets one written for it. Stop used to clear the
 * run's identity before the engine handled the abort, so the engine's `end` was
 * discarded and the stream simply stopped: `status` said finished and the
 * contract's terminal event was nowhere.
 *
 * **Nothing is persisted.** A session is a conversation with a model, not a
 * document; there is no draft of it to restore, and a page that reopened one
 * would be re-showing a proposal nobody accepted as though it were still on
 * offer.
 *
 * **The proposal is canonicalized here, once, and nowhere else.** An engine may
 * put any value on `document` — the contract says `unknown` — and a value with
 * a getter or a `toJSON` can answer one thing while the diff is computed,
 * another while the pane renders it and a third while the writer serializes it.
 * Three readings are three documents, and the one a person accepted would be
 * none of them. So the event that reaches the stream carries plain JSON data
 * and every reader downstream reads that. A document that cannot be read as
 * JSON data at all — a cycle, a throwing getter, a value that is not an object
 * — becomes an `error` on the stream and no proposal: there is nothing to show
 * and nothing to write.
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { loadEngine, resolveEngine } from './engines'
import { bindModelCall, openAssistantConnection, runAssistantSession } from './session'
import type { AssistantEvent } from './engine'
import type { AssistantConnection } from './session'
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

/** One run's identity and its terminal state, held together. */
interface Active {
  controller: AbortController
  connection: AssistantConnection | null
  /** True once this run's one `end` has been written. */
  ended: boolean
}

/**
 * One proposal event as plain JSON data, or the error that says why not.
 *
 * `JSON.parse(JSON.stringify(x))` over the whole payload — the document, the
 * unknowns and the critique together — so nothing an engine put on the event
 * survives as a live object: no getters, no `toJSON`, no functions, no symbol
 * keys and no prototype. Exported because the property it holds is worth
 * testing on its own.
 *
 * **A document that is not a JSON object is refused rather than shown.** A pack
 * is an object; `null`, `[]`, `7` and `"a pack"` are each a value this desk
 * cannot draw a diff of member by member and cannot write into a draft, and a
 * proposal offering one is a session that produced nothing to accept.
 */
export function canonicalProposal(
  event: Extract<AssistantEvent, { type: 'proposal' }>
): AssistantEvent {
  let held: { document?: unknown; unknowns?: unknown; critique?: { refuted: boolean } }
  try {
    const text = JSON.stringify({
      document: event.document,
      unknowns: event.unknowns,
      critique: event.critique
    })
    if (text === undefined) throw new Error('it is not JSON data')
    held = JSON.parse(text) as typeof held
  } catch (cause) {
    return {
      type: 'error',
      message:
        `the proposal could not be read as JSON data (${(cause as Error).message}); ` +
        `nothing was proposed and nothing was written`
    }
  }
  const document = held.document
  if (typeof document !== 'object' || document === null || Array.isArray(document)) {
    return {
      type: 'error',
      message:
        'the proposal carries no document object, so there is nothing to diff and nothing to ' +
        'write; nothing was written'
    }
  }
  return {
    type: 'proposal',
    document,
    unknowns: Array.isArray(held.unknowns) ? held.unknowns.map((entry) => String(entry)) : [],
    ...(held.critique === undefined ? {} : { critique: held.critique })
  }
}

export function useAssistantRun(options: {
  endpoint: AssistantEndpointConfig
  engine: AssistantEngine
  thinking: ThinkingTier
}): AssistantRun {
  const [status, setStatus] = useState<RunStatus>('idle')
  const [events, setEvents] = useState<AssistantEvent[]>([])
  const active = useRef<Active | null>(null)
  // Read at call time rather than captured, so a run started with one
  // configuration is not carried on with another.
  const settings = useRef(options)
  settings.current = options

  /**
   * Put one event on the stream, for the run that produced it.
   *
   * Events from a run this one replaced are dropped, and so is anything after
   * a run's terminal `end` — including a second `end` from an engine that
   * yielded one and then threw.
   */
  const push = useCallback((run: Active, event: AssistantEvent) => {
    if (active.current !== run || run.ended) return
    // The one canonicalization site. See the module doc.
    const held = event.type === 'proposal' ? canonicalProposal(event) : event
    if (held.type === 'end') run.ended = true
    setEvents((previous) => [...previous, held])
  }, [])

  /** The run's one terminal event, where nothing else has written it. */
  const finish = useCallback(
    (run: Active) => {
      if (run.ended) return
      push(run, { type: 'end' })
    },
    [push]
  )

  /** Abort and close, with no state change: safe from an unmount. */
  const release = useCallback((run: Active | null) => {
    if (run === null) return
    run.controller.abort()
    const open = run.connection
    run.connection = null
    // The socket is going away either way; a rejection here is not the
    // viewer's business.
    void open?.close().catch(() => undefined)
  }, [])

  const stop = useCallback(() => {
    const run = active.current
    if (run === null) return
    // The terminal event first, while this run is still the current one: the
    // engine's own `end` arrives after the abort has propagated, and by then
    // there is one on the stream already.
    finish(run)
    release(run)
    setStatus((current) => (current === 'running' ? 'finished' : current))
  }, [finish, release])

  // A run that is still open when this unmounts is a `jpack mcp` nobody is
  // watching. The route change that unmounts the pane is the same event.
  useEffect(() => {
    return () => release(active.current)
  }, [release])

  const start = useCallback(
    (prompt: string) => {
      if (active.current !== null && !active.current.ended) return
      const { endpoint, engine, thinking } = settings.current
      const run: Active = { controller: new AbortController(), connection: null, ended: false }
      active.current = run
      setEvents([])
      setStatus('running')

      void (async () => {
        try {
          // Recorded before anything is awaited: the handle exists now, and
          // `close()` on it is valid whatever stage the setup has reached.
          const opened = openAssistantConnection({
            allowed: endpoint.tools,
            onEvent: (event) => push(run, event),
            signal: run.controller.signal
          })
          run.connection = opened
          const ready = await opened.ready
          const { id } = resolveEngine(engine)
          const loaded = await loadEngine(id)
          await runAssistantSession(
            loaded,
            {
              prompt,
              tools: ready.tools,
              callTool: ready.callTool,
              model: { family: endpoint.kind, model: endpoint.model, call: bindModelCall() },
              thinking: { tier: thinking },
              signal: run.controller.signal
            },
            (event) => push(run, event)
          )
        } catch (cause) {
          // Everything before the engine's own `try` — the socket, the tool
          // listing, the engine's chunk. The engine reports its own failures
          // and always ends; this reports the ones it never got to see. A run
          // that was stopped has its terminal event already and says nothing
          // more.
          if (active.current === run && !run.ended) {
            push(run, {
              type: 'error',
              message: `${(cause as Error).name}: ${(cause as Error).message}`
            })
          }
        } finally {
          if (active.current === run) {
            finish(run)
            release(run)
            setStatus('finished')
          }
        }
      })()
    },
    [finish, push, release]
  )

  const { id, substituted } = resolveEngine(options.engine)
  return { status, events, engineId: id, substituted, start, stop }
}
