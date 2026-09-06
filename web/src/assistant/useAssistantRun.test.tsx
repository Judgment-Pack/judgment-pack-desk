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
import { useAssistantRun } from './useAssistantRun'
import type { AssistantEvent, AssistantSession, Engine } from './engine'

/** An engine that yields nothing, ends never, and ignores its abort signal. */
const deaf: Engine = {
  id: 'builtin',
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
  id: 'builtin',
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
  tools: scenario.scenarioTools
}

let runtime: ReturnType<typeof scriptedWebSocket> | null = null

function drive() {
  runtime = scriptedWebSocket({})
  vi.stubGlobal('WebSocket', runtime.WebSocket)
  vi.stubGlobal('fetch', async () => new Response('{}'))
  return renderHook(() =>
    useAssistantRun({ endpoint: ENDPOINT, engine: 'builtin', thinking: 'off' })
  )
}

const ends = (events: AssistantEvent[]) => events.filter((event) => event.type === 'end')

beforeEach(() => {
  engine = deaf
  window.sessionStorage.setItem('jpack-desk-token', 'a-token')
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
    await waitFor(() => expect(runtime!.closed).toBeGreaterThan(0))
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
    // be able to keep one alive past the pane that started it.
    await waitFor(() => expect(runtime!.closed).toBeGreaterThan(0))
  })
})
