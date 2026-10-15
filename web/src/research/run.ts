import {correctArtifact} from '../assistant/execution/correctArtifact'
import {proposalProblems, legacyPackBasis, type PackProposalBasis, type PackReview, type SavedPackReview} from '../chat/packProposal'
import {AIFailureError,readFailure,retryReady,type AIFailure} from '../assistant/failures'
import {ExecutionPaused,extendExecution,canExtendInterruptedExecution,type ExecutionCheckpoint} from '../assistant/execution/state'
import { modelTranscript } from './transcript'
import { readUserQuestion, validQuestionDraft, type QuestionDraft, type SavedQuestion } from '../assistant/questions'
import { whileChecking, runSnapshotCheck, verifyRetainedCheck, reusableRuntime } from './retainedCheck'
import { conversationMode, type AuthoringMode } from './mode'
import type { SearchReference } from '../search/results'
import { citedAttachments } from '../chat/messageOwnership'
import { summarizeWork, mergeReferences, type ResponseHistory } from '../chat/responseHistory'
import type { ChatAttachment } from '../chat/store'
import type { WebsiteReference } from '../documents/website'
import { matchesPageQuote, type DocumentReference, type VerifiedDocument } from '../documents/client'
import { needsPartialConsent } from '../documents/record'
import type { ResearchGatewayConfig } from '../config/deskConfig'
import { runtimeProbes, type RuntimeProbe } from './runtimeProbes'
import { sourceMessage } from '../i18n/source'
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
import { canonicalProposal } from '../assistant/useAssistantRun'
import type { AssistantEvent, CallTool, HostTool } from '../assistant/engine'
import { isCancelled, recoverableProposal } from '../assistant/engines/contract'
import { checkCandidate, digestOf, jsonIdentity, type AuthoringCase, type CandidateCheck } from './checkCandidate'
import { findingSummary, validateExpectations } from './expectations'
import type { Ledger, SourceRecord } from './ledger'
import { CASES_INSTRUCTIONS, CONTINUE_INSTRUCTIONS, CONVERSATION_CASES_INSTRUCTIONS, CONVERSATION_CITATION_INSTRUCTIONS, CONVERSATION_INSTRUCTIONS, REPAIR_INSTRUCTIONS, RESEARCH_INSTRUCTIONS } from './prompts'
import { memberOf, parseJsonText, stringMember, type JsonNode } from './verify/canon'
import { verifySession, type Finding, type HeldReceipt, type SessionVerdict } from './verify/session'

export type Phase = 'idle' | 'research' | 'cases' | 'check' | 'repair' | 'conversation' | 'review'
export type Status = 'idle' | 'running' | 'complete' | 'ready' | 'needs-input' | 'budget' | 'stalled' | 'stopped' | 'failed'

export interface AssistantProvenance { connectionId:string; connectionName:string; model:string }
export interface Turn {
  packContext?: PackProposalBasis
  /** Reviewed test-design conversation; proposals remain in the test workspace. */
  testDesign?: { owner: string; proposalId?: string }
  /** Per-request web choice. Separate from the conversation's default mode. */
  researchChoice?: import('../chat/researchChoice').ResearchChoice
  target?:AssistantProvenance
  id?: string
  /** Exact attachments selected when this message was submitted. */
  attachments?: ChatAttachment[]
  role: 'user' | 'assistant'
  kind: 'brief' | 'message' | 'unknowns' | 'note' | 'question'
  text: string
  /** Full main-assistant input, including Desk context; never a case-writing ground. */
  input?: string
  /** The person's text and supplied file material, excluding Desk-appended context. */
  statement?: string
  /** Desk annotation; kept separate from the model's verbatim response. */
  interrupted?: boolean
  at: string
}

export interface Candidate {
  packReview?: PackReview
  responseId?: string
  revision: number
  document: unknown
  text: string
  digest: string
  producedBy: 'research' | 'repair' | 'conversation'
  check?: CandidateCheck
  /** Last recorded check, for display only after reload. */
  previousCheck?: CandidateCheck
}

/** One `sources[]` entry of the candidate, traced to the ledger or not. */
export interface Citation {
  sourceId: string
  location: string | null
  excerptId: string | null
  url: string | null
  traced: boolean
  reason: string
  /** The quote a conversation draft's traced page citation was found by. */
  quote?: string
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
  failure?: AIFailure
  questions?: SavedQuestion[]
  phase: Phase
  status: Status
  /** A disk checkpoint is not a fresh check or approval. */
  restored?: boolean
  checkRecovery?: 'verifying' | 'current' | 'outdated' | 'unavailable'
  checkIssue?: string
  checkRun?: 'running' | 'complete' | 'failed' | 'stopped'
  /** Cited statement evidence inherited from an independent draft, without its chat. */
  inheritedStatements?: [string, string][]
  detail: string
  /** Ephemeral prose for the current engine step; never a trusted candidate. */
  streaming?: string
  streamingId?: string
  brief: string
  seedUrls: string[]
  turns: Turn[]
  execution?:ExecutionCheckpoint
  responses?: ResponseHistory[]
  events: AssistantEvent[]
  candidates: Candidate[]
  cases: AuthoringCase[]
  probes?: RuntimeProbe[]
  droppedCases: { id: string; reason: string }[]
  /** A screened proposal awaiting validation, never an established case. */
  heldProposal: HeldProposal | null
  expectationIssues: ExpectationIssue[]
  unknowns: string[]
  citations: Citation[]
  /** A conversation draft's citations are being traced; they say nothing yet. */
  tracing?: boolean
  /** What a chat draft's citations were traced on, as `documentBasis` keys it; absent where no trace landed. */
  tracedBasis?: string
  verdicts: Record<string, SessionVerdict>
  /** The registry text each session's verdict was reached with. */
  registries: Record<string, string>
  revisionsUsed: number
  /** The gateway sessions this run opened, in order. */
  sessions: string[]
  /** What the last settle that reached `ready` was about, or empty. See `readinessKey`. */
  readiness: string
}

export interface TurnRequest {
  onTarget?:(target:AssistantProvenance|undefined)=>void
  prompt: string
  context?: import('../assistant/context/session').ContextInput
  hostTools: HostTool[]
  /** A reviewer turn runs with thinking off: fresh, and no critic. */
  reviewer?: boolean
  conversation?: boolean
}

export interface RunPorts {
  savedPackReview?: SavedPackReview
  mode?: AuthoringMode
  /** One engine run to completion; events arrive in order, `end` last. */
  turn(request: TurnRequest, signal: AbortSignal, onEvent: (event: AssistantEvent) => void): Promise<void>
  /** The desk's own runtime connection, for validate and rehearsal. */
  callTool: CallTool
  /** Feature-detected exact snapshot tests; older runtimes retain the legacy path. */
  snapshotChecks?: boolean
  runtimeIdentity?: () => Promise<string>
  ledger: Ledger
  /** The research tools bound to the ledger, or none where research is not configured. */
  researchTools: HostTool[]
  /** The documents kept in this chat, which a conversation draft's citations are traced to. */
  documents?: () => readonly ChatAttachment[]
  /** The gateway pin as it is now, which a chat draft's documents are verified under. */
  pin?: () => ResearchGatewayConfig | null
  /** Load a kept document and verify it under the given pin. */
  loadDocument?: (reference: DocumentReference, pin: ResearchGatewayConfig, signal: AbortSignal) => Promise<VerifiedDocument>
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

export const CONTEXT_LIMIT_MESSAGE = sourceMessage("This conversation exceeds the 200,000-character context limit. Start a new chat with the relevant text, or attach a smaller excerpt. Nothing was sent to the model.")

/** The engine's own sentence for a final turn with no proposal fence in it. */
const NO_FENCE = /exactly one fenced JSON block.*carried 0/

export const INITIAL_STATE: RunState = {
  phase: 'idle',
  status: 'idle',
  detail: '',
  brief: '',
  seedUrls: [],
  turns: [],
  questions: [],
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
  sessions: [],
  readiness: ''
}

class AwaitingAnswer extends Error {}

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
      return { sourceId, location, excerptId: null, url, traced: false, reason: sourceMessage("no excerpt id in citation.location; this source was not read in this run") }
    }
    const excerpt = ledger.excerpt(location)
    if (!excerpt) return { sourceId, location, excerptId: location, url, traced: false, reason: sourceMessage("no excerpt {{value0}} was recorded in this run", { value0: location }) }
    const record = ledger.byId(excerpt.sourceId)
    if (quoted === null || foldSpace(quoted) !== foldSpace(excerpt.text)) {
      return { sourceId, location, excerptId: location, url, traced: false, reason: sourceMessage("citation.excerpt is not the recorded excerpt text") }
    }
    if (url === null) return { sourceId, location, excerptId: location, url, traced: false, reason: sourceMessage("locator.value is not a string URL") }
    if (!record?.document || (url !== record.document.url && url !== record.request.url)) {
      return { sourceId, location, excerptId: location, url, traced: false, reason: sourceMessage("locator.value is not the URL the excerpt was read from") }
    }
    if (record.verification.state !== 'verified') {
      return { sourceId, location, excerptId: location, url, traced: false, reason: record.verification.state === 'failed' ? sourceMessage('The receipt for {{value0}} failed verification; its excerpt is withheld.', { value0: record.id }) : sourceMessage('The receipt for {{value0}} has not been checked; its excerpt is withheld.', { value0: record.id }) }
    }
    return { sourceId, location, excerptId: location, url, traced: true, reason: '' }
  })
}

/** A page of a document kept in a chat, as the chat's reading tools and attachments give it to cite. */
const PAGE_CITATION = /^attachment:([a-f0-9-]{36})\/(sha256:[a-f0-9]{64})\/page\/([1-9][0-9]*)$/

/** One declared `sources[]` entry, as a citation reads it. */
function declared(entry: unknown, index: number): { sourceId: string; url: string | null; location: string | null; quoted: string } {
  const source = (entry ?? {}) as Record<string, unknown>
  const locator = (source.locator ?? {}) as Record<string, unknown>
  const citation = (source.citation ?? {}) as Record<string, unknown>
  return {
    sourceId: typeof source.id === 'string' ? source.id : `#${index}`,
    url: typeof locator.value === 'string' ? locator.value : null,
    location: typeof citation.location === 'string' ? citation.location : null,
    quoted: typeof citation.excerpt === 'string' ? citation.excerpt : ''
  }
}

/** Every citation a draft declares, none traced, for a trace that did not finish. */
function unchecked(document: unknown, reason: string): Citation[] {
  const sources = (document as { sources?: unknown })?.sources
  if (!Array.isArray(sources)) return []
  return sources.map((entry, index) => {
    const { sourceId, url, location } = declared(entry, index)
    return { sourceId, location, excerptId: null, url, traced: false, reason }
  })
}

/**
 * What a chat draft's trace rests on besides the draft: the gateway pin its
 * documents verify under and the documents the chat keeps, as one key. A trace
 * that ends on another basis than it began on is a trace of a moment that has
 * passed.
 */
export function documentBasis(pin: unknown, kept: readonly ChatAttachment[]): string {
  return JSON.stringify([pin ?? null, kept.map(file => file.document ? [file.document.id, file.document.digest, file.document.pages, file.document.allowPartial] : null)])
}

/** How many times a trace whose basis moved under it is taken again before it is given up. */
const TRACE_ATTEMPTS = 3

/** The document and page a conversation citation names, or null where it names none. */
export function citedPage(location: string | null): { documentId: string; digest: string; page: number } | null {
  const match = location === null ? null : PAGE_CITATION.exec(location)
  return match ? { documentId: match[1]!, digest: match[2]!, page: Number(match[3]) } : null
}

/**
 * A conversation draft's `sources[]`, each traced to a page of a document kept
 * in this chat or not. The conversation modes read through the chat's own
 * tools, which keep what they read as documents rather than ledger excerpts,
 * so the ledger's trace can never find them. A citation here is traced when
 * its location names a kept document by id and digest and one of that
 * document's selected pages, the document verifies under the gateway pin as it
 * is now, and the quoted excerpt is on that page -- the check the chat's own
 * source reader makes when a person opens a citation.
 *
 * Only a Stop ends it early; a document that cannot be loaded or verified
 * leaves its citations untraced and says so.
 */
export async function tracePageCitations(
  document: unknown,
  kept: readonly ChatAttachment[],
  load: (reference: DocumentReference, signal: AbortSignal) => Promise<VerifiedDocument>,
  signal: AbortSignal
): Promise<Citation[]> {
  const sources = (document as { sources?: unknown })?.sources
  if (!Array.isArray(sources)) return []
  // One load per document, however many sources quote it.
  const loads = new Map<string, Promise<VerifiedDocument>>()
  const citations: Citation[] = []
  for (const [index, entry] of sources.entries()) {
    const { sourceId, url, location, quoted } = declared(entry, index)
    const untraced = (reason: string): Citation => ({ sourceId, location, excerptId: null, url, traced: false, reason })
    const cited = citedPage(location)
    if (!cited) {
      citations.push(untraced(sourceMessage("citation.location is not a page of a document read in this chat")))
      continue
    }
    const file = kept.find(item => item.document?.id === cited.documentId && item.document.digest === cited.digest)
    if (!file?.document || !file.document.pages.includes(cited.page)) {
      citations.push(untraced(sourceMessage("page {{value0}} of document {{value1}} is not a page kept in this chat", { value0: cited.page, value1: cited.documentId })))
      continue
    }
    const reference = file.document
    const key = `${reference.id}/${reference.digest}`
    // A trace that was stopped or superseded loads nothing more.
    signal.throwIfAborted()
    if (!loads.has(key)) loads.set(key, Promise.resolve().then(() => load(reference, signal)))
    let verified: VerifiedDocument
    try {
      verified = await loads.get(key)!
    } catch (cause) {
      if (signal.aborted) throw cause
      citations.push(untraced(sourceMessage("The document {{value0}} did not verify under the current gateway pin, so its quote is withheld.", { value0: cited.documentId })))
      continue
    }
    // A partial extraction is used only once the person confirmed its pages;
    // until then its text was never evidence, as the chat never sent it.
    if (needsPartialConsent(verified.record) && !reference.allowPartial) {
      citations.push(untraced(sourceMessage("page {{value0}} of document {{value1}} is not a page kept in this chat", { value0: cited.page, value1: cited.documentId })))
      continue
    }
    if (!quoted.trim() || !matchesPageQuote(verified, cited.page, quoted)) {
      citations.push(untraced(sourceMessage("citation.excerpt is not on page {{value0}} of the document it cites", { value0: cited.page })))
      continue
    }
    // A web page is declared under the address it was read from, as `research`
    // holds a ledger citation to its page's URL: a quote from one page does
    // not stand behind a pack that names another. Only the signed record says
    // where that was; the chat's own note of the link is not signed.
    const read = verified.record.provenance.source
    if (read.kind === 'web' && (url === null || (url !== read.url && url !== read.requestedUrl))) {
      citations.push(untraced(sourceMessage("locator.value is not the URL the excerpt was read from")))
      continue
    }
    citations.push({ sourceId, location, excerptId: null, url, traced: true, reason: '', quote: quoted })
  }
  return citations
}

/** A loader for a run with no gateway to verify documents under. */
const unverifiable = async (): Promise<never> => {
  throw new Error('no gateway is configured to verify documents')
}

/**
 * Why a conversation draft's citations keep it from being created, or null.
 * Every citation must be traced; `web-research` must also make one, since the
 * mode exists to rest a pack on what it read. `draft` may cite nothing: the
 * person's own statements are then its basis.
 */
export function citationGap(citations: readonly Citation[], mode: AuthoringMode | undefined): string | null {
  const untraced = citations.filter(citation => !citation.traced)
  if (untraced.length > 0) {
    return sourceMessage("{{value0}} citation(s) in the draft could not be traced to a page read in this chat ({{value1}}). Ask the assistant to read the page with read_link, or attach the document, and cite the page it quotes; or remove the citation.", { value0: untraced.length, value1: untraced.map(citation => citation.sourceId).join(', ') })
  }
  if (citations.length === 0 && mode === 'web-research') {
    return sourceMessage("The draft cites no source. A web research draft rests on pages read in this chat: ask the assistant to read them with read_link and cite the page each rule quotes.")
  }
  return null
}

/**
 * The person's own statements in a conversation, by id: every message of
 * theirs, `you-1` onwards, in the order they sent them. The text is what they
 * typed, supplied file material included. Older augmented turns cannot be
 * separated reliably and provide no statement; ordinary text-only turns do. Never an assistant turn, which
 * paraphrases the draft, and never a note Desk wrote on the person's behalf.
 */
export function statementsOf(turns: readonly Turn[]): Map<string, string> {
  return new Map(statementTurns(turns).flatMap(({ id, turn }) => {
    const text = turn.statement ?? (turn.input === undefined ? turn.text : undefined)
    return text === undefined ? [] : [[id, text] as const]
  }))
}

/** The turns `statementsOf` reads, by the id it gives each, for a panel to show as they were sent. */
export function statementTurns(turns: readonly Turn[]): { id: string; turn: Turn }[] {
  return turns.filter(turn => turn.role === 'user' && turn.kind !== 'note').map((turn, index) => ({ id: `you-${index + 1}`, turn }))
}

/** Inherited statements keep their original ids; new messages use stable turn ids. */
export function caseStatements(state: Pick<RunState, 'turns' | 'inheritedStatements'>): Map<string, string> {
  if (state.inheritedStatements === undefined) return statementsOf(state.turns)
  const values = new Map(state.inheritedStatements)
  for (const {turn} of statementTurns(state.turns)) {
    const text = turn.statement ?? (turn.input === undefined ? turn.text : undefined)
    if (text !== undefined && turn.id) values.set(`message:${turn.id}`, text)
  }
  return values
}

/** What a chat draft's cases may rest on besides a recorded excerpt. */
export interface CaseGrounds {
  /** The person's own statements, by id. See `statementsOf`. */
  statements: ReadonlyMap<string, string>
  /** The draft's citations as traced at admission; only a traced one grounds a case. */
  citations: readonly Citation[]
}

/**
 * Screen source-grounded case proposals; runtime expectation validation must
 * follow before admission. A case rests on a recorded, verified excerpt; in a
 * chat draft, given `grounds`, it may instead rest on one of the person's
 * statements or on a page the draft cites and traced.
 */
export function admitCases(
  proposed: unknown,
  ledger: Ledger,
  established: AuthoringCase[],
  grounds?: CaseGrounds
): { admitted: AuthoringCase[]; dropped: { id: string; reason: string }[] } {
  const cases = (proposed as { cases?: unknown })?.cases
  const admitted: AuthoringCase[] = []
  const dropped: { id: string; reason: string }[] = []
  if (!Array.isArray(cases)) return { admitted, dropped: [{ id: '(all)', reason: sourceMessage("the reviewer answered without a cases array") }] }
  const seen = new Set(established.map((row) => row.id))
  cases.forEach((entry, index) => {
    const row = (entry ?? {}) as Record<string, unknown>
    const id = typeof row.id === 'string' ? row.id : `#${index}`
    if (!KEBAB.test(id)) return dropped.push({ id, reason: sourceMessage("the id is not kebab-case") })
    if (seen.has(id)) return dropped.push({ id, reason: sourceMessage("a case with this id is already established and is never rewritten") })
    if (row.facts === null || typeof row.facts !== 'object' || Array.isArray(row.facts)) return dropped.push({ id, reason: sourceMessage("facts must be an object") })
    const source = typeof row.expectationSource === 'string' ? row.expectationSource : ''
    // A chat draft's case may rest on what the person said, or on a page the
    // draft cites and traced as it stands now; anything else must be a
    // recorded, verified excerpt, as in `research`.
    const grounded = grounds !== undefined && (grounds.statements.has(source) || grounds.citations.some(citation => citation.traced && citation.location === source))
    if (!grounded && grounds !== undefined && !EXCERPT_ID.test(source)) {
      return dropped.push({ id, reason: sourceMessage("expectationSource {{value0}} is neither one of your messages nor a page the draft cites and traced", { value0: JSON.stringify(source) }) })
    }
    if (!grounded && (!EXCERPT_ID.test(source) || !ledger.excerpt(source))) {
      return dropped.push({ id, reason: sourceMessage("expectationSource {{value0}} is not an excerpt recorded in this run", { value0: JSON.stringify(source) }) })
    }
    if (!grounded && !ledger.verifiedExcerpt(source)) {
      return dropped.push({ id, reason: sourceMessage("expectationSource {{value0}} is from a source whose receipt did not verify, so it grounds nothing", { value0: source }) })
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
  /** What the action in flight took back, reported with whatever outcome it ends in. See `alsoUndone`. */
  private undone: string | null = null
  /** The citation traces started, so only the last one lands. See `traceConversation`. */
  private traces = 0
  /** The basis moved during an action, so its citations are traced again when it ends. */
  private retracePending = false
  /** A readiness the basis moving withdrew, to be settled again once a trace lands. See `traceAtRest`. */
  private pendingSettle = false
  /** A trace taken at rest, which Stop, the next action and the next such trace end. */
  private background: AbortController | null = null
  /** The run's owner is gone: no trace at rest starts. See `detach`. */
  private detached = false
  constructor(private readonly ports: RunPorts) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  getSnapshot = (): RunState => this.state

  /** Retain a task conversation in this session without invoking its model. */
  appendConversation(turns: Turn[]): RunState {
    if (this.running) throw Error('Finish the active chat before adding test-design messages.')
    const byId = new Map(this.state.turns.filter(turn => turn.id).map(turn => [turn.id!, turn]))
    const added: Turn[] = []
    for (const turn of turns) {
      if (!turn.id) throw Error('A retained message needs a stable identity.')
      const previous = byId.get(turn.id)
      if (previous) {
        if (previous.role !== turn.role || previous.text !== turn.text || previous.at !== turn.at) throw Error('A retained message conflicts with this session.')
      } else { byId.set(turn.id, turn); added.push(structuredClone(turn)) }
    }
    if (added.length) this.set({turns:[...this.state.turns,...added].sort((a,b)=>a.at.localeCompare(b.at)),
      ...(this.state.phase === 'idle' ? {phase:'conversation',status:'complete'} : {})})
    return this.state
  }

  private set(patch: Partial<RunState>): void {
    this.state = { ...this.state, ...patch }
    for (const listener of this.listeners) listener()
  }

  private stamp(): string {
    return (this.ports.now?.() ?? new Date()).toISOString()
  }

  private pendingExecution:ExecutionCheckpoint|undefined
  private lastMessage: string | null = null
  private responseId: string | undefined
  private responseEvents: AssistantEvent[] = []
  private updateResponse(patch: Partial<ResponseHistory>): void {
    if (!this.responseId) return
    this.set({ responses: (this.state.responses ?? []).map(row => row.id === this.responseId ? {...row, ...patch} : row) })
  }
  recordDocument(file: ChatAttachment): void {
    const row = this.state.responses?.find(row => row.id === this.responseId)
    if (row) this.updateResponse({documents: mergeReferences(row.documents, [structuredClone(file)])})
  }
  recordWebsite(reference: WebsiteReference): void {
    const row = this.state.responses?.find(row => row.id === this.responseId)
    if (row) this.updateResponse({websites: [...row.websites.filter(w => w.id !== reference.id), structuredClone(reference)]})
  }
  recordSearch(reference: SearchReference): void {
    const row = this.state.responses?.find(row => row.id === this.responseId)
    if (row) this.updateResponse({searches: [...(row.searches ?? []).filter(s => s.id !== reference.id), structuredClone(reference)]})
  }
  private finishResponse(): void {
    this.updateResponse({work: summarizeWork(this.responseEvents, false)})
    this.responseId = undefined
    this.responseEvents = []
  }

  private currentAITarget?:AssistantProvenance
  private addTurn(turn: Omit<Turn, 'at'>, patch: Partial<RunState> = {}): void {
    this.set({ ...patch, turns: [...this.state.turns, { ...turn, ...(turn.role==='assistant'&&turn.kind==='message'&&this.currentAITarget?{target:{...this.currentAITarget}}:{}), id: turn.id ?? crypto.randomUUID(), at: this.stamp() }] })
  }

  get running(): boolean {
    return this.state.status === 'running'
  }

  /** Stop the run at its last completed stage. */
  stop(): void {
    this.controller?.abort()
    this.background?.abort()
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

  /** Reconstruct a checkpoint; only verified complete check records can become current. */
  async restore(saved: RunState): Promise<void> {
    if (this.running || this.state.phase !== 'idle') throw new Error(sourceMessage("Cannot replace an active conversation"))
    const candidates = await Promise.all(saved.candidates.map(async item => ({
      revision: item.revision, text: item.text, document: deepFreeze(JSON.parse(item.text)) as unknown,
      digest: await digestOf(item.text), producedBy: item.producedBy, responseId: item.responseId, packReview:item.packReview,
      ...(item.previousCheck && (item.previousCheck.record || item.previousCheck.documentDigest === await digestOf(item.text)) ? { previousCheck: item.previousCheck } : {})
    })))
    this.set({ ...INITIAL_STATE, brief: saved.brief, seedUrls: saved.seedUrls,
      questions: structuredClone(saved.questions ?? []),
      turns: saved.turns.map((turn, index) => ({...turn, id: turn.id ?? `legacy-turn-${index}`})),
      responses: saved.responses?.map(row => ({...row, work: {...row.work, items: row.work.items.map(item => item.status === 'working' ? {...item, status: 'interrupted' as const} : item)}})), candidates, probes: saved.probes ?? [], cases: structuredClone(saved.cases),
      expectationIssues: saved.expectationIssues.map(item => ({ id: item.id, original: structuredClone(item.original),
        message: item.message, ...(item.resolved ? { resolved: structuredClone(item.resolved) } : {}) })),
      execution:saved.execution, failure:readFailure(saved.failure), unknowns: saved.unknowns, revisionsUsed: saved.revisionsUsed, sessions: saved.sessions,
      phase: candidates.length ? 'review' : saved.turns.length ? 'conversation' : 'idle',
      status: candidates.length ? 'needs-input' : !saved.turns.length ? 'idle'
        : saved.status === 'failed' ? 'failed' : saved.status === 'running' || saved.status === 'stopped' || saved.status === 'budget'
          || saved.turns.at(-1)?.role === 'user' ? 'stopped' : 'complete',
      restored: candidates.length > 0,
      detail: candidates.length ? sourceMessage("Saved draft. Recheck its sources and tests before creating it.")
        : saved.status === 'failed' ? saved.detail || sourceMessage("This response was interrupted. Send a message to continue; nothing restarted automatically.") : saved.status === 'running' || saved.status === 'stopped' || saved.status === 'budget' || saved.turns.at(-1)?.role === 'user'
          ? sourceMessage("This response was interrupted. Send a message to continue; nothing restarted automatically.") : '' })
    const lastUser = [...this.state.turns].reverse().find(turn => turn.role === 'user')
    if (conversationMode(this.ports.mode) && lastUser) this.lastMessage = lastUser.input ?? lastUser.text
    if (this.state.questions?.some(q => !q.resolution)) {
      this.set({phase:'conversation',status:'needs-input',detail:sourceMessage('Waiting for your answer')})
      return
    }
    if (this.state.questions?.some(q => q.resolution && q.resolution.kind !== 'superseded' && !q.resolution.continued && q.resolution.turnId === this.state.turns.at(-1)?.id)) {
      this.set({phase:'conversation',status:'needs-input',detail:sourceMessage('Ready to continue')})
      return
    }
    if(saved.execution?.status==='paused'){this.set({phase:'conversation',status:'budget',detail:new ExecutionPaused().message});return}
    const record = candidates.at(-1)?.previousCheck?.record
    // Independent draft artifacts keep only the case's cited statements, never
    // a hidden transcript. A new chat's you-1 must not replace their provenance.
    this.set({inheritedStatements: candidates.length && !saved.turns.length ? structuredClone(record?.statements ?? saved.inheritedStatements ?? []) : saved.inheritedStatements})
    this.set({checkRun: saved.checkRun === 'running' ? 'stopped' : saved.checkRun})
    // A draft and a conversational response have separate recovery states.
    // Restoring the draft must not erase an unfinished explanation or replay it.
    const response=saved.responses?.at(-1)
    const unfinished=saved.phase==='conversation'&&response!==undefined&&response.afterTurnId===lastUser?.id
      &&!candidates.some(candidate=>candidate.responseId===response.id)
    if (conversationMode(this.ports.mode) && (saved.status==='failed'&&saved.detail===CONTEXT_LIMIT_MESSAGE
      || unfinished&&['failed','stopped','running','budget'].includes(saved.status))) {
      this.set({phase:'conversation',status:saved.status==='failed'?'failed':'stopped',
        detail:saved.status==='failed'?saved.detail:sourceMessage('This response was interrupted. Send a message to continue; nothing restarted automatically.')})
      return
    }
    if (this.ports.savedPackReview) {
      this.set(saved.status === 'failed' || saved.status === 'stopped' || saved.status === 'running'
        ? {phase:'review',status:saved.status === 'failed' ? 'failed' : 'stopped',detail:saved.detail}
        : {phase:'review',status:'complete',detail:''})
      return
    }
    if (record && this.ports.runtimeIdentity) { await this.refreshCheck(); return }
    // A checkpoint keeps no citations, so a reopened conversation draft is
    // traced again before anything reads them: one that cites is never shown
    // as citing nothing. Nothing else is checked or changed here.
    if (candidates.length) this.traceAtRest()
  }

  /** Recover a complete proposal from an older assistant reply, without a model call. */
  recoverDraft(text: string): void {
    if (this.running || this.latest() || !this.state.turns.some(turn => turn.role === 'assistant' && turn.kind === 'message' && turn.text === text)) return
    const proposal = recoverableProposal(text)
    if (!proposal) return
    this.arm()
    const responseId = crypto.randomUUID()
    const owner = this.state.turns.find(turn => turn.role === 'assistant' && turn.text === text)
    this.set({responses: [...(this.state.responses ?? []), {id: responseId, messageId: owner?.id, documents: [], websites: [], sourceIds: [], work: {items: [], notices: []}}]})
    this.set({ status: 'running', phase: 'check', detail: sourceMessage("Validating the draft through the runtime.") })
    void this.drive(async signal => {
      const text = JSON.stringify(proposal.document, null, 2)
      const digest = await digestOf(text)
      this.check(signal)
      this.set({ candidates: [{ responseId, revision: 1, document: proposal.document, text, digest, producedBy: 'conversation' }], unknowns: proposal.unknowns, citations: conversationMode(this.ports.mode) ? [] : traceCitations(proposal.document, this.ports.ledger) })
      await this.traceConversation(signal)
      // Recovery never starts source acquisition, a paid reviewer or repair.
      const check = await this.checkCandidate(text, [], this.ports.callTool, signal)
      this.set({ candidates: this.state.candidates.map(candidate => ({ ...candidate, check })) })
      // A recovered chat draft has no cases yet; establishing them is a model
      // call, and the person asks for it.
      if (conversationMode(this.ports.mode)) await this.settle(signal, check.valid ? this.withoutCases() : sourceMessage("The draft needs corrections before it can be created."))
      else this.set({ status: 'needs-input', phase: 'review', detail: sourceMessage("No saved cases can be rechecked. Send a message to continue research.") })
    })
  }

  private async checkContext(): Promise<string> {
    return digestOf(jsonIdentity({mode:this.ports.mode ?? 'research', gateway:this.ports.gateway, documents:this.basisNow(),
      sources:this.ports.ledger.sources.map(source => ({session:source.session, request:source.request,
        text:source.response?.text ?? null, quotes:source.excerpts.map(e => e.text)}))}))
  }

  private async checkCandidate(document: string, inputs: AuthoringCase[], call: CallTool, signal: AbortSignal): Promise<CandidateCheck> {
    this.set({checkRun:'running',checkRecovery:undefined,checkIssue:undefined,readiness:''})
    try {
      const grounds = caseStatements(this.state)
      const statements = [...grounds].filter(([id]) => inputs.some(row => row.expectationSource === id))
      const result = this.ports.snapshotChecks && this.ports.runtimeIdentity
        ? await runSnapshotCheck(document, inputs, call, signal, this.ports.runtimeIdentity, () => this.checkContext(), statements, this.stamp())
        : await checkCandidate(document, inputs, call, signal)
      this.check(signal)
      if (jsonIdentity(inputs) !== jsonIdentity(this.state.cases) && inputs.length) throw Error(sourceMessage('The test inputs changed during the checks. Run checks again.'))
      this.set({checkRun:'complete',checkRecovery:'current'})
      return result
    } catch (error) {
      this.set({checkRun:signal.aborted ? 'stopped' : 'failed'})
      throw error
    }
  }

  /** Restore completed results, never replay evaluation, acquisition or model work. */
  async refreshCheck(): Promise<void> {
    if (this.ports.savedPackReview) return
    const candidate = this.latest(), previous = candidate?.check ?? candidate?.previousCheck, record = previous?.record
    if (this.detached || this.running || !candidate || !record || !this.ports.runtimeIdentity) return
    this.background?.abort()
    const controller = new AbortController(), signal = controller.signal
    this.background = controller
    const timeout = setTimeout(() => controller.abort(), this.ports.seconds * 1000)
    const active = () => !signal.aborted && this.background === controller && !this.running && this.latest()?.digest === candidate.digest
    this.set({restored:true,readiness:'',checkRecovery:'verifying',checkIssue:undefined,
      candidates:this.state.candidates.map(c => c === candidate ? {...c,previousCheck:previous,check:undefined} : c)})
    try {
      if (this.state.checkRun === 'stopped' || this.state.checkRun === 'failed') throw Error(sourceMessage('The last check did not finish. Run checks again.'))
      const check = await verifyRetainedCheck(record)
      if (!active()) return
      if (record.documentDigest !== candidate.digest || jsonIdentity(record.inputs) !== jsonIdentity(this.state.cases))
        throw Error(sourceMessage('The draft or test inputs changed. Run checks again.'))
      if (!reusableRuntime(record.runtime) || record.runtime !== await whileChecking(signal,this.ports.runtimeIdentity))
        throw Error(sourceMessage('The runtime version changed or cannot be verified. Run checks again.'))
      if (!active()) return
      if (record.context !== await this.checkContext()) throw Error(sourceMessage('The source context changed. Run checks again.'))
      const statements = caseStatements(this.state)
      if (record.statements.some(([id,text]) => statements.get(id) !== text)) throw Error(sourceMessage('The statements used by the tests changed. Run checks again.'))
      if (conversationMode(this.ports.mode)) await this.traceConversation(signal)
      else for (const session of new Set(this.ports.ledger.sources.map(s => s.session))) await this.verifyAcquisitions(session,signal,false)
      if (!active()) return
      const grounds = conversationMode(this.ports.mode) ? {statements,citations:this.state.citations} : undefined
      const admitted = admitCases({cases:record.inputs},this.ports.ledger,[],grounds)
      const withheld = this.withheld()
      if (withheld || admitted.dropped.length || admitted.admitted.length !== record.inputs.length)
        throw Error(withheld ?? sourceMessage('The test sources could not be verified. Run checks again.'))
      if (record.context !== await this.checkContext() || record.runtime !== await whileChecking(signal,this.ports.runtimeIdentity))
        throw Error(sourceMessage('The runtime or source context changed during verification. Run checks again.'))
      if (!active()) return
      this.set({restored:false,checkRecovery:'current',checkRun:'complete',checkIssue:undefined,
        candidates:this.state.candidates.map(c => c.digest === candidate.digest ? {...c,check} : c)})
      this.settleReview(check.valid ? record.inputs.length ? sourceMessage('Some test cases failed. Review the results.') : this.withoutCases() : sourceMessage('The draft needs corrections before it can be created.'), 'background')
    } catch (cause) {
      if (active()) {
        const reason = (cause as Error).message
        this.set({restored:true,checkRecovery:/changed/.test(reason) ? 'outdated' : 'unavailable',checkIssue:reason})
        this.setReviewResult({readiness:'',status:'needs-input',detail:reason}, 'background')
      }
    } finally {
      clearTimeout(timeout)
      if (this.background === controller) {
        this.background = null
        if (signal.aborted && this.state.checkRecovery === 'verifying') this.set({checkRecovery:'unavailable',checkIssue:sourceMessage('Saved check verification was interrupted. Run checks again.')})
      }
    }
  }

  /** Explicit, model-free recovery. A reload cannot restart paid or repair work. */
  recheck(): void {
    if (this.running || !this.latest()) return
    this.arm()
    this.set({ status: 'running', phase: 'check', detail: sourceMessage("Running draft checks."), checkRun: 'running', checkIssue: undefined, checkRecovery: undefined, readiness: '' })
    void this.drive(async signal => {
      this.set({restored:true})
      if (!this.ports.savedPackReview) await this.recheckRestored(signal)
      if (!this.ports.savedPackReview && !conversationMode(this.ports.mode) && !this.state.cases.length && !this.state.expectationIssues.length) throw new Error(sourceMessage("No saved cases can be rechecked. Send a message to continue research."))
      // Model-free: a chat draft saved without cases is checked and told so,
      // and its cases are established only on the person's own request.
      await this.casesAndCheck(signal, false, false)
      if (this.state.checkRun === 'running') this.set({checkRun:'failed'})
    })
  }

  /**
   * Establish test cases for a checked chat draft that has none, on the
   * person's request: a reviewer turn, given what the conversation holds and
   * never the draft's rules, then the same validation and check as `research`.
   */
  establishCases(): void {
    if (this.running || !canEstablishCases(this.state, this.ports.mode)) return
    this.arm()
    this.addTurn({ role: 'user', kind: 'note', text: sourceMessage("Asked for test cases written without the draft's rules.") })
    this.set({ phase: 'cases', status: 'running', detail: sourceMessage("Establishing test cases without the draft's rules.") })
    void this.drive(async signal => {
      await this.casesAndCheck(signal, false)
    })
  }

  private async recheckRestored(signal: AbortSignal): Promise<void> {
    if (!this.state.restored) return
    const conversation = conversationMode(this.ports.mode)
    // A chat draft's cases rest on its statements, which come back with its
    // turns, and on the pages it cites, traced again here before anything is
    // grounded on them.
    if (conversation) await this.traceConversation(signal)
    if (!conversation) for (const session of new Set(this.ports.ledger.sources.map(source => source.session))) await this.verifyAcquisitions(session, signal, false)
    const saved = this.state.cases
    const grounds = conversation ? { statements: caseStatements(this.state), citations: this.state.citations } : undefined
    const { admitted, dropped } = admitCases({ cases: saved }, this.ports.ledger, [], grounds)
    if (conversation && (dropped.length || admitted.length !== saved.length || this.state.expectationIssues.some(issue => !admitCases({ cases: [issue.original] }, this.ports.ledger, [], grounds).admitted.length))) {
      // A chat opened from the pack keeps none of the messages its cases were
      // written from, and a page may no longer trace. What cannot be grounded
      // again is not kept, and neither is the rest: a smaller suite is the
      // one thing this step exists to refuse. The person establishes new
      // cases from what this conversation holds.
      const reasons = new Map(dropped.map(row => [row.id, row.reason]))
      this.set({ cases: [], expectationIssues: [], heldProposal: null, restored: false,
        droppedCases: [...saved, ...this.state.expectationIssues.map(issue => issue.original)].map(row => ({ id: row.id,
          reason: reasons.get(row.id) ?? sourceMessage("set aside with the saved cases that could not be grounded again") })) })
      this.addTurn({ role: 'assistant', kind: 'note', text: sourceMessage("The saved test cases rest on messages or pages this conversation cannot ground again, so they were set aside. Establish new ones before creating the pack.") })
      return
    }
    if (dropped.length || admitted.length !== saved.length) throw new Error(sourceMessage("Saved test cases could not be grounded again. Start a new research chat; the original history is preserved."))
    const checked = await validateExpectations(admitted.map(row => row.expectedDisposition), this.ports.callTool, signal)
    const cases: AuthoringCase[] = []
    const issues = this.state.expectationIssues.map(item => ({ ...item, proposal: undefined }))
    admitted.forEach((row, index) => {
      const finding = checked[index]!
      const problem = finding.status === 'invalid' ? findingSummary(finding) : targetContradiction(finding.canonical, row.expectedHandoffTarget)
      if (problem) issues.push({ id: row.id, original: row, message: problem, proposal: undefined })
      else cases.push(deepFreeze(structuredClone(row)))
    })
    this.set({ cases, expectationIssues: issues })
    // Do not generate replacement cases on a model-free recovery action.
    this.set({ restored: false })
  }

  /** Begin: the brief, the URLs to read first, and the research turn. */
  start(brief: string, seedUrls: string[], display = brief, attachments: ChatAttachment[] = [], statement = display, researchChoice?: Turn['researchChoice'], packContext?: PackProposalBasis): void {
    if (this.running) return
    this.arm()
    this.lastMessage = brief
    this.state = { ...INITIAL_STATE, brief, seedUrls, phase: 'research', status: 'running', detail: 'Working…' }
    this.addTurn({ role: 'user', kind: 'brief', ...(packContext ? {packContext:structuredClone(packContext)} : {}), ...(researchChoice ? {researchChoice} : {}), statement, attachments: structuredClone(attachments), text: display + (seedUrls.length ? `\n\nRead first:\n${seedUrls.join('\n')}` : ''), ...(brief !== display ? { input: brief } : {}) })
    void this.drive(async (signal) => {
      await this.researchTurn(signal)
      await this.casesAndCheck(signal)
    })
  }

  /** A message from the person, at any rest state. */
  send(message: string, display = message, retry = false, attachments: ChatAttachment[] = [], statement = display, researchChoice?: Turn['researchChoice'], packContext?: PackProposalBasis): void {
    if (this.running || this.state.phase === 'idle') return
    this.arm()
    const previous = { status: this.state.status, phase: this.state.phase, detail: this.state.detail }
    this.lastMessage = message
    if (!retry) {
      this.pendingExecution=undefined
      this.set({execution:undefined,failure:undefined})
      const turnId = crypto.randomUUID()
      this.set({questions:this.state.questions?.map(q => q.resolution ? q : {...q,resolution:{kind:'superseded',at:this.stamp(),turnId}})})
      this.addTurn({ id:turnId, role: 'user', kind: 'message', ...(packContext ? {packContext:structuredClone(packContext)} : {}), ...(researchChoice ? {researchChoice} : {}), statement, attachments: structuredClone(attachments), text: display, ...(message !== display ? { input: message } : {}) })
    }
    this.set({ status: 'running', phase: 'conversation', detail: 'Working…', events: [], streaming: '' })
    void this.drive(async (signal) => {
      const before = this.latest()?.digest
      const proposal = await this.continuingTurn(signal, 'conversation', this.conversationPrompt(message), this.ports.researchTools)
      if (!this.latest()) this.set({ phase: 'conversation', status: 'complete', detail: '' })
      else if (this.latest()?.digest !== before || proposal && !conversationMode(this.ports.mode) && !this.latest()?.check?.cases.length) {
        if (this.state.restored && !this.ports.savedPackReview) await this.recheckRestored(signal)
        await this.casesAndCheck(signal)
      } else if (previous.status === 'ready') await this.settle(signal, sourceMessage("The candidate needs review."))
      else if (['failed','budget','stopped'].includes(previous.status) && (previous.phase === 'conversation' || previous.detail === CONTEXT_LIMIT_MESSAGE)) this.set({phase:'review',status:'needs-input',detail:sourceMessage('The candidate needs review.')})
      else this.set(previous)
    })
  }

  /** Edits stay in the normal private conversation checkpoint, including across reloads. */
  updateQuestionDraft(id: string, draft: QuestionDraft): void {
    const question = this.state.questions?.find(q => q.id === id)
    if (this.running || !question || question.resolution || !validQuestionDraft(question.prompt, draft)) return
    this.set({questions:this.state.questions!.map(q => q === question ? {...q,draft:structuredClone(draft)} : q)})
  }

  /** Record once, then let the chat save this checkpoint before starting the next model turn. */
  prepareQuestionAnswer(id: string, kind: 'answered' | 'help'): boolean {
    const question = this.state.questions?.find(q => q.id === id)
    if (this.running || !question || question.resolution || !validQuestionDraft(question.prompt, question.draft)
      || kind === 'answered' && !question.draft.selected.length && !question.draft.text.trim()) return false
    const turnId = crypto.randomUUID()
    const labels = question.prompt.options.filter(o => question.draft.selected.includes(o.id)).map(o => o.label)
    const answer = kind === 'help' ? sourceMessage('Help me decide') : labels.join('; ')
    const display = [answer, question.draft.text.trim()].filter(Boolean).join('\n')
    const previous = [...this.state.turns].reverse().find(t => t.role === 'user')
    const input = `${display}\n\nANSWER TO DESK QUESTION\n${JSON.stringify({questionId:id,question:question.prompt.question,kind,selected:kind === 'help' ? [] : labels,text:question.draft.text.trim()})}\nContinue the original task using this answer. Preserve its limits and review requirements. Do not repeat completed operations. If help was requested, briefly explain the options, then call ask_user with the same choices or a more useful follow-up question. Do not choose for the person or treat this as an accepted answer.`
    this.set({questions:this.state.questions!.map(q => q === question ? {...q,resolution:{kind,at:this.stamp(),turnId}} : q)})
    this.addTurn({id:turnId,role:'user',kind:'message',text:display,statement:display,input,researchChoice:previous?.researchChoice},
      {status:'needs-input',detail:sourceMessage('Ready to continue')})
    return true
  }

  /** Explicit continuation of a recorded answer. A changed thread makes this action stale. */
  continueQuestion(id: string): boolean {
    const question = this.state.questions?.find(q => q.id === id)
    const answer = this.state.turns.at(-1)
    if (this.running || !question?.resolution || question.resolution.kind === 'superseded' || question.resolution.continued
      || question.resolution.turnId !== answer?.id || answer.role !== 'user') return false
    this.set({questions:this.state.questions!.map(q => q === question ? {...q,resolution:{...q.resolution!,continued:true}} : q),
      detail:this.latest() ? sourceMessage('The candidate needs review.') : ''})
    this.send(answer.input ?? answer.text, '', true)
    return true
  }

  get canRetryResponse(): boolean {
    if(this.state.execution?.status==='paused'||this.state.execution&&this.state.execution.status!=='complete'&&this.state.execution.used>=this.state.execution.limit)return false
    const response=this.state.responses?.at(-1)
    const user=[...this.state.turns].reverse().find(turn=>turn.role==='user')
    const unfinishedConversation=this.state.phase==='conversation'&&response?.afterTurnId===user?.id
      &&response!==undefined&&!this.state.candidates.some(candidate=>candidate.responseId===response.id)
    return this.lastMessage !== null && (!this.latest() || unfinishedConversation || this.state.detail === CONTEXT_LIMIT_MESSAGE) && ['failed', 'stopped', 'budget'].includes(this.state.status)
  }

  get canExtendResponse(): boolean {
    return this.state.status==='failed'&&this.state.failure?.code!=='repeated-work'&&this.state.phase==='conversation'&&this.lastMessage!==null&&canExtendInterruptedExecution(this.state.execution)
  }

  /** UI records the grant durably before dispatch. No model tool calls this. */
  prepareBudgetContinuation():boolean {
    if(this.running||!this.state.execution||(this.state.execution.status!=='paused'&&!this.canExtendResponse)||!this.lastMessage||!retryReady(this.state.failure))return false
    this.set({execution:extendExecution(this.state.execution,this.canExtendResponse),status:'stopped',detail:sourceMessage('Ready to continue')})
    return true
  }
  continueExecution():void {
    const execution=this.state.execution
    if(this.running||!execution||execution.status!=='running'||execution.used>=execution.limit||!this.lastMessage)return
    this.pendingExecution=structuredClone(execution)
    this.send(this.lastMessage,'',true)
  }
  keepExecution():void {
    if(this.running||this.state.execution?.status!=='paused')return
    this.set({execution:{...this.state.execution,status:'stopped'},status:'stopped',detail:sourceMessage('Completed work kept. Send a new instruction when ready.')})
  }

  /** Explicit retry only, with the original context and no duplicate user turn. */
  retryResponse(): void {
    if (!retryReady(this.state.failure)) return
    if (this.canRetryResponse && this.lastMessage !== null) {
      if(this.state.execution&&this.state.execution.status!=='complete'){this.pendingExecution={...structuredClone(this.state.execution),status:'running'};this.send(this.lastMessage,'',true);return}
      const lastUser = [...this.state.turns].reverse().find(turn => turn.role === 'user')
      const response = this.state.responses?.at(-1)
      const work = response?.afterTurnId === lastUser?.id ? response?.work.items ?? [] : []
      const continuation = work.length ? '\n\nContinue the unfinished response using the saved conversation and sources. Retained steps: ' + JSON.stringify(work.map(({name,status})=>({name,status}))) + '. Preserve completed work; do not repeat completed actions or claim an interrupted operation succeeded. Ask before repeating an action with an uncertain outcome.' : ''
      const original = this.lastMessage
      this.send(original + continuation, '', true)
      this.lastMessage = original
    }
  }

  /** Resume only the research question; never replay an earlier authoring action. */
  chooseResearch(choice: 'research' | 'provided'): void {
    const user = [...this.state.turns].reverse().find(turn => turn.role === 'user')
    const response = this.state.responses?.at(-1)
    if (this.running || !user || !response || !conversationMode(this.ports.mode) || response.afterTurnId !== user.id || !response.work.items.some(item => item.researchSuggestion) || user.researchChoice === 'research' || user.researchChoice === 'provided') return
    this.set({turns:this.state.turns.map(turn => turn === user ? {...turn,researchChoice:choice} : turn)})
    this.send('Continue answering my previous request using the web-research choice recorded for that message. This continuation is read-only: do not repeat previous actions, create or edit packs or graphs, or send messages. If I chose Keep Chat, answer from the conversation and supplied sources, clearly identifying anything that remains unverified.', '', true)
  }

  /** A fresh reviewer corrects only the expectation; the candidate and case inputs are fixed. */
  proposeExpectationCorrection(id: string): void {
    const issue = this.state.expectationIssues.find(item => item.id === id && !item.resolved)
    if (this.running || !issue || !this.latest()) return
    this.arm()
    this.set({ phase: 'review', status: 'running', detail: sourceMessage("Reviewing the expectation for {{value0}}.", { value0: id }),
      expectationIssues: this.state.expectationIssues.map(item => item.id === id ? { ...item, proposal: undefined, proposalError: undefined } : item) })
    void this.drive(async signal => {
      const ground = this.groundOf(issue.original.expectationSource)
      if (!ground) throw new Error(sourceMessage("The expectation source is no longer verified. Restore its verification before reviewing the expectation."))
      const candidate = this.latest()!
      const document = candidate.document as Record<string, unknown>
      // No authoring prompt here: this turn reviews one expectation against the
      // source and the contract, and pack-authoring guidance is not its brief.
      const prompt = ['CORRECT AN INVALID EXPECTATION. Preserve the case id, facts, evidence availability, source and intended behavior. Propose only a complete expectedDisposition and a rationale explaining the correction under JPS §8.3. Empty reasons is valid only for outcome; unresolved must retain its reason(s). Do not remove a case, change the pack, or weaken the assertion to fit a candidate. If the cited source and contract do not settle the correction, return unknowns and no correction. The person must approve any correction before it is used.',
        `CASE\n${JSON.stringify(issue.original)}`, `VALIDATOR FINDING\n${issue.message}`,
        `${ground.label}\n${JSON.stringify(ground.value)}`, `DECLARED OUTCOMES\n${JSON.stringify(document.outcomes)}`,
        `HANDOFF CONFIGURATION\n${JSON.stringify(document.escalation ?? null)}`,
        `EXPECTED HANDOFF TARGET (asserted with the disposition and unchanged by this correction)\n${JSON.stringify(issue.original.expectedHandoffTarget ?? null)}`,
        'Answer with prose and exactly one fenced JSON block: {"proposal":{"document":{"expectedDisposition":{...},"rationale":"..."},"unknowns":[]}}.'
      ].join('\n\n')
      const response = await this.engineTurn(signal, 'research', prompt, [], true)
      const value = response?.document as { expectedDisposition?: unknown; rationale?: unknown } | null
      let proposal: ExpectationIssue['proposal']
      let proposalError = response?.unknowns.length ? response.unknowns.join('\n') : ''
      if (!proposalError && (!value || typeof value.rationale !== 'string' || !value.rationale.trim())) proposalError = sourceMessage("The reviewer did not explain a correction. The expectation remains blocked.")
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
      this.set({ phase: 'review', status: 'needs-input', detail: proposal ? sourceMessage("Review the proposed expectation correction before approving it.") : sourceMessage("The expectation still needs a decision."),
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
    this.set({ phase: 'review', status: 'running', detail: sourceMessage("Validating the approved correction for {{value0}}.", { value0: id }) })
    void this.drive(async signal => {
      if (!this.groundOf(issue.original.expectationSource)) throw new Error(sourceMessage("The expectation source is no longer verified; the correction was not applied."))
      const [finding] = await validateExpectations([proposal.expectedDisposition], this.ports.callTool, signal)
      if (finding!.status !== 'valid') throw new Error(sourceMessage("The correction is no longer valid: {{value0}}", { value0: findingSummary(finding) }))
      // The exact expectation is the pair. A corrected disposition that no
      // longer agrees with the target the case asserts could never pass, so it
      // is refused here rather than applied and left to stall the run.
      const contradiction = targetContradiction(finding.canonical, issue.original.expectedHandoffTarget)
      if (contradiction) throw new Error(sourceMessage("The correction was not applied: {{value0}}", { value0: contradiction }))
      // Nothing is applied on a closed run. A backup for a window that holds
      // only microtasks -- a validation that answers after Stop -- so a suite
      // that never lands in it is no evidence this is dead code.
      this.check(signal)
      const replacement = deepFreeze(structuredClone({ ...issue.original, expectedDisposition: proposal.expectedDisposition }))
      // No established case can be silently replaced by this approval. A backup:
      // admitted ids are unique and an id kept as an issue is never also a case,
      // so no run produces the collision it refuses.
      if (this.state.cases.some(row => row.id === id)) throw new Error(sourceMessage("A case with this id is already established; the correction was not applied."))
      // The approval and the retest it promises are one step. Applied first and
      // then interrupted -- a Stop, a spent budget, a transport failure -- it
      // left the correction in place with every check stripped and the issue
      // resolved, and a resolved issue closes both correction actions while an
      // unchanged message re-checks nothing: the bytes the person approved for
      // could never be tested again. So the retest rolls the approval back, and
      // the proposal they approved is still there to approve again.
      //
      // `candidates` is here for symmetry with what the approval touches, and
      // no test can discriminate it: an expectation issue is raised where the
      // cases are established, before any check, and no later turn raises one,
      // so at an approval there is no check to strip or to put back.
      const before = { phase: this.state.phase, cases: this.state.cases, candidates: this.state.candidates, expectationIssues: this.state.expectationIssues, readiness: this.state.readiness }
      this.set({ cases: [...this.state.cases, replacement],
        // Every check goes, so nothing downstream reads one taken before this
        // case joined the suite. A backup too: a run blocked on an expectation
        // never checked its draft, so today there is no check here to drop.
        candidates: this.state.candidates.map(({ check: _check, ...candidate }) => candidate),
        expectationIssues: this.state.expectationIssues.map(item => item.id === id ? { ...item, resolved: { replacement, approvedAt: this.stamp(), rationale: proposal.rationale } } : item) })
      try {
        await this.casesAndCheck(signal, false)
      } catch (cause) {
        this.set(before)
        this.undone = sourceMessage("The correction for {{value0}} was rolled back: its retest did not complete, so nothing was approved and the proposal is still on offer.", { value0: id })
        throw cause
      }
      // Written where the approval completed, so the transcript records no
      // approval the run then took back.
      this.addTurn({ role: 'user', kind: 'note', text: sourceMessage("Approved the corrected expectation for {{value0}}: {{value1}}", { value0: id, value1: proposal.rationale }) })
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
    this.addTurn({ role: 'user', kind: 'note', text: sourceMessage("Sent the held case proposal back for validation.") })
    this.set({ phase: 'cases', status: 'running', detail: sourceMessage("Validating the held case proposal again.") })
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
    this.set({failure:undefined})
    // An action traces and settles for itself where it needs to; a trace at
    // rest, and the settle it owed, are over.
    this.background?.abort()
    this.pendingSettle = false
    try {
      await work(controller.signal)
    } catch (cause) {
      if(cause instanceof ExecutionPaused){this.set({status:'budget',detail:cause.message,streaming:'',streamingId:undefined});return}
      if (cause instanceof AwaitingAnswer) {
        this.set({status:'needs-input',detail:sourceMessage('Waiting for your answer'),streaming:'',streamingId:undefined})
        return
      }
      if (this.state.streaming?.trim()) this.addTurn({ id: this.state.streamingId, role: 'assistant', kind: 'message', text: this.state.streaming, interrupted: true }, {streaming: '', streamingId: undefined})
      this.set({ streaming: '', ...(this.state.checkRun === 'running' ? {checkRun: controller.signal.aborted ? 'stopped' as const : 'failed' as const} : {}) })
      if (isCancelled(cause) || cause instanceof Stopped) {
        if (this.outOfTime) {
          this.set({ status: 'budget', detail: this.alsoUndone(sourceMessage("The time budget of {{value0}} seconds is spent. The last completed stage is kept.", { value0: this.ports.seconds })) })
        } else {
          this.set({ status: 'stopped', detail: this.alsoUndone(sourceMessage("Stopped. The last completed stage is kept.")) })
        }
      } else {
        this.set({ status: 'failed', failure:cause instanceof AIFailureError?cause.failure:undefined, detail: this.alsoUndone((cause as Error)?.message ?? String(cause)) })
      }
    } finally {
      if (this.responseId) {
        const last = this.state.turns.at(-1)
        if (last?.interrupted) this.updateResponse({messageId: last.id})
        this.finishResponse()
      }
      this.undone = null
      if (this.controller === controller) this.controller = null
      if (!this.running) this.disarm()
      // The pin or the documents moved while this ran, and its own settle did
      // not trace the draft on the basis they moved to. Stopped, or out of
      // time -- whatever the action then ended with -- it starts no new work:
      // none of its citations counts as traced until the person checks the
      // draft again. Otherwise it is traced now.
      if (!this.running && this.retracePending) {
        this.retracePending = false
        const latest = this.latest()
        if (latest && this.basisNow() !== this.state.tracedBasis) {
          if (controller.signal.aborted || this.state.status === 'stopped' || this.state.status === 'budget') {
            this.set({ citations: unchecked(latest.document, sourceMessage("The gateway pin or this chat's documents changed while the citations were checked. Recheck the draft.")), tracedBasis: undefined })
          } else this.traceAtRest()
        }
      }
    }
  }

  /**
   * The outcome of an action, and what the action took back before it ended.
   * "Stopped. The last completed stage is kept." reports the stop and explains
   * neither an approval whose retest rolled it back nor a readiness a verdict
   * withdrew -- and #82 is about exactly that, a reason that is not the real
   * one. Whatever was taken back says so here, in the line the panel reads.
   */
  private alsoUndone(outcome: string): string {
    if (this.undone === null) return outcome
    const said = outcome.trim()
    return `${said}\n\n${this.undone}`
  }

  private check(signal: AbortSignal): void {
    if (signal.aborted) throw new Stopped()
  }

  // ---- turns ---------------------------------------------------------------

  private researchPrompt(): string {
    const { brief, seedUrls } = this.state
    return [
      CONVERSATION_INSTRUCTIONS,
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

  /**
   * The cases prompt for a chat draft: what the conversation holds and nothing
   * of how the draft decides. The pages it cites are given by their quotes,
   * which is all a citation holds; the person's messages by their own words.
   * Never the draft's rules or exceptions, and never an assistant turn.
   */
  private conversationCasesPrompt(document: unknown): string {
    const outcomes = ((document as { outcomes?: unknown })?.outcomes ?? []) as unknown[]
    const evidence = ((document as { evidenceRequirements?: unknown })?.evidenceRequirements ?? []) as unknown[]
    const pages = this.state.citations.filter(citation => citation.traced).map(citation => ({ id: citation.location, url: citation.url, text: citation.quote }))
    const statements = [...caseStatements(this.state)].map(([id, text]) => ({ id, text }))
    return [
      CONVERSATION_CASES_INSTRUCTIONS,
      `OUTCOMES\n${JSON.stringify(outcomes)}`,
      `EVIDENCE REQUIREMENTS\n${JSON.stringify(evidence)}`,
      `FACT PATHS THE PACK READS\n${JSON.stringify(factPaths(document))}`,
      `QUOTED PAGES THE DRAFT CITES\n${JSON.stringify(pages, null, 1)}`,
      `THE PERSON'S MESSAGES\n${JSON.stringify(statements, null, 1)}`
    ].join('\n\n')
  }

  private repairPrompt(): string {
    const candidate = this.latest()!
    return [
      this.ports.authorPrompt,
      conversationMode(this.ports.mode) ? 'For a question or clarification, answer naturally without a JSON fence. When proposing a pack, return the required single JSON fence. Use supplied information and verified sources actually read with available tools. Claim research or behavioral testing only when the tool results establish it. List open questions and assumptions.' : RESEARCH_INSTRUCTIONS,
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
      CONVERSATION_INSTRUCTIONS,
      candidate ? `CHAT DRAFT SNAPSHOT (may predate the saved workspace; use the current workspace or current object reads for edits, and preserve its identity)\n${candidate.text}` : '',
      !this.ports.savedPackReview && this.state.cases.length ? `ESTABLISHED CASES\n${JSON.stringify(this.state.cases)}` : '',
      !this.ports.savedPackReview && candidate?.check ? `LATEST CHECK\n${JSON.stringify(candidate.check.cases)}` : '',
      candidate?.packReview?.check ? `PROPOSAL CHECK AGAINST SAVED TESTS (observations, not authority for expectations)\n${JSON.stringify(candidate.packReview.check)}` : '',
      this.transcript(true),
      `THE PERSON'S MESSAGE\n${message}`
    ]
      .filter(Boolean)
      .join('\n\n')
  }

  private transcript(excludeLatestUser = false): string {
    return modelTranscript(this.state, excludeLatestUser)
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
    const savedTarget = !reviewer && this.ports.savedPackReview ? await this.ports.savedPackReview.target(signal) : undefined
    this.check(signal)
    const editorTarget = [...this.state.turns].reverse().find(t => t.role === 'user')?.packContext
    const packBasis = producedBy==='repair' && this.ports.savedPackReview && this.latest()?.packReview ? this.latest()!.packReview!.basis : savedTarget && editorTarget?.packId === savedTarget.packId && editorTarget.path === savedTarget.path ? editorTarget : savedTarget
    if (this.responseId) this.finishResponse()
    this.currentAITarget = undefined
    this.responseId = crypto.randomUUID()
    this.responseEvents = []
    this.set({responses: [...(this.state.responses ?? []), {id: this.responseId, afterTurnId: this.state.turns.at(-1)?.id,
      documents: [], websites: [], sourceIds: [], work: {items: [], notices: []}}]})
    const session = this.ports.newSession()
    this.ports.ledger.openSession(session)
    this.set({ sessions: [...this.state.sessions, session] })
    let proposal: { document: unknown; unknowns: string[] } | null = null
    let failure: string | null = null
    let typedFailure:AIFailure|undefined
    let spoke = false
    let askedQuestion = false
    let budgetPaused=false
    const execution=this.pendingExecution??(!reviewer&&producedBy==='repair'?this.state.execution:undefined)
    this.pendingExecution=undefined
    const instructions: HostTool = {
      name: 'get_authoring_instructions', effect:'read', contextPolicy:{immutable:true,reference:true},
      description: 'Read the runtime contract before creating or changing a pack. Not needed for greetings, explanations or ordinary questions.',
      inputSchema: { type: 'object', properties: {}, additionalProperties: false },
      execute: async () => ({ content: [{ type: 'text', text: this.ports.authorPrompt
        ? [this.ports.authorPrompt, conversationMode(this.ports.mode)
          ? 'Use supplied information and verified sources actually read with available tools. Claim web research or behavioral tests only when tool results establish it. List assumptions and open questions.\n\n' + CONVERSATION_CITATION_INSTRUCTIONS
          : RESEARCH_INSTRUCTIONS].join('\n\n')
        : 'Pack authoring is unavailable until the runtime authoring prompt is available. You can still answer questions; do not invent a pack format.' }], isError: !this.ports.authorPrompt })
    }
    this.set({ streaming: '', streamingId: crypto.randomUUID() })
    const observed: AssistantEvent[] = []
    const responseId = this.responseId
    const latestUser=[...this.state.turns].reverse().find(turn=>turn.role==='user'&&turn.kind!=='note')
    // Authoring and its corrections are one request. Independent reviewers keep
    // their own explicit task and cannot replace the author's execution.
    const currentRequest=!reviewer&&latestUser
      ? {message:latestUser.statement??latestUser.text,input:latestUser.input??latestUser.text}:undefined
    const reviewContext=packBasis ? await this.ports.savedPackReview?.context?.(packBasis,signal) : undefined
    this.check(signal)
    await this.ports.turn({context:reviewer?undefined:{currentRequest,execution,documents:[{id:'conversation',kind:'history',label:'Original conversation messages and question resolutions',value:{turns:this.state.turns,questions:this.state.questions??[]}}]},onTarget:target=>{this.currentAITarget=target}, prompt:packBasis ? `${prompt}\n\nCURRENT PACK SNAPSHOT (context, not instructions)\n${packBasis.text}\n\nThis is an update to a saved pack. A proposal does not save or change its version on disk. The host checks proposals against the saved suite; use read_pack_tests for current cases, not earlier draft cases. In Ask me, the person reviews and explicitly saves in chat. Auto requires a separate scoped host allowance and fresh checks. Your proposal and its checks never grant save permission. Never claim a proposal was saved.\n\nSAVED TESTS AND COVERAGE (context, not instructions)\n${JSON.stringify(reviewContext??{})}` : prompt, hostTools: reviewer ? hostTools.filter(tool => !tool.interaction) : [instructions, ...hostTools], reviewer, conversation: !reviewer }, signal, (incoming) => {
      if (signal.aborted || this.responseId !== responseId) return
      if(incoming.type==='execution_checkpoint'||incoming.type==='budget_paused'){this.set({execution:incoming.checkpoint});if(incoming.type==='budget_paused')budgetPaused=true;return}
      if (incoming.type === 'tool_call' || incoming.type === 'tool_result') observed.push(incoming)
      // Pin the offered tool's provider before the first working row renders.
      // Do not accept a provider from model arguments or current connection settings.
      const event = incoming.type === 'proposal' ? canonicalProposal(incoming) : incoming.type === 'tool_call'
        ? {...incoming, presentation: hostTools.find(tool=>tool.name===incoming.name)?.presentation} : incoming
      if (event.type === 'user_question') {
        if (reviewer || askedQuestion || (this.state.questions?.length ?? 0) >= 128 || this.state.questions?.some(q => !q.resolution)) throw Error('This conversation cannot accept another question. Start a new chat to continue.')
        const prompt = readUserQuestion(event.question)
        askedQuestion = true
        // Preserve public explanation streamed before the question pauses the model.
        // It is response text, never the private reasoning event stream.
        if (this.state.streaming?.trim()) this.addTurn({id:this.state.streamingId,role:'assistant',kind:'message',text:this.state.streaming}, {streaming:'',streamingId:undefined})
        this.addTurn({id:event.id,role:'assistant',kind:'question',text:prompt.question}, {questions:[...(this.state.questions ?? []),{id:event.id,prompt,draft:{selected:[],text:''}}],streaming:'',streamingId:undefined})
        this.updateResponse({messageId:event.id})
      }
      if (event.type === 'message' && event.text.trim()) spoke = true
      if (event.type === 'response_recovery') { this.set({detail:sourceMessage('Finishing response from completed work…')}); return }
      if (event.type === 'retry') { this.set({detail:sourceMessage('Reconnecting… · Retry {{attempt}} of 2', {attempt:event.attempt})}); return }
      if (event.type === 'message_progress') { this.set({ streaming: event.text, detail:event.text?sourceMessage('Writing response…'):sourceMessage('Working…') }); return }
      this.set({ events: [...this.state.events, event] })
      this.responseEvents.push(event)
      if (event.type === 'message') {
        const messageId = this.state.streamingId ?? crypto.randomUUID()
        this.addTurn({ id: messageId, role: 'assistant', kind: 'message', text: event.text }, {streaming: '', streamingId: undefined})
        this.updateResponse({messageId})
        const known = [...this.state.turns.flatMap(turn => turn.attachments ?? []), ...(this.state.responses ?? []).flatMap(row => row.documents)]
        for (const file of citedAttachments(event.text, known)) this.recordDocument(file)
      }
      if (event.type === 'tool_result' && !event.isError && ['read_source', 'cite_excerpt'].includes(event.name)) {
        const data = event.structured as {sourceId?: unknown} | undefined
        if (typeof data?.sourceId === 'string' && this.ports.ledger.byId(data.sourceId)?.document) {
          const row = this.state.responses?.find(row => row.id === this.responseId)
          this.updateResponse({sourceIds: [...new Set([...(row?.sourceIds ?? []), data.sourceId])]})
        }
      }
      this.updateResponse({work: summarizeWork(this.responseEvents, true)})
      if (event.type === 'proposal') proposal = { document: event.document, unknowns: event.unknowns }
      if (event.type === 'error') {failure = event.message;typedFailure=readFailure(event.failure)}
    })
    this.check(signal)
    this.set({ streaming: '' })
    const probes = await runtimeProbes(observed, (this.ports.now?.() ?? new Date()).toISOString())
    this.check(signal)
    if (probes.length) this.set({ probes: [...(this.state.probes ?? []), ...probes].slice(-64) })
    await this.verifyAcquisitions(session, signal)
    // A verdict is also an answer to `withheld()`, and the only one the
    // readiness key cannot carry: a cited source whose receipt failed withholds
    // the draft even where no citation points at it. A turn that ends in an
    // error or a Stop never reaches the settle that would say so, so the
    // withdrawal happens where the verdict lands -- and says why, because the
    // outcome of such a turn is the engine's error and not the verdict.
    const withholds = this.state.readiness === '' ? null : this.withheld()
    if (withholds !== null) { this.set({ readiness: '' }); this.undone = withholds }
    if(budgetPaused){this.finishResponse();throw new ExecutionPaused()}
    if (askedQuestion) { this.finishResponse(); throw new AwaitingAnswer() }
    if (failure !== null) throw typedFailure ? new AIFailureError(typedFailure) : new Error(failure)
    if (proposal === null && spoke && !reviewer) {
      const afterTurnId = this.state.responses?.find(row => row.id === responseId)?.afterTurnId
      const help = this.state.questions?.find(question => question.resolution?.kind === 'help' && question.resolution.turnId === afterTurnId)
      this.finishResponse()
      // Help is not a decision. A prose-only explanation must not strand the
      // person without controls. Reuse the retained question without another
      // model request. A model-authored follow-up already exits above.
      if (help && !this.state.questions?.some(question => !question.resolution) && (this.state.questions?.length ?? 0) < 128) {
        const id = crypto.randomUUID(), prompt = structuredClone(help.prompt)
        this.addTurn({id,role:'assistant',kind:'question',text:prompt.question}, {questions:[...(this.state.questions ?? []),{id,prompt,draft:{selected:[],text:''}}]})
        throw new AwaitingAnswer()
      }
      return null
    }
    if (proposal === null) throw new Error(sourceMessage("the assistant ended without a proposal"))
    const taken = proposal as { document: unknown; unknowns: string[] }
    if (!reviewer) {
      const text = JSON.stringify(taken.document, null, 2)
      const digest = await digestOf(text)
      const identity = jsonIdentity(taken.document)
      const repeated = this.state.candidates.some((candidate) => jsonIdentity(candidate.document) === identity)
      const candidate: Candidate = { ...(packBasis ? {packReview:{basis:packBasis}} : {}), responseId: this.responseId, revision: this.state.candidates.length + 1, document: taken.document, text, digest, producedBy }
      if (!repeated) {
        this.set({
          candidates: [...this.state.candidates, candidate],
          unknowns: taken.unknowns,
          citations: conversationMode(this.ports.mode) ? [] : traceCitations(taken.document, this.ports.ledger),
          expectationIssues: this.state.expectationIssues.map(issue => issue.resolved || !issue.proposal ? issue : {
            ...issue, proposal: undefined, proposalError: sourceMessage("The draft changed. Request a new correction before approving.")
          })
        })
      } else if (producedBy === 'conversation') {
        // An answer that returned the document unchanged is an answer, and
        // the candidate on hand keeps its check.
        this.set({ unknowns: taken.unknowns })
      } else {
        this.set({ unknowns: taken.unknowns })
        this.addTurn({ role: 'assistant', kind: 'note', text: sourceMessage("The assistant repeated an earlier candidate.") })
        throw new Stalled()
      }
      if (taken.unknowns.length) this.addTurn({ role: 'assistant', kind: 'unknowns', text: taken.unknowns.map((line) => `• ${line}`).join('\n') })
      if (!repeated) await this.traceConversation(signal)
    }
    this.finishResponse()
    return taken
  }

  private async researchTurn(signal: AbortSignal): Promise<void> {
    this.set({ phase: 'conversation', detail: 'Working…' })
    this.ports.log(this.ports.mode === 'draft' ? sourceMessage("draft: authoring from supplied information") : sourceMessage("research: drafting from sources"))
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
        this.addTurn({ role: 'assistant', kind: 'note', text: sourceMessage("The turn's step budget was spent before a proposal was written; continuing ({{value0}} of {{value1}}).", { value0: continuation + 1, value1: MAX_CONTINUATIONS }) })
        this.ports.log(sourceMessage("continue: turn {{value0}} of {{value1}}", { value0: continuation + 1, value1: MAX_CONTINUATIONS }))
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
    const conversation = conversationMode(this.ports.mode)
    this.set({ phase: 'cases', detail: conversation ? sourceMessage("Establishing test cases without the draft's rules.") : sourceMessage("Establishing test cases from the sources.") })
    this.ports.log(conversation ? sourceMessage("cases: a reviewer establishes expectations from your messages and the pages the draft cites") : sourceMessage("cases: a reviewer establishes expectations from the excerpts"))
    // What a chat draft's cases rest on is read as it stands now: the pages
    // it cites are traced again before the reviewer is shown their quotes.
    if (conversation) await this.traceConversation(signal)
    const proposal = await this.engineTurn(signal, 'research', conversation ? this.conversationCasesPrompt(candidate.document) : this.casesPrompt(candidate.document), [], true)
    const { admitted, dropped } = admitCases(proposal?.document, this.ports.ledger, this.state.cases, this.grounds())
    const held: HeldProposal = { candidateDigest: candidate.digest, admitted, dropped, unknowns: proposal?.unknowns ?? [] }
    this.set({ heldProposal: held })
    return held
  }

  /**
   * Establish cases where none are, then check, and repair until the budget.
   *
   * A chat draft takes the same path with two differences. Its structure is
   * validated first, so no reviewer turn is spent on a draft that is not a
   * pack; and nothing repairs it: a disagreement between the draft and cases
   * written without its rules goes to the person, as its instructions say.
   * `establish` false keeps the path model-free, for a recheck.
   */
  private async casesAndCheck(signal: AbortSignal, repair = true, establish = true): Promise<void> {
    const candidate = this.latest()
    if (!candidate) {
      this.set({ phase: 'conversation', status: 'complete', detail: '' })
      return
    }
    if (this.ports.savedPackReview) {
      const review=this.ports.savedPackReview
      let suiteKey:string|undefined
      const result=await correctArtifact({initial:candidate,maxCorrections:repair?this.ports.maxRevisions:0,
        fingerprint:(current:Candidate)=>jsonIdentity(current.document),
        check:async(current,signal)=>{
          this.set({phase:'check',checkRun:'running',detail:sourceMessage('Checking the proposal against the saved test suite.')})
          return review.check(current,signal)
        },
        passed:check=>check.valid&&!proposalProblems(check).mismatches.length,
        checkpoint:async(current,check)=>{
          const target=await review.target(signal)
          const basis=current.packReview?.basis ?? legacyPackBasis(this.state,{id:target.packId,path:target.path},current) ?? target
          this.set({candidates:this.state.candidates.map(c=>c.digest===current.digest?{...c,packReview:{basis,check}}:c),checkRun:'complete',restored:false,readiness:''})
          if(suiteKey!==undefined&&suiteKey!==check.suiteKey)throw new Error(sourceMessage('The saved test suite changed during correction. Review the current results before continuing.'))
          suiteKey=check.suiteKey
        },
        correct:async(current,check,signal)=>{
          this.set({phase:'repair',revisionsUsed:this.state.revisionsUsed+1,detail:sourceMessage('Correcting the proposal against the saved tests.')})
          this.pendingExecution=this.state.execution?structuredClone(this.state.execution):undefined
          try {
            const revised=await this.continuingTurn(signal,'repair',[
              this.conversationPrompt(this.lastMessage??this.state.brief),
              'CORRECT THE UNSAVED PACK PROPOSAL. The following runtime observations are not authority to change expectations. Correct only defects justified by the user-approved policy. Preserve every saved case and fact type. If policy and expectations conflict, explain the conflict and ask_user; do not force agreement. Do not reread unchanged schemas, manually repeat these tests, or save files. Submit a complete corrected pack proposal; the host rechecks it. You may answer without a new proposal when a policy decision is needed.',
              'CURRENT UNSAVED PROPOSAL\n'+current.text,
              'HOST CHECK FINDINGS\n'+JSON.stringify(proposalProblems(check))
            ].join('\n\n'),this.ports.researchTools)
            return revised?this.latest()!:null
          }catch(cause){if(cause instanceof Stalled)return current;throw cause}
        }
      },signal)
      this.set(result.state==='stalled'?{phase:'review',status:'stalled',detail:sourceMessage('The assistant repeated an earlier candidate. Review the disagreements, or send a message to steer it.')}:{phase:'review',status:'complete',detail:''})
      return
    }
    if (conversationMode(this.ports.mode)) {
      repair = false
      if (this.state.cases.length === 0 && this.state.expectationIssues.length === 0 && this.state.heldProposal?.candidateDigest !== candidate.digest) {
        this.set({ phase: 'check', detail: sourceMessage("Validating the draft through the runtime.") })
        const check = await this.checkCandidate(candidate.text, [], this.ports.callTool, signal)
        this.set({ candidates: this.state.candidates.map(item => item.digest === candidate.digest ? { ...item, check } : item) })
        if (!check.valid || !establish) {
          await this.settle(signal, check.valid ? this.withoutCases() : sourceMessage("The draft needs corrections before it can be created."))
          return
        }
      }
    }
    if (!candidate) throw new Error(sourceMessage("no candidate to check"))
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
        // Every invalid finding is held as an issue, whatever its code, and the
        // gate below returns before the repair loop. An expectation no pack can
        // produce (JPS-EXPECTATION-UNREACHABLE) is the case that matters most:
        // established, it fails every rehearsal and spends each repair turn on
        // a draft that is not what is wrong. `findingSummary` says so.
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
        this.addTurn({ role: 'assistant', kind: 'note', text: sourceMessage("Proposed cases dropped: {{value0}}.", { value0: dropped.map(d => d.id).join(', ') }) })
      }
      if (held.unknowns.length) this.addTurn({ role: 'assistant', kind: 'unknowns', text: held.unknowns.map((line) => `• ${line}`).join('\n') })
      if (admitted.length === 0) {
        // Where nothing grounded a case because the sources did not verify,
        // that is the sentence, not the absence of cases.
        this.set({ status: 'needs-input', phase: 'review', detail: this.withheld() ?? (conversationMode(this.ports.mode)
          ? sourceMessage("No test case could be grounded in your messages or in a page the draft cites. Tell the assistant how the pack should decide a case, or have it read and cite the pages the pack rests on.")
          : sourceMessage("No test case could be grounded in a cited excerpt. Cite the requirements, or say what the cases should be.")) })
        return
      }
      this.ports.log(sourceMessage("cases: {{value0}} established, {{value1}} invalid expectations, {{value2}} dropped", { value0: cases.length, value1: issues.length, value2: dropped.length }))
    }
    if (this.unresolvedExpectations()) {
      this.set({ phase: 'review', status: 'needs-input', detail: this.unresolvedExpectations()! })
      return
    }
    const result=await correctArtifact({initial:this.latest()!,maxCorrections:repair?Math.max(0,this.ports.maxRevisions-this.state.revisionsUsed):0,
      fingerprint:(current:Candidate)=>jsonIdentity(current.document),
      check:async(current,signal)=>{
        this.check(signal)
        this.set({phase:'check',detail:sourceMessage('Checking revision {{value0}} through the runtime.',{value0:current.revision})})
        return this.checkCandidate(current.text,this.state.cases,this.ports.callTool,signal)
      },
      checkpoint:async(current,check)=>{this.set({candidates:this.state.candidates.map(c=>c.digest===current.digest?{...c,check}:c)})},
      passed:()=>completeCurrentCheck(this.state),
      correct:async(current,_check,signal)=>{
        this.set({phase:'repair',revisionsUsed:this.state.revisionsUsed+1,detail:sourceMessage('Repairing the candidate against the established cases.')})
        try{const revised=await this.continuingTurn(signal,'repair',this.repairPrompt(),this.ports.researchTools);return revised?this.latest()!:null}
        catch(cause){if(cause instanceof Stalled)return current;throw cause}
      }
    },signal)
    if(result.state==='ready'){await this.settle(signal,'');return}
    if(result.state==='answered'){this.set({phase:'review',status:'needs-input',detail:sourceMessage('Review the unresolved policy before continuing.')});return}
    if(result.state==='stalled'){this.set({phase:'review',status:'stalled',detail:sourceMessage('The assistant repeated an earlier candidate. Review the disagreements, or send a message to steer it.')});return}
    if(!repair){this.set({phase:'review',status:'needs-input',detail:result.check.valid
      ? sourceMessage('The unchanged draft disagrees with the reviewed expectations. Review the results or request a pack change.')
      : sourceMessage('The unchanged draft is not a valid pack: {{count}} diagnostics. Review them, or send a message to repair it.',{count:result.check.diagnostics.length})});return}
    this.set({phase:'review',status:'budget',detail:sourceMessage('The revision budget of {{value0}} is spent with disagreements remaining. Review the results, or send a message to continue.',{value0:this.ports.maxRevisions})})
  }

  /**
   * The one place readiness is decided: the latest check must pass **and**
   * nothing may be withheld. Every path that rests at review comes here --
   * a passed check, an unchanged answer to a message, a continuation -- so
   * no path reaches `ready` around the sources it rests on.
   */
  private unresolvedExpectations(): string | null {
    const count = this.state.expectationIssues.filter(issue => !issue.resolved).length
    return count ? sourceMessage("{{count}} invalid expectations must be corrected and approved before testing or creating the pack.", { count }) : null
  }

  /** Background draft verification owns readiness, never another action's response controls. */
  private setReviewResult(result: Pick<RunState, 'status' | 'detail' | 'readiness'>, owner: 'action' | 'background'): void {
    if (owner === 'background' && (this.state.phase !== 'review' || !['ready', 'needs-input'].includes(this.state.status))) {
      this.set({readiness: result.readiness})
      return
    }
    this.set({...result, phase: 'review'})
  }

  /**
   * Readiness is decided here, and what is decided is also recorded, as the key
   * of what it was decided about. `ready` is the status of the *last action*,
   * and a Stop, a spent budget or a failed follow-up turn overwrites it without
   * touching the candidate; Create asks about the candidate, so it reads the
   * record instead, which stands only while the candidate, the cases and the
   * citations it names are the ones on hand.
   */
  private settleReview(notPassing: string, owner: 'action' | 'background' = 'action'): void {
    // The same refusal `casesAndCheck` makes before it checks anything, kept
    // here as a backup: no path rests at `ready` with an expectation open. It
    // fires only where a passing check and an open issue coexist, which no run
    // produces -- so nothing failing without it means nothing.
    if (this.unresolvedExpectations()) {
      this.setReviewResult({ status: 'needs-input', detail: this.unresolvedExpectations()!, readiness: '' }, owner)
      return
    }
    const passing = completeCurrentCheck(this.state)
    if (!passing) {
      this.setReviewResult({ status: 'needs-input', detail: notPassing, readiness: '' }, owner)
      return
    }
    // Sources may have been verified, or failed, since the last trace. A chat
    // draft's citations were traced as its settle began, against the
    // documents as they are now.
    const latest = this.latest()
    if (latest && !conversationMode(this.ports.mode)) this.set({ citations: traceCitations(latest.document, this.ports.ledger) })
    const withheld = this.withheld()
    if (withheld !== null) {
      this.setReviewResult({ status: 'needs-input', detail: withheld, readiness: '' }, owner)
      return
    }
    this.setReviewResult({ status: 'ready', detail: !conversationMode(this.ports.mode)
      ? sourceMessage("Every established case agrees. Review the draft, its sources and the unknowns before creating the pack.")
      : this.state.citations.length
        ? sourceMessage("Every test case written without the draft's rules agrees, and every citation is traced to a page read in this chat. Review the draft and the unknowns before creating the pack.")
        : sourceMessage("Every test case written without the draft's rules agrees. This pack cites no source; it rests on what you told the assistant. Review the draft and the unknowns before creating the pack."),
      readiness: readinessKey(this.state) }, owner)
  }

  /** What a chat draft's cases may rest on now: the person's messages and its traced citations. */
  private grounds(): CaseGrounds | undefined {
    return conversationMode(this.ports.mode) ? { statements: caseStatements(this.state), citations: this.state.citations } : undefined
  }

  /** Why a checked chat draft without cases is not ready, and what completes it. */
  private withoutCases(): string {
    return sourceMessage("This draft has no test cases written without its rules yet. Establish them before creating the pack; nothing is created or changed until you do.")
  }

  /**
   * What an expectation rests on while it still rests on it: a verified
   * excerpt, or in a chat draft one of the person's messages or a page the
   * draft cites and traced. Null where it no longer does.
   */
  private groundOf(source: string): { label: string; value: unknown } | null {
    const excerpt = this.ports.ledger.verifiedExcerpt(source)
    if (excerpt) return { label: 'SOURCE EXCERPT', value: excerpt }
    const grounds = this.grounds()
    const statement = grounds?.statements.get(source)
    if (statement !== undefined) return { label: "THE PERSON'S STATEMENT", value: { id: source, text: statement } }
    const page = grounds?.citations.find(citation => citation.traced && citation.location === source)
    return page ? { label: 'SOURCE EXCERPT', value: { id: source, url: page.url, text: page.quote } } : null
  }

  /** Settle at review, a conversation draft's citations traced as they stand now. */
  private async settle(signal: AbortSignal, notPassing: string): Promise<void> {
    await this.traceConversation(signal)
    this.settleReview(notPassing)
  }

  /**
   * Trace a conversation draft's citations against the documents kept in this
   * chat, when a candidate is set, at settle, and when a saved draft is opened.
   * Asynchronous, because a citation counts only where its document verifies
   * under the gateway pin as it is now, and while it runs the draft is not
   * ready and says its citations are being checked. The last trace started is
   * the one that lands: an earlier one answering late would put back a trace
   * of a moment that has passed.
   */
  private async traceConversation(signal: AbortSignal): Promise<void> {
    const latest = this.latest()
    if (!conversationMode(this.ports.mode) || !latest) return
    const trace = ++this.traces
    this.set({ tracing: true })
    let citations: Citation[] | null = null
    let landed: string | null = null
    try {
      // One pin and one list of kept documents for each attempt, taken as it
      // begins: every document is verified under that pin, and the attempt
      // lands only where the pin and the documents are still those when it
      // ends. Where they moved it is taken again, a bounded number of times.
      for (let attempt = 0; attempt < TRACE_ATTEMPTS && citations === null; attempt += 1) {
        const pin = this.ports.pin?.() ?? null
        const kept = this.ports.documents?.() ?? []
        const basis = documentBasis(pin, kept)
        const load = this.ports.loadDocument
        const traced = await tracePageCitations(latest.document, kept, (reference, loading) => pin !== null && load ? load(reference, pin, loading) : unverifiable(), signal)
        this.check(signal)
        if (this.basisNow() === basis) {
          citations = traced
          landed = basis
        }
      }
      citations ??= unchecked(latest.document, sourceMessage("The gateway pin or this chat's documents changed while the citations were checked. Recheck the draft."))
    } catch (cause) {
      // An interrupted trace says nothing about the draft: nothing it was
      // checking counts as traced, and it never reads as citing nothing.
      citations = unchecked(latest.document, sourceMessage("The citation check was interrupted. Recheck the draft."))
      throw cause
    } finally {
      if (trace === this.traces) {
        if (citations !== null && this.latest()?.digest === latest.digest) this.set({ citations, tracedBasis: landed ?? undefined })
        this.set({ tracing: false })
      }
    }
  }

  /**
   * What a chat draft's trace rests on now: the gateway pin and the documents
   * the chat keeps. Create compares it with `tracedBasis` when it is asked.
   * See `documentBasis`.
   */
  basisNow(): string {
    return documentBasis(this.ports.pin?.() ?? null, this.ports.documents?.() ?? [])
  }

  /**
   * Trace a chat draft at rest: when it is reopened, and when its basis moved.
   * Bounded like any action, and ended by Stop, by the next action, by the
   * next such trace and by its owner going away. A draft whose readiness the
   * move withdrew is settled again, without a model, once a trace lands; a
   * trace another superseded leaves that to the one that superseded it.
   */
  private traceAtRest(): void {
    if (this.detached) return
    this.background?.abort()
    const controller = new AbortController()
    this.background = controller
    const deadline = setTimeout(() => controller.abort(), this.ports.seconds * 1000)
    void this.traceConversation(controller.signal).then(() => {
      if (this.pendingSettle && this.background === controller && !controller.signal.aborted && !this.running) {
        this.pendingSettle = false
        this.settleReview(sourceMessage("The candidate needs review."), 'background')
      }
    }, () => undefined).finally(() => {
      clearTimeout(deadline)
      if (this.background === controller) this.background = null
    })
  }

  /** The run's owner is mounted, and traces at rest may run. */
  attach(): void {
    this.detached = false
  }

  /** The run's owner is gone: any trace at rest ends, and none starts. */
  detach(): void {
    this.detached = true
    this.background?.abort()
    this.pendingSettle = false
    this.retracePending = false
  }

  /**
   * The gateway pin or the chat's documents may have changed. Where they are
   * no longer what the draft's citations were traced on, a readiness reached
   * on them is withdrawn at once, and the citations are traced again: now at
   * rest, settling a draft that was ready again without a model; or when the
   * action in flight ends, unless its own settle traced them on the new basis.
   */
  basisChanged(): void {
    if (this.detached || !conversationMode(this.ports.mode) || !this.latest() || this.basisNow() === this.state.tracedBasis) return
    if (!this.running && (this.latest()?.check?.record || this.latest()?.previousCheck?.record)) { void this.refreshCheck(); return }
    // Withdrawn at once either way. Settled again only at rest: an action in
    // flight settles for itself, and one that ends without a settle keeps the
    // outcome it ended with.
    if (this.state.readiness !== '') {
      if (!this.running) this.pendingSettle = true
      this.set({ readiness: '' })
    }
    if (this.running) {
      this.retracePending = true
      return
    }
    this.traceAtRest()
  }

  /** Why the candidate is not ready even where its cases agree, or null. */
  private withheld(): string | null {
    const failed = this.ports.ledger.sources.filter((record) => record.verification.state === 'failed' && record.excerpts.length > 0)
    if (failed.length > 0) {
      return sourceMessage("{{value0}} cited source(s) failed receipt verification ({{value1}}), so their excerpts are withheld and the draft is not ready.", { value0: failed.length, value1: failed.map((r) => r.id).join(', ') })
    }
    // A document that failed verification leaves its citations untraced, so
    // the conversation modes withhold for it here -- and for a trace taken on
    // a pin or documents that are no longer the ones standing.
    if (conversationMode(this.ports.mode)) {
      if (this.state.tracedBasis !== this.basisNow()) return sourceMessage("The gateway pin or this chat's documents changed while the citations were checked. Recheck the draft.")
      return citationGap(this.state.citations, this.ports.mode)
    }
    const untraced = this.state.citations.filter((citation) => !citation.traced)
    if (untraced.length > 0) {
      return sourceMessage("{{value0}} citation(s) in the draft could not be traced to a verified excerpt ({{value1}}). Ask the assistant to cite from what it read, or review the sources.", { value0: untraced.length, value1: untraced.map((c) => c.sourceId).join(', ') })
    }
    if (this.state.citations.length === 0) {
      return sourceMessage("The draft cites no source. A pack with no traced citation is an assumption; ask the assistant to cite what it read.")
    }
    return null
  }

  // ---- verification ----------------------------------------------------------

  /** Seal the session the turn acquired under, fetch the registry, verify, and mark every record. */
  private async verifyAcquisitions(session: string, signal: AbortSignal, seal = true): Promise<void> {
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
      if (seal) await this.ports.seal(session, signal)
      registryText = await this.ports.registry(signal)
    } catch (cause) {
      if ((cause as Error)?.name === 'AbortError') throw cause
      const finding: Finding = { sessionId: session, callIndex: null, status: 'seal-or-registry-unavailable' }
      this.ports.log(sourceMessage("verify: {{value0}} — {{value1}}", { value0: session, value1: (cause as Error).message }))
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
    this.ports.log(sourceMessage("verify: {{value0}} — {{value1}} ({{value2}})", { value0: session, value1: verdict.ok ? 'verified' : 'FAILED', value2: verdict.findings.map((f) => f.status).join(', ') }))
    records.forEach((record, position) => {
      const own = verdict.findings.filter((f) => f.callIndex === position || f.callIndex === null || f.file === `${position}.json`)
      ledger.verified(record.id, verdict.ok ? { state: 'verified', at: stamp, keyId: verdict.keyId } : { state: 'failed', at: stamp, findings: own })
    })
    // Citations traced before the verdict were traced against unchecked
    // receipts; they are traced again now that the verdict is in.
    const latest = this.latest()
    if (latest && !conversationMode(this.ports.mode)) this.set({ citations: traceCitations(latest.document, ledger) })
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
  // A chat draft's case may rest on the person's own words, and the transcript
  // does not travel with the pack, so the record keeps each message a case or
  // an expectation issue rests on, under the id the case names.
  const said = caseStatements(state)
  const statements = [...new Set([...state.cases, ...state.expectationIssues.map(issue => issue.original)].map(row => row.expectationSource))]
    .filter(id => said.has(id)).map(id => ({ id, text: said.get(id)! }))
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
    recordedAt: new Date().toISOString(),
    testHistory: state.candidates.flatMap(candidate => {
      const check=candidate.check ?? candidate.previousCheck
      return check ? [{text:candidate.text,check}] : []
    }),
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
    ...(statements.length ? { statements } : {}),
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
  return sourceMessage("The case expects a handoff target beside a disposition that requests no handoff. No evaluation reports a target for a handoff of \"none\", so the pair could never pass.")
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
    (!check.record || jsonIdentity(check.record.inputs) === jsonIdentity(state.cases)) &&
    check.cases.length === state.cases.length && check.cases.every((row, index) => row.passed && row.id === state.cases[index]?.id)
}

/**
 * What a settle that reached `ready` was about: the candidate it was reached
 * on, the cases that agreed, and the citation trace it was not withheld for.
 * Readiness is recorded as this key and read back by comparison, so it lapses
 * of its own accord the moment any of the three moves.
 */
export function readinessKey(state: RunState): string {
  return JSON.stringify([
    state.candidates.at(-1)?.digest ?? '',
    state.cases.map(row => [row.id, jsonIdentity(row)]),
    state.citations.map(citation => [citation.sourceId, citation.location, citation.traced]),
    state.tracedBasis ?? ''
  ])
}

/**
 * The one rule Create reads, in every mode. The run must have settled at
 * `ready` about this candidate, the candidate must be valid, and every citation
 * it makes must be traced -- a failed receipt or an unverified document
 * withholds it. A draft that rests on sources must make at least one citation;
 * only `draft` may cite nothing, because the person's own statements are then
 * its basis. And every intended, admitted case -- written without the draft's
 * rules, from its sources or the person's own words -- must have a current
 * passing result, with no expectation left open.
 *
 * Not `status === 'ready'`: the status reports the last action, so a Stop, a
 * spent budget or a failed follow-up turn withdrew a Create that the candidate
 * on hand still earns, and the panel then explained it with the one reason
 * that was not true.
 */
export function canCreateDraft(state: RunState, mode: AuthoringMode = 'research', basis?: string): boolean {
  // Never mid-turn. `status === 'ready'` made that impossible by construction,
  // and a rule that asks only about the candidate would answer yes while the
  // turn that is about to move it is still in flight. Both callers check it
  // already; the rule owns it, so the next caller inherits no trap.
  if (state.status === 'running') return false
  // Nor while a conversation draft's citations are being traced: until the
  // trace lands they say nothing, and none is a claim that it cites nothing.
  if (state.tracing) return false
  // And a chat draft's citations hold only on the pin and the documents they
  // were traced on. Asked with the basis standing now, a readiness reached on
  // another is no readiness, whether or not anything has noticed the move;
  // asked without one, it cannot say, and the answer is no.
  if (conversationMode(mode) && (basis === undefined || state.tracedBasis !== basis)) return false
  const latest = state.candidates.at(-1)
  return state.readiness !== '' && state.readiness === readinessKey(state) &&
    latest?.check?.valid === true && latest.check.documentDigest === latest.digest &&
    !state.expectationIssues.some(issue => !issue.resolved) && completeCurrentCheck(state) &&
    (mode === 'draft' || state.citations.length > 0) && state.citations.every(citation => citation.traced)
}

/**
 * Whether a checked chat draft without cases may ask for them: a model call,
 * offered only at rest, on a draft whose current check is valid and which has
 * neither cases nor an expectation waiting on the person.
 */
export function canEstablishCases(state: RunState, mode: AuthoringMode | undefined): boolean {
  const latest = state.candidates.at(-1)
  return conversationMode(mode) && state.status !== 'running' && !state.restored && latest?.check?.valid === true &&
    latest.check.documentDigest === latest.digest && state.cases.length === 0 && state.expectationIssues.length === 0
}

/** The rule for `research`, which the source-led page reads. */
export function canCreateResearchDraft(state: RunState): boolean {
  return canCreateDraft(state, 'research')
}
