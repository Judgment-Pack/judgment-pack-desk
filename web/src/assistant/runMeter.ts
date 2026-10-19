import type { AssistantEvent, ModelCall } from '@judgment-pack/harness/engine'
import type { AgentRun } from '@judgment-pack/harness/agent'

/**
 * What the Desk measures of a run as it makes it: for each request the
 * assistant sends, the model it names, the bytes it carries, how long until
 * the first byte of the answer and until the last, the status it came back
 * with; and for each tool the model called, its name and how long it took.
 * The harness's own figures (the provider's token counts, the requests it
 * sent again and why, each child's cost) come from its execution checkpoint;
 * **this is the half only the host can see**, because the host holds the
 * connection and the harness holds no clock for it.
 *
 * Nothing here is an estimate: a time is a measured interval, and a request
 * that never answered is recorded as it ended (`failed` or `aborted`) with
 * the time it ran, not omitted.
 */
export interface StepRecord {
  /** The model the request names, or the agent's. */
  model: string
  /** The bytes of the request body (a subscription run: its prompt and instructions). */
  requestBytes: number
  /** Milliseconds from sending to the first byte of the answer; absent where none came. */
  firstByteMs?: number
  /** Milliseconds from sending to the end of the answer, or to where the request ended. */
  durationMs?: number
  /** The HTTP status the relay answered, absent for a subscription run or a request that never was answered. */
  status?: number
  outcome: 'complete' | 'failed' | 'aborted'
  /**
   * How long the desk's relay held the request for a turn before sending it, when it did (the relay carries
   * four requests at once and queues the rest), and how many were waiting when it joined, itself among them.
   */
  queuedMs?: number
  queueDepth?: number
  /** The tools the model called after this request, in order, with how long each took. */
  tools?: { name: string; ms: number; failed?: true }[]
}

/** The steps of a run, in the order they began; the oldest are dropped past `MAX_STEPS`, and counted. */
export interface RunTiming {
  steps: StepRecord[]
  omitted: number
}

/** The headers the desk's relay puts on an answer whose request waited for a turn (`relay_queue.go`). */
export const QUEUE_WAIT_HEADER = 'X-Relay-Queue-Wait-Ms'
export const QUEUE_DEPTH_HEADER = 'X-Relay-Queue-Depth'

/** The most steps a report keeps, and the most tools one step keeps. */
export const MAX_STEPS = 100
export const MAX_TOOLS_PER_STEP = 40

const bytesOf = (text: string | undefined) => (text === undefined ? 0 : new TextEncoder().encode(text).length)

interface Open {
  record: StepRecord
  started: number
  settled: boolean
}

export class RunMeter {
  private readonly open: Open[] = []
  private readonly pending = new Map<string, { name: string; started: number; step: Open | undefined }[]>()
  private readonly clock: () => number
  constructor(clock: () => number = () => performance.now()) {
    this.clock = clock
  }

  private begin(model: string, requestBytes: number): Open {
    const step: Open = { record: { model, requestBytes, outcome: 'aborted' }, started: this.clock(), settled: false }
    this.open.push(step)
    return step
  }

  private settle(step: Open, outcome: StepRecord['outcome']) {
    if (step.settled) return
    step.settled = true
    step.record.outcome = outcome
    step.record.durationMs = Math.round(this.clock() - step.started)
  }

  /**
   * A model call that is measured: each request is a step, its first byte and
   * its end timed on the answer as the engine reads it. The answer is passed on
   * whole (status, headers and body), and a body that is never read to its end
   * leaves its step `aborted` with the time it ran.
   */
  call(call: ModelCall, model: string): ModelCall {
    return async (suffix, request) => {
      const step = this.begin(model, bytesOf(request.body))
      let answered: Response
      try {
        answered = await call(suffix, request)
      } catch (error) {
        this.settle(step, request.signal?.aborted ? 'aborted' : 'failed')
        throw error
      }
      step.record.status = answered.status
      const waited = Number(answered.headers.get(QUEUE_WAIT_HEADER)), depth = Number(answered.headers.get(QUEUE_DEPTH_HEADER))
      if (Number.isSafeInteger(waited) && waited > 0 && Number.isSafeInteger(depth) && depth > 0) {
        step.record.queuedMs = waited
        step.record.queueDepth = depth
      }
      const ok = answered.ok
      if (answered.body === null) {
        step.record.firstByteMs = Math.round(this.clock() - step.started)
        this.settle(step, ok ? 'complete' : 'failed')
        return answered
      }
      let first = true
      const timed = answered.body.pipeThrough(
        new TransformStream<Uint8Array, Uint8Array>({
          transform: (chunk, controller) => {
            if (first) {
              first = false
              step.record.firstByteMs = Math.round(this.clock() - step.started)
            }
            controller.enqueue(chunk)
          },
          flush: () => this.settle(step, ok ? 'complete' : 'failed'),
          cancel: () => this.settle(step, 'aborted')
        } as Transformer<Uint8Array, Uint8Array>)
      )
      return new Response(timed, { status: answered.status, statusText: answered.statusText, headers: answered.headers })
    }
  }

  /** A subscription run that is measured: one step, to its first event and to its end, and each tool it called. */
  agent(run: AgentRun, model: string): AgentRun {
    return async (request, callbacks) => {
      const step = this.begin(model, bytesOf(request.prompt) + bytesOf(request.instructions))
      const reached = () => {
        step.record.firstByteMs ??= Math.round(this.clock() - step.started)
      }
      try {
        await run(request, {
          ...callbacks,
          event: async (event) => {
            reached()
            return callbacks.event(event)
          },
          tool: async (call, signal) => {
            reached()
            const started = this.clock()
            try {
              const answer = await callbacks.tool(call, signal)
              this.addTool(step, call.name, started, answer.isError)
              return answer
            } catch (error) {
              this.addTool(step, call.name, started, true)
              throw error
            }
          }
        })
        this.settle(step, 'complete')
      } catch (error) {
        this.settle(step, callbacks.signal.aborted ? 'aborted' : 'failed')
        throw error
      }
    }
  }

  private addTool(step: Open, name: string, started: number, failed: boolean) {
    const tools = (step.record.tools ??= [])
    if (tools.length < MAX_TOOLS_PER_STEP) tools.push({ name, ms: Math.round(this.clock() - started), ...(failed ? { failed: true as const } : {}) })
  }

  /**
   * An event of the run, as the page receives it: a tool the model called is
   * timed from the call to its result, and belongs to the request that asked
   * for it (the latest one begun). A call and its result are paired by id where
   * the session gives one, and by name in order where it does not.
   */
  event(event: AssistantEvent): void {
    if (event.type !== 'tool_call' && event.type !== 'tool_result') return
    const key = event.callId ?? `name:${event.name}`
    if (event.type === 'tool_call') {
      const queue = this.pending.get(key) ?? []
      queue.push({ name: event.name, started: this.clock(), step: this.open.at(-1) })
      this.pending.set(key, queue)
      return
    }
    const started = this.pending.get(key)?.shift()
    if (started?.step) this.addTool(started.step, started.name, started.started, event.isError)
  }

  /** What has been measured, with any request still open recorded as it stands. */
  snapshot(): RunTiming {
    const now = this.clock()
    const steps = this.open.map(({ record, started, settled }) => ({
      ...structuredClone(record),
      ...(settled ? {} : { durationMs: Math.round(now - started), outcome: 'aborted' as const })
    }))
    const kept = steps.slice(-MAX_STEPS)
    return { steps: kept, omitted: steps.length - kept.length }
  }
}

/** Two timings joined, the second after the first: what one response's turns made. */
export function joinTiming(first: RunTiming | undefined, second: RunTiming): RunTiming {
  if (first === undefined) return second
  const steps = [...first.steps, ...second.steps]
  const kept = steps.slice(-MAX_STEPS)
  return { steps: kept, omitted: first.omitted + second.omitted + (steps.length - kept.length) }
}

/** What a timing adds up to: requests, the time they took in all, how long the first byte took on average, and the tools. */
export function timingTotals(timing: RunTiming): { requests: number; modelMs: number; firstByteMs: number | undefined; toolCalls: number; toolMs: number; failed: number; queued: number; queuedMs: number } {
  const first = timing.steps.flatMap((step) => (step.firstByteMs === undefined ? [] : [step.firstByteMs]))
  const tools = timing.steps.flatMap((step) => step.tools ?? [])
  return {
    requests: timing.steps.length + timing.omitted,
    modelMs: timing.steps.reduce((total, step) => total + (step.durationMs ?? 0), 0),
    firstByteMs: first.length === 0 ? undefined : Math.round(first.reduce((total, ms) => total + ms, 0) / first.length),
    toolCalls: tools.length,
    toolMs: tools.reduce((total, tool) => total + tool.ms, 0),
    failed: timing.steps.filter((step) => step.outcome !== 'complete').length,
    queued: timing.steps.filter((step) => step.queuedMs !== undefined).length,
    queuedMs: timing.steps.reduce((total, step) => total + (step.queuedMs ?? 0), 0)
  }
}
