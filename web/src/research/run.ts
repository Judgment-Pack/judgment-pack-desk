import type { PackProposalBasis, PackReview } from '../chat/packProposal'
import type { AIFailure } from '@judgment-pack/harness/failures'
import type { ExecutionCheckpoint } from '@judgment-pack/harness/execution/state'
import type { SavedQuestion } from '@judgment-pack/harness/questions'
import { conversationMode, type AuthoringMode } from './mode'
import type { ResponseHistory } from '../chat/responseHistory'
import type { ChatAttachment } from '../chat/store'
import { matchesPageQuote, type DocumentReference, type VerifiedDocument } from '../documents/client'
import { needsPartialConsent } from '../documents/record'
import type { RuntimeProbe } from './runtimeProbes'
import { sourceMessage } from '../i18n/source'
import { readinessKey } from './readiness'
import type { AssistantEvent } from '@judgment-pack/harness/engine'
import { jsonIdentity, type AuthoringCase, type CandidateCheck } from './checkCandidate'
import type { Ledger, SourceRecord } from './ledger'
import { memberOf, parseJsonText, stringMember, type JsonNode } from './verify/canon'
import type { SessionVerdict } from './verify/session'

/** Historical authoring data and pure readiness, citation and record readers. */
export type Phase = 'idle' | 'research' | 'cases' | 'check' | 'repair' | 'conversation' | 'review'
export type Status = 'idle' | 'running' | 'complete' | 'ready' | 'needs-input' | 'budget' | 'stalled' | 'stopped' | 'failed'

export interface AssistantProvenance { connectionId:string; connectionName:string; model:string }
export interface Turn {
	creation?: import('../assistant/serverRuns/creation').CreationAction
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
  position?:number
	finding?: {status:'invalid';message:string;admitted?:boolean;unreachable?:boolean}
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
  /** Each running delegated task's prose so far, by task id (`message_progress` with a `taskId`); ephemeral, like `streaming`. */
  taskProgress?: Readonly<Record<string, string>>
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

export { INITIAL_STATE } from './initialState'

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

/**
 * What a chat draft's trace rests on besides the draft: the gateway pin its
 * documents verify under and the documents the chat keeps, as one key. A trace
 * that ends on another basis than it began on is a trace of a moment that has
 * passed.
 */
export function documentBasis(pin: unknown, kept: readonly ChatAttachment[]): string {
  return JSON.stringify([pin ?? null, kept.map(file => file.document ? [file.document.id, file.document.digest, file.document.pages, file.document.allowPartial] : null)])
}

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
export {readinessKey} from './readiness'

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
