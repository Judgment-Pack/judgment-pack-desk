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
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import type { AIRegistry } from './aiConnections'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { scriptedWebSocket } from '@judgment-pack/harness/certification/scriptedServer'
import { scenario } from '@judgment-pack/harness/certification'
import { canonicalProposal, frozen, plain, useAssistantRun } from './useAssistantRun'
import type { AssistantEvent, AssistantSession, Engine } from '@judgment-pack/harness/engine'

/** What the engine was handed, for the case that is about the model. */
let handed: AssistantSession | null = null

/** An engine that records its session and ends at once. */
const records: Engine = {
  id: 'vercel',
  async *start(session: AssistantSession): AsyncGenerator<AssistantEvent> {
    handed = session
    yield { type: 'end' }
  }
}

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

vi.mock('@judgment-pack/harness/engines/index', async (importOriginal) => {
  const original = await importOriginal<typeof import('@judgment-pack/harness/engines/index')>()
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

function drive(model = ENDPOINT.model) {
  runtime = scriptedWebSocket({})
  vi.stubGlobal('WebSocket', runtime.WebSocket)
  vi.stubGlobal('fetch', async () => new Response('{}'))
  return renderHook(() =>
    useAssistantRun({ endpoint: ENDPOINT, model, engine: 'vercel', thinking: 'off' })
  )
}

const ends = (events: AssistantEvent[]) => events.filter((event) => event.type === 'end')

beforeEach(() => {
  engine = deaf
  handed = null
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  runtime = null
  window.sessionStorage.clear()
})

describe('the model a run is given', () => {
  it('is the one the caller picked, and never the endpoint s own default', async () => {
    // **Which of the enabled models a piece of work wants is decided at the
    // run.** A hook that reached past the pick for the file's default would be
    // running something nobody chose, under a picker showing something else.
    engine = records
    const { result } = drive('a-second-model')
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(handed).not.toBeNull())
    expect(handed!.model.model).toBe('a-second-model')
    // The family is still the endpoint's: the pick names a model, not a wire.
    expect(handed!.model.family).toBe(ENDPOINT.kind)
  })

  it('refuses to run at all where nothing is enabled', async () => {
    // The empty string is the state the schema has, and it is the one state a
    // run cannot carry: a request naming no model is a request whose answer
    // means nothing. The tab offers no run there, and this is the layer under.
    engine = records
    const { result } = drive('')
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(result.current.status).toBe('finished'))
    expect(handed).toBeNull()
    expect(result.current.failure).toContain('no model chosen yet')
  })
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

it('generates briefs without opening runtime tools or a socket', async () => {
  engine = records
  const socket = vi.fn(() => { throw Error('A brief must not open MCP') })
  vi.stubGlobal('WebSocket', socket)
  vi.stubGlobal('fetch', async () => new Response('{}'))
  const { result } = renderHook(() => useAssistantRun({ purpose: 'brief', endpoint: ENDPOINT, model: ENDPOINT.model, engine: 'vercel', thinking: 'off' }))
  expect(handed).toBeNull()
  act(() => result.current.start('Summarize the retained snapshot.'))
  await waitFor(() => expect(result.current.status).toBe('finished'))
  expect(socket).not.toHaveBeenCalled()
  expect(handed?.purpose).toBe('brief')
  expect(handed?.tools).toEqual([])
  expect(handed?.adversarialReview).toBe(false)
  await expect(handed!.callTool('evaluate', {})).rejects.toThrow('no tool capability')
})

it('graph authoring keeps the ToolGate ceiling and adds only the two read-only host tools', async () => {
  engine = records
  runtime = scriptedWebSocket({})
  vi.stubGlobal('WebSocket', runtime.WebSocket)
  vi.stubGlobal('fetch', async () => new Response('{}'))
  const { graphHostTools } = await import('../graphs/author')
  const hostTools = graphHostTools()
  const { result } = renderHook(() => useAssistantRun({ purpose: 'graph', endpoint: ENDPOINT, model: ENDPOINT.model, engine: 'vercel', thinking: 'ultra', hostTools }))
  act(() => result.current.start('The runtime author_graph prompt.'))
  await waitFor(() => expect(result.current.status).toBe('finished'))
  expect(handed?.purpose).toBe('graph')
  expect(handed?.adversarialReview).toBe(false)
  expect(handed?.hostTools.map(tool => tool.name)).toEqual(['graph_validate', 'graph_explain'])
  expect(handed?.tools.map(tool => tool.name).sort()).toEqual([...scenario.scenarioTools].sort())
  await expect(handed!.callTool('write_file', {})).rejects.toThrow()
  await expect(handed!.callTool('packs_lock', {})).rejects.toThrow()
})

it('pins the provided connection and revision instead of resolving the desk default again',async()=>{
 engine=records
 const calls:Headers[]=[]
 vi.stubGlobal('fetch',async(_url:unknown,init?:RequestInit)=>{calls.push(new Headers(init?.headers));return Response.json({})})
 const other={id:'ai-222222222222222222222222',name:'Default',revision:'b'.repeat(64),enabled:true,assistant:{engine:'vercel' as const,thinking:'off' as const,endpoint:ENDPOINT}}
 const registry:AIRegistry={version:1,defaultConnection:other.id,connections:[other],path:'registry',sha256:'c'.repeat(64)}
 const effective={...effectiveConfig(undefined),aiConnections:{loading:false,data:registry}}
 const target={connectionId:'ai-111111111111111111111111',connectionRevision:'a'.repeat(64)}
 const {result}=renderHook(()=>useAssistantRun({target,purpose:'brief',endpoint:ENDPOINT,model:ENDPOINT.model,engine:'vercel',thinking:'off'}),{wrapper:({children})=><DeskConfigFixture value={effective}>{children}</DeskConfigFixture>})
 act(()=>result.current.start('Make a brief'))
 await waitFor(()=>expect(handed).not.toBeNull())
 await handed!.model.call('chat/completions',{method:'POST',body:'{}'})
 expect(calls.at(-1)?.get('X-Assistant-Connection')).toBe(target.connectionId)
 expect(calls.at(-1)?.get('X-Assistant-Revision')).toBe(target.connectionRevision)
})


it('uses explicit run settings when the connection slot also carries default engine settings', async () => {
  engine = records
  const target = {connectionName:'Test connection', endpoint:null, engine:'codex'}
  const {result}=renderHook(()=>useAssistantRun({target,purpose:'brief',endpoint:ENDPOINT,model:ENDPOINT.model,engine:'vercel',thinking:'off'}))
  act(()=>result.current.start('Design tests'))
  await waitFor(()=>expect(result.current.status).toBe('finished'))
  expect(result.current.failure).toBeUndefined()
  expect(handed?.model.model).toBe(ENDPOINT.model)
})

describe('what a run is told of the critic and of project memory', () => {
  const memoryReply = (use: boolean) => ({ sha256: 'a'.repeat(64), content: { version: 1, settings: { use, suggest: false }, entries: [] } })
  const run = async (options: Partial<Parameters<typeof useAssistantRun>[0]>, memory?: boolean) => {
    engine = records
    runtime = scriptedWebSocket({})
    vi.stubGlobal('WebSocket', runtime.WebSocket)
    vi.stubGlobal('fetch', async (url: unknown) => String(url).includes('/api/ai-memory') && memory !== undefined ? Response.json(memoryReply(memory)) : new Response('{}'))
    const { result } = renderHook(() => useAssistantRun({ endpoint: ENDPOINT, model: ENDPOINT.model, engine: 'vercel', thinking: 'off', ...options }))
    act(() => result.current.start('the runtime’s prompt'))
    await waitFor(() => expect(handed).not.toBeNull())
    return handed!
  }

  it('asks for the critic where the panes ran it: the API engine at any thinking tier but off, never Codex', async () => {
    expect((await run({ thinking: 'off' })).adversarialReview).toBe(false)
    handed = null; cleanup()
    expect((await run({ thinking: 'on' })).adversarialReview).toBe(true)
    handed = null; cleanup()
    // Codex carries its own tier and never the critic.
    expect((await run({ engine: 'codex', endpoint: null, agent: { model: 'a-model', provider: 'openai', authMethod: 'subscription', tools: scenario.scenarioTools } as never, thinking: 'on' })).adversarialReview).toBe(false)
  })

  it('hands the critic the cases it was given, on the default purpose alone', async () => {
    const cases = [{ source: 'test' as const, id: 'c1', facts: { claim: 60 }, expectedDisposition: { kind: 'decline', reasons: [], handoff: null } }]
    expect((await run({ thinking: 'on', criticCases: cases })).criticCases).toEqual(cases)
    handed = null; cleanup()
    expect(await run({ thinking: 'on' })).not.toHaveProperty('criticCases')
    handed = null; cleanup()
    expect(await run({ purpose: 'test-design', thinking: 'on', criticCases: cases })).not.toHaveProperty('criticCases')
  })

  it('asks for no critic on the purposes that never had one', async () => {
    for (const purpose of ['test-design', 'graph'] as const) {
      expect((await run({ purpose, thinking: 'on' })).adversarialReview, purpose).toBe(false)
      handed = null; cleanup()
    }
  })

  it('tells the harness whether project memory is on, so the reserve is left out of a run that has none', async () => {
    expect((await run({}, false)).context?.memory).toBe(false)
    handed = null; cleanup()
    expect((await run({}, true)).context?.memory).toBe(true)
    handed = null; cleanup()
    // A setting that cannot be read is left unsaid, and the harness keeps the reserve.
    expect((await run({})).context).not.toHaveProperty('memory')
  })
})
