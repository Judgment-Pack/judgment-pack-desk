/**
 * The run hook's terminal event, against an engine that will not cooperate.
 *
 * The pane's own suite drives Stop against the built-in engine, which honours
 * its abort signal and yields its `end` from its own `finally` — so the hook
 * could have written nothing at all and those cases would still have passed.
 * That is what round 2 found: the mutation that removed the hook's `finish` did
 * not discriminate, because the engine was doing the hook's job for it.
 *
 * The engine here ignores the signal and never settles. Nothing but the hook
 * can end that session, so what is asserted is the hook, alone.
 */
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { scriptedWebSocket } from './conformance/scriptedServer'
import scenario from './conformance/scenario.json'
import { canonicalProposal, frozen, plain, useAssistantRun } from './useAssistantRun'
import type { AssistantEvent, AssistantSession, Engine } from './engine'

/** An engine that yields nothing, ends never, and ignores its abort signal. */
const deaf: Engine = {
  id: 'vercel',
  start(_session: AssistantSession): AsyncIterable<AssistantEvent> {
    return {
      [Symbol.asyncIterator]() {
        return {
          // Never resolves. Not even on abort — that is the whole fixture.
          next: () => new Promise<IteratorResult<AssistantEvent>>(() => {})
        }
      }
    }
  }
}

/** An engine that yields its own `end` and then throws. */
const endsThenThrows: Engine = {
  id: 'vercel',
  async *start(_session: AssistantSession): AsyncGenerator<AssistantEvent> {
    yield { type: 'end' }
    throw new Error('an engine that kept going after it said it had stopped')
  }
}

let engine: Engine = deaf

vi.mock('./engines', async (importOriginal) => {
  const original = await importOriginal<typeof import('./engines')>()
  return { ...original, loadEngine: async () => engine }
})

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible' as const,
  model: 'a-model',
  models: ['a-model'],
  tools: scenario.scenarioTools
}

let runtime: ReturnType<typeof scriptedWebSocket> | null = null

function drive() {
  runtime = scriptedWebSocket({})
  vi.stubGlobal('WebSocket', runtime.WebSocket)
  vi.stubGlobal('fetch', async () => new Response('{}'))
  return renderHook(() =>
    useAssistantRun({
      endpoint: ENDPOINT,
      model: ENDPOINT.model,
      engine: 'vercel',
      thinking: 'off'
    })
  )
}

const ends = (events: AssistantEvent[]) => events.filter((event) => event.type === 'end')

beforeEach(() => {
  engine = deaf
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  runtime = null
  window.sessionStorage.clear()
})

describe('the run hook writes the terminal event itself', () => {
  it('Stop ends a session whose engine ignores the abort and never settles', async () => {
    const { result } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(runtime!.opened).toHaveLength(1))
    // The engine will never end this. Only the hook can.
    expect(ends(result.current.events)).toHaveLength(0)
    act(() => result.current.stop())
    expect(ends(result.current.events)).toHaveLength(1)
    expect(result.current.status).toBe('finished')
    // Exactly once: an engine that ignores its signal must not be able to make
    // the hook close the same socket twice either.
    await waitFor(() => expect(runtime!.closed).toBe(1))
  })

  it('a second Stop adds nothing', async () => {
    const { result } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(runtime!.opened).toHaveLength(1))
    act(() => result.current.stop())
    act(() => result.current.stop())
    act(() => result.current.stop())
    expect(ends(result.current.events)).toHaveLength(1)
  })

  it('an engine that yields end and then throws still ends exactly once', async () => {
    engine = endsThenThrows
    const { result } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(result.current.status).toBe('finished'))
    expect(ends(result.current.events)).toHaveLength(1)
  })

  it('closes the connection when a stopped engine never notices', async () => {
    const { result, unmount } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(runtime!.opened).toHaveLength(1))
    unmount()
    // The socket is a `jpack mcp`; an engine that ignores its signal must not
    // be able to keep one alive past the pane that started it. Once.
    await waitFor(() => expect(runtime!.closed).toBe(1))
  })

  it('accounts for the terminal event on unmount, where nobody is left to render one', async () => {
    // **The unmount path, measured.** `setEvents` on a tree that is going away
    // is a no-op and the array it would have produced is never rendered, so an
    // assertion about `events` after an unmount can say nothing — which is why
    // the row for this was retired as unobservable. The counter is the
    // observation: a reference taken while the hook is alive, read after it is
    // not.
    const { result, unmount } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(runtime!.opened).toHaveLength(1))
    const terminals = result.current.terminals
    expect(terminals.count).toBe(0)
    unmount()
    expect(terminals.count, 'the unmount ended the run without accounting for it').toBe(1)
  })

  it('accounts for exactly one where Stop is followed by an unmount', async () => {
    const { result, unmount } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(runtime!.opened).toHaveLength(1))
    const terminals = result.current.terminals
    act(() => result.current.stop())
    expect(terminals.count).toBe(1)
    unmount()
    expect(terminals.count, 'the unmount ended an already ended run again').toBe(1)
    await waitFor(() => expect(runtime!.closed).toBe(1))
  })
})

describe('the proposal is canonicalized once, where it arrives', () => {
  /** An engine that puts one value on the stream and ends. */
  const emits = (event: AssistantEvent): Engine => ({
    id: 'vercel',
    // eslint-disable-next-line require-yield
    async *start(): AsyncGenerator<AssistantEvent> {
      yield event
      yield { type: 'end' }
    }
  })

  it('reads a live document exactly once, and puts plain data on the stream', async () => {
    let reads = 0
    const document = {
      get title() {
        reads += 1
        return `title ${reads}`
      }
    }
    engine = emits({ type: 'proposal', document, unknowns: ['one'] })
    const { result } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(ends(result.current.events)).toHaveLength(1))
    const proposal = result.current.events.find((event) => event.type === 'proposal')
    expect(reads).toBe(1)
    expect((proposal as { document: unknown }).document).toEqual({ title: 'title 1' })
    // And what is on the stream is inert: reading it again cannot move it.
    expect((proposal as { document: { title: string } }).document.title).toBe('title 1')
    expect(reads).toBe(1)
  })

  it('refuses a document that cannot be read as JSON data, and proposes nothing', async () => {
    const document: Record<string, unknown> = {}
    document.self = document
    engine = emits({ type: 'proposal', document, unknowns: [] })
    const { result } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(ends(result.current.events)).toHaveLength(1))
    expect(result.current.events.find((event) => event.type === 'proposal')).toBeUndefined()
    const failure = result.current.events.find((event) => event.type === 'error')
    expect((failure as { message: string }).message).toContain('could not be read as JSON data')
  })

  it('refuses a proposal whose document is not an object', () => {
    for (const document of [null, [1, 2], 7, 'a pack', undefined]) {
      const held = canonicalProposal({ type: 'proposal', document, unknowns: [] })
      expect(held.type).toBe('error')
      expect((held as { message: string }).message).toContain('no document object')
    }
  })

  it('keeps the unknowns and the critique, as data', () => {
    const held = canonicalProposal({
      type: 'proposal',
      document: { a: 1 },
      unknowns: ['one', 'two'],
      critique: { refuted: false }
    })
    expect(held).toEqual({
      type: 'proposal',
      document: { a: 1 },
      unknowns: ['one', 'two'],
      critique: { refuted: false }
    })
  })
})

describe('what ingestion hands on cannot be moved afterwards', () => {
  const emits = (event: AssistantEvent): Engine => ({
    id: 'vercel',
    async *start(): AsyncGenerator<AssistantEvent> {
      yield event
      yield { type: 'end' }
    }
  })

  it('freezes the document all the way down, so nothing between diff and accept can move it', async () => {
    engine = emits({
      type: 'proposal',
      document: { title: 'as proposed', rules: [{ id: 'a', when: { value: '5000' } }] },
      unknowns: ['one']
    })
    const { result } = drive()
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(ends(result.current.events)).toHaveLength(1))
    const proposal = result.current.events.find((event) => event.type === 'proposal') as {
      document: { title: string; rules: { id: string; when: { value: string } }[] }
      unknowns: string[]
    }
    // A consumer holding the public event cannot reach into it between the
    // memoised diff and the accept: the diff would describe one document and
    // the writer write another.
    expect(Object.isFrozen(proposal.document)).toBe(true)
    expect(Object.isFrozen(proposal.document.rules)).toBe(true)
    expect(Object.isFrozen(proposal.document.rules[0]!.when)).toBe(true)
    expect(Object.isFrozen(proposal.unknowns)).toBe(true)
    expect(() => {
      proposal.document.rules[0]!.when.value = '9999'
    }).toThrow(TypeError)
    expect(() => {
      proposal.document.title = 'something else'
    }).toThrow(TypeError)
    expect(() => proposal.document.rules.push({ id: 'b', when: { value: '1' } })).toThrow(TypeError)
    expect(proposal.document).toEqual({
      title: 'as proposed',
      rules: [{ id: 'a', when: { value: '5000' } }]
    })
  })

  it('canonicalizes and freezes as two functions with one job each', () => {
    expect(plain({ a: 1, b: undefined, c: () => 1 })).toEqual({ a: 1 })
    expect(plain(undefined)).toBeUndefined()
    const cyclic: Record<string, unknown> = {}
    cyclic.self = cyclic
    expect(plain(cyclic)).toBeUndefined()
    // A getter is read once, and what comes back is inert.
    let reads = 0
    const held = plain({
      get title() {
        reads += 1
        return `title ${reads}`
      }
    })
    expect(held).toEqual({ title: 'title 1' })
    expect(reads).toBe(1)

    const deep = frozen({ a: { b: [{ c: 1 }] } })
    expect(Object.isFrozen(deep.a.b[0])).toBe(true)
    expect(frozen('a string')).toBe('a string')
    expect(frozen(null)).toBeNull()
  })
})
