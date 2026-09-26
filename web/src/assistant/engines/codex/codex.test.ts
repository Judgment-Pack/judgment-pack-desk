import { afterEach, describe, expect, it, vi } from 'vitest'
import { codex, type CodexSession } from './index'
import type { AgentRun } from '../../agent'
import type { AssistantEvent } from '../../engine'
import { scriptedRuntime } from '../../conformance/scriptedServer'
import recording from '../../conformance/runtime.json'
import scenario from '../../conformance/scenario.json'
import { openAssistantConnection } from '../../session'
import { CERTIFIED_ENGINES, AGENT_ENGINES, loadEngine } from '../index'

const proposal = (document: unknown = { id: 'pack' }) => 'Ready.\n\n```json\n' + JSON.stringify({ proposal: { document, unknowns: ['Who reviews?'] } }) + '\n```'
function session(run: AgentRun, changes: Partial<CodexSession> = {}): CodexSession {
  return { prompt: 'Author a pack', testPrompt: 'Test the pack', tools: [], hostTools: [], callTool: vi.fn(),
    signal: new AbortController().signal, agent: { model: 'model', run }, ...changes }
}
async function collect(s: CodexSession) {
  const events: AssistantEvent[] = []
  for await (const event of codex.start(s)) events.push(event)
  return events
}
const reply = (text: string): AgentRun => async (_request, callbacks) => {
  await callbacks.event({ type: 'text', id: 'message', text })
  await callbacks.event({ type: 'message', id: 'message', text, phase: 'final' })
}
afterEach(() => vi.unstubAllGlobals())

describe.each(AGENT_ENGINES)('agent engine %s', engineId => {
  it('keeps the full recorded runtime scenario behind the real ToolGate, including its critic', async () => {
    const runtime = await scriptedRuntime()
    const events: AssistantEvent[] = []
    const connection = openAssistantConnection({ allowed: scenario.scenarioTools, transport: runtime.transport, onEvent: event => events.push(event) })
    try {
      const loaded = await loadEngine(engineId)
      const ready = await connection.ready
      let passes = 0
      const run: AgentRun = async (request, callbacks) => {
        passes++
        expect(request.phase).toBe(passes === 1 ? 'author' : 'critic')
        expect(request.tools.map(tool => tool.inputSchema)).toEqual(ready.tools.map(tool => tool.inputSchema))
        const calls = passes === 1 ? recording.calls : recording.calls.filter(call => call.step === 'T5' || call.step === 'T6')
        for (const [index, call] of calls.entries()) {
          const route = ready.tools.findIndex(tool => tool.name === call.tool)
          const args = structuredClone(call.arguments) as Record<string, unknown>
          if (call.tool === 'experimental_evaluate') args.rehearsal = false
          await callbacks.tool({ id: `p${passes}-c${index}`, name: `desk_runtime_${route}`, arguments: args }, callbacks.signal)
        }
        const text = passes === 1 ? proposal(scenario.documents.DRAFT_V2) : 'I claim this was refuted.'
        await callbacks.event({ type: 'message', id: `p${passes}`, text, phase: 'final' })
      }
      for (const name of ['fetch', 'WebSocket', 'XMLHttpRequest', 'EventSource']) {
        vi.stubGlobal(name, () => { throw new Error('Adapter touched a network global') })
      }
      for await (const event of loaded.start(session(run, { ...ready, adversarialReview: true }))) events.push(event)
      expect(events.filter(event => event.type === 'error')).toEqual([])
      expect(passes).toBe(2)
      expect(events.find(event => event.type === 'proposal')).toMatchObject({ document: scenario.documents.DRAFT_V2 })
      expect(events.find(event => event.type === 'critique')).toMatchObject({ refuted: false })
      const evaluations = runtime.seen.filter(call => call.name === 'experimental_evaluate')
      expect(evaluations).toHaveLength(2)
      expect(evaluations.every(call => call.args.rehearsal === true && call.refusal === '')).toBe(true)
      const guardIndex = events.findIndex(event => event.type === 'guardrail' && event.tool === 'experimental_evaluate')
      expect(events[guardIndex - 1]?.type).toBe('tool_call')
      expect(events[guardIndex + 1]?.type).toBe('tool_result')
      expect(events.filter(event => event.type === 'end')).toHaveLength(1)
    } finally {
      vi.unstubAllGlobals()
      await connection.close()
      await runtime.close()
    }
  })

  it('streams prose while keeping proposal JSON hidden and uses the shared parser', async () => {
    const events = await collect(session(reply(proposal()), { interactive: true }))
    expect(events.find(event => event.type === 'message_progress')).toEqual({ type: 'message_progress', text: 'Ready.' })
    expect(events.find(event => event.type === 'proposal')).toMatchObject({ document: { id: 'pack' }, unknowns: ['Who reviews?'] })
    expect(events.at(-1)?.type).toBe('end')
  })

  it.each(['brief', 'test-design'] as const)('preserves the %s workflow and explicit language instructions', async purpose => {
    let instructions = ''
    const events = await collect(session(async (request, callbacks) => {
      instructions = request.instructions
      await reply(proposal())(request, callbacks)
    }, { purpose, replyLanguage: 'fr' }))
    expect(instructions).toContain(purpose === 'brief' ? 'one-page brief' : 'design tests')
    expect(instructions.toLowerCase()).toContain('french')
    expect(events.some(event => event.type === 'proposal')).toBe(true)
  })

  it('does not manufacture proposals or claim verification for an ordinary chat reply', async () => {
    const events = await collect(session(reply('Hello.'), { allowConversation: true }))
    expect(events).toEqual([{ type: 'message', text: 'Hello.' }, { type: 'end' }])
  })

  it('executes a host tool with cancellation and returns structured content without treating it as a runtime check', async () => {
    const execute = vi.fn(async () => ({ structuredContent: { source: 'verified-document' } }))
    const events = await collect(session(async (request, callbacks) => {
      const answer = await callbacks.tool({ id: 'call', name: request.tools[0]!.name, arguments: {} }, callbacks.signal)
      expect(JSON.parse(answer.text)).toMatchObject({ structuredContent: { source: 'verified-document' } })
      await reply(proposal())(request, callbacks)
    }, { hostTools: [{ name: 'read_source', description: 'Read', inputSchema: { type: 'object' }, execute }] }))
    expect(execute).toHaveBeenCalledOnce()
    expect(events.find(event => event.type === 'tool_result')).toMatchObject({ structured: { source: 'verified-document' } })
  })

  it('refuses colliding or unoffered tools and never falls back to another engine', async () => {
    const run = vi.fn(reply(proposal()))
    const tool = { name: 'validate', inputSchema: { type: 'object' } }
    const events = await collect(session(run, { tools: [tool], hostTools: [{ ...tool, description: '', execute: vi.fn() }] }))
    expect(run).not.toHaveBeenCalled()
    expect(events[0]?.type).toBe('error')
    const callTool = vi.fn()
    const refused = await collect(session(async (_r, c) => {
      await c.tool({ id: 'unknown', name: 'write_file', arguments: {} }, c.signal)
    }, { callTool }))
    expect(callTool).not.toHaveBeenCalled()
    expect(refused[0]?.type).toBe('error')
    expect(CERTIFIED_ENGINES).toContain(engineId)
  })

  it('reports an unverified critique when the critic only speaks', async () => {
    const events = await collect(session(reply(proposal()), { adversarialReview: true }))
    expect(events.find(event => event.type === 'critique')).toMatchObject({ checks: [] })
    expect(events.find(event => event.type === 'proposal')).not.toHaveProperty('critique')
  })

  it('stops before executing a tool when the consumer leaves at tool_call', async () => {
    const callTool = vi.fn()
    const iterator = codex.start(session(async (request, c) => {
      await c.tool({ id: 'call', name: request.tools[0]!.name, arguments: {} }, c.signal)
    }, { tools: [{ name: 'validate', inputSchema: { type: 'object' } }], callTool }))[Symbol.asyncIterator]()
    expect((await iterator.next()).value?.type).toBe('tool_call')
    await iterator.return?.()
    await Promise.resolve()
    expect(callTool).not.toHaveBeenCalled()
  })

  it('cancels a pending native capability and accepts no late output', async () => {
    const stop = new AbortController()
    let entered!: () => void
    const started = new Promise<void>(resolve => { entered = resolve })
    const events: AssistantEvent[] = []
    const promise = (async () => {
      for await (const event of codex.start(session(async (_r, c) => {
        entered()
        await new Promise<void>(resolve => c.signal.addEventListener('abort', () => resolve(), { once: true }))
        await c.event({ type: 'message', id: 'late', text: proposal(), phase: 'final' })
      }, { signal: stop.signal }))) events.push(event)
    })()
    await started
    stop.abort()
    await promise
    expect(events).toEqual([])
  })
})
