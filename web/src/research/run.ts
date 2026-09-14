/**
 * The research-backed authoring run: research and draft, establish test cases
 * independently from the sources, check the candidate through the runtime,
 * repair, and present for review. A conversation the person can join at any
 * stage, and a Stop that ends it at the last completed stage.
 *
 * **What this controller owns and what it does not.** It owns the sequence,
 * the budgets, the transcript, the candidates and the cases; it decides no
 * verdict of its own. The engine proposes; the runtime validates and
 * rehearses; the gateway acquires and signs; the verifier checks receipts
 * under the pinned key. Every port is injected, so the whole run is driven in
 * tests by fixtures and no framework type crosses this boundary.
 *
 * The draft/check/review shape and the rule that an established expectation is
 * never rewritten to make a candidate pass are carried over from desk PR #73's
 * proof; the research phase, the source ledger, the citation trace and the
 * conversation are this run's.
 */
import type { AssistantEvent, CallTool, HostTool } from '../assistant/engine'
import { checkCandidate, digestOf, jsonIdentity, type AuthoringCase, type CandidateCheck } from './checkCandidate'
import type { Ledger, SourceRecord } from './ledger'
import { CASES_INSTRUCTIONS, CONVERSATION_INSTRUCTIONS, REPAIR_INSTRUCTIONS, RESEARCH_INSTRUCTIONS } from './prompts'
import { memberOf, parseJsonText, stringMember, type JsonNode } from './verify/canon'
import { verifySession, type Finding, type HeldReceipt, type SessionVerdict } from './verify/session'

export type Phase = 'idle' | 'research' | 'cases' | 'check' | 'repair' | 'conversation' | 'review'
export type Status = 'idle' | 'running' | 'ready' | 'needs-input' | 'budget' | 'stalled' | 'stopped' | 'failed'

export interface Turn {
  role: 'user' | 'assistant'
  kind: 'brief' | 'message' | 'unknowns' | 'note'
  text: string
  at: string
}

export interface Candidate {
  revision: number
  document: unknown
  text: string
  digest: string
  producedBy: 'research' | 'repair' | 'conversation'
  check?: CandidateCheck
}

/** One `sources[]` entry of the candidate, traced to the ledger or not. */
export interface Citation {
  sourceId: string
  location: string | null
  excerptId: string | null
  url: string | null
  traced: boolean
  reason: string
}

export interface RunState {
  phase: Phase
  status: Status
  detail: string
  brief: string
  seedUrls: string[]
  turns: Turn[]
  events: AssistantEvent[]
  candidates: Candidate[]
  cases: AuthoringCase[]
  droppedCases: { id: string; reason: string }[]
  unknowns: string[]
  citations: Citation[]
  verdicts: Record<string, SessionVerdict>
  revisionsUsed: number
  /** The gateway sessions this run opened, in order. */
  sessions: string[]
}

export interface TurnRequest {
  prompt: string
  hostTools: HostTool[]
  /** A reviewer turn runs with thinking off: fresh, and no critic. */
  reviewer?: boolean
}

export interface RunPorts {
  /** One engine run to completion; events arrive in order, `end` last. */
  turn(request: TurnRequest, signal: AbortSignal, onEvent: (event: AssistantEvent) => void): Promise<void>
  /** The desk's own runtime connection, for validate and rehearsal. */
  callTool: CallTool
  ledger: Ledger
  /** The research tools bound to the ledger, or none where research is not configured. */
  researchTools: HostTool[]
  seal(session: string, signal: AbortSignal): Promise<void>
  registry(signal: AbortSignal): Promise<string>
  gateway: { authority: string; publicKeyHex: string } | null
  newSession(): string
  authorPrompt: string
  maxRevisions: number
  log(text: string): void
  now?: () => Date
}

export const INITIAL_STATE: RunState = {
  phase: 'idle',
  status: 'idle',
  detail: '',
  brief: '',
  seedUrls: [],
  turns: [],
  events: [],
  candidates: [],
  cases: [],
  droppedCases: [],
  unknowns: [],
  citations: [],
  verdicts: {},
  revisionsUsed: 0,
  sessions: []
}

class Stopped extends Error {
  constructor() {
    super('stopped')
    this.name = 'AbortError'
  }
}

const KEBAB = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/
const EXCERPT_ID = /^src-\d+#e\d+$/

function foldSpace(text: string): string {
  return text.replace(/\s+/g, ' ').trim()
}

/** The candidate's `sources[]`, each traced to a recorded excerpt or not. */
export function traceCitations(document: unknown, ledger: Ledger): Citation[] {
  const sources = (document as { sources?: unknown })?.sources
  if (!Array.isArray(sources)) return []
  return sources.map((entry, index) => {
    const source = (entry ?? {}) as Record<string, unknown>
    const sourceId = typeof source.id === 'string' ? source.id : `#${index}`
    const locator = (source.locator ?? {}) as Record<string, unknown>
    const url = typeof locator.value === 'string' ? locator.value : null
    const citation = (source.citation ?? {}) as Record<string, unknown>
    const location = typeof citation.location === 'string' ? citation.location : null
    const quoted = typeof citation.excerpt === 'string' ? citation.excerpt : null
    if (location === null || !EXCERPT_ID.test(location)) {
      return { sourceId, location, excerptId: null, url, traced: false, reason: 'no excerpt id in citation.location; this source was not read in this run' }
    }
    const excerpt = ledger.excerpt(location)
    if (!excerpt) return { sourceId, location, excerptId: location, url, traced: false, reason: `no excerpt ${location} was recorded in this run` }
    const record = ledger.byId(excerpt.sourceId)
    if (quoted === null || foldSpace(quoted) !== foldSpace(excerpt.text)) {
      return { sourceId, location, excerptId: location, url, traced: false, reason: 'citation.excerpt is not the recorded excerpt text' }
    }
    if (record?.document && url !== null && url !== record.document.url && url !== record.request.url) {
      return { sourceId, location, excerptId: location, url, traced: false, reason: 'locator.value is not the URL the excerpt was read from' }
    }
    return { sourceId, location, excerptId: location, url, traced: true, reason: '' }
  })
}

/** Cases the reviewer proposed, each held to its shape and to an excerpt this run recorded. */
export function admitCases(
  proposed: unknown,
  ledger: Ledger,
  established: AuthoringCase[]
): { admitted: AuthoringCase[]; dropped: { id: string; reason: string }[] } {
  const cases = (proposed as { cases?: unknown })?.cases
  const admitted: AuthoringCase[] = []
  const dropped: { id: string; reason: string }[] = []
  if (!Array.isArray(cases)) return { admitted, dropped: [{ id: '(all)', reason: 'the reviewer answered without a cases array' }] }
  const seen = new Set(established.map((row) => row.id))
  cases.forEach((entry, index) => {
    const row = (entry ?? {}) as Record<string, unknown>
    const id = typeof row.id === 'string' ? row.id : `#${index}`
    if (!KEBAB.test(id)) return dropped.push({ id, reason: 'the id is not kebab-case' })
    if (seen.has(id)) return dropped.push({ id, reason: 'a case with this id is already established and is never rewritten' })
    if (row.facts === null || typeof row.facts !== 'object' || Array.isArray(row.facts)) return dropped.push({ id, reason: 'facts must be an object' })
    const disposition = row.expectedDisposition as Record<string, unknown> | undefined
    if (!disposition || typeof disposition !== 'object' || typeof disposition.kind !== 'string') {
      return dropped.push({ id, reason: 'expectedDisposition must carry a kind' })
    }
    const source = typeof row.expectationSource === 'string' ? row.expectationSource : ''
    if (!EXCERPT_ID.test(source) || !ledger.excerpt(source)) {
      return dropped.push({ id, reason: `expectationSource ${JSON.stringify(source)} is not an excerpt recorded in this run` })
    }
    seen.add(id)
    admitted.push({
      id,
      facts: row.facts,
      ...(row.evidenceAvailability !== undefined ? { evidenceAvailability: row.evidenceAvailability } : {}),
      expectedDisposition: row.expectedDisposition,
      ...(row.expectedHandoffTarget !== undefined ? { expectedHandoffTarget: row.expectedHandoffTarget } : {}),
      expectationSource: source,
      rationale: typeof row.rationale === 'string' ? row.rationale : ''
    })
  })
  return { admitted, dropped }
}

export class AuthoringRun {
  private state: RunState = INITIAL_STATE
  private listeners = new Set<() => void>()
  private controller: AbortController | null = null

  constructor(private readonly ports: RunPorts) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  getSnapshot = (): RunState => this.state

  private set(patch: Partial<RunState>): void {
    this.state = { ...this.state, ...patch }
    for (const listener of this.listeners) listener()
  }

  private stamp(): string {
    return (this.ports.now?.() ?? new Date()).toISOString()
  }

  private addTurn(turn: Omit<Turn, 'at'>): void {
    this.set({ turns: [...this.state.turns, { ...turn, at: this.stamp() }] })
  }

  get running(): boolean {
    return this.state.status === 'running'
  }

  /** Stop the run at its last completed stage. */
  stop(): void {
    this.controller?.abort()
  }

  /** Begin: the brief, the URLs to read first, and the research turn. */
  start(brief: string, seedUrls: string[]): void {
    if (this.running) return
    this.state = { ...INITIAL_STATE, brief, seedUrls, phase: 'research', status: 'running', detail: 'Researching sources and drafting.' }
    this.addTurn({ role: 'user', kind: 'brief', text: brief + (seedUrls.length ? `\n\nRead first:\n${seedUrls.join('\n')}` : '') })
    void this.drive(async (signal) => {
      await this.researchTurn(signal)
      await this.casesAndCheck(signal)
    })
  }

  /** A message from the person, at any rest state. */
  send(message: string): void {
    if (this.running || this.state.phase === 'idle') return
    this.addTurn({ role: 'user', kind: 'message', text: message })
    this.set({ status: 'running', phase: 'conversation', detail: 'Answering.' })
    void this.drive(async (signal) => {
      const before = this.latest()?.digest
      await this.engineTurn(signal, 'conversation', this.conversationPrompt(message), this.ports.researchTools)
      if (this.latest()?.digest !== before) await this.casesAndCheck(signal)
      else {
        const check = this.latest()?.check
        const passing = check !== undefined && check.valid && check.cases.length > 0 && check.cases.every((c) => c.passed)
        this.set({ status: passing ? 'ready' : 'needs-input', phase: 'review', detail: passing ? this.state.detail : 'Answered. The candidate still has disagreements or no established cases.' })
      }
    })
  }

  private latest(): Candidate | undefined {
    return this.state.candidates.at(-1)
  }

  private async drive(work: (signal: AbortSignal) => Promise<void>): Promise<void> {
    const controller = new AbortController()
    this.controller = controller
    try {
      await work(controller.signal)
    } catch (cause) {
      if ((cause as Error)?.name === 'AbortError' || cause instanceof Stopped) {
        this.set({ status: 'stopped', detail: 'Stopped. The last completed stage is kept.' })
      } else {
        this.set({ status: 'failed', detail: (cause as Error)?.message ?? String(cause) })
      }
    } finally {
      if (this.controller === controller) this.controller = null
    }
  }

  private check(signal: AbortSignal): void {
    if (signal.aborted) throw new Stopped()
  }

  // ---- turns ---------------------------------------------------------------

  private researchPrompt(): string {
    const { brief, seedUrls } = this.state
    return [
      this.ports.authorPrompt,
      RESEARCH_INSTRUCTIONS,
      `THE BRIEF\n${brief}`,
      seedUrls.length ? `URLS TO READ FIRST\n${seedUrls.join('\n')}` : ''
    ]
      .filter(Boolean)
      .join('\n\n')
  }

  private casesPrompt(document: unknown): string {
    const outcomes = ((document as { outcomes?: unknown })?.outcomes ?? []) as unknown[]
    const excerpts = this.ports.ledger.sources.flatMap((record) =>
      record.excerpts.map((excerpt) => ({ id: excerpt.id, url: record.document?.url ?? record.request.url, title: record.document?.title, text: excerpt.text }))
    )
    const evidence = ((document as { evidenceRequirements?: unknown })?.evidenceRequirements ?? []) as unknown[]
    return [
      CASES_INSTRUCTIONS,
      `OUTCOMES\n${JSON.stringify(outcomes)}`,
      `EVIDENCE REQUIREMENTS\n${JSON.stringify(evidence)}`,
      `FACT PATHS THE PACK READS\n${JSON.stringify(factPaths(document))}`,
      `EXCERPTS\n${JSON.stringify(excerpts, null, 1)}`
    ].join('\n\n')
  }

  private repairPrompt(): string {
    const candidate = this.latest()!
    return [
      this.ports.authorPrompt,
      RESEARCH_INSTRUCTIONS,
      REPAIR_INSTRUCTIONS,
      `CANDIDATE\n${candidate.text}`,
      `CHECK\n${JSON.stringify({ valid: candidate.check?.valid, diagnostics: candidate.check?.diagnostics, cases: candidate.check?.cases })}`,
      `ESTABLISHED CASES\n${JSON.stringify(this.state.cases)}`,
      this.transcript()
    ].join('\n\n')
  }

  private conversationPrompt(message: string): string {
    const candidate = this.latest()
    return [
      this.ports.authorPrompt,
      RESEARCH_INSTRUCTIONS,
      CONVERSATION_INSTRUCTIONS,
      candidate ? `CURRENT DOCUMENT\n${candidate.text}` : '',
      this.state.cases.length ? `ESTABLISHED CASES\n${JSON.stringify(this.state.cases)}` : '',
      candidate?.check ? `LATEST CHECK\n${JSON.stringify(candidate.check.cases)}` : '',
      this.transcript(),
      `THE PERSON'S MESSAGE\n${message}`
    ]
      .filter(Boolean)
      .join('\n\n')
  }

  private transcript(): string {
    const lines = this.state.turns
      .filter((turn) => turn.kind !== 'note')
      .map((turn) => `${turn.role === 'user' ? 'PERSON' : 'ASSISTANT'}: ${turn.text}`)
    return `TRANSCRIPT SO FAR\n${lines.join('\n\n')}`
  }

  /** One engine turn: events recorded, the proposal taken as the next candidate. */
  private async engineTurn(
    signal: AbortSignal,
    producedBy: Candidate['producedBy'],
    prompt: string,
    hostTools: HostTool[],
    reviewer = false
  ): Promise<{ document: unknown; unknowns: string[] } | null> {
    this.check(signal)
    const session = this.ports.newSession()
    this.ports.ledger.openSession(session)
    this.set({ sessions: [...this.state.sessions, session] })
    let proposal: { document: unknown; unknowns: string[] } | null = null
    let failure: string | null = null
    await this.ports.turn({ prompt, hostTools, reviewer }, signal, (event) => {
      this.set({ events: [...this.state.events, event] })
      if (event.type === 'message') this.addTurn({ role: 'assistant', kind: 'message', text: event.text })
      if (event.type === 'proposal') proposal = { document: event.document, unknowns: event.unknowns }
      if (event.type === 'error') failure = event.message
    })
    this.check(signal)
    await this.verifyAcquisitions(session, signal)
    if (failure !== null) throw new Error(failure)
    if (proposal === null) throw new Error('the assistant ended without a proposal')
    const taken = proposal as { document: unknown; unknowns: string[] }
    if (!reviewer) {
      const text = JSON.stringify(taken.document, null, 2)
      const digest = await digestOf(text)
      const identity = jsonIdentity(taken.document)
      const repeated = this.state.candidates.some((candidate) => jsonIdentity(candidate.document) === identity)
      const candidate: Candidate = { revision: this.state.candidates.length + 1, document: taken.document, text, digest, producedBy }
      if (!repeated) {
        this.set({
          candidates: [...this.state.candidates, candidate],
          unknowns: taken.unknowns,
          citations: traceCitations(taken.document, this.ports.ledger)
        })
      } else if (producedBy === 'conversation') {
        // An answer that returned the document unchanged is an answer, and
        // the candidate on hand keeps its check.
        this.set({ unknowns: taken.unknowns })
      } else {
        this.set({ unknowns: taken.unknowns })
        this.addTurn({ role: 'assistant', kind: 'note', text: 'The assistant repeated an earlier candidate.' })
        throw new Stalled()
      }
      if (taken.unknowns.length) this.addTurn({ role: 'assistant', kind: 'unknowns', text: taken.unknowns.map((line) => `• ${line}`).join('\n') })
    }
    return taken
  }

  private async researchTurn(signal: AbortSignal): Promise<void> {
    this.set({ phase: 'research', detail: 'Researching sources and drafting.' })
    this.ports.log('research: drafting from sources')
    await this.engineTurn(signal, 'research', this.researchPrompt(), this.ports.researchTools)
  }

  /** Establish cases where none are, then check, and repair until the budget. */
  private async casesAndCheck(signal: AbortSignal): Promise<void> {
    const candidate = this.latest()
    if (!candidate) throw new Error('no candidate to check')
    if (this.state.cases.length === 0) {
      this.set({ phase: 'cases', detail: 'Establishing test cases from the sources.' })
      this.ports.log('cases: a reviewer establishes expectations from the excerpts')
      const proposal = await this.engineTurn(signal, 'research', this.casesPrompt(candidate.document), [], true)
      const { admitted, dropped } = admitCases(proposal?.document, this.ports.ledger, this.state.cases)
      this.set({ cases: admitted, droppedCases: dropped })
      if (dropped.length) {
        this.addTurn({ role: 'assistant', kind: 'note', text: `Dropped ${dropped.length} proposed case(s): ${dropped.map((d) => `${d.id} (${d.reason})`).join('; ')}` })
      }
      if (proposal?.unknowns.length) this.addTurn({ role: 'assistant', kind: 'unknowns', text: proposal.unknowns.map((line) => `• ${line}`).join('\n') })
      if (admitted.length === 0) {
        this.set({ status: 'needs-input', phase: 'review', detail: 'No test case could be grounded in a cited excerpt. Cite the requirements, or say what the cases should be.' })
        return
      }
      this.ports.log(`cases: ${admitted.length} established, ${dropped.length} dropped`)
    }
    for (;;) {
      this.check(signal)
      const current = this.latest()!
      this.set({ phase: 'check', detail: `Checking revision ${current.revision} through the runtime.` })
      this.ports.log(`check: revision ${current.revision} — validate, then ${this.state.cases.length} rehearsal(s)`)
      const check = await checkCandidate(current.text, this.state.cases, this.ports.callTool, signal)
      this.set({
        candidates: this.state.candidates.map((candidate) => (candidate.digest === current.digest ? { ...candidate, check } : candidate))
      })
      const passed = check.valid && check.cases.length > 0 && check.cases.every((row) => row.passed)
      this.ports.log(`check: revision ${current.revision} — ${check.valid ? 'valid' : 'invalid'}, ${check.cases.filter((c) => c.passed).length}/${check.cases.length} agree`)
      if (passed) {
        this.set({ phase: 'review', status: 'ready', detail: 'Every established case agrees. Review the draft, its sources and the unknowns before creating the pack.' })
        return
      }
      if (this.state.revisionsUsed >= this.ports.maxRevisions) {
        this.set({ phase: 'review', status: 'budget', detail: `The revision budget of ${this.ports.maxRevisions} is spent with disagreements remaining. Review the results, or send a message to continue.` })
        return
      }
      this.set({ phase: 'repair', revisionsUsed: this.state.revisionsUsed + 1, detail: 'Repairing the candidate against the established cases.' })
      this.ports.log(`repair: revision ${this.state.revisionsUsed} of ${this.ports.maxRevisions}`)
      try {
        await this.engineTurn(signal, 'repair', this.repairPrompt(), this.ports.researchTools)
      } catch (cause) {
        if (cause instanceof Stalled) {
          this.set({ phase: 'review', status: 'stalled', detail: 'The assistant repeated an earlier candidate. Review the disagreements, or send a message to steer it.' })
          return
        }
        throw cause
      }
    }
  }

  // ---- verification ----------------------------------------------------------

  /** Seal the session the turn acquired under, fetch the registry, verify, and mark every record. */
  private async verifyAcquisitions(session: string, signal: AbortSignal): Promise<void> {
    const ledger = this.ports.ledger
    const records = ledger.sources.filter((record) => record.acquisition?.session === session)
    if (records.length === 0) return
    if (!this.ports.gateway) {
      for (const record of records) ledger.verified(record.id, { state: 'failed', at: this.stamp(), findings: [{ sessionId: session, callIndex: null, status: 'no-pinned-key' }] })
      return
    }
    const stamp = this.stamp()
    const held: HeldReceipt[] = [...records]
      .sort((a, b) => a.acquisition!.callIndex - b.acquisition!.callIndex)
      .map((record) => ({ receipt: record.response!.receipt, result: record.response!.result }))
    let registryText = ''
    try {
      await this.ports.seal(session, signal)
      registryText = await this.ports.registry(signal)
    } catch (cause) {
      if ((cause as Error)?.name === 'AbortError') throw cause
      const finding: Finding = { sessionId: session, callIndex: null, status: 'seal-or-registry-unavailable' }
      this.ports.log(`verify: ${session} — ${(cause as Error).message}`)
      for (const record of records) ledger.verified(record.id, { state: 'failed', at: stamp, findings: [finding] })
      return
    }
    const verdict = await verifySession({
      sessionId: session,
      authority: this.ports.gateway.authority,
      publicKeyHex: this.ports.gateway.publicKeyHex,
      receipts: held,
      registryText
    })
    this.set({ verdicts: { ...this.state.verdicts, [session]: verdict } })
    this.ports.log(`verify: ${session} — ${verdict.ok ? 'verified' : 'FAILED'} (${verdict.findings.map((f) => f.status).join(', ')})`)
    for (const record of records) {
      const own = verdict.findings.filter((f) => f.callIndex === record.acquisition!.callIndex || f.callIndex === null)
      const ok = verdict.ok
      ledger.verified(record.id, ok ? { state: 'verified', at: stamp, keyId: verdict.keyId } : { state: 'failed', at: stamp, findings: own })
    }
  }
}

class Stalled extends Error {
  constructor() {
    super('stalled')
    this.name = 'Stalled'
  }
}

/** Every `fact.path` the candidate reads, for the reviewer's facts documents. */
export function factPaths(document: unknown): string[] {
  const paths = new Set<string>()
  const walk = (value: unknown): void => {
    if (Array.isArray(value)) return value.forEach(walk)
    if (value !== null && typeof value === 'object') {
      const record = value as Record<string, unknown>
      if (record.op === 'fact' && typeof record.path === 'string') paths.add(record.path)
      Object.values(record).forEach(walk)
    }
  }
  walk(document)
  return [...paths].sort()
}

/** The research record written beside a created pack: every source, receipt and excerpt. */
export function researchRecord(state: RunState, ledger: Ledger, packDigest: string): unknown {
  const sourceOf = (record: SourceRecord) => ({
    id: record.id,
    kind: record.kind,
    requestedAt: record.requestedAt,
    request: record.request,
    ...(record.failure ? { failure: record.failure } : {}),
    ...(record.acquisition
      ? {
          receipt: {
            sessionId: record.acquisition.session,
            callIndex: record.acquisition.callIndex,
            signature: record.acquisition.signature,
            resultDigest: record.acquisition.resultDigest,
            observedAt: record.acquisition.observedAt,
            endpoint: record.acquisition.endpoint,
            snapshot: record.acquisition.snapshot,
            peerIdentity: record.acquisition.peerIdentity,
            adapter: record.acquisition.adapter
          }
        }
      : {}),
    ...(record.document
      ? {
          page: {
            url: record.document.url,
            title: record.document.title,
            chars: record.document.text.length,
            httpStatus: record.document.httpStatus ?? null,
            pageDates: record.document.pageDates,
            providerReportedTime: record.document.providerReportedTime ?? null
          }
        }
      : {}),
    ...(record.hits ? { hits: record.hits } : {}),
    verification: record.verification,
    excerpts: record.excerpts
  })
  return {
    researchRecordVersion: '1',
    packSha256: packDigest,
    brief: state.brief,
    seedUrls: state.seedUrls,
    sessions: state.sessions,
    verdicts: state.verdicts,
    citations: state.citations,
    cases: state.cases,
    droppedCases: state.droppedCases,
    unknowns: state.unknowns,
    sources: ledger.sources.map(sourceOf),
    note: 'A receipt establishes byte lineage from the gateway within its stated bounds, not the truth, currency or legal authority of a source, nor the origin of a page beyond the provider that rendered it.'
  }
}

/** The matrix rows a created pack is registered with: every established case, citing its receipts. */
export function matrixDocument(state: RunState, ledger: Ledger): unknown {
  return {
    matrixVersion: '3',
    cases: state.cases.map((row) => {
      const excerpt = ledger.excerpt(row.expectationSource)
      const record = excerpt ? ledger.byId(excerpt.sourceId) : undefined
      const cites =
        record?.acquisition && record.verification.state === 'verified'
          ? [{ sessionId: record.acquisition.session, callIndex: record.acquisition.callIndex, signature: record.acquisition.signature }]
          : []
      return {
        id: row.id,
        origin: 'research',
        facts: row.facts,
        ...(row.evidenceAvailability !== undefined ? { evidenceAvailability: row.evidenceAvailability } : {}),
        expectedDisposition: row.expectedDisposition,
        ...(row.expectedHandoffTarget !== undefined ? { expectedHandoffTarget: row.expectedHandoffTarget } : {}),
        focus: `${row.rationale} [${row.expectationSource}]`,
        ...(cites.length ? { cites } : {})
      }
    })
  }
}

/** For the review: which receipts a document's own values came from. */
export function receiptsOf(record: SourceRecord): JsonNode | null {
  return record.response?.receipt ?? null
}

export { parseJsonText, memberOf, stringMember }
