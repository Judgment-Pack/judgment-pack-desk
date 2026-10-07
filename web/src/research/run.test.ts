import { composeMessageInput } from '../chat/messageInput'
import { readFileSync, writeFileSync } from 'node:fs'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { AssistantEvent, CallTool, McpToolResult } from '../assistant/engine'
import { Ledger } from './ledger'
import { AuthoringRun, admitCases, canCreateDraft, canCreateResearchDraft, canEstablishCases, documentBasis, factPaths, statementsOf, matrixDocument, readinessKey, researchRecord, traceCitations, type Candidate, type RunPorts, type RunState, type TurnRequest } from './run'
import { digestOf, type AuthoringCase, type CandidateCheck } from './checkCandidate'
import { researchTools } from './tools'
import { TEST_PUBLIC_KEY, fakeGateway } from './__fixtures__/fakeGateway'
import { fixtureExpectations } from './__fixtures__/expectationRuntime'
import recordedFindings from './__fixtures__/expectations.json'
import { EXPECTATION_TOOL } from './expectations'
import { parseJsonText } from './verify/canon'
import { canRetryExpectationValidation, INITIAL_STATE } from './run'
import type { AuthoringMode } from './mode'
import type { ChatAttachment } from '../chat/store'
import type { VerifiedDocument } from '../documents/client'
import type { DocumentRecord } from '../documents/record'
import type { ResearchGatewayConfig } from '../config/deskConfig'
import webSnapshot from '../documents/__fixtures__/web-snapshot.json'

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

/** The same pack citing nothing, as a draft resting on what the person said. */
const UNCITED = {
  ...Object.fromEntries(Object.entries(PACK).filter(([key]) => key !== 'sources')),
  rules: PACK.rules.map(({ sourceRefs: _refs, ...rule }) => rule)
}

/** The rule asked with the basis the state's citations were traced on, for a test that moves nothing. */
const onItsBasis = (state: RunState, mode: AuthoringMode) => canCreateDraft(state, mode, state.tracedBasis ?? '')

const MEETS = { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }
const MISSES = { kind: 'outcome', outcomeId: 'does-not-meet', reasons: [], handoff: { state: 'none' } }
/** Cases a chat draft's reviewer writes from the person's first message. */
const STATEMENT_CASES = { cases: [
  { id: 'at-the-threshold', facts: { work: { hours: '1560' } }, expectedDisposition: MEETS, expectationSource: 'you-1', rationale: 'You said 1,560 hours qualify.' },
  { id: 'just-under', facts: { work: { hours: '1559' } }, expectedDisposition: MISSES, expectationSource: 'you-1', rationale: 'One hour short does not.' }
] }
/** The reviewer turn of a chat draft: no tools, and the given cases. */
const chatReviewer = (cases: unknown = STATEMENT_CASES): Script => async (request, _signal, emit) => {
  expect(request.reviewer).toBe(true)
  expect(request.hostTools).toEqual([])
  emit({ type: 'proposal', document: cases, unknowns: [] })
}

const CASES = {
  cases: [
    { id: 'meets-hours', facts: { work: { hours: '1560' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }, expectationSource: 'src-1#e1', rationale: 'at the threshold' },
    { id: 'under-hours', facts: { work: { hours: '1559' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'does-not-meet', reasons: [], handoff: { state: 'none' } }, expectationSource: 'src-1#e1', rationale: 'just under' },
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
    if (name === EXPECTATION_TOOL) return fixtureExpectations(args)
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
            : { kind: 'outcome', outcomeId: 'does-not-meet', reasons: [], handoff: { state: 'none' } }
      return { structuredContent: { status: 'evaluated', rehearsal: true, disposition } }
    }
    throw new Error(`unexpected tool ${name}`)
  }
  return { callTool, calls }
}

/**
 * Resolve one of the record's own digest-legend keys against the record, in the
 * language the legend states it is in: JSON Pointer, `*` for any array index.
 *
 * The legend is machine-readable, so it is only worth what it corresponds to. A
 * key that resolves to nothing names a member the record does not have, which is
 * worse than the source comment the legend replaced -- a wrong legend is read as
 * authority. `reached` is false when a segment is missing; a `*` over an empty
 * array reaches nothing legitimately and returns no values.
 */
function legendPath(record: unknown, pointer: string): { reached: boolean; values: unknown[] } {
  let nodes: unknown[] = [record]
  for (const segment of pointer.split('/').slice(1)) {
    const next: unknown[] = []
    for (const node of nodes) {
      if (segment === '*') {
        if (!Array.isArray(node)) return { reached: false, values: [] }
        next.push(...node)
      } else {
        if (typeof node !== 'object' || node === null || !(segment in node)) return { reached: false, values: [] }
        next.push((node as Record<string, unknown>)[segment])
      }
    }
    nodes = next
  }
  return { reached: true, values: nodes }
}

const LEGEND_KEYS = ['/packSha256', '/checkedCandidateSha256', '/expectationIssues/*/proposal/candidateDigest']

function harness(scripts: Script[], overrides: Partial<RunPorts> = {}, tamper: (acquired: import('./gatewayClient').Acquired) => import('./gatewayClient').Acquired = (a) => a) {
  const ledger = new Ledger('unset')
  const gateway = fakeGateway()
  const logged: string[] = []
  const tools = researchTools({
    config: {
      gateway: { url: 'http://127.0.0.1:8787', authority: gateway.authority, signer: { algorithm: 'ed25519', public: PUBLIC_KEY } },
      sources: { search: { source: 'search', dialect: 'tavily-search' }, read: { source: 'read', dialect: 'jina-reader' }, web: null },
      limits: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 }
    },
    ledger,
    budget: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 },
    spent: { searches: 0, reads: 0, bytes: 0, startedAt: Date.now() },
    log: (line) => logged.push(line),
    acquire: async (session, source, args) => tamper(await gateway.acquire(session, source, (answers[source === 'search' ? 'tavilySearch' : 'jinaReader'] as { body: unknown }).body, args))
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
    seconds: 600,
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

/** Wait for something the run does while it is still running. */
async function until(reached: () => boolean, what: string, timeout = 5000): Promise<void> {
  const started = Date.now()
  while (!reached()) {
    if (Date.now() - started > timeout) throw new Error(`never reached: ${what}`)
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
    // The contract is asked once, at admission, and the case then carries the
    // canonical answer; the runtime was asked, in rehearsal, once per case
    // after one validate.
    expect(runtime.calls).toEqual([EXPECTATION_TOOL, 'validate', 'experimental_evaluate', 'experimental_evaluate', 'experimental_evaluate'])
    expect(logged.some((line) => line.startsWith('verify: s1 — verified'))).toBe(true)
    // What a created pack carries beside it.
    const matrix = matrixDocument(state, ledger) as { matrixVersion: string; cases: { id: string; cites?: unknown[] }[] }
    expect(matrix.matrixVersion).toBe('3')
    expect(matrix.cases).toHaveLength(3)
    expect(matrix.cases[0]!.cites).toEqual([{ sessionId: 's1', callIndex: 0, signature: expect.stringMatching(/^[0-9a-f]{128}$/) }])
    const record = researchRecord(state, ledger, 'abc') as {
      sources: { id: string; receipt?: unknown; verification: unknown; acquireResponse?: string }[]
      packSha256: string
      digests: { pathSyntax: string; means: Record<string, string> }
      registries: Record<string, string>
      verdicts: Record<string, { ok: boolean }>
    }
    expect(record.packSha256).toBe('abc')
    // An ordinary run names its digests too. The legend is the record saying
    // what its own digests are of, and a run with nothing to correct is the
    // common case -- one that shipped a record without the legend would leave
    // the reader exactly where the source comment left them.
    expect(record.digests.pathSyntax).toContain('JSON Pointer')
    expect(Object.keys(record.digests.means)).toEqual(LEGEND_KEYS)
    // And the legend is resolved against this record, not against itself: a
    // member renamed out from under it fails here rather than shipping a legend
    // that names something the record does not have.
    for (const key of LEGEND_KEYS) expect(legendPath(record, key).reached, key).toBe(true)
    expect(legendPath(record, '/packSha256').values).toEqual(['abc'])
    expect(legendPath(record, '/expectationIssues/*/proposal/candidateDigest').values).toEqual([]) // Nothing was corrected.
    expect(record.sources[0]).toMatchObject({ id: 'src-1', verification: { state: 'verified' } })
    // Enough to check the claim again: the acquire response as received and
    // the registry the verdict was reached with.
    expect(record.sources[0]!.acquireResponse).toContain('"receipt":')
    expect(record.registries.s1).toContain('"finalCount":1')
    expect(record.verdicts.s1!.ok).toBe(true)
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

  it('continues a turn whose steps ran out before the proposal, with what was read', async () => {
    let attempts = 0
    const spent: Script = async (request, signal, onEvent) => {
      attempts += 1
      if (attempts === 1) {
        // Reads and cites, then runs out of steps: the engine's own sentence.
        const read = request.hostTools.find((tool) => tool.name === 'read_source')!
        await read.execute({ url: PAGE_URL }, signal)
        const cite = request.hostTools.find((tool) => tool.name === 'cite_excerpt')!
        await cite.execute({ source_id: 'src-1', quote: "We don't count any hours you work above 30 hours/week." }, signal)
        onEvent({ type: 'error', message: 'the final message must carry exactly one fenced JSON block holding the proposal; this one carried 0' })
        onEvent({ type: 'end' })
        return
      }
      expect(request.prompt).toContain('CONTINUE')
      expect(request.prompt).toContain('SOURCES ALREADY READ AND CITED')
      expect(request.prompt).toContain('src-1#e1')
      expect(request.prompt).toContain('re-open with read_source source_id src-1')
      // A cached re-read costs no budget and makes no gateway call.
      const read = request.hostTools.find((tool) => tool.name === 'read_source')!
      const again = await read.execute({ source_id: 'src-1', offset: 0 }, signal)
      expect(again.isError).toBeUndefined()
      onEvent({ type: 'proposal', document: PACK, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const { run, ledger, sealed } = harness([spent, spent, casesTurn])
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    expect(state.status, state.detail).toBe('ready')
    expect(attempts).toBe(2)
    expect(state.turns.some((t) => t.kind === 'note' && t.text.includes('continuing (1 of 3)'))).toBe(true)
    // The source read in the spent turn was verified with that turn's session, and kept.
    expect(ledger.byId('src-1')!.verification.state).toBe('verified')
    expect(sealed).toEqual(['s1'])
    // A turn that keeps running out is reported as failed after the bound.
    const always: Script = async (_request, _signal, onEvent) => {
      onEvent({ type: 'error', message: 'the final message must carry exactly one fenced JSON block holding the proposal; this one carried 0' })
      onEvent({ type: 'end' })
    }
    const stuck = harness([always])
    stuck.run.start('brief', [])
    const failed = await settled(stuck.run)
    expect(failed.status).toBe('failed')
    expect(failed.turns.filter((t) => t.kind === 'note')).toHaveLength(3)
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

  it('withholds ready where a cited source’s receipt failed, and where a citation cannot be traced', async () => {
    // The reader's text is changed after the gateway signed it: the page
    // reads the changed text, cites it, and the verifier catches the artifact.
    const swapped = harness([researchTurn(), casesTurn], {}, (acquired) => {
      const text = acquired.text.replace('1,560 hours', '1,500 hours')
      const parsed = parseJsonText(text)
      const member = (name: string) => (parsed.kind === 'object' ? parsed.members.find((m) => m.name === name)!.value : parsed)
      return { ...acquired, text, result: member('result') }
    })
    swapped.run.start('brief', [PAGE_URL])
    const state = await settled(swapped.run)
    expect(state.status).toBe('needs-input')
    expect(state.detail).toContain('failed receipt verification')
    expect(swapped.ledger.byId('src-1')!.verification).toMatchObject({ state: 'failed', findings: [{ status: 'artifact-mismatch', callIndex: 0 }] })
    expect(state.cases).toEqual([])
    expect(state.droppedCases.filter((d) => d.id !== 'ungrounded').every((d) => d.reason.includes('did not verify'))).toBe(true)
    expect(state.droppedCases).toHaveLength(4)
    expect(state.citations[0]).toMatchObject({ traced: false, reason: expect.stringContaining('failed') })
    // A draft citing nothing is not ready either.
    const uncited = harness([researchTurn({ ...PACK, sources: [], rules: [{ ...PACK.rules[0]!, sourceRefs: [] }] }), casesTurn])
    uncited.run.start('brief', [PAGE_URL])
    const state2 = await settled(uncited.run)
    expect(state2.status).toBe('needs-input')
    expect(state2.detail).toContain('cites no source')
  })

  it('keeps a withheld candidate withheld through an unchanged answer to a message', async () => {
    const answerOnly: Script = async (_request, _signal, onEvent) => {
      onEvent({ type: 'message', text: 'It is fine.' })
      onEvent({ type: 'proposal', document: { ...PACK, sources: [], rules: [{ ...PACK.rules[0]!, sourceRefs: [] }] }, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const uncited = { ...PACK, sources: [], rules: [{ ...PACK.rules[0]!, sourceRefs: [] }] }
    const { run } = harness([researchTurn(uncited), casesTurn, answerOnly])
    run.start('brief', [PAGE_URL])
    let state = await settled(run)
    expect(state.status).toBe('needs-input')
    run.send('Is it ready?')
    state = await settled(run)
    expect(state.status).toBe('needs-input')
    expect(state.detail).toContain('cites no source')
  })

  it('keeps Create offered for the same candidate through a later stop and a failed turn, and withholds it mid-turn', async () => {
    // `ready` is the status of the last action, and neither a Stop nor a failed
    // follow-up turn touches the candidate, its cases or its citations. Reading
    // the status withdrew Create from a draft that had passed everything, and
    // said the cases disagreed, which was the one thing that was not true.
    // `running` is the one status the rule still keeps: a turn in flight is
    // about to move the candidate the readiness was recorded about.
    const hanging: Script = (_request, signal) =>
      new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('stopped', 'AbortError'))))
    const failing: Script = async (_request, _signal, event) => {
      event({ type: 'error', message: 'the provider refused the turn' })
      event({ type: 'end' })
    }
    const { run } = harness([researchTurn(), casesTurn, hanging, failing])
    run.start('brief', [PAGE_URL])
    const ready = await settled(run)
    expect(ready.status).toBe('ready')
    const digest = ready.candidates.at(-1)!.digest
    run.send('Anything else worth knowing?')
    expect(run.getSnapshot().status).toBe('running')
    expect(canCreateResearchDraft(run.getSnapshot())).toBe(false)
    run.stop()
    let state = await settled(run)
    expect(state.status).toBe('stopped')
    expect(state.candidates.at(-1)!.digest).toBe(digest)
    expect(canCreateResearchDraft(state)).toBe(true)
    run.send('And now?')
    state = await settled(run)
    expect(state.status).toBe('failed')
    expect(state.candidates.at(-1)!.digest).toBe(digest)
    expect(canCreateResearchDraft(state)).toBe(true)
  })

  it('withdraws Create where a later turn’s receipt fails, even with the draft and its citations unchanged', async () => {
    // The one withheld reason the recorded readiness cannot carry: the failed
    // source is not one the draft cites, so nothing in the candidate, the cases
    // or the trace moves. The turn ends in an error, so no settle says it --
    // and the outcome the person is shown is the engine's refusal, so the
    // withdrawal has to say why it withdrew or nothing does.
    let acquisitions = 0
    const readAgain: Script = async (request, signal, event) => {
      const read = request.hostTools.find(tool => tool.name === 'read_source')!
      await read.execute({ url: PAGE_URL + '?annex' }, signal)
      const cite = request.hostTools.find(tool => tool.name === 'cite_excerpt')!
      await cite.execute({ source_id: 'src-2', quote: "We don't count any hours you work above 30 hours/week." }, signal)
      event({ type: 'error', message: 'the provider refused the turn' })
      event({ type: 'end' })
    }
    const { run, ledger } = harness([researchTurn(), casesTurn, readAgain], {}, (acquired) => {
      acquisitions += 1
      if (acquisitions !== 2) return acquired
      const text = acquired.text.replace('1,560 hours', '1,500 hours')
      const parsed = parseJsonText(text)
      const member = (name: string) => (parsed.kind === 'object' ? parsed.members.find((m) => m.name === name)!.value : parsed)
      return { ...acquired, text, result: member('result') }
    })
    run.start('brief', [PAGE_URL])
    expect((await settled(run)).status).toBe('ready')
    run.send('Read the annex too.')
    const state = await settled(run)
    expect(state.status).toBe('failed')
    expect(ledger.byId('src-1')!.verification.state).toBe('verified')
    expect(ledger.byId('src-2')!.verification.state).toBe('failed')
    expect(state.citations.every(citation => citation.traced)).toBe(true)
    expect(canCreateResearchDraft(state)).toBe(false)
    expect(state.detail).toContain('the provider refused the turn')
    expect(state.detail).toContain('failed receipt verification (src-2)')
  })

  it('holds receipts in the order it opened them, so a reordered or relabelled answer fails', async () => {
    // The gateway answers the second read with the first read's receipt and
    // vice versa: each receipt is genuine, but neither is at the position the
    // desk asked for, and the verifier says so.
    const answers: import('./gatewayClient').Acquired[] = []
    const scripted: Script = async (request, signal, onEvent) => {
      const read = request.hostTools.find((tool) => tool.name === 'read_source')!
      await read.execute({ url: PAGE_URL }, signal)
      await read.execute({ url: PAGE_URL + '?second' }, signal)
      onEvent({ type: 'proposal', document: PACK, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const { run, ledger } = harness([scripted, casesTurn], {}, (acquired) => {
      answers.push(acquired)
      // Hand the first call the second receipt when it exists: swap by
      // returning the previous answer for the second call and the second for
      // the first is impossible in one pass, so swap the receipt members.
      if (answers.length === 2) {
        const [first, second] = answers
        return { ...second!, receipt: first!.receipt, result: first!.result }
      }
      return acquired
    })
    run.start('brief', [PAGE_URL])
    await settled(run)
    expect(ledger.byId('src-2')!.verification.state).toBe('failed')
    expect(ledger.byId('src-1')!.verification.state).toBe('failed')
  })

  it('stops a run at its time budget and reports it as budget, not stopped', async () => {
    const slow: Script = (_request, signal) =>
      new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('stopped', 'AbortError'))))
    const { run } = harness([slow], { seconds: 0.05 })
    run.start('brief', [])
    const state = await settled(run)
    expect(state.status).toBe('budget')
    expect(state.detail).toContain('time budget')
  })

  it('keeps an admitted case its own: the reviewer’s object can no longer reach it', async () => {
    const proposal = structuredClone(CASES)
    const mutating: Script = async (_request, _signal, onEvent) => {
      onEvent({ type: 'proposal', document: proposal, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const { run } = harness([researchTurn(), mutating])
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    ;(proposal.cases[0]!.expectedDisposition as { outcomeId: string }).outcomeId = 'changed'
    expect((state.cases[0]!.expectedDisposition as { outcomeId: string }).outcomeId).toBe('meets')
    expect(Object.isFrozen(state.cases[0]!.facts)).toBe(true)
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
    const doc = (citation: unknown, value: unknown = 'https://a.example/p') => ({ sources: [{ id: 'p', locator: { kind: 'uri', value }, citation }] })
    // Unchecked: nothing traces until the receipt has verified.
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim of $50 or less' }), ledger)[0]!.reason).toContain('has not been checked')
    ledger.verified('src-1', { state: 'verified', at: 't', keyId: 'k' })
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim   of $50 or less' }), ledger)[0]!.traced).toBe(true)
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'something else' }), ledger)[0]!.reason).toContain('not the recorded excerpt text')
    expect(traceCitations(doc({ location: 'src-1#e2', excerpt: 'x' }), ledger)[0]!.reason).toContain('no excerpt src-1#e2')
    expect(traceCitations(doc({ location: 'section 4', excerpt: 'x' }), ledger)[0]!.reason).toContain('no excerpt id')
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim of $50 or less' }, 'https://b.example/'), ledger)[0]!.reason).toContain('not the URL')
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim of $50 or less' }, 42), ledger)[0]!.reason).toContain('not a string URL')
    expect(traceCitations({ sources: [{ id: 'p', citation: { location: 'src-1#e1', excerpt: 'A claim of $50 or less' } }] }, ledger)[0]!.traced).toBe(false)
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

const INVALID_CASES = structuredClone(CASES)
INVALID_CASES.cases[2]!.expectedDisposition.reasons = []
const blockedCasesTurn: Script = async (_request, _signal, event) => {
  event({ type: 'proposal', document: INVALID_CASES, unknowns: [] })
  event({ type: 'end' })
}
const correctionTurn = (expectedDisposition: unknown = CASES.cases[2]!.expectedDisposition, unknowns: string[] = []): Script => async (request, _signal, event) => {
  expect(request.reviewer).toBe(true)
  expect(request.hostTools).toEqual([])
  expect(request.prompt).toContain('SOURCE EXCERPT')
  expect(request.prompt).not.toContain('LATEST CHECK')
  expect(request.prompt).not.toContain('"rules":')
  event({ type: 'proposal', document: { expectedDisposition, rationale: 'A missing fact retains unknown and the declared handoff trigger.' }, unknowns })
  event({ type: 'end' })
}

/**
 * A legal §8.3 shape no conforming pack can produce (the runtime's ADR-0037):
 * §8 step 1 is the only step that records `not-applicable`, and it reports the
 * halt under kind `not-applicable`, never `unresolved`. A runtime before that
 * ADR answered it valid, and a run then repaired a draft that could never agree.
 */
const UNREACHABLE = { kind: 'unresolved', reasons: ['not-applicable'], handoff: { state: 'none' } }
const UNREACHABLE_CASES = { cases: CASES.cases.map((row, index) => index === 2 ? { ...row, expectedDisposition: UNREACHABLE } : row) }
const UNREACHABLE_FINDING = recordedFindings.find(row => row.name === 'unresolved retaining not-applicable')!.result as { status: string; code: string; message: string }
/** What a person reads: the expectation is what must change, then the runtime's rule in its own words. */
const UNREACHABLE_SUMMARY = `This expectation names a disposition no pack can produce, so the expectation must change, not the draft: ${UNREACHABLE_FINDING.message}`
const unreachableCasesTurn: Script = async (_request, _signal, event) => {
  event({ type: 'proposal', document: UNREACHABLE_CASES, unknowns: [] })
  event({ type: 'end' })
}

async function blockedRun(scripts: Script[] = [], overrides: Partial<RunPorts> = {}) {
  const h = harness([researchTurn(), blockedCasesTurn, ...scripts], overrides)
  h.run.start('brief', [PAGE_URL])
  const state = await settled(h.run)
  expect(state.status, state.detail).toBe('needs-input')
  expect(state.expectationIssues).toHaveLength(1)
  return h
}

describe('invalid expectation review', () => {
  it('keeps the invalid case visible and never tests, repairs or marks the smaller suite ready', async () => {
    const answer: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: PACK, unknowns: [] })
      event({ type: 'end' })
    }
    const { run, runtime } = await blockedRun([answer])
    const state = run.getSnapshot()
    expect(state.cases).toHaveLength(2)
    expect(state.expectationIssues[0]!.original).toEqual(INVALID_CASES.cases[2])
    expect(state.droppedCases).toHaveLength(1) // Only the separately ungrounded case.
    expect(state.candidates[0]!.check).toBeUndefined()
    expect(state.revisionsUsed).toBe(0)
    expect(runtime.calls).toEqual([EXPECTATION_TOOL])
    expect(canCreateResearchDraft(state)).toBe(false)
    run.send('Is this ready?')
    expect((await settled(run)).status).toBe('needs-input')
    expect(runtime.calls).toEqual([EXPECTATION_TOOL])
  })

  it('proposes without applying, then requires the displayed approval to retest every case on unchanged bytes', async () => {
    const { run, runtime, ledger } = await blockedRun([correctionTurn()])
    const before = structuredClone(run.getSnapshot().candidates[0])!
    run.proposeExpectationCorrection('hours-missing')
    let state = await settled(run)
    const issue = state.expectationIssues[0]!
    expect(issue.proposal?.candidateDigest).toBe(before.digest)
    expect(issue.resolved).toBeUndefined()
    expect(state.cases).toHaveLength(2)
    expect(runtime.calls).not.toContain('experimental_evaluate')
    run.approveExpectationCorrection(issue.id, 'stale-token')
    expect(run.getSnapshot()).toBe(state)
    run.approveExpectationCorrection(issue.id, issue.proposal!.token)
    state = await settled(run)
    expect(state.status, state.detail).toBe('ready')
    expect(canCreateResearchDraft(state)).toBe(true)
    expect(state.candidates).toHaveLength(1)
    expect(state.candidates[0]).toMatchObject(before)
    expect(state.revisionsUsed).toBe(0)
    expect(state.cases).toHaveLength(3)
    expect(state.candidates[0]!.check?.cases).toHaveLength(3)
    expect(runtime.calls.filter(name => name === 'experimental_evaluate')).toHaveLength(3)
    const resolved = state.expectationIssues[0]!.resolved!
    expect(resolved.replacement).toEqual(CASES.cases[2])
    expect(state.expectationIssues[0]!.original).toEqual(INVALID_CASES.cases[2])
    expect(resolved.approvedAt).toBeTruthy()
    expect(researchRecord(state, ledger, before.digest)).toMatchObject({ expectationIssues: state.expectationIssues })
    // The record names its own digests, so the candidate a correction was
    // proposed against is never read as a second claim about the written pack.
    const record = researchRecord(state, ledger, before.digest) as { digests: { means: Record<string, string> }; matrixFocus: string }
    expect(Object.keys(record.digests.means)).toEqual(LEGEND_KEYS)
    expect(record.digests.means['/expectationIssues/*/proposal/candidateDigest']).toContain('proposed against')
    expect(record.digests.means['/checkedCandidateSha256']).toContain('checked against')
    expect(record.digests.means['/packSha256']).toContain('Create wrote')
    // On a corrected run every legend key reaches a digest actually in this
    // record. A legend is only worth the correspondence it holds.
    for (const key of LEGEND_KEYS) {
      const { reached, values } = legendPath(record, key)
      expect(reached, key).toBe(true)
      expect(values, key).toEqual([expect.stringMatching(/^[0-9a-f]{64}$/)])
    }
    // The corrected row is registered under the reason the correction gave, not
    // the superseded one it replaced, and keeps its source tag.
    const registered = (matrixDocument(state, ledger) as { cases: { id: string; focus: string }[] }).cases
    expect(registered.find(row => row.id === 'hours-missing')!.focus).toBe(`${resolved.rationale} [src-1#e1]`)
    expect(registered.find(row => row.id === 'hours-missing')!.focus).not.toContain(CASES.cases[2]!.rationale)
    expect(registered.find(row => row.id === 'meets-hours')!.focus).toBe(`${CASES.cases[0]!.rationale} [src-1#e1]`)
    // The two companions spell a corrected row's reason differently on purpose,
    // so the record says which one the matrix carries, and the sentence is held
    // against the matrix rather than left as prose: the reason it points at is
    // the reason the registered row is under.
    expect(record.matrixFocus).toContain('/expectationIssues/*/resolved/rationale')
    const [pointed] = legendPath(record, '/expectationIssues/*/resolved/rationale').values as string[]
    expect(registered.find(row => row.id === 'hours-missing')!.focus).toBe(`${pointed} [src-1#e1]`)
    run.approveExpectationCorrection(issue.id, issue.proposal!.token)
    expect(run.getSnapshot()).toBe(state) // Cannot apply twice.
    // No status, partial report, stale digest or missing id can bypass Create.
    const check = state.candidates[0]!.check!
    // slice(1) shifts every id, so the id rule catches it; slice(0, -1) is the
    // shape only the count rule catches -- a check taken before the approved
    // case joined the suite, on the same bytes and the same digest.
    for (const changed of [{ ...check, documentDigest: 'stale' }, { ...check, cases: check.cases.slice(1) }, { ...check, cases: check.cases.slice(0, -1) }, { ...check, cases: check.cases.map(row => ({ ...row, id: 'other' })) }]) {
      expect(canCreateResearchDraft({ ...state, candidates: [{ ...state.candidates[0]!, check: changed }] })).toBe(false)
    }
    expect(canCreateResearchDraft({ ...state, expectationIssues: [issue] })).toBe(false)
    // The recorded readiness is a key and is read back by comparison, so it
    // stands only for the candidate, the cases and the trace it was recorded
    // about. The trace is the one of the three no other rule here reads: move
    // a citation and Create lapses, with nobody having cleared anything.
    expect(state.readiness).not.toBe('')
    expect(state.citations.length).toBeGreaterThan(0)
    expect(canCreateResearchDraft({ ...state, citations: state.citations.map(citation => ({ ...citation, location: 'src-1#e9' })) })).toBe(false)
    expect(canCreateResearchDraft({ ...state, citations: [...state.citations, ...state.citations] })).toBe(false)
  })

  it.each([
    [INVALID_CASES.cases[2]!.expectedDisposition, []],
    [CASES.cases[2]!.expectedDisposition, ['The source does not settle this expectation.']]
  ])('keeps invalid or undetermined corrections blocked', async (disposition, unknowns) => {
    const { run } = await blockedRun([correctionTurn(disposition, unknowns as string[])])
    run.proposeExpectationCorrection('hours-missing')
    const state = await settled(run)
    expect(state.status).toBe('needs-input')
    expect(state.expectationIssues[0]!.proposal).toBeUndefined()
    expect(state.expectationIssues[0]!.proposalError).toBeTruthy()
    expect(state.cases).toHaveLength(2)
  })

  it('blocks an expectation no pack can produce as the expectation to change, and starts no repair round on it', async () => {
    // The issue list. Established, this case fails every rehearsal whatever the
    // draft says, so each repair turn would spend the budget on a draft that is
    // not what is wrong. It is held as an issue, worded as the expectation's
    // defect, and the draft is neither checked nor repaired.
    const { run, runtime, requests } = harness([researchTurn(), unreachableCasesTurn, correctionTurn()])
    run.start('brief', [PAGE_URL])
    let state = await settled(run)
    // No repair round: no revision spent, no turn after the reviewer's, no rehearsal.
    expect(state.revisionsUsed).toBe(0)
    expect(requests).toHaveLength(2)
    expect(runtime.calls).toEqual([EXPECTATION_TOOL])
    expect(state.candidates).toHaveLength(1)
    expect(state.candidates[0]!.check).toBeUndefined()
    expect(state.status, state.detail).toBe('needs-input')
    expect(state.phase).toBe('review')
    expect(state.expectationIssues.map(issue => [issue.id, issue.message])).toEqual([['hours-missing', UNREACHABLE_SUMMARY]])
    expect(state.expectationIssues[0]!.original.expectedDisposition).toEqual(UNREACHABLE)
    expect(state.cases.map(row => row.id)).toEqual(['meets-hours', 'under-hours'])
    // What it offers is the expectation's own repair: a reviewer who changes only
    // the expectation, told that the expectation is what must change.
    const draft = state.candidates[0]!
    run.proposeExpectationCorrection('hours-missing')
    state = await settled(run)
    expect(requests).toHaveLength(3)
    expect(requests[2]!.reviewer).toBe(true)
    expect(requests[2]!.prompt).toContain(`VALIDATOR FINDING\n${UNREACHABLE_SUMMARY}`)
    const issue = state.expectationIssues[0]!
    expect(issue.proposal?.expectedDisposition).toEqual(CASES.cases[2]!.expectedDisposition)
    run.approveExpectationCorrection(issue.id, issue.proposal!.token)
    state = await settled(run)
    expect(state.status, state.detail).toBe('ready')
    expect(state.revisionsUsed).toBe(0)
    expect(state.candidates).toHaveLength(1)
    expect(state.candidates[0]!.text).toBe(draft.text)
  })

  it('keeps a proposed correction no pack can produce blocked, and says the expectation must change again', async () => {
    // The proposal check: a reviewer's correction is put to the runtime before it
    // is offered, and one no pack can produce is refused in the same words.
    const { run, runtime, requests } = await blockedRun([correctionTurn(UNREACHABLE)])
    run.proposeExpectationCorrection('hours-missing')
    const state = await settled(run)
    expect(state.status, state.detail).toBe('needs-input')
    expect(state.expectationIssues[0]!.proposal).toBeUndefined()
    expect(state.expectationIssues[0]!.proposalError).toBe(UNREACHABLE_SUMMARY)
    expect(state.cases).toHaveLength(2)
    expect(state.revisionsUsed).toBe(0)
    expect(state.candidates).toHaveLength(1)
    expect(runtime.calls).not.toContain('experimental_evaluate')
    expect(requests).toHaveLength(3)
  })

  it('applies nothing where approval finds the correction is one no pack can produce', async () => {
    // The correction check: approval asks the runtime again, and a runtime that
    // moved on since the proposal can answer that no pack produces it. Nothing
    // is applied, the draft is not repaired, and the person reads why.
    const native = fakeRuntime()
    let moved = false
    const callTool: CallTool = async (name, args) => moved && name === EXPECTATION_TOOL
      ? { structuredContent: { specVersion: args.spec_version, status: 'invalid', results: [{ index: 0, ...UNREACHABLE_FINDING }] } }
      : native.callTool(name, args)
    const { run, requests } = await blockedRun([correctionTurn()], { callTool })
    run.proposeExpectationCorrection('hours-missing')
    const issue = (await settled(run)).expectationIssues[0]!
    expect(issue.proposal).toBeDefined()
    moved = true
    run.approveExpectationCorrection(issue.id, issue.proposal!.token)
    const state = await settled(run)
    expect(state.status).toBe('failed')
    expect(state.detail).toBe(`The correction is no longer valid: ${UNREACHABLE_SUMMARY}`)
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
    expect(state.cases).toHaveLength(2)
    expect(state.revisionsUsed).toBe(0)
    expect(state.candidates).toHaveLength(1)
    expect(native.calls).not.toContain('experimental_evaluate')
    expect(requests).toHaveLength(3)
  })

  it('blocks a saved case an earlier runtime admitted, when the pinned one finds no pack can produce it', async () => {
    // A reopened draft. A case established before the runtime reported this
    // shape is put to the runtime again on recheck, and becomes the issue it
    // always was -- not a rehearsal failure the person is sent to repair.
    const { checkpoint, decodeCheckpoint, restoreLedger } = await import('../chat/checkpoint')
    const first = harness([researchTurn(), casesTurn])
    first.run.start('Research the requirement', [])
    const ready = await settled(first.run)
    expect(canCreateResearchDraft(ready)).toBe(true)
    const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(ready, first.ledger.sources))))
    saved.state.cases = saved.state.cases.map(row => row.id === 'hours-missing' ? { ...row, expectedDisposition: UNREACHABLE } : row)
    const resumed = harness([async () => { throw new Error('Reload must not call the model') }], { registry: async () => Object.values(ready.registries).at(-1)!, seal: async () => { throw new Error('A sealed session cannot be sealed twice') } })
    restoreLedger(resumed.ledger, saved.sources)
    await resumed.run.restore(saved.state)
    resumed.run.recheck()
    const state = await settled(resumed.run)
    expect(state.status, state.detail).toBe('needs-input')
    expect(state.expectationIssues.map(issue => [issue.id, issue.message])).toEqual([['hours-missing', UNREACHABLE_SUMMARY]])
    expect(state.cases.map(row => row.id)).toEqual(['meets-hours', 'under-hours'])
    expect(state.revisionsUsed).toBe(ready.revisionsUsed)
    expect(resumed.runtime.calls).not.toContain('experimental_evaluate')
    expect(resumed.requests).toHaveLength(0)
    expect(canCreateResearchDraft(state)).toBe(false)
  })

  it('holds the screened proposal when validation fails, and retries it without a second reviewer turn', async () => {
    // Validation failing on transport says nothing about the question the
    // reviewer answered, and that turn is the most expensive in the run. The
    // failure is still closed: no case, no drop and no unknown is established
    // until the runtime has returned a canonical for every admitted row.
    const native = fakeRuntime()
    let drop = true
    // The reviewer's open question is part of what the proposal answered, and a
    // hold that kept the cases but dropped it would report a settled suite the
    // reviewer did not settle.
    const openQuestion = 'The page does not say whether part-time weeks count toward the hours.'
    const casesWithUnknowns: Script = async (request, signal, onEvent) =>
      casesTurn(request, signal, event => onEvent(event.type === 'proposal' ? { ...event, unknowns: [openQuestion] } : event))
    const { run, requests } = harness([researchTurn(), casesWithUnknowns], {
      callTool: async (name, args) => {
        if (name === EXPECTATION_TOOL && drop) {
          drop = false
          throw new Error('the runtime connection dropped')
        }
        return native.callTool(name, args)
      }
    })
    run.start('brief', [PAGE_URL])
    let state = await settled(run)
    expect(state.status).toBe('failed')
    expect(state.detail).toContain('connection dropped')
    expect(state.cases).toEqual([])
    expect(state.expectationIssues).toEqual([])
    expect(state.droppedCases).toEqual([])
    expect(state.heldProposal?.admitted.map(row => row.id)).toEqual(['meets-hours', 'under-hours', 'hours-missing'])
    expect(state.heldProposal?.dropped.map(row => row.id)).toEqual(['ungrounded'])
    expect(state.heldProposal?.unknowns).toEqual([openQuestion])
    expect(state.heldProposal?.candidateDigest).toBe(state.candidates[0]!.digest)
    expect(canRetryExpectationValidation(state)).toBe(true)
    run.retryExpectationValidation()
    // In flight, the hold is still there and the retry is not offered again:
    // sending the same proposal twice is not a retry of it.
    expect(run.getSnapshot().status).toBe('running')
    expect(run.getSnapshot().heldProposal).not.toBeNull()
    expect(canRetryExpectationValidation(run.getSnapshot())).toBe(false)
    state = await settled(run)
    expect(state.status, state.detail).toBe('ready')
    expect(state.cases.map(row => row.id)).toEqual(['meets-hours', 'under-hours', 'hours-missing'])
    expect(state.droppedCases.map(row => row.id)).toEqual(['ungrounded'])
    // One reviewer turn for the whole run: the retry judged what was held.
    expect(requests.filter(request => request.reviewer)).toHaveLength(1)
    // The transcript carries the person's action and the reviewer's open
    // question, in the order they happened.
    expect(state.turns.map(turn => [turn.role, turn.kind])).toEqual([
      ['user', 'brief'],
      ['assistant', 'message'],
      ['assistant', 'unknowns'],
      ['assistant', 'message'],
      ['user', 'note'],
      ['assistant', 'note'],
      ['assistant', 'unknowns']
    ])
    expect(state.turns[4]!.text).toBe('Sent the held case proposal back for validation.')
    expect(state.turns.at(-1)!.text).toBe(`• ${openQuestion}`)
    // Answered is not held: nothing offers a retry of a settled question.
    expect(state.heldProposal).toBeNull()
    expect(canRetryExpectationValidation(state)).toBe(false)
    const answered = run.getSnapshot()
    run.retryExpectationValidation()
    expect(run.getSnapshot()).toBe(answered)
  })

  it('asks a fresh reviewer where the draft moved on under a held proposal', async () => {
    // A suite is proposed from the draft's own outcomes and fact paths, so a
    // hold taken against other bytes is not an answer for these -- holding must
    // not quietly become a mismatched suite for a draft nobody proposed it for.
    const native = fakeRuntime()
    let drop = true
    const changed: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: { ...PACK, title: 'Changed title' }, unknowns: [] })
      event({ type: 'end' })
    }
    const reviewerFails: Script = async (_request, _signal, event) => {
      event({ type: 'error', message: 'the endpoint answered 429' })
      event({ type: 'end' })
    }
    const { run, requests } = harness([researchTurn(), casesTurn, changed, reviewerFails], {
      callTool: async (name, args) => {
        if (name === EXPECTATION_TOOL && drop) {
          drop = false
          throw new Error('the runtime connection dropped')
        }
        return native.callTool(name, args)
      }
    })
    run.start('brief', [PAGE_URL])
    let state = await settled(run)
    expect(state.status).toBe('failed')
    const held = state.heldProposal!
    run.send('Call it Changed title')
    state = await settled(run)
    // The changed draft sent a second reviewer turn, which failed in its turn:
    // the hold is still the earlier draft's, and it is not a retry for this one.
    expect(requests.filter(request => request.reviewer)).toHaveLength(2)
    expect(state.status).toBe('failed')
    expect(state.detail).toBe('the endpoint answered 429')
    expect(state.cases).toEqual([])
    expect(state.heldProposal).toEqual(held)
    expect(state.candidates.at(-1)!.digest).not.toBe(held.candidateDigest)
    expect(canRetryExpectationValidation(state)).toBe(false)
    const stale = run.getSnapshot()
    run.retryExpectationValidation()
    expect(run.getSnapshot()).toBe(stale)
  })

  it('repairs a draft that disagrees after a retried validation, as the run it resumes would have', async () => {
    // The retry resumes the run validation interrupted: what follows a
    // successful validation is the check and the revision budget `start()`
    // would have spent. A retry that declined to repair would settle at
    // needs-input with a revision still in hand, over a draft the run was
    // entitled to fix.
    const wrong = { ...PACK, rules: [{ ...PACK.rules[0]!, when: { ...PACK.rules[0]!.when, value: '1600' } }] }
    const native = fakeRuntime()
    let drop = true
    const repairTurn: Script = async (request, _signal, onEvent) => {
      expect(request.prompt).toContain('REPAIR')
      onEvent({ type: 'message', text: 'repaired to the threshold the excerpt states' })
      onEvent({ type: 'proposal', document: PACK, unknowns: [] })
      onEvent({ type: 'end' })
    }
    const { run, requests } = harness([researchTurn(wrong), casesTurn, repairTurn], {
      callTool: async (name, args) => {
        if (name === EXPECTATION_TOOL && drop) {
          drop = false
          throw new Error('the runtime connection dropped')
        }
        return native.callTool(name, args)
      }
    })
    run.start('brief', [PAGE_URL])
    let state = await settled(run)
    expect(state.status).toBe('failed')
    expect(state.revisionsUsed).toBe(0)
    run.retryExpectationValidation()
    state = await settled(run)
    expect(state.status, state.detail).toBe('ready')
    // The disagreement the retried validation uncovered was repaired, not
    // reported: one revision spent, a second candidate, still one reviewer.
    expect(state.revisionsUsed).toBe(1)
    expect(state.candidates).toHaveLength(2)
    expect(requests.filter(request => request.reviewer)).toHaveLength(1)
    expect(state.candidates.at(-1)!.check?.cases.every(row => row.passed)).toBe(true)
  })

  it('invalidates a proposed correction when conversation changes the draft', async () => {
    const changed: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: { ...PACK, title: 'Changed title' }, unknowns: [] })
      event({ type: 'end' })
    }
    const { run } = await blockedRun([correctionTurn(), changed])
    run.proposeExpectationCorrection('hours-missing')
    const proposal = (await settled(run)).expectationIssues[0]!.proposal!
    run.send('Change the title')
    const state = await settled(run)
    expect(state.expectationIssues[0]!.proposal).toBeUndefined()
    expect(state.expectationIssues[0]!.proposalError).toContain('draft changed')
    run.approveExpectationCorrection('hours-missing', proposal.token)
    expect(run.getSnapshot()).toBe(state)
  })

  it('requires the source still to be verified at approval', async () => {
    const { run, ledger } = await blockedRun([correctionTurn()])
    run.proposeExpectationCorrection('hours-missing')
    const proposal = (await settled(run)).expectationIssues[0]!.proposal!
    ledger.verified('src-1', { state: 'failed', at: new Date().toISOString(), findings: [] })
    run.approveExpectationCorrection('hours-missing', proposal.token)
    const state = await settled(run)
    expect(state.detail).toContain('no longer verified')
    expect(state.cases).toHaveLength(2)
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
  })

  it('reports a valid corrected expectation that disagrees without automatically repairing the pack', async () => {
    const { run } = await blockedRun([correctionTurn({ kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } })])
    run.proposeExpectationCorrection('hours-missing')
    const proposal = (await settled(run)).expectationIssues[0]!.proposal!
    run.approveExpectationCorrection('hours-missing', proposal.token)
    const state = await settled(run)
    expect(state.status).toBe('needs-input')
    expect(state.detail).toContain('unchanged draft disagrees')
    expect(state.candidates).toHaveLength(1)
    expect(state.candidates[0]!.check?.cases.filter(row => row.passed)).toHaveLength(2)
    expect(state.cases).toHaveLength(3)
    expect(state.revisionsUsed).toBe(0)
    expect(canCreateResearchDraft(state)).toBe(false)
  })

  it('does not apply a correction when approval validation is stopped', async () => {
    const native = fakeRuntime()
    let checkingApproval = false
    const { run } = await blockedRun([correctionTurn()], { callTool: (name, args) => checkingApproval && name === EXPECTATION_TOOL ? new Promise(() => {}) : native.callTool(name, args) })
    run.proposeExpectationCorrection('hours-missing')
    const proposal = (await settled(run)).expectationIssues[0]!.proposal!
    checkingApproval = true
    run.approveExpectationCorrection('hours-missing', proposal.token)
    run.stop()
    const state = await settled(run)
    expect(state.status).toBe('stopped')
    expect(state.cases).toHaveLength(2)
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
  })

  it('applies nothing where Stop lands after the approval’s validation has answered', async () => {
    // The case above stops the call itself, and the call's own abort refuses
    // it. This one stops the run in the microtasks between the runtime's
    // answer and the approval being applied, which no call is guarding: the
    // answer is read, and reading it is when Stop is pressed. Without the check
    // there, the correction joined the suite, its retest refused on the closed
    // run, and the rollback reported an approval the person had stopped before
    // it was made.
    const native = fakeRuntime()
    let stopOnAnswer = false
    let stop = (): void => {}
    const { run } = await blockedRun([correctionTurn()], {
      callTool: async (name, args) => {
        const answer = await native.callTool(name, args)
        if (!stopOnAnswer || name !== EXPECTATION_TOOL) return answer
        stopOnAnswer = false
        return {
          ...answer,
          get content() {
            stop()
            return answer.content
          }
        }
      }
    })
    stop = () => run.stop()
    run.proposeExpectationCorrection('hours-missing')
    const issue = (await settled(run)).expectationIssues[0]!
    const seen: RunState[] = []
    const unsubscribe = run.subscribe(() => seen.push(run.getSnapshot()))
    stopOnAnswer = true
    run.approveExpectationCorrection(issue.id, issue.proposal!.token)
    const state = await settled(run)
    unsubscribe()
    // The answer was read, so the stop landed where this case says it did.
    expect(stopOnAnswer).toBe(false)
    expect(state.status).toBe('stopped')
    expect(state.detail).toBe('Stopped. The last completed stage is kept.')
    expect(state.cases).toHaveLength(2)
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
    // Not applied and taken back: never applied at all.
    expect(seen.filter(snapshot => snapshot.cases.length !== 2)).toEqual([])
    expect(state.expectationIssues[0]!.proposal?.token).toBe(issue.proposal!.token)
  })

  it('rolls an approval back when its retest is stopped, and applies it when it is approved again', async () => {
    // Applied before its retest, a Stop left the correction in place with the
    // issue resolved and nothing checked: propose and approve both close on
    // `resolved`, and an unchanged message re-checks nothing, so the bytes the
    // person approved for could never be tested.
    const native = fakeRuntime()
    let stopDuringRetest = false
    let stop = (): void => {}
    const { run } = await blockedRun([correctionTurn()], {
      // The retest's own first call. Stopping there and still answering puts
      // the abort inside checkCandidate, after the correction was applied.
      callTool: (name, args) => {
        if (stopDuringRetest && name === 'validate') stop()
        return native.callTool(name, args)
      }
    })
    stop = () => run.stop()
    run.proposeExpectationCorrection('hours-missing')
    const issue = (await settled(run)).expectationIssues[0]!
    stopDuringRetest = true
    run.approveExpectationCorrection(issue.id, issue.proposal!.token)
    let state = await settled(run)
    expect(state.status).toBe('stopped')
    expect(state.cases).toHaveLength(2)
    // The retest had moved the run to `check`; taken back, it rests at review
    // again, where the open issue is -- not on a Tests view of nothing checked.
    expect(state.phase).toBe('review')
    // A run blocked on an invalid expectation has never checked its draft --
    // issues are raised where the cases are established, before any check, and
    // no later turn raises one -- so the approval strips no check here and the
    // rollback restores none. `candidates` is in the snapshot for the symmetry
    // with what the approval touches, and is given no mutation row.
    expect(state.candidates[0]!.check).toBeUndefined()
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
    expect(state.turns.some(turn => turn.text.startsWith('Approved the corrected expectation'))).toBe(false)
    // And the outcome says what it took back. "Stopped. The last completed
    // stage is kept." is true of the stop and no account of the approval.
    expect(state.detail).toContain('rolled back')
    expect(state.detail).toContain('its retest did not complete')
    // The proposal the person read is still the one on offer, and taking it
    // again is the recovery.
    expect(state.expectationIssues[0]!.proposal?.token).toBe(issue.proposal!.token)
    stopDuringRetest = false
    run.approveExpectationCorrection(issue.id, issue.proposal!.token)
    state = await settled(run)
    expect(state.status, state.detail).toBe('ready')
    expect(state.cases).toHaveLength(3)
    expect(state.candidates).toHaveLength(1)
    expect(state.candidates[0]!.check?.cases).toHaveLength(3)
    expect(canCreateResearchDraft(state)).toBe(true)
  })

  it('blocks a case whose handoff target contradicts its own disposition, and refuses a correction that would', async () => {
    // The exact expectation is the pair. No evaluation reports a target for a
    // handoff of "none", so this pair could never pass, for any pack.
    const targeted = structuredClone(CASES) as { cases: Record<string, unknown>[] }
    targeted.cases[0] = { ...targeted.cases[0]!, expectedHandoffTarget: { kind: 'human-role', name: 'Screening officer' } }
    const casesTurn: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: targeted, unknowns: [] })
      event({ type: 'end' })
    }
    const { run, runtime } = harness([researchTurn(), casesTurn])
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    expect(state.expectationIssues.map(issue => issue.id)).toEqual(['meets-hours'])
    expect(state.expectationIssues[0]!.message).toContain('requests no handoff')
    expect(state.cases.map(row => row.id)).toEqual(['under-hours', 'hours-missing'])
    // Nothing was rehearsed: a blocked expectation stops before the pack is tested.
    expect(runtime.calls).toEqual([EXPECTATION_TOOL])
  })




  it('stores the canonical assertion the runtime returned, not the reviewer\'s spelling of it', async () => {
    // Order carries no meaning in a §8.3 set, so the runtime answers with the
    // canonical text. Storing the reviewer's spelling instead would leave the
    // saved matrix row and the assertion that was checked two different texts
    // for one set, and a stricter decoder later would refuse the saved one.
    const unordered = { cases: [{ ...CASES.cases[2]!, expectedDisposition: { kind: 'unresolved', reasons: ['unknown', 'conflict'], handoff: { state: 'none' } } }] }
    const unorderedTurn: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: unordered, unknowns: [] })
      event({ type: 'end' })
    }
    const { run, ledger } = harness([researchTurn(), unorderedTurn])
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    expect(state.cases).toHaveLength(1)
    expect((state.cases[0]!.expectedDisposition as { reasons: string[] }).reasons).toEqual(['conflict', 'unknown'])
    const matrix = matrixDocument(state, ledger) as { cases: { expectedDisposition: { reasons: string[] } }[] }
    expect(matrix.cases[0]!.expectedDisposition.reasons).toEqual(['conflict', 'unknown'])
  })

  it('refuses an approval carrying an earlier proposal\'s token', async () => {
    // The token identifies the proposal a person looked at. A second correction
    // replaces what is displayed, so the first token must no longer approve
    // anything: what is applied is what was shown.
    const { run } = await blockedRun([correctionTurn(), correctionTurn({ kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } })])
    run.proposeExpectationCorrection('hours-missing')
    const first = (await settled(run)).expectationIssues[0]!.proposal!
    run.proposeExpectationCorrection('hours-missing')
    const second = (await settled(run)).expectationIssues[0]!.proposal!
    expect(second.token).not.toBe(first.token)
    const state = run.getSnapshot()
    run.approveExpectationCorrection('hours-missing', first.token)
    expect(run.getSnapshot()).toBe(state)
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
  })

  it('judges expectations against the evaluator, so a draft that declares the wrong version is repaired and not refused', async () => {
    // An expectation describes a disposition an evaluator produces. Keying the
    // check on the draft's own header turned a draft defect -- the version the
    // runtime's own authoring prompt warns models get wrong -- into a failed run
    // that threw away the reviewer's whole proposal.
    const wrongVersion = { ...PACK, specVersion: '0.1.0-draft' }
    const seen: Record<string, unknown>[] = []
    const native = fakeRuntime()
    const { run } = harness([researchTurn(wrongVersion), casesTurn], {
      callTool: (name, args) => {
        if (name === EXPECTATION_TOOL) seen.push(args)
        return native.callTool(name, args)
      }
    })
    run.start('brief', [PAGE_URL])
    const state = await settled(run)
    expect(seen.map(args => args.spec_version)).toEqual(['0.2.0-draft'])
    expect(state.status, state.detail).toBe('ready')
    expect(state.cases).toHaveLength(3)
  })

  it('never re-establishes cases over a blocked expectation, even when every proposal was invalid', async () => {
    // The all-invalid shape leaves no established case, and a later draft change
    // must not send the reviewer back to propose a fresh, smaller suite: that is
    // the reduced suite this flow exists to refuse.
    const onlyInvalid = { cases: [INVALID_CASES.cases[2]] }
    const invalidOnlyTurn: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: onlyInvalid, unknowns: [] })
      event({ type: 'end' })
    }
    const changed: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: { ...PACK, title: 'Changed title' }, unknowns: [] })
      event({ type: 'end' })
    }
    const fresh: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: { cases: [CASES.cases[0], CASES.cases[1]] }, unknowns: [] })
      event({ type: 'end' })
    }
    const { run, runtime } = harness([researchTurn(), invalidOnlyTurn, changed, fresh])
    run.start('brief', [PAGE_URL])
    const blocked = await settled(run)
    expect(blocked.cases).toHaveLength(0)
    expect(blocked.expectationIssues.map(issue => issue.id)).toEqual(['hours-missing'])
    run.send('Change the title')
    const state = await settled(run)
    expect(state.expectationIssues.map(issue => issue.id)).toEqual(['hours-missing'])
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
    expect(state.cases).toHaveLength(0)
    expect(state.status).toBe('needs-input')
    expect(canCreateResearchDraft(state)).toBe(false)
    expect(runtime.calls.filter(name => name === 'experimental_evaluate')).toEqual([])
  })
})


/**
 * The correction flow's backup guards, each held in the position it exists for.
 *
 * Every one of these refuses a state another, tested guard already prevents, so
 * driving the controller through its public methods reaches none of them and a
 * green suite says nothing about whether they work. These tests write the
 * controller's state directly to put each guard in front of the state it
 * refuses -- the one thing a refactor that removed the primary guard would need
 * somebody to have written down.
 */
describe('the correction flow\'s backup guards', () => {
  /**
   * The controller's own state, written as no public call can write it. The
   * field reached for is private, so a rename would leave this writing a
   * property nobody reads and every test below passing on a fixture that never
   * landed -- which is the failure these tests exist to refuse. Both ends are
   * asserted here: the field was there to write, and what the run reports is
   * what was written.
   */
  function writeState(run: AuthoringRun, patch: Partial<RunState>): void {
    const inner = run as unknown as { state: RunState }
    expect(inner.state, 'AuthoringRun.state').toBeDefined()
    inner.state = { ...inner.state, ...patch }
    expect(run.getSnapshot()).toBe(inner.state)
  }

  async function proposed(overrides: Partial<RunPorts> = {}) {
    const harnessed = await blockedRun([correctionTurn()], overrides)
    harnessed.run.proposeExpectationCorrection('hours-missing')
    const proposal = (await settled(harnessed.run)).expectationIssues[0]!.proposal!
    return { ...harnessed, proposal }
  }

  it('refuses an approval whose proposal was read against another candidate', async () => {
    const { run, proposal } = await proposed()
    // A later candidate with the proposal still standing beside it. Taking one
    // through a turn clears every pending proposal, which is what keeps the
    // digest comparison from firing, so the candidate is appended by hand.
    const text = JSON.stringify({ ...PACK, title: 'A later draft' }, null, 2)
    const later: Candidate = { revision: 2, document: JSON.parse(text), text, digest: await digestOf(text), producedBy: 'conversation' }
    writeState(run, { candidates: [...run.getSnapshot().candidates, later] })
    const before = run.getSnapshot()
    run.approveExpectationCorrection('hours-missing', proposal.token)
    // Nothing was armed and nothing was set: the approval never started, so the
    // correction cannot be applied to a draft nobody read it against.
    expect(run.getSnapshot()).toBe(before)
    expect(before.expectationIssues[0]!.resolved).toBeUndefined()
  })

  it('does not apply a correction whose validation answered after the run was stopped', async () => {
    const native = fakeRuntime()
    const held: { release: (() => void) | null } = { release: null }
    let approving = false
    const { run, proposal } = await proposed({
      // Hold the validation's answer so the abort can be placed in the window
      // this guard covers: after the validation resolved, before the correction
      // is applied. The held promise is resolved with a value already in hand,
      // so releasing it queues `withAbort`'s own reaction first and the test's
      // stop next -- after the abort listener is dropped, before the approval
      // resumes. `withAbort` no longer refuses it, and nothing else would.
      callTool: (name, args) => {
        if (!approving || name !== EXPECTATION_TOOL) return native.callTool(name, args)
        return new Promise<McpToolResult>(resolve => {
          void native.callTool(name, args).then(answer => { held.release = () => resolve(answer) })
        })
      }
    })
    approving = true
    run.approveExpectationCorrection('hours-missing', proposal.token)
    await until(() => held.release !== null, 'the approval validating the correction')
    held.release!()
    // One microtask on: `withAbort` has taken the value and dropped its abort
    // listener, and the approval has not resumed yet. An abort placed a hop too
    // early is refused by `withAbort` instead, which ends the run the same way
    // and would quietly stop testing anything -- the mutation row `a correction
    // is applied after the run was stopped` is what says so if that drifts.
    void Promise.resolve().then(() => run.stop())
    const state = await settled(run)
    expect(state.status).toBe('stopped')
    expect(state.cases).toHaveLength(2)
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
  })

  it('refuses an approval whose case id is already established', async () => {
    const { run, proposal } = await proposed()
    // The id the open issue names, established as a case: admission drops an id
    // that is already established and an invalid expectation is kept as an issue
    // rather than a case, so the two never meet without this.
    writeState(run, { cases: [...run.getSnapshot().cases, CASES.cases[2] as AuthoringCase] })
    run.approveExpectationCorrection('hours-missing', proposal.token)
    const state = await settled(run)
    expect(state.status).toBe('failed')
    expect(state.detail).toContain('already established')
    expect(state.cases.filter(row => row.id === 'hours-missing')).toHaveLength(1)
    expect(state.expectationIssues[0]!.resolved).toBeUndefined()
  })

  it('drops a check taken before the corrected case joined the suite', async () => {
    const native = fakeRuntime()
    const held: { stop: (() => void) | null } = { stop: null }
    let rechecking = false
    const { run, proposal } = await proposed({
      // The desk's own runtime call goes through `withAbort`, so a call in
      // flight when the person stops rejects; the fixture does it by hand.
      callTool: (name, args) => rechecking && name === 'validate'
        ? new Promise<McpToolResult>((_resolve, reject) => { held.stop = () => reject(new DOMException('stopped', 'AbortError')) })
        : native.callTool(name, args)
    })
    const candidate = run.getSnapshot().candidates[0]!
    // A check over exactly the ids the suite has *after* the approval, on these
    // bytes: the shape whose survival would let Create read results the
    // corrected case never had. No run reaches it -- a run blocked on an
    // expectation never checked its draft at all.
    const stale: CandidateCheck = {
      documentDigest: candidate.digest,
      valid: true,
      diagnostics: [],
      cases: ['meets-hours', 'under-hours', 'hours-missing'].map(id => ({ id, passed: true, expected: null, actual: null }))
    }
    writeState(run, { candidates: [{ ...candidate, check: stale }] })
    // Every assertion in this test also holds on a candidate that never
    // carried a check, so the injection is stated as a precondition: without
    // this line the test could pass having tested nothing.
    expect(run.getSnapshot().candidates[0]!.check).toBe(stale)
    rechecking = true
    run.approveExpectationCorrection('hours-missing', proposal.token)
    // The approval's own write, read while its retest is still in flight: the
    // corrected case has joined the suite and the check taken before it did is
    // gone. This is the one point where a surviving check is observable -- the
    // rollback below restores the candidate whole -- so these two lines, and
    // nothing after them, are what the row "a stale check survives an approved
    // correction" turns on.
    await until(() => held.stop !== null, 'the recheck validating the draft')
    const inFlight = run.getSnapshot()
    expect(inFlight.cases).toHaveLength(3)
    expect(inFlight.candidates[0]!.check).toBeUndefined()
    // Stop the recheck before it can write a fresh check. An approval does not
    // stand on an interrupted retest: it is rolled back whole, to the suite the
    // run had before it, and the run says so.
    run.stop()
    held.stop!()
    const state = await settled(run)
    expect(state.status).toBe('stopped')
    expect(state.detail).toContain('rolled back')
    expect(state.cases).toHaveLength(2)
  })

  it('never settles a run at ready while an expectation is open', async () => {
    const unchanged: Script = async (_request, _signal, event) => {
      event({ type: 'proposal', document: PACK, unknowns: [] })
      event({ type: 'end' })
    }
    const { run } = harness([researchTurn(), casesTurn, unchanged])
    run.start('brief', [PAGE_URL])
    expect((await settled(run)).status).toBe('ready')
    // A passing check beside an open issue: `casesAndCheck` returns at its own
    // copy of this refusal long before a check exists, so the pair is written.
    writeState(run, { expectationIssues: [{ id: 'hours-missing', original: INVALID_CASES.cases[2] as AuthoringCase, message: 'reasons must not be empty' }] })
    run.send('Is this ready?')
    const state = await settled(run)
    expect(state.status).toBe('needs-input')
    expect(state.detail).toContain('1 invalid expectation')
    expect(canCreateResearchDraft(state)).toBe(false)
  })
})

it.runIf(Boolean(process.env.JPACK_EXPECTATION_BINARY))('replays admission, explicit correction and all cases through the native runtime', async () => {
  const execute = promisify(execFile)
  const callTool: CallTool = async (name, args) => {
    // One isolated stdio runtime per call, with no project or audit configured.
    const { spawn } = await import('node:child_process')
    return new Promise((resolve, reject) => {
      const child = spawn(process.env.JPACK_EXPECTATION_BINARY!, ['mcp'], { stdio: ['pipe', 'pipe', 'pipe'] })
      let output = ''
      let errors = ''
      child.stdout.on('data', chunk => { output += chunk })
      child.stderr.on('data', chunk => { errors += chunk })
      child.on('error', reject)
      child.on('close', code => {
        if (code !== 0) { reject(new Error(errors)); return }
        try { resolve(JSON.parse(output).result as McpToolResult) } catch (error) { reject(error) }
      })
      child.stdin.end(JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } }) + '\n')
    })
  }
  const { stdout: version } = await execute(process.env.JPACK_EXPECTATION_BINARY!, ['version'])
  expect(version).toContain('0.2.0-draft')
  const { run, ledger } = await blockedRun([correctionTurn()], { callTool })
  const before = run.getSnapshot().candidates[0]!
  run.proposeExpectationCorrection('hours-missing')
  const issue = (await settled(run)).expectationIssues[0]!
  run.approveExpectationCorrection(issue.id, issue.proposal!.token)
  const state = await settled(run)
  expect(state.status, state.detail + JSON.stringify(state.candidates[0]?.check)).toBe('ready')
  expect(canCreateResearchDraft(state)).toBe(true)
  expect(state.candidates).toHaveLength(1)
  expect(state.candidates[0]!.text).toBe(before.text)
  expect(state.candidates[0]!.check?.cases.map(row => [row.id, row.passed])).toEqual([
    ['meets-hours', true], ['under-hours', true], ['hours-missing', true]
  ])
  if (process.env.JPACK_HANDOVER_FIXTURE) writeFileSync(process.env.JPACK_HANDOVER_FIXTURE, JSON.stringify({
    document: state.candidates[0]!.document, name: 'Reviewed fixture', description: 'Native regression replay', unknowns: [],
    matrix: matrixDocument(state, ledger), research: researchRecord(state, ledger, state.candidates[0]!.digest)
  }, null, 2) + '\n')
}, 15000)

describe('chat workspace modes and checkpoint recovery', () => {
  it('allows a draft conversation to ask a question without forcing a fabricated pack or repeated turns', async () => {
    const h = harness([async (_request,_signal,emit) => {
      emit({ type: 'message', text: 'Which outcomes should this decision allow?' })
    }],{ mode: 'draft' })
    h.run.start('Help me create a pack',[])
    const state = await settled(h.run)
    expect(state.status).toBe('complete'); expect(state.candidates).toHaveLength(0)
    expect(h.requests).toHaveLength(1); expect(h.runtime.calls).toHaveLength(0)
    expect(state.events.some(event => event.type === 'error')).toBe(false)
  })
  it.each(['draft', 'web-research'] as const)('validates %s drafts and grounds their cases only in what the chat holds', async mode => {
    const h = harness([async (_request,_signal,emit) => emit({ type: 'proposal', document: PACK, unknowns: [] }), chatReviewer({ cases: [
      ...STATEMENT_CASES.cases,
      { ...STATEMENT_CASES.cases[0]!, id: 'from-an-excerpt', expectationSource: 'src-1#e1' }
    ] })],{ mode })
    h.run.start('Screen applicants: 1,560 hours of work qualify.',[])
    const state = await settled(h.run)
    expect(h.requests.map(request => request.reviewer === true)).toEqual([false, true])
    expect(h.runtime.calls).toEqual(['validate', EXPECTATION_TOOL, 'validate', 'experimental_evaluate', 'experimental_evaluate'])
    expect(state.cases.map(row => [row.id, row.expectationSource])).toEqual([['at-the-threshold', 'you-1'], ['just-under', 'you-1']])
    // The ledger holds no excerpt in a chat, so a case citing one grounds nothing.
    expect(state.droppedCases).toEqual([{ id: 'from-an-excerpt', reason: 'expectationSource "src-1#e1" is not an excerpt recorded in this run' }])
    // The ledger excerpt id this pack cites is no page of a document read in
    // this chat: its cases agree, and the draft is still not ready.
    expect(state.status).toBe('needs-input')
    expect(state.citations).toMatchObject([{ sourceId: 'ircc-fswp', traced: false, reason: 'citation.location is not a page of a document read in this chat' }])
    expect(canCreateResearchDraft(state)).toBe(false)
    expect(onItsBasis(state, mode)).toBe(false)
  })
  it('restores transcripts and candidate bytes, discards trusted state, and requires a fresh model-free check', async () => {
    const { checkpoint, decodeCheckpoint, restoreLedger } = await import('../chat/checkpoint')
    const first = harness([researchTurn(), casesTurn])
    first.run.start('Research the requirement',[])
    const ready = await settled(first.run)
    expect(canCreateResearchDraft(ready)).toBe(true)
    const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(ready,first.ledger.sources))))
    const resumed = harness([async () => { throw new Error('Reload must not call the model') }],{ registry: async () => Object.values(ready.registries).at(-1)!, seal: async () => { throw new Error('A sealed session cannot be sealed twice') } })
    restoreLedger(resumed.ledger,saved.sources)
    await resumed.run.restore(saved.state)
    expect(resumed.run.getSnapshot().restored).toBe(true)
    expect(resumed.run.getSnapshot().candidates.at(-1)?.check).toBeUndefined()
    expect(canCreateResearchDraft(resumed.run.getSnapshot())).toBe(false)
    expect(resumed.requests).toHaveLength(0)
    resumed.run.recheck()
    const checked = await settled(resumed.run)
    expect(canCreateResearchDraft(checked),checked.detail).toBe(true)
    expect(resumed.requests).toHaveLength(0)
  })
})

it('does not revive saved research authority when its current registry is unavailable', async () => {
  const { checkpoint, decodeCheckpoint, restoreLedger } = await import('../chat/checkpoint')
  const first = harness([researchTurn(), casesTurn])
  first.run.start('Research the requirement', [])
  const original = await settled(first.run)
  expect(canCreateResearchDraft(original)).toBe(true)
  const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(original, first.ledger.sources))))
  const resumed = harness([], { registry: async () => { throw new Error('Registry unavailable') }, seal: async () => { throw new Error('Do not reseal') } })
  restoreLedger(resumed.ledger, saved.sources)
  await resumed.run.restore(saved.state)
  resumed.run.recheck()
  const checked = await settled(resumed.run)
  expect(canCreateResearchDraft(checked)).toBe(false)
  expect(resumed.requests).toHaveLength(0)
  expect(resumed.ledger.sources.every(source => source.verification.state !== 'verified')).toBe(true)
})

describe('conversation-first task lifecycle', () => {
  it.each(['draft', 'web-research', 'research'] as const)('completes a greeting without an authoring prompt or checks in %s', async mode => {
    const h = harness([async (request, _signal, emit) => {
      expect(request.prompt).not.toContain('RUNTIME AUTHORING ONLY')
      expect(request.conversation).toBe(true)
      expect(request.hostTools.some(tool => tool.name === 'get_authoring_instructions')).toBe(true)
      emit({ type: 'message', text: 'Hello! How can I help?' })
    }], { mode, authorPrompt: 'RUNTIME AUTHORING ONLY' })
    h.run.start('Hi', [])
    const state = await settled(h.run)
    expect(state.status).toBe('complete')
    expect(state.detail).toBe('')
    expect(h.runtime.calls).toEqual([])
    expect(state.candidates).toEqual([])
    expect(h.requests).toHaveLength(1)
  })

  it('loads runtime instructions only when the model explicitly authors', async () => {
    const h = harness([async (request, signal, emit) => {
      const instructions = request.hostTools.find(tool => tool.name === 'get_authoring_instructions')!
      const answer = await instructions.execute({}, signal)
      expect(answer.content?.[0]?.text).toContain('RUNTIME AUTHORING ONLY')
      emit({ type: 'proposal', document: UNCITED, unknowns: [] })
    }, chatReviewer()], { mode: 'draft', authorPrompt: 'RUNTIME AUTHORING ONLY' })
    h.run.start('Draft this decision: 1,560 hours of work qualify.', [])
    expect((await settled(h.run)).status).toBe('ready')
    expect(h.runtime.calls[0]).toBe('validate')
  })

  it('answers a question beside a restored draft without restarting paid work or checks', async () => {
    const first = harness([async (_request, _signal, emit) => emit({ type: 'proposal', document: PACK, unknowns: [] })], { mode: 'draft' })
    first.run.start('Create a draft', [])
    const saved = await settled(first.run)
    const resumed = harness([async (request, _signal, emit) => {
      expect(request.prompt).not.toContain('Always end with')
      emit({ type: 'message', text: 'The rule compares the supplied hours with the threshold.' })
    }], { mode: 'draft' })
    await resumed.run.restore(saved)
    resumed.run.send('What does the rule mean?')
    const answered = await settled(resumed.run)
    expect(answered.restored).toBe(true)
    expect(answered.candidates[0]?.check).toBeUndefined()
    expect(resumed.runtime.calls).toEqual([])
    expect(answered.status).toBe('needs-input')
  })

  it('restores a finished reply as complete, while an interrupted reply stays interrupted', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const first = harness([async (_request, _signal, emit) => emit({ type: 'message', text: 'Hello.' })], { mode: 'draft' })
    first.run.start('Hi', [])
    const finished = await settled(first.run)
    const saved = decodeCheckpoint(checkpoint(finished, []))
    const restored = harness([])
    await restored.run.restore(saved.state)
    expect(restored.run.getSnapshot()).toMatchObject({ status: 'complete', detail: '', restored: false })
    const interrupted = harness([])
    await interrupted.run.restore({ ...saved.state, status: 'running' })
    expect(interrupted.run.getSnapshot().status).toBe('stopped')
    expect(interrupted.run.getSnapshot().detail).toContain('interrupted')
    expect(restored.requests).toEqual([])
    expect(interrupted.requests).toEqual([])
  })

  it('keeps progress ephemeral, clears prior work, and retries only on explicit request', async () => {
    let attempt = 0
    const h = harness([async (_request, _signal, emit) => {
      if (attempt++ === 0) { emit({ type: 'message_progress', text: 'A partial reply' }); throw new Error('Connection lost') }
      emit({ type: 'message', text: 'A complete response.' })
    }], { mode: 'draft' })
    h.run.start('Help', [])
    const failed = await settled(h.run)
    expect(failed.status).toBe('failed')
    expect(failed.streaming).toBe('')
    expect(failed.turns.at(-1)?.text).toBe('A partial reply')
    expect(failed.turns.at(-1)?.interrupted).toBe(true)
    expect(h.requests).toHaveLength(1)
    h.run.retryResponse()
    const retried = await settled(h.run)
    expect(retried.status).toBe('complete')
    expect(retried.turns.filter(turn => turn.role === 'user')).toHaveLength(1)
    expect(h.requests).toHaveLength(2)
  })
})
it('retains attachment and pack context for follow-up questions and restores it without cluttering display text', async () => {
  const h = harness([async (_request, _signal, emit) => emit({ type: 'message', text: 'Read it.' })], { mode: 'draft' })
  h.run.start('hello', [])
  await settled(h.run)
  h.run.send('Read this.\nAttached file: policy.txt\nA supplied policy requirement.', 'Read this.\nAttached: policy.txt')
  await settled(h.run)
  const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
  const saved = decodeCheckpoint(checkpoint(h.run.getSnapshot(), []))
  const next = harness([async (request, _signal, emit) => {
    expect(request.prompt).toContain('A supplied policy requirement.')
    emit({ type: 'message', text: 'It requires the supplied condition.' })
  }], { mode: 'draft' })
  await next.run.restore(saved.state)
  next.run.send('What does that policy require?')
  await settled(next.run)
  expect(next.run.getSnapshot().turns[2]?.text).not.toContain('A supplied policy requirement.')
  expect(next.requests).toHaveLength(1)
})

describe('conversation draft citations', () => {
  const QUOTE = 'At least 1,560 hours of paid work in the last ten years.'
  const PAGE_TEXT = `Eligibility\n\nYou need ${QUOTE} Part-time work counts.`
  let minted = 0
  /** A gateway pin, by the authority that signs under it. */
  const pinOf = (authority: string) => ({ url: 'http://127.0.0.1:8787', authority, signer: { algorithm: 'ed25519', public: authority } }) as unknown as ResearchGatewayConfig
  /** Which authority signed each kept document; a document verifies only under its own. */
  const signers = new Map<string, string>()
  /** A verified web document kept in the chat, as read_link keeps one, with the given pages. */
  function kept(pages: string[] = [PAGE_TEXT], partial = false, signedBy = 'gateway-a'): { file: ChatAttachment; document: VerifiedDocument } {
    const record = structuredClone(webSnapshot) as unknown as DocumentRecord
    const first = record.content.pages[0]!
    record.content.pages = pages.map((body, index) => ({ ...first, number: index + 1, text: body, chars: [...body].length }))
    record.content.pageCount = pages.length
    // The signed record says where the page was read from.
    record.provenance.source.url = PAGE_URL
    record.provenance.source.requestedUrl = PAGE_URL
    if (partial) record.processing.status = 'partial'
    minted += 1
    const digest = `sha256:${minted.toString(16).padStart(64, '0')}`
    const id = `12345678-1234-1234-1234-${minted.toString().padStart(12, '0')}`
    const reference = { id, digest, pages: pages.map((_, index) => index + 1), allowPartial: false }
    signers.set(id, signedBy)
    return { file: { id, name: 'Eligibility', text: '', document: reference, link: { url: 'https://example.org/what-the-chat-noted' } }, document: { record, digest, object: { version: 1, original: { name: 'Eligibility', mediaType: 'text/plain', bytes: '', sha256: record.document.id } } } }
  }
  const citing = (location: string, excerpt = QUOTE) => ({ ...PACK, sources: [{ ...PACK.sources[0]!, citation: { location, excerpt } }] })
  const pageOf = (file: ChatAttachment, page = 1) => `attachment:${file.document!.id}/${file.document!.digest}/page/${page}`
  /** A conversation run whose chat keeps the given documents, each loading as given. */
  function chat(mode: 'draft' | 'web-research', document: unknown, files: { file: ChatAttachment; document: VerifiedDocument }[], load?: RunPorts['loadDocument'], pin: () => ResearchGatewayConfig | null = () => pinOf('gateway-a'), cases: unknown = STATEMENT_CASES) {
    const loads: string[] = []
    return {
      loads,
      ...harness([async (_request, _signal, emit) => emit({ type: 'proposal', document, unknowns: [] }), chatReviewer(cases)], {
        mode,
        pin,
        documents: () => files.map(item => item.file),
        loadDocument: load ?? (async (reference, under) => {
          loads.push(reference.id)
          const found = files.find(item => item.file.document!.id === reference.id)
          if (!found) throw new Error('not stored')
          if (signers.get(reference.id) !== under.authority) throw new Error('does not verify under this pin')
          return found.document
        })
      })
    }
  }

  it.each(['draft', 'web-research'] as const)('traces a %s citation to the quoted page of a document kept in the chat, and is then ready', async mode => {
    const page = kept()
    const h = chat(mode, citing(pageOf(page.file)), [page])
    h.run.start('Draft from the page I shared', [])
    const state = await settled(h.run)
    expect(state.citations).toEqual([{ sourceId: 'ircc-fswp', location: pageOf(page.file), excerptId: null, url: PAGE_URL, traced: true, reason: '', quote: QUOTE }])
    expect(state.status, state.detail).toBe('ready')
    expect(state.detail).toContain('every citation is traced to a page read in this chat')
    expect(state.readiness).toBe(readinessKey(state))
    expect(onItsBasis(state, mode)).toBe(true)
    // Traced when the candidate was set, before its cases were written, and at the settle.
    expect(h.loads).toEqual([page.file.document!.id, page.file.document!.id, page.file.document!.id])
  })

  const untraced: [string, (page: { file: ChatAttachment }) => unknown, string][] = [
    ['a bare URL', () => ({ ...PACK, sources: [{ ...PACK.sources[0]!, citation: undefined }] }), 'citation.location is not a page of a document read in this chat'],
    ['an excerpt id the ledger does not hold', () => PACK, 'citation.location is not a page of a document read in this chat'],
    ['a document the chat does not keep', page => citing(pageOf(page.file).replace(/-\d{12}\//, '-999999999999/')), 'is not a page kept in this chat'],
    ['a page that was not selected', page => citing(pageOf(page.file, 2)), 'page 2 of document'],
    ['a quote that is not on the page', page => citing(pageOf(page.file), 'At least 1,000 hours of paid work.'), 'citation.excerpt is not on page 1 of the document it cites'],
    ['a locator naming another page than the one quoted', page => {
      const pack = citing(pageOf(page.file))
      return { ...pack, sources: [{ ...pack.sources[0]!, locator: { kind: 'uri', value: 'https://example.org/another-page' } }] }
    }, 'locator.value is not the URL the excerpt was read from'],
    // The chat's note of the link is not signed; only the record says where
    // the page was read from.
    ['a locator naming only the address the chat noted', page => {
      const pack = citing(pageOf(page.file))
      return { ...pack, sources: [{ ...pack.sources[0]!, locator: { kind: 'uri', value: page.file.link!.url } }] }
    }, 'locator.value is not the URL the excerpt was read from']
  ]
  it.each(untraced)('does not trace %s, and the draft is not ready', async (_name, draft, reason) => {
    const page = kept()
    const h = chat('draft', draft(page), [page])
    h.run.start('Draft this', [])
    const state = await settled(h.run)
    expect(state.citations).toHaveLength(1)
    expect(state.citations[0]!.traced).toBe(false)
    expect(state.citations[0]!.reason).toContain(reason)
    expect(state.status).toBe('needs-input')
    expect(state.readiness).toBe('')
    // The detail names the citation and says what to do about it.
    expect(state.detail).toContain('(ircc-fswp)')
    expect(state.detail).toContain('read the page with read_link')
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('does not trace a citation whose document does not verify under the gateway pin', async () => {
    const page = kept()
    const h = chat('web-research', citing(pageOf(page.file)), [page], async () => { throw new Error('receipt signature does not verify') })
    h.run.start('Research this', [])
    const state = await settled(h.run)
    expect(state.citations[0]).toMatchObject({ traced: false, reason: `The document ${page.file.document!.id} did not verify under the current gateway pin, so its quote is withheld.` })
    expect(state.status).toBe('needs-input')
    expect(onItsBasis(state, 'web-research')).toBe(false)
  })

  it('does not trace a quote from a partial extraction whose pages the person never confirmed', async () => {
    const page = kept([PAGE_TEXT], true)
    const h = chat('draft', citing(pageOf(page.file)), [page])
    h.run.start('Draft this', [])
    const state = await settled(h.run)
    expect(state.citations[0]).toMatchObject({ traced: false, reason: `page 1 of document ${page.file.document!.id} is not a page kept in this chat` })
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('reads the document again at the settle, so one that stops verifying withholds the draft', async () => {
    const page = kept()
    let calls = 0
    const h = chat('draft', citing(pageOf(page.file)), [page], async () => {
      if (calls++ === 0) return page.document
      throw new Error('the pin changed')
    })
    h.run.start('Draft this', [])
    const state = await settled(h.run)
    expect(calls).toBe(3)
    expect(state.citations[0]!.traced).toBe(false)
    expect(state.status).toBe('needs-input')
  })

  it('makes a draft that cites nothing ready in draft mode and says what it rests on, and not in web research', async () => {
    const draft = chat('draft', UNCITED, [])
    draft.run.start('Draft from what I tell you', [])
    const drafted = await settled(draft.run)
    expect(drafted.status).toBe('ready')
    expect(drafted.detail).toContain('This pack cites no source; it rests on what you told the assistant.')
    expect(onItsBasis(drafted, 'draft')).toBe(true)
    const researched = chat('web-research', UNCITED, [])
    researched.run.start('Research this', [])
    const state = await settled(researched.run)
    expect(state.status).toBe('needs-input')
    expect(state.detail).toContain('A web research draft rests on pages read in this chat')
    expect(onItsBasis(state, 'web-research')).toBe(false)
  })

  it('holds every mode to one rule over the same settled state', () => {
    const digest = 'fixture-digest'
    const row = { ...STATEMENT_CASES.cases[0]! }
    const base: RunState = { ...INITIAL_STATE, status: 'ready', phase: 'review', tracedBasis: 'basis-1', cases: [row],
      candidates: [{ revision: 1, producedBy: 'conversation', document: UNCITED, text: '{}', digest, check: { documentDigest: digest, valid: true, diagnostics: [], cases: [{ id: row.id, passed: true, expected: {}, actual: {} }] } }] }
    const settledAt = (state: RunState): RunState => ({ ...state, readiness: readinessKey(state) })
    const none = settledAt(base)
    // Cases join the rule in every mode: none, or one disagreeing, or one left
    // open for the person, and nothing is ready.
    const caseless = settledAt({ ...base, cases: [], candidates: [{ ...base.candidates[0]!, check: { ...base.candidates[0]!.check!, cases: [] } }] })
    expect([onItsBasis(caseless, 'draft'), onItsBasis(caseless, 'web-research')]).toEqual([false, false])
    const disagreeing = settledAt({ ...base, candidates: [{ ...base.candidates[0]!, check: { ...base.candidates[0]!.check!, cases: [{ id: row.id, passed: false, expected: {}, actual: {} }] } }] })
    expect(onItsBasis(disagreeing, 'draft')).toBe(false)
    expect(onItsBasis(settledAt({ ...base, expectationIssues: [{ id: 'open', original: row, message: 'invalid' }] }), 'draft')).toBe(false)
    expect([onItsBasis(none, 'draft'), onItsBasis(none, 'web-research'), canCreateDraft(none, 'research')]).toEqual([true, false, false])
    const traced = settledAt({ ...base, citations: [{ sourceId: 'a', location: 'attachment:x', excerptId: null, url: null, traced: true, reason: '', quote: 'q' }] })
    expect([onItsBasis(traced, 'draft'), onItsBasis(traced, 'web-research')]).toEqual([true, true])
    const open = settledAt({ ...base, citations: [...traced.citations, { sourceId: 'b', location: null, excerptId: null, url: null, traced: false, reason: 'r' }] })
    expect([onItsBasis(open, 'draft'), onItsBasis(open, 'web-research'), canCreateDraft(open, 'research')]).toEqual([false, false, false])
    // Asked with the basis standing now, a readiness reached on another is
    // none; asked with none, the answer is no.
    expect([canCreateDraft(none, 'draft', 'basis-1'), canCreateDraft(none, 'draft', 'basis-2'), canCreateDraft(none, 'draft')]).toEqual([true, false, false])
    // Research reads no such basis.
    expect(canCreateDraft({ ...none, citations: [{ sourceId: 'a', location: 'src-1#e1', excerptId: 'src-1#e1', url: null, traced: true, reason: '' }] }, 'research')).toBe(false)
    // While the citations are being traced they say nothing, and nothing is ready.
    expect(onItsBasis({ ...none, tracing: true }, 'draft')).toBe(false)
    expect(onItsBasis({ ...traced, tracing: true }, 'web-research')).toBe(false)
    // An invalid or stale check is never ready.
    expect(onItsBasis({ ...none, candidates: [{ ...none.candidates[0]!, check: { ...none.candidates[0]!.check!, valid: false } }] }, 'draft')).toBe(false)
    expect(onItsBasis({ ...none, candidates: [{ ...none.candidates[0]!, check: { ...none.candidates[0]!.check!, documentDigest: 'other' } }] }, 'draft')).toBe(false)
  })

  it('traces a reopened draft again before anything reads its citations', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const page = kept()
    const first = chat('draft', citing(pageOf(page.file)), [page])
    first.run.start('Draft this', [])
    const ready = await settled(first.run)
    expect(ready.status).toBe('ready')
    const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(ready, []))))
    expect(saved.state.citations).toEqual([])
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    const reopened = chat('draft', citing(pageOf(page.file)), [page], async () => { await gate; return page.document })
    await reopened.run.restore(saved.state)
    // Tracing: not "cites nothing", and not ready.
    expect(reopened.run.getSnapshot()).toMatchObject({ tracing: true, citations: [], restored: true })
    expect(onItsBasis(reopened.run.getSnapshot(), 'draft')).toBe(false)
    release()
    await until(() => reopened.run.getSnapshot().tracing === false, 'the reopened trace landed')
    expect(reopened.run.getSnapshot().citations).toMatchObject([{ sourceId: 'ircc-fswp', traced: true }])
    // Reopening checks and changes nothing else; the person rechecks.
    expect(reopened.requests).toEqual([])
    expect(reopened.runtime.calls).toEqual([])
    expect(onItsBasis(reopened.run.getSnapshot(), 'draft')).toBe(false)
  })

  it('leaves nothing traced, and the draft not ready, when a Stop interrupts the settle\'s trace', async () => {
    const page = kept()
    let calls = 0
    let started!: () => void
    const loading = new Promise<void>(resolve => { started = resolve })
    const h = chat('draft', citing(pageOf(page.file)), [page], async () => {
      // The settle's load answers even after the Stop: verification itself succeeded.
      if (calls++ === 1) { started(); await new Promise(resolve => setTimeout(resolve, 30)) }
      return page.document
    })
    h.run.start('Draft this', [])
    await loading
    h.run.stop()
    const state = await settled(h.run)
    expect(state.status).toBe('stopped')
    expect(state.citations).toEqual([expect.objectContaining({ sourceId: 'ircc-fswp', traced: false, reason: 'The citation check was interrupted. Recheck the draft.' })])
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('does not keep the readiness of an earlier trace when a later one is interrupted', async () => {
    const page = kept()
    let hold = false
    let started!: () => void
    const loading = new Promise<void>(resolve => { started = resolve })
    const h = chat('draft', citing(pageOf(page.file)), [page], async (_reference, _under, signal) => {
      if (hold) { started(); await new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('stopped', 'AbortError')))) }
      return page.document
    })
    h.run.start('Draft this', [])
    const ready = await settled(h.run)
    expect(onItsBasis(ready, 'draft')).toBe(true)
    hold = true
    h.run.recheck()
    await loading
    h.run.stop()
    const state = await settled(h.run)
    expect(state.status).toBe('stopped')
    expect(state.readiness).toBe(ready.readiness)
    expect(state.citations[0]!.traced).toBe(false)
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('does not keep an earlier readiness when a recheck of a reopened draft is interrupted mid-trace', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const first = kept(), second = kept()
    const pack = { ...PACK, sources: [
      { ...PACK.sources[0]!, id: 'first', citation: { location: pageOf(first.file), excerpt: QUOTE } },
      { ...PACK.sources[0]!, id: 'second', citation: { location: pageOf(second.file), excerpt: QUOTE } }
    ] }
    const original = chat('draft', pack, [first, second])
    original.run.start('Draft this', [])
    expect((await settled(original.run)).status).toBe('ready')
    const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(original.run.getSnapshot(), []))))
    let rechecking = false
    let started!: () => void
    const loading = new Promise<void>(resolve => { started = resolve })
    const reopened = chat('draft', pack, [first, second], async (reference, _pin, signal) => {
      if (!rechecking) return reference.id === first.file.document!.id ? first.document : second.document
      if (reference.id === first.file.document!.id) throw new Error('no longer verifies')
      started()
      // A load in flight ends with the Stop, as the document loader's does.
      return new Promise<never>((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('stopped', 'AbortError'))))
    })
    await reopened.run.restore(saved.state)
    await until(() => reopened.run.getSnapshot().tracing === false, 'the reopened trace landed')
    rechecking = true
    reopened.run.recheck()
    await loading
    reopened.run.stop()
    const state = await settled(reopened.run)
    expect(state.citations.every(citation => !citation.traced)).toBe(true)
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('traces again when the pin moves under a trace, and lands only on the basis it began on', async () => {
    const page = kept()
    let current = 'gateway-a'
    let calls = 0
    const h = chat('draft', citing(pageOf(page.file)), [page], async (_reference, under) => {
      calls += 1
      // The settle's first load: verified under the old pin, which is replaced
      // before the trace lands. Under the new one the document does not verify.
      if (calls === 3) current = 'gateway-b'
      if (under.authority !== 'gateway-a') throw new Error('does not verify under the new pin')
      return page.document
    }, () => pinOf(current))
    h.run.start('Draft this', [])
    const state = await settled(h.run)
    expect(calls).toBe(4)
    expect(state.citations[0]!.traced).toBe(false)
    expect(state.status).toBe('needs-input')
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('gives up a trace whose basis keeps moving after three attempts, and says so', async () => {
    const page = kept()
    let moves = 0
    let loads = 0
    const h = chat('draft', citing(pageOf(page.file)), [page], async () => { loads += 1; return page.document }, () => pinOf(`gateway-${moves++}`))
    h.run.start('Draft this', [])
    const state = await settled(h.run)
    expect(state.citations[0]).toMatchObject({ traced: false, reason: "The gateway pin or this chat's documents changed while the citations were checked. Recheck the draft." })
    expect(onItsBasis(state, 'draft')).toBe(false)
    // Three traces -- when the candidate was set, before its cases were written,
    // and at the settle -- each three attempts.
    expect(loads).toBe(9)
  })

  it('verifies every document of one attempt under the one pin it began with', async () => {
    // Two documents signed under different authorities, and a pin switched
    // from A to B and back to A while the attempt loads them: what lands must
    // hold under A, and the one signed under B does not.
    const one = kept([PAGE_TEXT], false, 'gateway-a'), two = kept([PAGE_TEXT], false, 'gateway-b')
    const pack = { ...PACK, sources: [
      { ...PACK.sources[0]!, id: 'one', citation: { location: pageOf(one.file), excerpt: QUOTE } },
      { ...PACK.sources[0]!, id: 'two', citation: { location: pageOf(two.file), excerpt: QUOTE } }
    ] }
    let current = 'gateway-a'
    const h = chat('draft', pack, [one, two], async (reference, under) => {
      const verified = signers.get(reference.id) === under.authority
      // Switched to B once the first document has loaded, so B is the pin
      // standing when the second is asked for; back to A once it has loaded.
      current = reference.id === one.file.document!.id ? 'gateway-b' : 'gateway-a'
      if (!verified) throw new Error('does not verify under this pin')
      return (reference.id === one.file.document!.id ? one : two).document
    }, () => pinOf(current))
    h.run.start('Draft this', [])
    const state = await settled(h.run)
    expect(state.citations.map(citation => [citation.sourceId, citation.traced])).toEqual([['one', true], ['two', false]])
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('withdraws readiness at once when the basis moves at rest, and settles again on what the new basis holds', async () => {
    const page = kept()
    signers.set(page.file.document!.id, 'gateway-a')
    let current = 'gateway-a'
    const h = chat('draft', citing(pageOf(page.file)), [page], undefined, () => pinOf(current))
    h.run.start('Draft this', [])
    const ready = await settled(h.run)
    expect(onItsBasis(ready, 'draft')).toBe(true)
    const before = h.loads.length
    // Nothing moved: nothing is traced again.
    h.run.basisChanged()
    expect(h.loads).toHaveLength(before)
    expect(onItsBasis(h.run.getSnapshot(), 'draft')).toBe(true)
    // A pin the document is also signed under: withdrawn while it is traced, then ready again.
    current = 'gateway-a2'
    signers.set(page.file.document!.id, 'gateway-a2')
    h.run.basisChanged()
    expect(h.run.getSnapshot().readiness).toBe('')
    expect(onItsBasis(h.run.getSnapshot(), 'draft')).toBe(false)
    await until(() => !h.run.getSnapshot().tracing && h.run.getSnapshot().readiness !== '', 'settled again on the new pin')
    expect(onItsBasis(h.run.getSnapshot(), 'draft')).toBe(true)
    // A pin it is not signed under: not ready, and the citation says why.
    current = 'gateway-b'
    h.run.basisChanged()
    await until(() => !h.run.getSnapshot().tracing && h.run.getSnapshot().status === 'needs-input', 'settled again on the other pin')
    expect(h.run.getSnapshot().citations[0]!.traced).toBe(false)
    expect(onItsBasis(h.run.getSnapshot(), 'draft')).toBe(false)
    expect(h.requests).toHaveLength(2)
  })

  /** A ready chat draft whose follow-up turn is in flight when the basis moves, and ends as `end` says. */
  async function movedMidAction(end: 'stop' | 'fail') {
    const page = kept(), other = kept()
    const files = [page]
    let loads = 0
    let started!: () => void
    const answering = new Promise<void>(resolve => { started = resolve })
    let finish!: () => void
    const h = harness([
      async (_request, _signal, emit) => emit({ type: 'proposal', document: citing(pageOf(page.file)), unknowns: [] }),
      chatReviewer(),
      async (_request, signal) => {
        started()
        await new Promise<void>((resolve, reject) => { finish = resolve; signal.addEventListener('abort', () => reject(new DOMException('stopped', 'AbortError'))) })
        throw new Error('the model answered with nothing usable')
      }
    ], { mode: 'draft', pin: () => pinOf('gateway-a'), documents: () => files.map(item => item.file), loadDocument: async () => { loads += 1; return page.document } })
    h.run.start('Draft this', [])
    expect(onItsBasis(await settled(h.run), 'draft')).toBe(true)
    h.run.send('And another thing.')
    await answering
    // The chat keeps another document, mid-action.
    files.push(other)
    h.run.basisChanged()
    expect(onItsBasis(h.run.getSnapshot(), 'draft')).toBe(false)
    const before = loads
    if (end === 'stop') h.run.stop()
    else finish()
    return { h, before, loads: () => loads, state: await settled(h.run) }
  }

  it('starts no new work after a Stop when the basis moved during the action, and counts nothing traced', async () => {
    const { h, before, loads, state } = await movedMidAction('stop')
    expect(state.status).toBe('stopped')
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(loads()).toBe(before)
    expect(h.run.getSnapshot().tracing).toBeFalsy()
    expect(h.run.getSnapshot().citations[0]).toMatchObject({ traced: false, reason: "The gateway pin or this chat's documents changed while the citations were checked. Recheck the draft." })
    expect(onItsBasis(h.run.getSnapshot(), 'draft')).toBe(false)
  })

  it('starts no new work after a Stop even where the action then ends with an ordinary error', async () => {
    const page = kept(), other = kept()
    const files = [page]
    let loads = 0
    let hold = false
    let started!: () => void
    const validating = new Promise<void>(resolve => { started = resolve })
    let fail!: () => void
    const runtime = fakeRuntime()
    const h = harness([async (_request, _signal, emit) => emit({ type: 'proposal', document: citing(pageOf(page.file)), unknowns: [] }), chatReviewer()], {
      mode: 'draft', pin: () => pinOf('gateway-a'), documents: () => files.map(item => item.file), loadDocument: async () => { loads += 1; return page.document },
      callTool: async (name, args) => {
        if (name === 'validate' && hold) {
          started()
          // The runtime answers after the Stop with a failure of its own.
          await new Promise<void>(resolve => { fail = resolve })
          throw new Error('the runtime connection was lost')
        }
        return runtime.callTool(name, args)
      }
    })
    h.run.start('Draft this', [])
    expect(onItsBasis(await settled(h.run), 'draft')).toBe(true)
    hold = true
    h.run.recheck()
    await validating
    files.push(other)
    h.run.basisChanged()
    h.run.stop()
    const before = loads
    fail()
    const state = await settled(h.run)
    expect(state.status).toBe('failed')
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(loads).toBe(before)
    expect(h.run.getSnapshot().citations[0]!.traced).toBe(false)
    expect(canCreateDraft(h.run.getSnapshot(), 'draft', h.run.basisNow())).toBe(false)
  })

  it('traces again when an action that saw the basis move ends otherwise, and keeps its readiness withdrawn', async () => {
    const { h, before, loads, state } = await movedMidAction('fail')
    expect(state.status).toBe('failed')
    await until(() => loads() > before && !h.run.getSnapshot().tracing, 'traced again after the action')
    expect(h.run.getSnapshot().citations[0]!.traced).toBe(true)
    expect(h.run.getSnapshot().tracedBasis).toBe(h.run.basisNow())
    // Traced again, and not made ready again: the action did not settle.
    expect(h.run.getSnapshot().readiness).toBe('')
    expect(canCreateDraft(h.run.getSnapshot(), 'draft', h.run.basisNow())).toBe(false)
  })

  it('answers Create on the basis standing when it is asked, before anything noticed the move', async () => {
    const page = kept()
    let current = 'gateway-a'
    const files = [page]
    const h = harness([async (_request, _signal, emit) => emit({ type: 'proposal', document: citing(pageOf(page.file)), unknowns: [] }), chatReviewer()],
      { mode: 'draft', pin: () => pinOf(current), documents: () => files.map(item => item.file), loadDocument: async () => page.document })
    h.run.start('Draft this', [])
    const ready = await settled(h.run)
    expect(canCreateDraft(ready, 'draft', h.run.basisNow())).toBe(true)
    current = 'gateway-b'
    expect(canCreateDraft(h.run.getSnapshot(), 'draft', h.run.basisNow())).toBe(false)
    current = 'gateway-a'
    files.length = 0
    expect(canCreateDraft(h.run.getSnapshot(), 'draft', h.run.basisNow())).toBe(false)
  })

  it('does not settle at ready on a trace whose basis moved before the settle read it', async () => {
    const page = kept()
    let current = 'gateway-a'
    let traces = 0
    const h = chat('draft', citing(pageOf(page.file)), [page], async () => page.document, () => pinOf(current))
    // The settle's trace lands, and the pin moves before the settle reads it.
    let tracing = false
    h.run.subscribe(() => {
      const now = h.run.getSnapshot().tracing === true
      // The third trace lands: when the candidate was set, before its cases were written, and at the settle.
      if (tracing && !now && ++traces === 3) current = 'gateway-b'
      tracing = now
    })
    h.run.start('Draft this', [])
    const state = await settled(h.run)
    expect(state.status).toBe('needs-input')
    expect(state.detail).toBe("The gateway pin or this chat's documents changed while the citations were checked. Recheck the draft.")
    expect(canCreateDraft(state, 'draft', h.run.basisNow())).toBe(false)
  })

  it('keeps the settle a moved basis owes when a later move supersedes its trace', async () => {
    const page = kept()
    const files = [page]
    let hold = true
    let release!: () => void
    const held = new Promise<void>(resolve => { release = resolve })
    const h = harness([async (_request, _signal, emit) => emit({ type: 'proposal', document: citing(pageOf(page.file)), unknowns: [] }), chatReviewer()],
      { mode: 'draft', pin: () => pinOf('gateway-a'), documents: () => files.map(item => item.file), loadDocument: async (_reference, _pin, signal) => {
        if (hold) await new Promise<void>((resolve, reject) => { held.then(resolve); signal.addEventListener('abort', () => reject(new DOMException('superseded', 'AbortError'))) })
        return page.document
      } })
    hold = false
    h.run.start('Draft this', [])
    expect(onItsBasis(await settled(h.run), 'draft')).toBe(true)
    hold = true
    files.push(kept())
    h.run.basisChanged()
    await until(() => h.run.getSnapshot().tracing === true, 'the first trace at rest is loading')
    hold = false
    files.push(kept())
    h.run.basisChanged()
    release()
    await until(() => !h.run.getSnapshot().tracing && h.run.getSnapshot().readiness !== '', 'settled again by the later trace')
    expect(h.run.getSnapshot().status).toBe('ready')
    expect(canCreateDraft(h.run.getSnapshot(), 'draft', h.run.basisNow())).toBe(true)
  })

  it('ends a trace at rest when its owner goes away, and starts none', async () => {
    const page = kept()
    let current = 'gateway-a'
    let aborted = false
    let hold = false
    const h = chat('draft', citing(pageOf(page.file)), [page], async (_reference, _under, signal) => {
      if (hold) await new Promise((_resolve, reject) => signal.addEventListener('abort', () => { aborted = true; reject(new DOMException('gone', 'AbortError')) }))
      return page.document
    }, () => pinOf(current))
    h.run.start('Draft this', [])
    expect(onItsBasis(await settled(h.run), 'draft')).toBe(true)
    hold = true
    current = 'gateway-a2'
    h.run.basisChanged()
    await until(() => h.run.getSnapshot().tracing === true, 'the trace at rest is loading')
    h.run.detach()
    await until(() => aborted && !h.run.getSnapshot().tracing, 'the trace at rest ended')
    current = 'gateway-a3'
    h.run.basisChanged()
    expect(h.run.getSnapshot().tracing).toBe(false)
    expect(canCreateDraft(h.run.getSnapshot(), 'draft', h.run.basisNow())).toBe(false)
  })

  it('ends a trace at rest with Stop, and it settles nothing afterwards', async () => {
    const page = kept()
    let current = 'gateway-a'
    let hold = false
    const h = chat('draft', citing(pageOf(page.file)), [page], async (_reference, _under, signal) => {
      if (hold) await new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('stopped', 'AbortError'))))
      return page.document
    }, () => pinOf(current))
    h.run.start('Draft this', [])
    expect(onItsBasis(await settled(h.run), 'draft')).toBe(true)
    hold = true
    current = 'gateway-a2'
    h.run.basisChanged()
    await until(() => h.run.getSnapshot().tracing === true, 'the trace at rest is loading')
    h.run.stop()
    await until(() => h.run.getSnapshot().tracing === false, 'the trace at rest ended')
    expect(h.run.getSnapshot().citations[0]).toMatchObject({ traced: false, reason: 'The citation check was interrupted. Recheck the draft.' })
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(h.run.getSnapshot().readiness).toBe('')
    expect(onItsBasis(h.run.getSnapshot(), 'draft')).toBe(false)
  })

  it('keys a trace to the pin and to every kept document\'s identity and pages', () => {
    const page = kept()
    const base = documentBasis({ public: 'a' }, [page.file])
    expect(documentBasis({ public: 'a' }, [page.file])).toBe(base)
    expect(documentBasis({ public: 'b' }, [page.file])).not.toBe(base)
    expect(documentBasis({ public: 'a' }, [])).not.toBe(base)
    expect(documentBasis({ public: 'a' }, [{ ...page.file, document: { ...page.file.document!, pages: [] } }])).not.toBe(base)
    expect(documentBasis({ public: 'a' }, [{ ...page.file, document: { ...page.file.document!, allowPartial: true } }])).not.toBe(base)
  })

  // ---- cases written without the rules, in the conversation modes ----------

  it('writes a chat draft\'s cases from its pages and the person\'s messages, never from its rules or the assistant\'s turns', async () => {
    const page = kept()
    const RULE = { id: 'rule-zq-hours-floor', description: 'Description-zq of the hours floor', when: { op: 'fact', path: '/work/hours', operator: 'greater-than-or-equal', value: '1560' }, outcome: 'meets', onUnknown: 'escalate', sourceRefs: ['ircc-fswp'] }
    const EXCEPTION = { id: 'exception-zq-veteran', when: { op: 'fact', path: '/work/veteran', operator: 'equals', value: 'yes-zq' }, outcome: 'does-not-meet', sourceRefs: ['ircc-fswp'] }
    const pack = { ...citing(pageOf(page.file)), rules: [RULE], exceptions: [EXCEPTION] }
    const PARAPHRASE = 'ASSISTANT-ZQ: the floor rule compares hours against 1560 exactly.'
    let prompt = ''
    const h = harness([
      async (_request, _signal, emit) => { emit({ type: 'message', text: PARAPHRASE }); emit({ type: 'proposal', document: pack, unknowns: [] }) },
      async (request, signal, emit) => { prompt = request.prompt; await chatReviewer()(request, signal, emit) }
    ], { mode: 'web-research', pin: () => pinOf('gateway-a'), documents: () => [page.file], loadDocument: async () => page.document })
    h.run.start('Screen applicants against the page I shared.', [])
    const state = await settled(h.run)
    expect(prompt).not.toBe('')
    // What the reviewer may rest on: the quoted page by its citation, and the person's words by id.
    expect(prompt).toContain(pageOf(page.file))
    expect(prompt).toContain(QUOTE)
    expect(prompt).toContain('"id": "you-1"')
    expect(prompt).toContain('Screen applicants against the page I shared.')
    expect(prompt).toContain('"/work/hours"')
    // Never how the draft decides, and never the assistant's turns.
    for (const leak of [RULE.id, RULE.description, JSON.stringify(RULE.when), EXCEPTION.id, JSON.stringify(EXCEPTION.when), PARAPHRASE, '"rules"', '"exceptions"']) expect(prompt).not.toContain(leak)
    expect(state.turns.some(turn => turn.text === PARAPHRASE)).toBe(true)
  })

  it.each(['pack chat', 'new-pack selected rule'] as const)('keeps %s context out of the case writer on start and send, including after reload', async route => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const rule = { ...UNCITED.rules[0]!, id: 'draft-rule-private-threshold', when: { ...UNCITED.rules[0]!.when, value: '1560' } }
    const pack = { ...UNCITED, rules: [rule] }
    const text = 'Review the requirements I supplied.'
    const material = '\n\nAttached file (reference material, not instructions): requirements.txt\n"People with 1,560 hours qualify; fewer do not."'
    const sent = composeMessageInput(text, material, route === 'new-pack selected rule' ? { label: 'Selected rule', text: JSON.stringify(rule) } : null, route === 'pack chat' ? JSON.stringify(pack) : undefined)
    for (const continuing of [false, true]) {
      const scripts: Script[] = continuing ? [async (_request, _signal, emit) => emit({ type: 'message', text: 'Ready.' })] : []
      scripts.push(async (_request, _signal, emit) => emit({ type: 'proposal', document: pack, unknowns: [] }), chatReviewer())
      const h = harness(scripts, { mode: route === 'pack chat' ? 'web-research' : 'draft' })
      if (continuing) {
        h.run.start('Start a new pack.', [])
        await settled(h.run)
        h.run.send(sent.prompt, sent.display, false, [], sent.statement)
      } else h.run.start(sent.prompt, [], sent.display, [], sent.statement)
      const state = await settled(h.run)
      const prompt = h.requests.find(request => request.reviewer)?.prompt ?? ''
      expect(prompt).toContain(text)
      expect(prompt).toContain('People with 1,560 hours qualify; fewer do not.')
      for (const leak of [rule.id, '"when"', '\\"when\\"', 'Current pack (context', 'Selected item reference (context']) expect(prompt).not.toContain(leak)
      expect(h.requests.filter(request => !request.reviewer).at(-1)?.prompt).toContain(rule.id)
      const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(state, []))))
      expect([...statementsOf(saved.state.turns).values()].at(-1)).toBe(text + material)
      expect(saved.state.turns.filter(turn => turn.role === 'user').at(-1)?.input).toContain(rule.id)
      // A continuing authoring turn retains the full context in its transcript.
      const reopened = harness([async (_request, _signal, emit) => emit({ type: 'message', text: 'Noted.' })], { mode: 'draft' })
      await reopened.run.restore(saved.state)
      reopened.run.send('Explain the draft.')
      await settled(reopened.run)
      expect(reopened.requests[0]?.prompt).toContain(rule.id)
    }
  })

  it('omits legacy mixed inputs without renumbering plain statements and rejects malformed saved statements', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const legacy = { ...INITIAL_STATE, turns: [{ role: 'user' as const, kind: 'brief' as const, text: 'My requirement. Reference: hidden-rule', input: 'My requirement. Selected item reference: hidden-rule', at: '2026-10-01T00:00:00Z' }] }
    expect([...statementsOf(decodeCheckpoint(checkpoint(legacy, [])).state.turns)]).toEqual([])
    expect([...statementsOf([...legacy.turns, { role: 'user', kind: 'message', text: 'Plain words.', at: '' }])]).toEqual([['you-2', 'Plain words.']])
    expect(() => decodeCheckpoint(checkpoint({ ...legacy, turns: [{ ...legacy.turns[0]!, statement: 42 as never }] }, []))).toThrow('Saved chat data is not supported')
  })

  it('admits a statement or a traced page as a chat case\'s ground, and nothing else', () => {
    const ledger = new Ledger('unset')
    const traced = { sourceId: 'page', location: 'attachment:a/sha256:b/page/1', excerptId: null, url: null, traced: true, reason: '', quote: QUOTE }
    const grounds = { statements: new Map([['you-1', 'People with 1,560 hours qualify.']]), citations: [traced, { ...traced, sourceId: 'other', location: 'attachment:c/sha256:d/page/2', traced: false }] }
    const row = (id: string, expectationSource: string) => ({ ...STATEMENT_CASES.cases[0]!, id, expectationSource })
    const { admitted, dropped } = admitCases({ cases: [row('said', 'you-1'), row('quoted', traced.location), row('never-said', 'you-9'), row('untraced-page', 'attachment:c/sha256:d/page/2'), row('excerpt', 'src-1#e1')] }, ledger, [], grounds)
    expect(admitted.map(item => [item.id, item.expectationSource])).toEqual([['said', 'you-1'], ['quoted', traced.location]])
    expect(dropped).toEqual([
      { id: 'never-said', reason: 'expectationSource "you-9" is neither one of your messages nor a page the draft cites and traced' },
      { id: 'untraced-page', reason: 'expectationSource "attachment:c/sha256:d/page/2" is neither one of your messages nor a page the draft cites and traced' },
      { id: 'excerpt', reason: 'expectationSource "src-1#e1" is not an excerpt recorded in this run' }
    ])
    // Research admits neither: its cases rest on recorded excerpts only.
    expect(admitCases({ cases: [row('said', 'you-1')] }, ledger, []).dropped).toEqual([{ id: 'said', reason: 'expectationSource "you-1" is not an excerpt recorded in this run' }])
  })

  it.each(['draft', 'web-research'] as const)('takes a %s draft that disagrees with its cases to the person, never to a repair turn', async mode => {
    const page = kept()
    const wrong = { cases: [{ ...STATEMENT_CASES.cases[0]!, expectedDisposition: MISSES }] }
    const h = chat(mode, citing(pageOf(page.file)), [page], undefined, undefined, wrong)
    h.run.start('People with 1,560 hours do not qualify.', [])
    const state = await settled(h.run)
    expect(h.requests.map(request => request.reviewer === true)).toEqual([false, true])
    expect(state.revisionsUsed).toBe(0)
    expect(state.phase).toBe('review')
    expect(state.status).toBe('needs-input')
    expect(state.detail).toBe('The unchanged draft disagrees with the reviewed expectations. Review the results or request a pack change.')
    expect(state.candidates).toHaveLength(1)
    expect(onItsBasis(state, mode)).toBe(false)
  })

  it('does not make ready a draft whose messages and pages ground no case', async () => {
    const h = chat('draft', UNCITED, [], undefined, undefined, { cases: [{ ...STATEMENT_CASES.cases[0]!, expectationSource: 'you-7' }] })
    h.run.start('Draft something.', [])
    const state = await settled(h.run)
    expect(state.cases).toEqual([])
    expect(state.status).toBe('needs-input')
    expect(state.detail).toBe('No test case could be grounded in your messages or in a page the draft cites. Tell the assistant how the pack should decide a case, or have it read and cite the pages the pack rests on.')
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('round-trips a statement-grounded case through a saved chat, into the matrix and the record', async () => {
    const { checkpoint, decodeCheckpoint, restoreLedger } = await import('../chat/checkpoint')
    const BRIEF = 'People with 1,560 hours qualify; fewer do not.'
    const first = chat('draft', UNCITED, [])
    first.run.start(BRIEF, [])
    const ready = await settled(first.run)
    expect(ready.status, ready.detail).toBe('ready')
    const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(ready, first.ledger.sources))))
    expect(saved.state.cases.map(row => row.expectationSource)).toEqual(['you-1', 'you-1'])
    const reopened = harness([async () => { throw new Error('Reload and recheck must not call the model') }], { mode: 'draft' })
    restoreLedger(reopened.ledger, saved.sources)
    await reopened.run.restore(saved.state)
    reopened.run.recheck()
    const checked = await settled(reopened.run)
    expect(checked.status, checked.detail).toBe('ready')
    expect(onItsBasis(checked, 'draft')).toBe(true)
    expect(reopened.requests).toEqual([])
    expect(checked.cases).toEqual(ready.cases)
    const matrix = matrixDocument(checked, reopened.ledger) as { cases: { id: string; focus: string; cites?: unknown }[] }
    expect(matrix.cases.map(row => [row.id, row.focus, row.cites])).toEqual([
      ['at-the-threshold', 'You said 1,560 hours qualify. [you-1]', undefined],
      ['just-under', 'One hour short does not. [you-1]', undefined]
    ])
    const record = researchRecord(checked, reopened.ledger, 'abc') as { statements?: unknown }
    expect(record.statements).toEqual([{ id: 'you-1', text: BRIEF }])
  })

  it('sets aside saved cases a chat opened from the pack cannot ground again, and a later message never stands in for them', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const { artifactCheckpoint } = await import('../packs/drafts/model')
    const first = chat('draft', UNCITED, [])
    first.run.start('People with 1,560 hours qualify; fewer do not.', [])
    expect((await settled(first.run)).status).toBe('ready')
    // A pack's draft keeps its candidates and cases, and none of the turns.
    const artifact = artifactCheckpoint(decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(first.run.getSnapshot(), [])))))
    expect(artifact.state.turns).toEqual([])
    const opened = harness([async (_request, _signal, emit) => emit({ type: 'message', text: 'Noted.' })], { mode: 'draft' })
    await opened.run.restore(artifact.state)
    // The first message of the new chat is `you-1` here too, and grounds nothing saved.
    opened.run.send('Something else entirely.')
    await settled(opened.run)
    expect(statementsOf(opened.run.getSnapshot().turns).get('you-1')).toBe('Something else entirely.')
    opened.run.recheck()
    const state = await settled(opened.run)
    expect(state.cases).toEqual([])
    expect(state.expectationIssues).toEqual([])
    expect(state.droppedCases.map(row => row.id)).toEqual(['at-the-threshold', 'just-under'])
    expect(state.droppedCases[0]!.reason).toBe('expectationSource "you-1" is neither one of your messages nor a page the draft cites and traced')
    expect(state.turns.some(turn => turn.kind === 'note' && turn.text.startsWith('The saved test cases rest on messages'))).toBe(true)
    expect(onItsBasis(state, 'draft')).toBe(false)
  })

  it('keeps no smaller suite when only some saved cases can be grounded again', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const { artifactCheckpoint } = await import('../packs/drafts/model')
    const page = kept()
    const mixed = { cases: [
      { ...STATEMENT_CASES.cases[0]!, id: 'from-the-page', expectationSource: pageOf(page.file) },
      { ...STATEMENT_CASES.cases[1]!, id: 'from-what-you-said', expectationSource: 'you-1' }
    ] }
    const first = chat('draft', citing(pageOf(page.file)), [page], undefined, undefined, mixed)
    first.run.start('People with 1,560 hours qualify; fewer do not.', [])
    expect((await settled(first.run)).cases.map(row => row.id)).toEqual(['from-the-page', 'from-what-you-said'])
    const artifact = artifactCheckpoint(decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(first.run.getSnapshot(), [])))))
    // The page still traces in the chat opened from the pack; the message is not in it.
    const opened = chat('draft', citing(pageOf(page.file)), [page])
    await opened.run.restore(artifact.state)
    opened.run.recheck()
    const state = await settled(opened.run)
    expect(state.cases).toEqual([])
    expect(state.droppedCases).toEqual([
      { id: 'from-the-page', reason: 'set aside with the saved cases that could not be grounded again' },
      { id: 'from-what-you-said', reason: 'expectationSource "you-1" is neither one of your messages nor a page the draft cites and traced' }
    ])
    expect(opened.requests).toEqual([])
    expect(canEstablishCases(state, 'draft')).toBe(true)
  })

  it('rechecks a saved chat draft without cases without a model, and establishes them only when asked', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const recovered = harness([async () => { throw new Error('No model before the person asks') }, chatReviewer()], { mode: 'draft' })
    const reply = '```json\n' + JSON.stringify({ proposal: { document: UNCITED, unknowns: [] } }) + '\n```'
    await recovered.run.restore({ ...INITIAL_STATE, status: 'complete', turns: [
      { id: 't1', role: 'user', kind: 'brief', text: 'People with 1,560 hours qualify; fewer do not.', at: '2026-09-30T00:00:00Z' },
      { id: 't2', role: 'assistant', kind: 'message', text: reply, at: '2026-09-30T00:00:01Z' }
    ] })
    recovered.run.recoverDraft(reply)
    const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(await settled(recovered.run), []))))
    expect(saved.state.cases).toEqual([])
    const h = harness([chatReviewer()], { mode: 'draft' })
    await h.run.restore(saved.state)
    expect(canEstablishCases(h.run.getSnapshot(), 'draft')).toBe(false)
    h.run.establishCases()
    expect(h.requests).toEqual([])
    h.run.recheck()
    const checked = await settled(h.run)
    expect(h.requests).toEqual([])
    expect(checked.status).toBe('needs-input')
    expect(checked.detail).toBe('This draft has no test cases written without its rules yet. Establish them before creating the pack; nothing is created or changed until you do.')
    expect(canEstablishCases(checked, 'draft')).toBe(true)
    expect(onItsBasis(checked, 'draft')).toBe(false)
    h.run.establishCases()
    const established = await settled(h.run)
    expect(h.requests.map(request => request.reviewer)).toEqual([true])
    expect(established.status, established.detail).toBe('ready')
    expect(established.cases.map(row => row.expectationSource)).toEqual(['you-1', 'you-1'])
    expect(canEstablishCases(established, 'draft')).toBe(false)
    expect(canEstablishCases(established, 'research')).toBe(false)
  })

  it('gives the correction reviewer the person\'s message where a case rests on one', async () => {
    const blocked = { cases: [{ ...STATEMENT_CASES.cases[0]!, id: 'missing-hours', facts: {}, expectedDisposition: { kind: 'unresolved', reasons: [], handoff: { state: 'requested', triggeredBy: ['unknown'] } } }] }
    let prompt = ''
    const h = harness([
      async (_request, _signal, emit) => emit({ type: 'proposal', document: UNCITED, unknowns: [] }),
      chatReviewer(blocked),
      async (request, _signal, emit) => {
        prompt = request.prompt
        emit({ type: 'proposal', document: { expectedDisposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'requested', triggeredBy: ['unknown'] } }, rationale: 'A missing fact retains unknown.' }, unknowns: [] })
      }
    ], { mode: 'draft' })
    h.run.start('Without the hours, a person must decide.', [])
    const state = await settled(h.run)
    expect(state.expectationIssues.map(issue => issue.id)).toEqual(['missing-hours'])
    h.run.proposeExpectationCorrection('missing-hours')
    const proposed = await settled(h.run)
    expect(prompt).toContain("THE PERSON'S STATEMENT\n" + JSON.stringify({ id: 'you-1', text: 'Without the hours, a person must decide.' }))
    expect(prompt).not.toContain('SOURCE EXCERPT')
    expect(proposed.expectationIssues[0]!.proposal).toBeDefined()
  })

  it('tells the model how a chat draft cites, only in the conversation modes', async () => {
    const text = async (mode: 'draft' | 'web-research' | 'research') => {
      let said = ''
      const h = harness([async (request, signal, emit) => {
        const answer = await request.hostTools.find(tool => tool.name === 'get_authoring_instructions')!.execute({}, signal)
        said = answer.content?.[0]?.text ?? ''
        emit({ type: 'message', text: 'Read.' })
      }], { mode })
      h.run.start('Draft', [])
      await settled(h.run)
      return said
    }
    for (const mode of ['draft', 'web-research'] as const) {
      const said = await text(mode)
      expect(said).toContain('attachment:<id>/<digest>/page/<n>')
      expect(said).toContain('the exact quote from that page')
    }
    expect(await text('research')).not.toContain('attachment:<id>/<digest>/page/<n>')
  })
})
