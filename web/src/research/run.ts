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
import { isCancelled } from '../assistant/engines/contract'
import { checkCandidate, digestOf, jsonIdentity, type AuthoringCase, type CandidateCheck } from './checkCandidate'
import { findingSummary, validateExpectations } from './expectations'
import type { Ledger, SourceRecord } from './ledger'
import { CASES_INSTRUCTIONS, CONTINUE_INSTRUCTIONS, CONVERSATION_INSTRUCTIONS, REPAIR_INSTRUCTIONS, RESEARCH_INSTRUCTIONS } from './prompts'
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

export interface ExpectationIssue {
  id: string
  original: AuthoringCase
  message: string
  proposal?: { expectedDisposition: unknown; rationale: string; token: string; candidateDigest: string }
  proposalError?: string
  resolved?: { replacement: AuthoringCase; approvedAt: string; rationale: string }
}

/**
 * A reviewer's screened case proposal, held while its expectations are put to
 * the runtime. It is bound to the candidate it was proposed against, because a
 * suite is proposed from that draft's outcomes and fact paths and a draft that
 * has moved on gets a fresh reviewer, as establishment has always answered a
 * changed draft.
 */
export interface HeldProposal {
  candidateDigest: string
  admitted: AuthoringCase[]
  dropped: { id: string; reason: string }[]
  unknowns: string[]
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
  /** A screened proposal awaiting validation, never an established case. */
  heldProposal: HeldProposal | null
  expectationIssues: ExpectationIssue[]
  unknowns: string[]
  citations: Citation[]
  verdicts: Record<string, SessionVerdict>
  /** The registry text each session's verdict was reached with. */
  registries: Record<string, string>
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
  /** The seconds any one action may take, after which it is stopped as over budget. */
  seconds: number
  log(text: string): void
  now?: () => Date
}

/** How many times a turn that spent its steps before proposing is continued. */
export const MAX_CONTINUATIONS = 3

/** The engine's own sentence for a final turn with no proposal fence in it. */
const NO_FENCE = /exactly one fenced JSON block.*carried 0/

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
  heldProposal: null,
  expectationIssues: [],
  unknowns: [],
  citations: [],
  verdicts: {},
  registries: {},
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

function deepFreeze<T>(value: T): T {
  if (value !== null && typeof value === 'object') {
    Object.freeze(value)
    for (const member of Object.values(value as Record<string, unknown>)) deepFreeze(member)
  }
  return value
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
    if (url === null) return { sourceId, location, excerptId: location, url, traced: false, reason: 'locator.value is not a string URL' }
    if (!record?.document || (url !== record.document.url && url !== record.request.url)) {
      return { sourceId, location, excerptId: location, url, traced: false, reason: 'locator.value is not the URL the excerpt was read from' }
    }
    if (record.verification.state !== 'verified') {
      return { sourceId, location, excerptId: location, url, traced: false, reason: `the receipt for ${record.id} is ${record.verification.state === 'failed' ? 'failed' : 'unchecked'}, so its excerpt is withheld` }
    }
    return { sourceId, location, excerptId: location, url, traced: true, reason: '' }
  })
}

/** Screen source-grounded case proposals; runtime expectation validation must follow before admission. */
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
    const source = typeof row.expectationSource === 'string' ? row.expectationSource : ''
    if (!EXCERPT_ID.test(source) || !ledger.excerpt(source)) {
      return dropped.push({ id, reason: `expectationSource ${JSON.stringify(source)} is not an excerpt recorded in this run` })
    }
    if (!ledger.verifiedExcerpt(source)) {
      return dropped.push({ id, reason: `expectationSource ${source} is from a source whose receipt did not verify, so it grounds nothing` })
    }
    seen.add(id)
    // An owned, frozen copy: the reviewer's objects stay the reviewer's, and
    // an established case cannot be reached through them afterwards.
    admitted.push(
      deepFreeze(
        structuredClone({
          id,
          facts: row.facts,
          ...(row.evidenceAvailability !== undefined ? { evidenceAvailability: row.evidenceAvailability } : {}),
          expectedDisposition: row.expectedDisposition ?? null,
          ...(row.expectedHandoffTarget !== undefined ? { expectedHandoffTarget: row.expectedHandoffTarget } : {}),
          expectationSource: source,
          rationale: typeof row.rationale === 'string' ? row.rationale : ''
        })
      )
    )
  })
  return { admitted, dropped }
}

export class AuthoringRun {
  private state: RunState = INITIAL_STATE
  private listeners = new Set<() => void>()
  private controller: AbortController | null = null
  private deadline: ReturnType<typeof setTimeout> | null = null
  private outOfTime = false

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

  /**
   * The time budget, armed for each action the person takes -- the run itself, a
   * message, a correction request, an approval. When it fires, whatever is in
   * flight is cancelled through the same signal Stop uses, and the outcome is
   * reported as `budget` rather than `stopped`. Per action rather than per run,
   * because a conversation turn after a settled run had no bound at all, and a
   * Stop after a spent budget was reported as the budget rather than as a stop.
   */
  private arm(): void {
    this.outOfTime = false
    if (this.deadline !== null) clearTimeout(this.deadline)
    this.deadline = setTimeout(() => {
      this.outOfTime = true
      this.controller?.abort()
    }, this.ports.seconds * 1000)
  }

  private disarm(): void {
    if (this.deadline !== null) clearTimeout(this.deadline)
    this.deadline = null
  }

  /** Begin: the brief, the URLs to read first, and the research turn. */
  start(brief: string, seedUrls: string[]): void {
    if (this.running) return
    this.arm()
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
    this.arm()
    this.addTurn({ role: 'user', kind: 'message', text: message })
    this.set({ status: 'running', phase: 'conversation', detail: 'Answering.' })
    void this.drive(async (signal) => {
      const before = this.latest()?.digest
      await this.continuingTurn(signal, 'conversation', this.conversationPrompt(message), this.ports.researchTools)
      if (this.latest()?.digest !== before) await this.casesAndCheck(signal)
      else this.settleReview('Answered. The candidate still has disagreements or no established cases.')
    })
  }

  /** A fresh reviewer corrects only the expectation; the candidate and case inputs are fixed. */
  proposeExpectationCorrection(id: string): void {
    const issue = this.state.expectationIssues.find(item => item.id === id && !item.resolved)
    if (this.running || !issue || !this.latest()) return
    this.arm()
    this.set({ phase: 'review', status: 'running', detail: `Reviewing the expectation for ${id}.`,
      expectationIssues: this.state.expectationIssues.map(item => item.id === id ? { ...item, proposal: undefined, proposalError: undefined } : item) })
    void this.drive(async signal => {
      const excerpt = this.ports.ledger.verifiedExcerpt(issue.original.expectationSource)
      if (!excerpt) throw new Error('The expectation source is no longer verified. Restore its verification before reviewing the expectation.')
      const candidate = this.latest()!
      const document = candidate.document as Record<string, unknown>
      // No authoring prompt here: this turn reviews one expectation against the
      // source and the contract, and pack-authoring guidance is not its brief.
      const prompt = ['CORRECT AN INVALID EXPECTATION. Preserve the case id, facts, evidence availability, source and intended behavior. Propose only a complete expectedDisposition and a rationale explaining the correction under JPS §8.3. Empty reasons is valid only for outcome; unresolved must retain its reason(s). Do not remove a case, change the pack, or weaken the assertion to fit a candidate. If the cited source and contract do not settle the correction, return unknowns and no correction. The person must approve any correction before it is used.',
        `CASE\n${JSON.stringify(issue.original)}`, `VALIDATOR FINDING\n${issue.message}`,
        `SOURCE EXCERPT\n${JSON.stringify(excerpt)}`, `DECLARED OUTCOMES\n${JSON.stringify(document.outcomes)}`,
        `HANDOFF CONFIGURATION\n${JSON.stringify(document.escalation ?? null)}`,
        `EXPECTED HANDOFF TARGET (asserted with the disposition and unchanged by this correction)\n${JSON.stringify(issue.original.expectedHandoffTarget ?? null)}`,
        'Answer with prose and exactly one fenced JSON block: {"proposal":{"document":{"expectedDisposition":{...},"rationale":"..."},"unknowns":[]}}.'
      ].join('\n\n')
      const response = await this.engineTurn(signal, 'research', prompt, [], true)
      const value = response?.document as { expectedDisposition?: unknown; rationale?: unknown } | null
      let proposal: ExpectationIssue['proposal']
      let proposalError = response?.unknowns.length ? response.unknowns.join('\n') : ''
      if (!proposalError && (!value || typeof value.rationale !== 'string' || !value.rationale.trim())) proposalError = 'The reviewer did not explain a correction. The expectation remains blocked.'
      if (!proposalError && value) {
        const [finding] = await validateExpectations([value.expectedDisposition], this.ports.callTool, signal)
        if (finding!.status === 'invalid') proposalError = findingSummary(finding!)
        else if (targetContradiction(finding!.canonical, issue.original.expectedHandoffTarget)) proposalError = targetContradiction(finding!.canonical, issue.original.expectedHandoffTarget)!
        else {
          const rationale = (value.rationale as string).trim()
          proposal = deepFreeze({ expectedDisposition: JSON.parse(finding!.canonical), rationale, candidateDigest: candidate.digest,
            token: await digestOf(JSON.stringify({ id, original: issue.original, disposition: finding!.canonical, rationale, candidateDigest: candidate.digest })) })
        }
      }
      this.check(signal)
      this.set({ phase: 'review', status: 'needs-input', detail: proposal ? 'Review the proposed expectation correction before approving it.' : 'The expectation still needs a decision.',
        expectationIssues: this.state.expectationIssues.map(item => item.id === id ? { ...item, proposal, proposalError: proposalError || undefined } : item) })
    })
  }

  /** Explicit approval, bound to the visible proposal. Recheck all cases on unchanged pack bytes. */
  approveExpectationCorrection(id: string, token: string): void {
    const issue = this.state.expectationIssues.find(item => item.id === id && !item.resolved)
    const proposal = issue?.proposal
    // The digest comparison is a backup, and a passing suite is not evidence it
    // is reachable: every new candidate already clears the pending proposals, so
    // no sequence of public calls leaves one standing beside a draft it was not
    // read against. `run.test.ts` writes that state directly to hold it here.
    if (this.running || !issue || !proposal || proposal.token !== token || proposal.candidateDigest !== this.latest()?.digest) return
    this.arm()
    this.set({ phase: 'review', status: 'running', detail: `Validating the approved correction for ${id}.` })
    void this.drive(async signal => {
      if (!this.ports.ledger.verifiedExcerpt(issue.original.expectationSource)) throw new Error('The expectation source is no longer verified; the correction was not applied.')
      const [finding] = await validateExpectations([proposal.expectedDisposition], this.ports.callTool, signal)
      if (finding!.status !== 'valid') throw new Error(`The correction is no longer valid: ${findingSummary(finding)}`)
      // The exact expectation is the pair. A corrected disposition that no
      // longer agrees with the target the case asserts could never pass, so it
      // is refused here rather than applied and left to stall the run.
      const contradiction = targetContradiction(finding.canonical, issue.original.expectedHandoffTarget)
      if (contradiction) throw new Error(`The correction was not applied: ${contradiction}`)
      // Nothing is applied on a closed run. A backup for a window that holds
      // only microtasks -- a validation that answers after Stop -- so a suite
      // that never lands in it is no evidence this is dead code.
      this.check(signal)
      const replacement = deepFreeze(structuredClone({ ...issue.original, expectedDisposition: proposal.expectedDisposition }))
      // No established case can be silently replaced by this approval. A backup:
      // admitted ids are unique and an id kept as an issue is never also a case,
      // so no run produces the collision it refuses.
      if (this.state.cases.some(row => row.id === id)) throw new Error('A case with this id is already established; the correction was not applied.')
      this.set({ cases: [...this.state.cases, replacement],
        // Every check goes, so nothing downstream reads one taken before this
        // case joined the suite. A backup too: a run blocked on an expectation
        // never checked its draft, so today there is no check here to drop.
        candidates: this.state.candidates.map(({ check: _check, ...candidate }) => candidate),
        expectationIssues: this.state.expectationIssues.map(item => item.id === id ? { ...item, resolved: { replacement, approvedAt: this.stamp(), rationale: proposal.rationale } } : item) })
      this.addTurn({ role: 'user', kind: 'note', text: `Approved the corrected expectation for ${id}: ${proposal.rationale}` })
      await this.casesAndCheck(signal, false)
    })
  }

  /**
   * Put the held proposal to the runtime again, where validation itself is what
   * ended the run. No reviewer turn, no smaller suite: the same admitted cases,
   * the same drops, the same unknowns, judged again.
   */
  retryExpectationValidation(): void {
    if (this.running || !canRetryExpectationValidation(this.state)) return
    this.arm()
    // A person sent these cases back, and what follows establishes them. The
    // record is the transcript, so the action that changed what got established
    // says so in it, as a message and an approved correction do.
    this.addTurn({ role: 'user', kind: 'note', text: 'Sent the held case proposal back for validation.' })
    this.set({ phase: 'cases', status: 'running', detail: 'Validating the held case proposal again.' })
    void this.drive(async signal => {
      // Repair stays on, spelled out rather than taken from the default: this
      // resumes the run validation interrupted, so a draft that then disagrees
      // with the established cases is repaired against the same budget `start()`
      // would have spent on it. Declining to repair here would settle at
      // needs-input with revisions still in hand.
      await this.casesAndCheck(signal, true)
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
      if (isCancelled(cause) || cause instanceof Stopped) {
        if (this.outOfTime) {
          this.set({ status: 'budget', detail: `The time budget of ${this.ports.seconds} seconds is spent. The last completed stage is kept.` })
        } else {
          this.set({ status: 'stopped', detail: 'Stopped. The last completed stage is kept.' })
        }
      } else {
        this.set({ status: 'failed', detail: (cause as Error)?.message ?? String(cause) })
      }
    } finally {
      if (this.controller === controller) this.controller = null
      if (!this.running) this.disarm()
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
          citations: traceCitations(taken.document, this.ports.ledger),
          expectationIssues: this.state.expectationIssues.map(issue => issue.resolved || !issue.proposal ? issue : {
            ...issue, proposal: undefined, proposalError: 'The draft changed. Request a new correction before approving.'
          })
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
    await this.continuingTurn(signal, 'research', this.researchPrompt(), this.ports.researchTools)
  }

  /** What a continuation is handed: every source read and every excerpt, by id. */
  private readSoFar(): string {
    const lines = this.ports.ledger.sources.map((record) => {
      const head = record.kind === 'search' ? `${record.id}: search ${JSON.stringify(record.request.query ?? '')}` : `${record.id}: ${record.document?.title ?? ''} — ${record.request.url ?? ''}` + (record.document ? ` (${record.document.text.length} characters retrieved; re-open with read_source source_id ${record.id})` : '')
      const hits = (record.hits ?? []).map((hit) => `  hit ${hit.rank}: ${hit.title} — ${hit.url}`)
      const excerpts = record.excerpts.map((excerpt) => `  ${excerpt.id}: ${JSON.stringify(excerpt.text)}`)
      return [head + (record.failure ? ` (failed: ${record.failure})` : ''), ...hits, ...excerpts].join('\n')
    })
    return `SOURCES ALREADY READ AND CITED\n${lines.join('\n') || '(none)'}`
  }

  /**
   * An engine turn that may be continued: a turn whose steps ran out before
   * the proposal is not a failure of the task, only of the budget the engine
   * gives one run, so it is resumed with what was read so far, a bounded
   * number of times, before it is reported as failed.
   */
  private async continuingTurn(
    signal: AbortSignal,
    producedBy: Candidate['producedBy'],
    prompt: string,
    hostTools: HostTool[]
  ): Promise<{ document: unknown; unknowns: string[] } | null> {
    let attempt = prompt
    for (let continuation = 0; ; continuation += 1) {
      try {
        return await this.engineTurn(signal, producedBy, attempt, hostTools)
      } catch (cause) {
        if (!(cause instanceof Error) || !NO_FENCE.test(cause.message) || continuation >= MAX_CONTINUATIONS) throw cause
        this.addTurn({ role: 'assistant', kind: 'note', text: `The turn's step budget was spent before a proposal was written; continuing (${continuation + 1} of ${MAX_CONTINUATIONS}).` })
        this.ports.log(`continue: turn ${continuation + 1} of ${MAX_CONTINUATIONS}`)
        attempt = [prompt, CONTINUE_INSTRUCTIONS, this.readSoFar()].join('\n\n')
      }
    }
  }

  /**
   * The reviewer turn that proposes the suite, screened and held before a word
   * of it is put to the runtime.
   *
   * Held is not established. Nothing reaches `state.cases`, the matrix or the
   * record until the runtime returns a canonical for it, so a run that fails
   * here still reports no result -- what holding buys is the turn, not the
   * verdict.
   */
  private async proposeCases(signal: AbortSignal, candidate: Candidate): Promise<HeldProposal> {
    this.set({ phase: 'cases', detail: 'Establishing test cases from the sources.' })
    this.ports.log('cases: a reviewer establishes expectations from the excerpts')
    const proposal = await this.engineTurn(signal, 'research', this.casesPrompt(candidate.document), [], true)
    const { admitted, dropped } = admitCases(proposal?.document, this.ports.ledger, this.state.cases)
    const held: HeldProposal = { candidateDigest: candidate.digest, admitted, dropped, unknowns: proposal?.unknowns ?? [] }
    this.set({ heldProposal: held })
    return held
  }

  /** Establish cases where none are, then check, and repair until the budget. */
  private async casesAndCheck(signal: AbortSignal, repair = true): Promise<void> {
    const candidate = this.latest()
    if (!candidate) throw new Error('no candidate to check')
    if (this.state.cases.length === 0 && this.state.expectationIssues.length === 0) {
      // A proposal already screened and held is validated as it stands. The
      // reviewer turn is the most expensive turn in the run and validation
      // failing on transport is no answer to the question it asked, so it is
      // not asked again -- but a changed draft is a different question, and a
      // proposal held against other bytes is not reused for it.
      //
      // Nor is a hold screened again: `admitCases` turns on ledger
      // verification, and a verified record cannot become unverified, since
      // every turn opens its own session and only ever marks records under it.
      // A change to how sessions are allocated would have to re-screen here.
      const kept = this.state.heldProposal
      const held = kept !== null && kept.candidateDigest === candidate.digest ? kept : await this.proposeCases(signal, candidate)
      const { admitted, dropped } = held
      const checked = await validateExpectations(admitted.map(row => row.expectedDisposition), this.ports.callTool, signal)
      const issues: ExpectationIssue[] = []
      const cases: AuthoringCase[] = []
      admitted.forEach((row, index) => {
        const finding = checked[index]!
        if (finding.status === 'invalid') return void issues.push({ id: row.id, original: row, message: findingSummary(finding) })
        // The exact expectation is the disposition and the target together. A
        // target that contradicts its own disposition can never pass, whatever
        // the pack says, so it is blocked for review rather than admitted and
        // left to spend the repair budget.
        const contradiction = targetContradiction(finding.canonical, row.expectedHandoffTarget)
        if (contradiction) return void issues.push({ id: row.id, original: row, message: contradiction })
        // What the runtime compared is what is stored: the canonical text, not
        // the reviewer's spelling of the same set. The saved matrix row is then
        // byte-identical to the assertion that was checked.
        cases.push(deepFreeze(structuredClone({ ...row, expectedDisposition: JSON.parse(finding.canonical) })))
      })
      // The proposal has been answered, so nothing is held any more: what is
      // kept from here is an established case or a blocked issue, and a stale
      // hold would offer a retry of a question that is already settled.
      this.set({ cases, expectationIssues: issues, droppedCases: dropped, heldProposal: null })
      if (dropped.length) {
        this.addTurn({ role: 'assistant', kind: 'note', text: `Dropped ${dropped.length} proposed case(s): ${dropped.map((d) => `${d.id} (${d.reason})`).join('; ')}` })
      }
      if (held.unknowns.length) this.addTurn({ role: 'assistant', kind: 'unknowns', text: held.unknowns.map((line) => `• ${line}`).join('\n') })
      if (admitted.length === 0) {
        // Where nothing grounded a case because the sources did not verify,
        // that is the sentence, not the absence of cases.
        this.set({ status: 'needs-input', phase: 'review', detail: this.withheld() ?? 'No test case could be grounded in a cited excerpt. Cite the requirements, or say what the cases should be.' })
        return
      }
      this.ports.log(`cases: ${cases.length} established, ${issues.length} invalid expectations, ${dropped.length} dropped`)
    }
    if (this.unresolvedExpectations()) {
      this.set({ phase: 'review', status: 'needs-input', detail: this.unresolvedExpectations()! })
      return
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
      const passed = completeCurrentCheck(this.state)
      this.ports.log(`check: revision ${current.revision} — ${check.valid ? 'valid' : 'invalid'}, ${check.cases.filter((c) => c.passed).length}/${check.cases.length} agree`)
      if (passed) {
        // Agreement is not enough where what the draft rests on did not
        // verify: a source whose receipt failed, or a citation the run cannot
        // trace, withholds `ready` -- the draft is shown, and what stands in
        // its way is said.
        this.settleReview('')
        return
      }
      if (!repair) {
        // A run blocked on an expectation never checked its draft, so this is
        // the first the person hears of either kind of failure. Saying the
        // wrong one sends them to the cases when the pack is what is broken.
        this.set({ phase: 'review', status: 'needs-input', detail: check.valid
          ? 'The unchanged draft disagrees with the reviewed expectations. Review the results or request a pack change.'
          : `The unchanged draft is not a valid pack: ${check.diagnostics.length} diagnostic${check.diagnostics.length === 1 ? '' : 's'}. Review them, or send a message to repair it.` })
        return
      }
      if (this.state.revisionsUsed >= this.ports.maxRevisions) {
        this.set({ phase: 'review', status: 'budget', detail: `The revision budget of ${this.ports.maxRevisions} is spent with disagreements remaining. Review the results, or send a message to continue.` })
        return
      }
      this.set({ phase: 'repair', revisionsUsed: this.state.revisionsUsed + 1, detail: 'Repairing the candidate against the established cases.' })
      this.ports.log(`repair: revision ${this.state.revisionsUsed} of ${this.ports.maxRevisions}`)
      try {
        await this.continuingTurn(signal, 'repair', this.repairPrompt(), this.ports.researchTools)
      } catch (cause) {
        if (cause instanceof Stalled) {
          this.set({ phase: 'review', status: 'stalled', detail: 'The assistant repeated an earlier candidate. Review the disagreements, or send a message to steer it.' })
          return
        }
        throw cause
      }
    }
  }

  /**
   * The one place readiness is decided: the latest check must pass **and**
   * nothing may be withheld. Every path that rests at review comes here --
   * a passed check, an unchanged answer to a message, a continuation -- so
   * no path reaches `ready` around the sources it rests on.
   */
  private unresolvedExpectations(): string | null {
    const count = this.state.expectationIssues.filter(issue => !issue.resolved).length
    return count ? `${count} invalid expectation${count === 1 ? '' : 's'} must be corrected and approved before testing or creating the pack.` : null
  }

  private settleReview(notPassing: string): void {
    // The same refusal `casesAndCheck` makes before it checks anything, kept
    // here as a backup: no path rests at `ready` with an expectation open. It
    // fires only where a passing check and an open issue coexist, which no run
    // produces -- so nothing failing without it means nothing.
    if (this.unresolvedExpectations()) {
      this.set({ phase: 'review', status: 'needs-input', detail: this.unresolvedExpectations()! })
      return
    }
    const passing = completeCurrentCheck(this.state)
    if (!passing) {
      this.set({ phase: 'review', status: 'needs-input', detail: notPassing })
      return
    }
    // Sources may have been verified, or failed, since the last trace.
    const latest = this.latest()
    if (latest) this.set({ citations: traceCitations(latest.document, this.ports.ledger) })
    const withheld = this.withheld()
    if (withheld !== null) {
      this.set({ phase: 'review', status: 'needs-input', detail: withheld })
      return
    }
    this.set({ phase: 'review', status: 'ready', detail: 'Every established case agrees. Review the draft, its sources and the unknowns before creating the pack.' })
  }

  /** Why the candidate is not ready even where its cases agree, or null. */
  private withheld(): string | null {
    const failed = this.ports.ledger.sources.filter((record) => record.verification.state === 'failed' && record.excerpts.length > 0)
    if (failed.length > 0) {
      return `${failed.length} cited source(s) failed receipt verification (${failed.map((r) => r.id).join(', ')}), so their excerpts are withheld and the draft is not ready.`
    }
    const untraced = this.state.citations.filter((citation) => !citation.traced)
    if (untraced.length > 0) {
      return `${untraced.length} citation(s) in the draft could not be traced to a verified excerpt (${untraced.map((c) => c.sourceId).join(', ')}). Ask the assistant to cite from what it read, or review the sources.`
    }
    if (this.state.citations.length === 0) {
      return 'The draft cites no source. A pack with no traced citation is an assumption; ask the assistant to cite what it read.'
    }
    return null
  }

  // ---- verification ----------------------------------------------------------

  /** Seal the session the turn acquired under, fetch the registry, verify, and mark every record. */
  private async verifyAcquisitions(session: string, signal: AbortSignal): Promise<void> {
    const ledger = this.ports.ledger
    // The records this desk opened under the session, in the order it opened
    // them: membership and position come from the desk's own ledger, never
    // from the receipts, which are the thing being verified. A record with
    // no answer holds no receipt and is a hole the seal's count will show.
    const records = ledger.under(session).filter((record) => record.response !== null)
    if (ledger.under(session).length === 0) return
    if (!this.ports.gateway) {
      for (const record of records) ledger.verified(record.id, { state: 'failed', at: this.stamp(), findings: [{ sessionId: session, callIndex: null, status: 'no-pinned-key' }] })
      return
    }
    const stamp = this.stamp()
    const held: HeldReceipt[] = records.map((record) => ({ receipt: record.response!.receipt, result: record.response!.result }))
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
    this.set({ verdicts: { ...this.state.verdicts, [session]: verdict }, registries: { ...this.state.registries, [session]: registryText } })
    this.ports.log(`verify: ${session} — ${verdict.ok ? 'verified' : 'FAILED'} (${verdict.findings.map((f) => f.status).join(', ')})`)
    records.forEach((record, position) => {
      const own = verdict.findings.filter((f) => f.callIndex === position || f.callIndex === null || f.file === `${position}.json`)
      ledger.verified(record.id, verdict.ok ? { state: 'verified', at: stamp, keyId: verdict.keyId } : { state: 'failed', at: stamp, findings: own })
    })
    // Citations traced before the verdict were traced against unchecked
    // receipts; they are traced again now that the verdict is in.
    const latest = this.latest()
    if (latest) this.set({ citations: traceCitations(latest.document, ledger) })
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

/**
 * The research record written beside a created pack: every source, its
 * receipt and result **as the acquire response carried them**, every
 * excerpt, the verdict per session and the registry text it was reached
 * with, so the saved verification claim can be checked again by anyone
 * holding the gateway's public key -- and the record says which key id.
 */
export function researchRecord(state: RunState, ledger: Ledger, packDigest: string): unknown {
  const sourceOf = (record: SourceRecord) => ({
    id: record.id,
    kind: record.kind,
    requestedAt: record.requestedAt,
    request: record.request,
    ...(record.failure ? { failure: record.failure } : {}),
    ...(record.response ? { acquireResponse: record.response.text } : {}),
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
    // The digest of the bytes these cases were actually checked against, beside
    // the digest of the pack that was written. Create shapes four members of the
    // reviewed draft on its way to disk -- title, id, version, description --
    // so the two differ by construction, and a reader should be able to see
    // that rather than infer it. Nothing else may differ: a handed-over draft
    // is not edited on the way through.
    checkedCandidateSha256: state.candidates.at(-1)?.digest ?? '',
    shapedOnCreate: ['title', 'id', 'version', 'description'],
    // Three digests of two different things, and a reader who compares the
    // wrong two learns nothing. A resolved issue's `proposal.candidateDigest`
    // is the candidate that correction was proposed against -- a fact about a
    // moment in the run, not a third claim about the pack -- and the record
    // has to say so itself, because the record is what leaves here.
    //
    // One path language for all three, stated in the record, so every key can
    // be resolved the same way rather than two of them reading as member names
    // and the third as a path expression in no language. A legend is
    // machine-readable, and a legend naming a member the record has since
    // renamed is worse than the source comment it replaced -- so run.test.ts
    // resolves every key here against a real record, on an ordinary run and a
    // corrected one, and drift fails a named test.
    digests: {
      pathSyntax: 'JSON Pointer into this record, with * standing for any array index',
      means: {
        '/packSha256': 'sha256 of the pack bytes Create wrote',
        '/checkedCandidateSha256': 'sha256 of the research candidate text these cases were checked against',
        '/expectationIssues/*/proposal/candidateDigest': 'sha256 of the research candidate text that correction was proposed against'
      }
    },
    // The matrix Create writes beside this record spells a corrected row's
    // reason the other way round, and both spellings are deliberate: the matrix
    // registers the row under the reason still standing, while the case kept
    // here is the case as it was authored. A reader holding the two companions
    // should be told that, not left to notice it.
    matrixFocus:
      'For a corrected row the matrix registers cases[].focus under the approved correction rationale, which this record carries at /expectationIssues/*/resolved/rationale; /cases/*/rationale here keeps the rationale the case was authored with.',
    brief: state.brief,
    seedUrls: state.seedUrls,
    sessions: state.sessions,
    verdicts: state.verdicts,
    registries: state.registries,
    citations: state.citations,
    cases: state.cases,
    droppedCases: state.droppedCases,
    expectationIssues: state.expectationIssues,
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
      // A corrected case was established for the reason the approved correction
      // gave; the rationale it carries from authoring argued the expectation
      // that correction replaced. `focus` is the line a reader of the
      // registered matrix meets beside the row, so it carries the reason that
      // is still standing -- under the same source tag, which the correction
      // preserved.
      const corrected = state.expectationIssues.find((item) => item.id === row.id && item.resolved)
      const focus = corrected?.resolved?.rationale ?? row.rationale
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
        focus: `${focus} [${row.expectationSource}]`,
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

/**
 * Why a handoff target and the disposition it is asserted with cannot both hold,
 * or nothing where they can.
 *
 * §8.3 keeps the configured target outside the disposition, and this runtime
 * reports one exactly when the disposition requests a handoff. So a row that
 * asserts a target beside a handoff of "none" asserts a pair no evaluation can
 * produce, for any pack and any escalation configuration. The other direction is
 * pack-dependent -- a requested handoff has no target where the pack configures
 * none -- and is left to the evaluation that can answer it.
 */
export function targetContradiction(canonical: string, target: unknown): string | null {
  if (target === undefined || target === null) return null
  const handoff = (JSON.parse(canonical) as { handoff?: { state?: unknown } }).handoff
  if (handoff?.state === 'requested') return null
  return 'The case expects a handoff target beside a disposition that requests no handoff. No evaluation reports a target for a handoff of "none", so the pair could never pass.'
}

/**
 * Whether the held proposal can be put to the runtime again as it stands.
 *
 * A proposal is held only between the turn that proposed it and the
 * establishment that consumes it, so a run at rest still holding one is a run
 * that failed, stopped or spent its budget inside validation. The digest is the
 * binding: a hold left over from an earlier draft answers a question the
 * current one no longer asks, and that needs a reviewer, not a retry.
 *
 * `running` is the one status excluded: the hold is live while validation is in
 * flight, and sending it again there would put the same proposal twice. An
 * `idle` run cannot hold one -- `start()` resets through `INITIAL_STATE`, whose
 * hold is null -- so there is no clause for it.
 */
export function canRetryExpectationValidation(state: RunState): boolean {
  const held = state.heldProposal
  return held !== null && state.status !== 'running' &&
    held.candidateDigest === state.candidates.at(-1)?.digest
}

/** A complete check is bound to the current candidate and every established case. */
function completeCurrentCheck(state: RunState): boolean {
  const latest = state.candidates.at(-1)
  const check = latest?.check
  return check !== undefined && check.valid && check.documentDigest === latest?.digest && state.cases.length > 0 &&
    check.cases.length === state.cases.length && check.cases.every((row, index) => row.passed && row.id === state.cases[index]?.id)
}

/** Every intended, admitted case must have a current passing result before Create. */
export function canCreateResearchDraft(state: RunState): boolean {
  return state.status === 'ready' && !state.expectationIssues.some(issue => !issue.resolved) && completeCurrentCheck(state) &&
    state.citations.length > 0 && state.citations.every(citation => citation.traced)
}
