import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { AssistantEvent, CallTool, McpToolResult } from '../assistant/engine'
import { Ledger } from './ledger'
import { AuthoringRun, admitCases, factPaths, matrixDocument, researchRecord, traceCitations, type RunPorts, type RunState, type TurnRequest } from './run'
import { researchTools } from './tools'
import { TEST_PUBLIC_KEY, fakeGateway } from './__fixtures__/fakeGateway'

const answers = JSON.parse(readFileSync(join(import.meta.dirname, '__fixtures__', 'provider-answers.json'), 'utf8')) as Record<string, unknown>
const PUBLIC_KEY = TEST_PUBLIC_KEY
const PAGE_URL = 'https://www.canada.ca/en/immigration-refugees-citizenship/services/immigrate-canada/express-entry/eligibility/federal-skilled-workers.html'

const PACK = {
  specVersion: '0.2.0-draft',
  id: 'https://example.org/packs/fswp-screening',
  version: '0.1.0',
  title: 'FSWP minimum requirements screening',
  decision: { intent: 'screen', question: 'Does the applicant meet the FSWP minimum requirements?' },
  sources: [
    {
      id: 'ircc-fswp',
      title: 'Express Entry: Federal Skilled Worker Program',
      locator: { kind: 'uri', value: PAGE_URL },
      publisher: 'Immigration, Refugees and Citizenship Canada',
      publishedAt: '2024-12-13',
      citation: { location: 'src-1#e1', excerpt: "We don't count any hours you work above 30 hours/week." }
    }
  ],
  outcomes: [
    { id: 'meets', label: 'Meets the minimum requirements' },
    { id: 'does-not-meet', label: 'Does not meet the minimum requirements' }
  ],
  rules: [
    {
      id: 'hours',
      description: 'At least 1,560 hours',
      when: { op: 'fact', path: '/work/hours', operator: 'greater-than-or-equal', value: '1560' },
      outcome: 'meets',
      onUnknown: 'escalate',
      sourceRefs: ['ircc-fswp']
    }
  ],
  fallbackOutcome: 'does-not-meet',
  escalation: { triggers: ['unknown'], target: { kind: 'human-role', name: 'Screening officer' } }
}

const CASES = {
  cases: [
    { id: 'meets-hours', facts: { work: { hours: '1560' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }, expectationSource: 'src-1#e1', rationale: 'at the threshold' },
    { id: 'under-hours', facts: { work: { hours: '1559' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'does-not-meet', reasons: ['no-match'], handoff: { state: 'none' } }, expectationSource: 'src-1#e1', rationale: 'just under' },
    { id: 'hours-missing', facts: {}, expectedDisposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'requested', triggeredBy: ['unknown'] } }, expectationSource: 'src-1#e1', rationale: 'missing fact escalates' },
    { id: 'ungrounded', facts: {}, expectedDisposition: { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }, expectationSource: 'src-9#e9', rationale: 'no excerpt' }
  ]
}

type Script = (request: TurnRequest, signal: AbortSignal, onEvent: (event: AssistantEvent) => void) => Promise<void>

/** A fake runtime: validate is valid, evaluate decides by the hours threshold the candidate states. */
function fakeRuntime(): { callTool: CallTool; calls: string[] } {
  const calls: string[] = []
  const callTool: CallTool = async (name, args): Promise<McpToolResult> => {
    calls.push(name)
    if (name === 'validate') return { structuredContent: { status: 'valid', diagnostics: [] } }
    if (name === 'experimental_evaluate') {
      if (args.rehearsal !== true) throw new Error('rehearsal not set')
      const pack = JSON.parse(args.pack as string) as typeof PACK
      const facts = JSON.parse(args.facts as string) as { work?: { hours?: string } }
      const threshold = Number(pack.rules[0]!.when.value)
      const hours = facts.work?.hours
      const disposition =
        hours === undefined
          ? { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'requested', triggeredBy: ['unknown'] } }
          : Number(hours) >= threshold
            ? { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }
            : { kind: 'outcome', outcomeId: 'does-not-meet', reasons: ['no-match'], handoff: { state: 'none' } }
      return { structuredContent: { status: 'evaluated', rehearsal: true, disposition } }
    }
    throw new Error(`unexpected tool ${name}`)
  }
  return { callTool, calls }
}

function harness(scripts: Script[], overrides: Partial<RunPorts> = {}) {
  const ledger = new Ledger('unset')
  const gateway = fakeGateway()
  const logged: string[] = []
  const tools = researchTools({
    config: {
      gateway: { url: 'http://127.0.0.1:8787', authority: gateway.authority, signer: { algorithm: 'ed25519', public: PUBLIC_KEY } },
      sources: { search: { source: 'search', dialect: 'tavily-search' }, read: { source: 'read', dialect: 'jina-reader' } },
      limits: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 }
    },
    ledger,
    budget: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 },
    spent: { searches: 0, reads: 0, bytes: 0, startedAt: Date.now() },
    log: (line) => logged.push(line),
    acquire: async (session, source, args) => gateway.acquire(session, source, (answers[source === 'search' ? 'tavilySearch' : 'jinaReader'] as { body: unknown }).body, args)
  })
  const runtime = fakeRuntime()
  let turns = 0
  let sessions = 0
  const requests: TurnRequest[] = []
  const sealed: string[] = []
  const ports: RunPorts = {
    turn: async (request, signal, onEvent) => {
      requests.push(request)
      const script = scripts[Math.min(turns, scripts.length - 1)]!
      turns += 1
      await script(request, signal, onEvent)
    },
    callTool: runtime.callTool,
    ledger,
    researchTools: tools,
    seal: async (session) => {
      sealed.push(session)
      await gateway.seal(session)
    },
    registry: () => gateway.registry(),
    gateway: { authority: gateway.authority, publicKeyHex: PUBLIC_KEY },
    newSession: () => `s${++sessions}`,
    authorPrompt: 'AUTHOR PROMPT',
    maxRevisions: 2,
    log: (line) => logged.push(line),
    ...overrides
  }
  const run = new AuthoringRun(ports)
  return { run, ledger, logged, requests, sealed, runtime }
}

async function settled(run: AuthoringRun, timeout = 5000): Promise<RunState> {
  const started = Date.now()
  for (;;) {
    const state = run.getSnapshot()
    if (state.status !== 'running' && state.status !== 'idle') return state
    if (Date.now() - started > timeout) throw new Error(`still ${state.status} in ${state.phase}`)
    await new Promise((resolve) => setTimeout(resolve, 5))
  }
}

/** The model's research turn: reads the page through the tool, cites, proposes. */
const researchTurn =
  (document: unknown = PACK, prose = 'I read the IRCC page and encoded the hours threshold.'): Script =>
  async (request, signal, onEvent) => {
    const read = request.hostTools.find((tool) => tool.name === 'read_source')!
    onEvent({ type: 'tool_call', name: 'read_source', args: { url: PAGE_URL } })
    const answer = await read.execute({ url: PAGE_URL }, signal)
    onEvent({ type: 'tool_result', name: 'read_source', isError: false, text: answer.content![0]!.text! })
    const cite = request.hostTools.find((tool) => tool.name === 'cite_excerpt')!
    const cited = await cite.execute({ source_id: 'src-1', quote: "We don't count any hours you work above 30 hours/week." }, signal)
    onEvent({ type: 'tool_result', name: 'cite_excerpt', isError: false, text: cited.content![0]!.text! })
    onEvent({ type: 'message', text: prose })
    onEvent({ type: 'proposal', document, unknowns: ['Whether student work experience counts is not settled by the page.'] })
    onEvent({ type: 'end' })
  }

const casesTurn: Script = async (request, _signal, onEvent) => {
  expect(request.reviewer).toBe(true)
  expect(request.hostTools).toEqual([])
  expect(request.prompt).toContain('src-1#e1')
  expect(request.prompt).toContain('"/work/hours"')
  onEvent({ type: 'message', text: 'Four cases from the excerpt.' })
  onEvent({ type: 'proposal', document: CASES, unknowns: [] })
  onEvent({ type: 'end' })
}

describe('the authoring run', () => {
  it('researches, cites, establishes cases from excerpts, checks, and is ready', async () => {
    const { run, ledger, logged, sealed, runtime } = harness([researchTurn(), casesTurn])
    run.start('Screen applicants against the FSWP minimum requirements.', [PAGE_URL])
    const state = await settled(run)
    expect(state.status).toBe('ready')
    expect(state.phase).toBe('review')
    expect(state.candidates).toHaveLength(1)
    expect(state.candidates[0]!.check?.valid).toBe(true)
    expect(state.candidates[0]!.check?.cases.map((c) => [c.id, c.passed])).toEqual([
      ['meets-hours', true],
      ['under-hours', true],
      ['hours-missing', true]
    ])
    expect(state.cases.map((c) => c.id)).toEqual(['meets-hours', 'under-hours', 'hours-missing'])
    expect(state.droppedCases).toEqual([{ id: 'ungrounded', reason: 'expectationSource "src-9#e9" is not an excerpt recorded in this run' }])
    expect(state.unknowns).toEqual(['Whether student work experience counts is not settled by the page.'])
    expect(state.turns.map((t) => [t.role, t.kind])).toEqual([
      ['user', 'brief'],
      ['assistant', 'message'],
      ['assistant', 'unknowns'],
      ['assistant', 'message'],
      ['assistant', 'note']
    ])
    // Sources were verified under the pinned key, session by session.
    expect(sealed).toEqual(['s1'])
    expect(ledger.byId('src-1')!.verification).toMatchObject({ state: 'verified', keyId: 'ddb406e95cad582adc111a7d6fbff25d' })
    expect(state.verdicts.s1?.ok).toBe(true)
    // The citation traced to the recorded excerpt.
    expect(state.citations).toEqual([{ sourceId: 'ircc-fswp', location: 'src-1#e1', excerptId: 'src-1#e1', url: PAGE_URL, traced: true, reason: '' }])
    // The runtime was asked, in rehearsal, once per case after one validate.
    expect(runtime.calls).toEqual(['validate', 'experimental_evaluate', 'experimental_evaluate', 'experimental_evaluate'])
    expect(logged.some((line) => line.startsWith('verify: s1 — verified'))).toBe(true)
    // What a created pack carries beside it.
    const matrix = matrixDocument(state, ledger) as { matrixVersion: string; cases: { id: string; cites?: unknown[] }[] }
    expect(matrix.matrixVersion).toBe('3')
    expect(matrix.cases).toHaveLength(3)
    expect(matrix.cases[0]!.cites).toEqual([{ sessionId: 's1', callIndex: 0, signature: expect.stringMatching(/^[0-9a-f]{128}$/) }])
    const record = researchRecord(state, ledger, 'abc') as { sources: { id: string; receipt?: unknown; verification: unknown }[]; packSha256: string }
    expect(record.packSha256).toBe('abc')
    expect(record.sources[0]).toMatchObject({ id: 'src-1', verification: { state: 'verified' } })
  })

  it('repairs a candidate that disagrees, never rewriting a case, and stops at the budget', async () => {
    const wrong = { ...PACK, rules: [{ ...PACK.rules[0]!, when: { ...PACK.rules[0]!.when, value: '1600' } }] }
    let repairs = 0
    const repairTurn: Script = async (request, _signal, onEvent) => {
      repairs += 1
      expect(request.prompt).toContain('REPAIR')
      expect(request.prompt).toContain('"passed":false')
      onEvent({ type: 'message', text: `repair ${repairs}` })
      // The first repair still disagrees; the second lands on the source's threshold.
      onEvent({ type: 'proposal', document: repairs === 1 ? { ...wrong, version: '0.1.1' } : PACK, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const { run } = harness([researchTurn(wrong), casesTurn, repairTurn, repairTurn])
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    expect(state.status).toBe('ready')
    expect(state.candidates.map((c) => [c.revision, c.producedBy, c.check?.cases.filter((x) => x.passed).length])).toEqual([
      [1, 'research', 2],
      [2, 'repair', 2],
      [3, 'repair', 3]
    ])
    expect(state.revisionsUsed).toBe(2)
    expect(state.cases).toHaveLength(3)

    // With a budget of one, the same run stops short and says so.
    const short = harness([researchTurn(wrong), casesTurn, repairTurn], { maxRevisions: 1 })
    repairs = 0
    short.run.start('brief', [PAGE_URL])
    const stopped = await settled(short.run)
    expect(stopped.status).toBe('budget')
    expect(stopped.detail).toContain('revision budget of 1')
  })

  it('reports a repeated candidate as stalled', async () => {
    const wrong = { ...PACK, rules: [{ ...PACK.rules[0]!, when: { ...PACK.rules[0]!.when, value: '1600' } }] }
    const sameAgain: Script = async (_request, _signal, onEvent) => {
      onEvent({ type: 'proposal', document: wrong, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const { run } = harness([researchTurn(wrong), casesTurn, sameAgain])
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    expect(state.status).toBe('stalled')
    expect(state.candidates).toHaveLength(1)
  })

  it('stops at the last completed stage, and reports an engine error as failed', async () => {
    const slow: Script = (_request, signal) =>
      new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('stopped', 'AbortError'))))
    const { run } = harness([slow])
    run.start('brief', [])
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(run.getSnapshot().status).toBe('running')
    run.stop()
    const state = await settled(run)
    expect(state.status).toBe('stopped')
    expect(state.candidates).toEqual([])

    const failing: Script = async (_request, _signal, onEvent) => {
      onEvent({ type: 'error', message: 'the endpoint answered 429' })
      onEvent({ type: 'end' })
    }
    const failed = harness([failing])
    failed.run.start('brief', [])
    expect((await settled(failed.run)).detail).toBe('the endpoint answered 429')
  })

  it('needs input where no case could be grounded', async () => {
    const noCases: Script = async (_request, _signal, onEvent) => {
      onEvent({ type: 'proposal', document: { cases: [] }, unknowns: ['Which excerpt states the threshold?'] })
      onEvent({ type: 'end' })
    }
    const { run } = harness([researchTurn(), noCases])
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    expect(state.status).toBe('needs-input')
    expect(state.turns.at(-1)).toMatchObject({ kind: 'unknowns' })
  })

  it('answers a message without changing the document, and rechecks one that does', async () => {
    const answerOnly: Script = async (request, _signal, onEvent) => {
      expect(request.prompt).toContain("THE PERSON'S MESSAGE\nWhy 1,560?")
      onEvent({ type: 'message', text: 'Because the page says a year of full-time work is 1,560 hours.' })
      onEvent({ type: 'proposal', document: PACK, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const changed: Script = async (_request, _signal, onEvent) => {
      onEvent({ type: 'message', text: 'Renamed the pack.' })
      onEvent({ type: 'proposal', document: { ...PACK, title: 'FSWP screening' }, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const { run, runtime } = harness([researchTurn(), casesTurn, answerOnly, changed])
    run.start('brief', [PAGE_URL])
    await settled(run)
    const before = runtime.calls.length
    run.send('Why 1,560?')
    let state = await settled(run)
    expect(state.status).toBe('ready')
    expect(state.candidates).toHaveLength(1)
    expect(state.turns.at(-1)).toMatchObject({ role: 'assistant', kind: 'message', text: expect.stringContaining('1,560') })
    expect(runtime.calls.length).toBe(before)
    run.send('Call it FSWP screening')
    state = await settled(run)
    expect(state.status).toBe('ready')
    expect(state.candidates).toHaveLength(2)
    expect(state.candidates[1]!.producedBy).toBe('conversation')
    expect(runtime.calls.length).toBeGreaterThan(before)
  })

  it('marks sources failed where the registry cannot be fetched, or the key is not pinned', async () => {
    const { run, ledger } = harness([researchTurn(), casesTurn], {
      registry: async () => {
        throw new Error('research-relay-upstream')
      }
    })
    run.start('brief', [PAGE_URL])
    await settled(run)
    expect(ledger.byId('src-1')!.verification).toMatchObject({ state: 'failed', findings: [{ status: 'seal-or-registry-unavailable' }] })
    const unpinned = harness([researchTurn(), casesTurn], { gateway: null })
    unpinned.run.start('brief', [PAGE_URL])
    await settled(unpinned.run)
    expect(unpinned.ledger.byId('src-1')!.verification).toMatchObject({ state: 'failed', findings: [{ status: 'no-pinned-key' }] })
  })
})

describe('citation tracing and case admission', () => {
  it('traces only an excerpt the run recorded, with its text and URL', () => {
    const ledger = new Ledger('s1')
    ledger.open('page', { source: 'read', dialect: 'jina-reader', url: 'https://a.example/p' })
    ledger.settle('src-1', {
      response: { text: '', result: { kind: 'null' }, receipt: { kind: 'null' }, salts: {} },
      document: { url: 'https://a.example/p', title: 'P', text: 'A claim of $50 or less is approved.', pageDates: {} }
    })
    ledger.cite('src-1', 'A claim of $50 or less')
    const doc = (citation: unknown, value = 'https://a.example/p') => ({ sources: [{ id: 'p', locator: { kind: 'uri', value }, citation }] })
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim   of $50 or less' }), ledger)[0]!.traced).toBe(true)
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'something else' }), ledger)[0]!.reason).toContain('not the recorded excerpt text')
    expect(traceCitations(doc({ location: 'src-1#e2', excerpt: 'x' }), ledger)[0]!.reason).toContain('no excerpt src-1#e2')
    expect(traceCitations(doc({ location: 'section 4', excerpt: 'x' }), ledger)[0]!.reason).toContain('no excerpt id')
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim of $50 or less' }, 'https://b.example/'), ledger)[0]!.reason).toContain('not the URL')
    expect(traceCitations({ sources: 'no' }, ledger)).toEqual([])
    expect(factPaths(PACK)).toEqual(['/work/hours'])
  })
  it('never admits a case that would rewrite an established one', () => {
    const ledger = new Ledger('s1')
    const established = [CASES.cases[0]!] as never
    const { admitted, dropped } = admitCases({ cases: [{ ...CASES.cases[0]!, facts: { work: { hours: '1' } } }, { id: 'Bad Id' }] }, ledger, established)
    expect(admitted).toEqual([])
    expect(dropped.map((d) => d.reason)).toEqual(['a case with this id is already established and is never rewritten', 'the id is not kebab-case'])
    expect(admitCases(null, ledger, []).dropped[0]!.reason).toContain('without a cases array')
  })
})
