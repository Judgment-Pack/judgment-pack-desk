import type { CriticMode, CriticTier } from '@judgment-pack/harness/contract'
import {failureEvent,authoredFailure} from '@judgment-pack/harness/failures'
import type { AITarget } from './aiConnections'
import { bindExecution } from './target'
import { memoryInUse, runtimeIdentityOf } from './runContext'
import { ChatContext } from '@judgment-pack/harness/context/chat'
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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { RunMeter, type RunTiming } from './runMeter'
import { reportFromExecution, type RunReport } from './report'
import { loadEngine } from '@judgment-pack/harness/engines/index'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useMcp } from '../mcp/McpProvider'
import { openAssistantConnection, runAssistantSession } from '@judgment-pack/harness/session'
import { sessionBearer, whenSessionEnds } from '../mcp/session'
import type { HostTool, AssistantEvent, McpToolResult } from '@judgment-pack/harness/engine'
import type { CriticCase } from '@judgment-pack/harness/refutation'
import type { AssistantConnection } from '@judgment-pack/harness/session'
import {
  NO_MODEL_CHOSEN,
  type AssistantEndpointConfig,
  type AssistantAgentConfig,
  type AssistantEngine,
  type ThinkingTier
} from '../config/deskConfig'

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
  /**
   * The prose the assistant has written so far, while it writes it: the latest `message_progress` of the
   * run, and empty once the answer is whole (the `message` event) or the run has ended. Only the default
   * purpose streams; the others show their answer whole, as before.
   */
  streaming: string
  /**
   * What the run reports about itself once it has ended or paused: the requests it made, the provider's
   * own token counts where returned, the requests sent again and why, how long the model and the tools
   * took, and each delegated child's figures. Read from the harness's execution checkpoint and what the
   * Desk measured on the connection; nothing in it is an estimate.
   */
  report: RunReport | undefined
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
  /** What the Desk measures on this run's connection. */
  meter: RunMeter
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
export { plain, frozen, canonicalProposal } from './proposalValue'
import { canonicalProposal } from './proposalValue'

/**
 * The model id a run is given, or a refusal.
 *
 * **It is the model the caller picked**, and the caller is the surface with the
 * picker on it: which of the enabled models a piece of work wants is a decision
 * made at the run, and a hook that reached past the pick for the file's default
 * would be running something nobody chose.
 *
 * The empty string is the state the schema has — "no model chosen yet", where
 * nothing is enabled — and a run is the one place it cannot be carried: a
 * request naming no model is a request whose answer means nothing. The tab does
 * not offer a run there, and this is the second layer under that.
 */
function modelOf(picked: string): string {
  if (picked === '') throw authoredFailure('configuration',NO_MODEL_CHOSEN)
  return picked
}

export function useAssistantRun(options: {
  /** Identity from the same slot as endpoint/agent, captured when the run starts. */
  target?: AITarget
  endpoint: AssistantEndpointConfig | null
  agent?: AssistantAgentConfig
  /**
   * Which of the endpoint's enabled models this run uses.
   *
   * Passed in rather than read off the endpoint, because the choice belongs to
   * the surface a person is working on: `usePickedModel` resolves the tab's own
   * pick against the enabled set and hands the answer here. `''` where nothing
   * is enabled, which is the state a run refuses.
   */
  model: string
  engine: AssistantEngine
  thinking: ThinkingTier
  /**
   * The runtime's `test_pack` prompt, for the refutation pass, or `''`.
   *
   * Read by the surface that already reads the runtime's other prompts and
   * passed down rather than fetched here: an engine has no reach to
   * `prompts/get` — `callTool` is the whole of it — and a query does not belong
   * in this hook.
   */
  purpose?: 'test-design' | 'brief' | 'graph'
  testPrompt?: string
  /**
   * The desk's own tools for this run, executed on the page: the research
   * tools where a surface offers them, and none otherwise. Read at start, so a
   * run holds the set it began with.
   */
  hostTools?: HostTool[]
  /**
   * The cases whose outcomes the critic compares with the proposal's (the pack's
   * saved tests and any the request declares), read at start. Only the default
   * purpose runs a critic, so only it is handed them.
   */
  critic?:CriticMode
  criticReasoning?:CriticTier
  criticCases?: CriticCase[]
}): AssistantRun {
  const profile=useEffectiveConfig().assistantProfile?.value
  const contextConfiguration=profile?.context
  const criticProject=profile?.critic
  const mcp = useMcp()
  const connectionTarget = options.target ?? {}
  const [status, setStatus] = useState<RunStatus>('idle')
  const [runningEngine, setRunningEngine] = useState(options.engine)
  const [events, setEvents] = useState<AssistantEvent[]>([])
  const [failure, setFailure] = useState<string | undefined>(undefined)
  const [timing, setTiming] = useState<RunTiming | undefined>(undefined)
  const active = useRef<Active | null>(null)
  // The mounted authoring pane owns its retained context, independently of saved chats.
  // Reuse Runtime reads across its runs only under the Runtime identity; dispose on unmount.
  const retained = useRef<ChatContext | null>(null)
  // One object for the life of the hook. See `AssistantRun.terminals`.
  const terminals = useRef({ count: 0 })
  // Read at call time rather than captured, so a run started with one
  // configuration is not carried on with another.
  const settings = useRef({...options,connectionTarget,contextConfiguration,criticProject,mcp})
  settings.current = {...options,connectionTarget,contextConfiguration,criticProject,mcp}

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
    run.meter.event(held)
    // **Progress is the prose so far, whole each time**, so only the latest one is kept: a long answer
    // streamed in hundreds of events would otherwise hold each of its prefixes.
    setEvents((previous) => {
      if (held.type !== 'message_progress') return [...previous, held]
      // Each task's progress is a complete snapshot, even when tasks alternate.
      return [...previous.filter((event) => event.type !== 'message_progress' || event.taskId !== held.taskId), held]
    })
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
      retained.current?.dispose()
      retained.current = null
    }
  }, [finish, release])

  /**
   * The session ending stops the run, and **stopping it is an abort**.
   *
   * Closing the connection is not enough on its own. The engine's loop is
   * driving a model over the relay and a tool over the socket; a close ends the
   * socket, and the engine goes on awaiting a model answer that will arrive,
   * with nothing to deliver it to but the pane. So the run's own controller is
   * aborted — the same abort Stop performs — which is what every await in the
   * loop is already written against, and `finish` accounts for the one terminal
   * event as it does for every other ending.
   *
   * `stop()` rather than `release()` for that reason: a run that ended with no
   * terminal event is a pane that says it is still running for ever.
   */
  useEffect(() => whenSessionEnds(() => stop()), [stop])

  const start = useCallback(
    (prompt: string) => {
      if (active.current !== null && !active.current.ended) return
      const { endpoint, agent, model, engine, thinking, testPrompt, hostTools, purpose, connectionTarget, criticCases } = settings.current
      const run: Active = { controller: new AbortController(), connection: null, ended: false, meter: new RunMeter() }
      active.current = run
      setEvents([])
      setTiming(undefined)
      setFailure(undefined)
      setStatus('running')
      setRunningEngine(engine)

      void (async () => {
        try {
          // **The session id first.** The assistant opens its own socket and
          // the id travels in the subprotocol offer, so it has to be in hand
          // before the transport is built. `sessionBearer()` awaits the page's
          // one exchange, memoised — by the time a run can start the desk's own
          // provider has already awaited it, so this resolves from memory — and
          // it refuses where a `401` has already ended this page's session, so
          // a run in that state opens no socket and says why. Nothing is open
          // yet at this await, so there is nothing an abort here could leak.
          const sessionId = await sessionBearer()
          // Recorded before anything else is awaited: the handle exists now,
          // and `close()` on it is valid whatever stage the setup has reached.
          const opened = purpose === 'brief' ? null : openAssistantConnection({
            allowed: (engine === 'codex' ? agent?.tools : endpoint?.tools) ?? [],
            onEvent: (event) => push(run, event),
            sessionId,
            signal: run.controller.signal
          })
          run.connection = opened
          const ready = opened ? await opened.ready : { tools: [], callTool: async () => { throw new Error('Brief generation has no tool capability.') } }
          const loaded = await loadEngine(engine)
          // Whether project memory is on, so the memory reserve is left out of a run that has none (agent#11).
          // Runtime identity is host metadata, outside the model's tool offer.
          const runtimeClient = settings.current.mcp.status === 'ready' ? settings.current.mcp.client : null
          const [memory, runtime] = await Promise.all([
            purpose === 'brief' ? undefined : memoryInUse(run.controller.signal),
            purpose !== 'brief' && runtimeClient
              ? runtimeIdentityOf(runtimeClient, async (name, args) => await runtimeClient.callTool({ name, arguments: args }) as McpToolResult)
              : undefined
          ])
          if (run.controller.signal.aborted) return
          if (!retained.current || retained.current.isDisposed) retained.current = new ChatContext()
          await runAssistantSession(
            loaded,
            {
              prompt,
              // The connection the run is bound to, so the harness asks the host for that connection's
              // review and helper models and its capacity (`modelsFor`), and not for a model name's.
              context:{configuration:settings.current.contextConfiguration,chat:retained.current,...(runtime?{runtime}:{}),...(memory===undefined?{}:{memory}),...(connectionTarget.connectionId?{connection:connectionTarget}:{})},
              testPrompt: testPrompt ?? '',
              purpose,
              // Agent resolves critic precedence and the reasoning floor from these exact choices.
              ...(purpose === undefined ? { interactive: true } : {}),
              critic:settings.current.critic, criticProject:settings.current.criticProject, criticReasoning:settings.current.criticReasoning,
              ...(purpose === undefined && criticCases?.length ? { criticCases } : {}),

              ...(purpose === 'test-design' || purpose === 'brief' ? { allowConversation: true, interactive: true, critic: 'off' } : {}),
              tools: ready.tools,
              callTool: ready.callTool,
              hostTools: hostTools ?? [],
              // **Refused rather than defaulted.** An endpoint with no model
              // chosen is a saved endpoint whose assistant is not ready; a run
              // that substituted the empty string would put a request on the
              // wire naming no model and read whatever came back as an answer.
              ...bindExecution({ ...connectionTarget, endpoint, agent, engine }, modelOf(model), thinking, run.meter),
              signal: run.controller.signal
            },
            (event) => push(run, event)
          )
        } catch (cause) {
          // Everything before the engine's own `try` — the socket, the tool
          // listing, the engine's chunk. The engine reports its own failures
          // and always ends; this reports the ones it never got to see.
          const event = failureEvent(cause)
          const said = event.message
          if (active.current === run) {
            // **Recorded whether or not the run has ended.** A failure after
            // the terminal event cannot go on the stream — one `end` is the
            // contract and nothing follows it — and dropping it entirely is
            // what made `proposal → end → throw` read as a clean run to
            // everything downstream. It goes beside the stream instead.
            setFailure(said)
            if (!run.ended) push(run, event)
          }
        } finally {
          if (active.current === run) {
            setTiming(run.meter.snapshot())
            finish(run)
            release(run)
            setStatus('finished')
          }
        }
      })()
    },
    [finish, push, release]
  )

  const lastCheckpoint = [...events].reverse().find((event) => event.type === 'execution_checkpoint' || event.type === 'budget_paused')
  const execution = lastCheckpoint === undefined ? undefined : (lastCheckpoint as { checkpoint: import('@judgment-pack/harness/execution/state').ExecutionCheckpoint }).checkpoint
  const report = useMemo(() => (status === 'running' ? undefined : reportFromExecution(execution, timing)), [status, execution, timing])
  const progress = events.at(-1)
  const streaming = status === 'running' && progress?.type === 'message_progress' && progress.taskId === undefined ? progress.text : ''

  return {
    status,
    events,
    failure,
    streaming,
    report,
    terminals: terminals.current,
    engineId: status === 'idle' ? options.engine : runningEngine,
    start,
    stop
  }
}
