import { describe, expect, it } from 'vitest'
import type { AgentRun } from '@judgment-pack/harness/agent'
import type { AssistantEvent, ModelCall } from '@judgment-pack/harness/engine'
import { joinTiming, MAX_STEPS, RunMeter, timingTotals, type RunTiming } from './runMeter'

/** A clock the test moves. */
function clock() {
  let now = 0
  return { now: () => now, tick: (ms: number) => { now += ms } }
}

/** A streamed answer whose chunks the test releases one at a time. */
function stream() {
  let controller!: ReadableStreamDefaultController<Uint8Array>
  const body = new ReadableStream<Uint8Array>({ start: (c) => { controller = c } })
  return { body, send: (text: string) => controller.enqueue(new TextEncoder().encode(text)), end: () => controller.close() }
}

describe('a model request, measured', () => {
  it('records the model, the bytes sent, the time to the first byte and to the end, and the status', async () => {
    const time = clock()
    const meter = new RunMeter(time.now)
    const answer = stream()
    const call = meter.call(async () => { time.tick(120); return new Response(answer.body, { status: 200 }) }, 'a-model')
    const response = await call('chat/completions', { method: 'POST', body: JSON.stringify({ messages: ['é'] }) })
    time.tick(380)
    const reader = response.body!.getReader()
    answer.send('data: one')
    await reader.read()
    time.tick(1000)
    answer.send('data: two')
    answer.end()
    while (!(await reader.read()).done);
    expect(meter.snapshot()).toEqual({ omitted: 0, steps: [{ model: 'a-model', requestBytes: 19, firstByteMs: 500, durationMs: 1500, status: 200, outcome: 'complete' }] })
  })

  it('passes the answer on whole: its status, its headers and its bytes', async () => {
    const meter = new RunMeter(clock().now)
    const call = meter.call(async () => new Response('whole answer', { status: 429, statusText: 'Too Many', headers: { 'Retry-After': '2' } }), 'a-model')
    const response = await call('chat/completions', { method: 'POST', body: '{}' })
    expect([response.status, response.statusText, response.headers.get('Retry-After'), response.ok]).toEqual([429, 'Too Many', '2', false])
    expect(await response.text()).toBe('whole answer')
    expect(meter.snapshot().steps[0]).toMatchObject({ status: 429, outcome: 'failed' })
  })

  it('records a request that never answered, with the time it ran, and one the engine stopped reading', async () => {
    const time = clock()
    const meter = new RunMeter(time.now)
    const refused = meter.call(async () => { time.tick(50); throw new Error('network down') }, 'a-model')
    await expect(refused('chat/completions', { method: 'POST', body: '{}' })).rejects.toThrow('network down')
    const abandoned = stream()
    const reading = meter.call(async () => new Response(abandoned.body), 'a-model')
    const response = await reading('chat/completions', { method: 'POST', body: '{}' })
    abandoned.send('x')
    const reader = response.body!.getReader()
    await reader.read()
    time.tick(70)
    await reader.cancel()
    expect(meter.snapshot().steps).toMatchObject([
      { outcome: 'failed', durationMs: 50 },
      { outcome: 'aborted', firstByteMs: 0, durationMs: 70 }
    ])
  })

  it('counts a request still open as it stands, and leaves the snapshot out of its own bookkeeping', async () => {
    const time = clock()
    const meter = new RunMeter(time.now)
    const open = stream()
    await meter.call(async () => new Response(open.body), 'a-model')('chat/completions', { method: 'POST', body: '{}' })
    time.tick(90)
    const first = meter.snapshot()
    expect(first.steps[0]).toMatchObject({ outcome: 'aborted', durationMs: 90 })
    first.steps[0]!.model = 'changed'
    expect(meter.snapshot().steps[0]!.model).toBe('a-model')
  })
})

describe('the tools a model called', () => {
  const call = (name: string, callId?: string): AssistantEvent => ({ type: 'tool_call', name, args: {}, ...(callId ? { callId } : {}) })
  const result = (name: string, callId?: string, isError = false): AssistantEvent => ({ type: 'tool_result', name, isError, text: '', ...(callId ? { callId } : {}) })

  it('times each from its call to its result, under the request that asked for it, paired by id', async () => {
    const time = clock()
    const meter = new RunMeter(time.now)
    const fake: ModelCall = async () => new Response('{}')
    await meter.call(fake, 'a-model')('chat/completions', { method: 'POST', body: '{}' })
    meter.event(call('get_schema', 'a'))
    meter.event(call('validate', 'b'))
    time.tick(300)
    meter.event(result('validate', 'b'))
    time.tick(200)
    meter.event(result('get_schema', 'a', true))
    expect(meter.snapshot().steps[0]!.tools).toEqual([{ name: 'validate', ms: 300 }, { name: 'get_schema', ms: 500, failed: true }])
  })

  it('pairs by name in order where the session gives no id', async () => {
    const time = clock()
    const meter = new RunMeter(time.now)
    await meter.call(async () => new Response('{}'), 'a-model')('chat/completions', { method: 'POST', body: '{}' })
    meter.event(call('read_link'))
    time.tick(100)
    meter.event(call('read_link'))
    time.tick(100)
    meter.event(result('read_link'))
    time.tick(100)
    meter.event(result('read_link'))
    expect(meter.snapshot().steps[0]!.tools).toEqual([{ name: 'read_link', ms: 200 }, { name: 'read_link', ms: 200 }])
  })

  it('ignores a result with no call, and every other event', () => {
    const meter = new RunMeter(clock().now)
    meter.event(result('validate', 'x'))
    meter.event({ type: 'message', text: 'Hello' })
    expect(meter.snapshot()).toEqual({ steps: [], omitted: 0 })
  })
})

describe('a subscription run, measured', () => {
  it('is one step, to its first event and to its end, with the time each tool took', async () => {
    const time = clock()
    const meter = new RunMeter(time.now)
    const run: AgentRun = async (_request, callbacks) => {
      time.tick(400)
      await callbacks.event({ type: 'text', id: '1', text: 'x' })
      time.tick(100)
      await callbacks.tool({ id: 'c', name: 'validate', arguments: {} }, callbacks.signal)
      time.tick(50)
    }
    await meter.agent(run, 'an-agent')(
      { prompt: 'p', instructions: 'ii', tools: [], phase: 'author' },
      { signal: new AbortController().signal, event: async () => {}, tool: async () => { time.tick(250); return { text: '', isError: false } } }
    )
    expect(meter.snapshot().steps).toEqual([{ model: 'an-agent', requestBytes: 3, firstByteMs: 400, durationMs: 800, outcome: 'complete', tools: [{ name: 'validate', ms: 250 }] }])
  })

  it('records a run that failed, or was stopped, as it ended', async () => {
    const meter = new RunMeter(clock().now)
    const stopped = new AbortController()
    stopped.abort()
    await expect(meter.agent(async () => { throw new Error('stopped') }, 'an-agent')({ prompt: 'p', instructions: '', tools: [], phase: 'author' }, { signal: stopped.signal, event: async () => {}, tool: async () => ({ text: '', isError: false }) })).rejects.toThrow()
    await expect(meter.agent(async () => { throw new Error('failed') }, 'an-agent')({ prompt: 'p', instructions: '', tools: [], phase: 'author' }, { signal: new AbortController().signal, event: async () => {}, tool: async () => ({ text: '', isError: false }) })).rejects.toThrow()
    expect(meter.snapshot().steps.map((step) => step.outcome)).toEqual(['aborted', 'failed'])
  })
})

describe('what a timing adds up to', () => {
  const step = (more: Partial<RunTiming['steps'][number]>): RunTiming['steps'][number] => ({ model: 'm', requestBytes: 100, outcome: 'complete', ...more })

  it('sums the time the requests took, averages the first byte, and sums the tools', () => {
    const timing: RunTiming = { omitted: 0, steps: [step({ firstByteMs: 400, durationMs: 2000, tools: [{ name: 'a', ms: 100 }, { name: 'b', ms: 300 }] }), step({ firstByteMs: 800, durationMs: 4000 }), step({ outcome: 'failed', status: 503, durationMs: 100 })] }
    expect(timingTotals(timing)).toEqual({ requests: 3, modelMs: 6100, firstByteMs: 600, toolCalls: 2, toolMs: 400, failed: 1 })
  })

  it('keeps the latest steps and counts the ones it dropped, across turns', () => {
    const many: RunTiming = { omitted: 0, steps: Array.from({ length: MAX_STEPS }, () => step({ durationMs: 1 })) }
    const joined = joinTiming(many, { omitted: 2, steps: [step({ durationMs: 5 }), step({ durationMs: 6 })] })
    expect(joined.steps).toHaveLength(MAX_STEPS)
    expect(joined.omitted).toBe(4)
    expect(joined.steps.at(-1)!.durationMs).toBe(6)
    expect(timingTotals(joined).requests).toBe(MAX_STEPS + 4)
    expect(joinTiming(undefined, many)).toBe(many)
  })
})
