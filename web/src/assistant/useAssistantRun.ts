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
import { loadEngine } from './engines'
import { bindModelCall, openAssistantConnection, runAssistantSession } from './session'
import { normalize } from './thinking'
import type { AssistantEvent } from './engine'
import type { AssistantConnection } from './session'
import type { AssistantEndpointConfig, AssistantEngine, ThinkingTier } from '../config/deskConfig'

export type RunStatus = 'idle' | 'running' | 'finished'

export interface AssistantRun {
  status: RunStatus
  events: AssistantEvent[]
  /**
   * What went wrong in this run, **including after its terminal event**.
   *
   * The stream cannot carry it: exactly one `end` is the contract, and nothing
   * after it reaches the list. But an engine that yields `end` and then throws
   * while unwinding — a `finally` that fails, a transport that rejects on close
   * — has not had a clean run, and a reader that saw only the event list would
   * be told it did. So the failure is reported here, beside the stream rather
   * than in it, and a consumer deciding whether to *act* on what the run
   * produced reads this as well as the events.
   *
   * Undefined for a run that ended without one, and cleared where a run starts.
   */
  failure: string | undefined
  /**
   * Which engine ran. The configured one, always: every id a `desk.json` may
   * name is certified in this build, and the registry is a total map over them,
   * so there is no substitution left for this to report.
   */
  engineId: AssistantEngine
  /**
   * How many terminal events this hook has accounted for, in an object whose
   * identity is stable for the life of the hook.
   *
   * **It exists to make the unmount path observable, and that is the whole of
   * it.** A run that ends while its component is on screen puts its `end` on
   * `events` and anybody can count it there. A run ended *by* the unmount
   * cannot: `setEvents` on a tree that is going away is a no-op and the array
   * it would have produced is never rendered. So the count lives in an object a
   * caller can take a reference to **before** the unmount and read after it —
   * which is what turns "exactly one terminal event, on every path" from a
   * claim into a measurement, and what the mutation harness breaks.
   *
   * It counts terminal events and not runs: a second `end` is dropped before it
   * gets here, so Stop followed by an unmount is one.
   */
  terminals: { count: number }
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
 * One value as plain JSON data, or undefined where it is not JSON data at all.
 *
 * **The only canonicalization in the assistant**, and `enforcement.test.ts`
 * holds that: no other module here round-trips a proposal, because a second
 * round trip is a second reading, and the whole point of ingesting once is that
 * there is one. A cycle, a `BigInt` and a `toJSON` that throws all end here as
 * `undefined`.
 */
export function plain(value: unknown): unknown {
  let text: string | undefined
  try {
    text = JSON.stringify(value)
  } catch {
    return undefined
  }
  if (text === undefined) return undefined
  return JSON.parse(text) as unknown
}

/**
 * Plain data, frozen all the way down.
 *
 * Canonicalizing once is not enough on its own: what comes out is an ordinary
 * object, and it travels to the pane on `AssistantRun.events` where anything
 * holding the event could reach into it between the diff and the accept. The
 * diff would then describe one document and the writer write another — which is
 * the defect ingestion exists to prevent, one layer out. Frozen, the two are
 * the same object and there is nothing to disagree about.
 *
 * The recursion terminates because this only ever runs over the output of
 * `JSON.parse`, which has no cycles.
 */
export function frozen<T>(value: T): T {
  if (typeof value !== 'object' || value === null) return value
  for (const held of Object.values(value as Record<string, unknown>)) frozen(held)
  return Object.freeze(value)
}

/**
 * One proposal event as plain, frozen JSON data — or the error that says why
 * there is none.
 *
 * The whole payload together: the document, the unknowns and the critique. So
 * nothing an engine put on the event survives as a live object — no getters, no
 * `toJSON`, no functions, no symbol keys, no prototype — and nothing downstream
 * can move what is left.
 *
 * **A document that is not a JSON object is refused rather than shown.** A pack
 * is an object; `null`, `[]`, `7` and `"a pack"` are each a value this desk
 * cannot draw a diff of member by member and cannot write into a draft, and a
 * proposal offering one is a session that produced nothing to accept.
 */
export function canonicalProposal(
  event: Extract<AssistantEvent, { type: 'proposal' }>
): AssistantEvent {
  const held = plain({
    document: event.document,
    unknowns: event.unknowns,
    critique: event.critique
  }) as { document?: unknown; unknowns?: unknown; critique?: { refuted: boolean } } | undefined
  if (held === undefined) {
    return {
      type: 'error',
      message:
        'the proposal could not be read as JSON data; nothing was proposed and nothing was written'
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
  return frozen({
    type: 'proposal',
    document,
    unknowns: Array.isArray(held.unknowns) ? held.unknowns.map((entry) => String(entry)) : [],
    ...(held.critique === undefined ? {} : { critique: held.critique })
  })
}

export function useAssistantRun(options: {
  endpoint: AssistantEndpointConfig
  engine: AssistantEngine
  thinking: ThinkingTier
}): AssistantRun {
  const [status, setStatus] = useState<RunStatus>('idle')
  const [events, setEvents] = useState<AssistantEvent[]>([])
  const [failure, setFailure] = useState<string | undefined>(undefined)
  const active = useRef<Active | null>(null)
  // One object for the life of the hook. See `AssistantRun.terminals`.
  const terminals = useRef({ count: 0 })
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
    if (held.type === 'end') {
      run.ended = true
      terminals.current.count += 1
    }
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
  //
  // **It goes through `finish` and not through `release` alone.** Releasing
  // aborts and closes; it does not account for the run's one terminal event,
  // and this hook's whole claim is that every run has exactly one however it
  // ended. An unmounted component paints nothing, so the `setEvents` is a
  // no-op — but `run.ended` is not: it is what a second `end` is dropped
  // against, and what lets the next run start where the component comes back.
  useEffect(() => {
    return () => {
      const run = active.current
      if (run !== null) finish(run)
      release(run)
    }
  }, [finish, release])

  const start = useCallback(
    (prompt: string) => {
      if (active.current !== null && !active.current.ended) return
      const { endpoint, engine, thinking } = settings.current
      const run: Active = { controller: new AbortController(), connection: null, ended: false }
      active.current = run
      setEvents([])
      setFailure(undefined)
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
          const loaded = await loadEngine(engine)
          await runAssistantSession(
            loaded,
            {
              prompt,
              tools: ready.tools,
              callTool: ready.callTool,
              model: { family: endpoint.kind, model: endpoint.model, call: bindModelCall() },
              // **Normalized here, once.** The engine is handed the desk's own
              // table's result rather than a tier it would have to interpret.
              thinking: normalize(thinking, endpoint.kind),
              signal: run.controller.signal
            },
            (event) => push(run, event)
          )
        } catch (cause) {
          // Everything before the engine's own `try` — the socket, the tool
          // listing, the engine's chunk. The engine reports its own failures
          // and always ends; this reports the ones it never got to see.
          const said = `${(cause as Error).name}: ${(cause as Error).message}`
          if (active.current === run) {
            // **Recorded whether or not the run has ended.** A failure after
            // the terminal event cannot go on the stream — one `end` is the
            // contract and nothing follows it — and dropping it entirely is
            // what made `proposal → end → throw` read as a clean run to
            // everything downstream. It goes beside the stream instead.
            setFailure(said)
            if (!run.ended) push(run, { type: 'error', message: said })
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

  return {
    status,
    events,
    failure,
    terminals: terminals.current,
    engineId: options.engine,
    start,
    stop
  }
}
