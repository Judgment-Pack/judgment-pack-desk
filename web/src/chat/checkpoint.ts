import { readAttachments, readMessageId, readResponseHistory } from './responseHistory'
import { readPreviousCheck, readProbes } from '../research/runtimeProbes'
import { INITIAL_STATE, type RunState, type Turn } from '../research/run'
import { Ledger, type SourceRecord } from '../research/ledger'
import { READ_DIALECTS, SEARCH_DIALECTS } from '../research/providers'
import { memberOf, parseJsonText } from '../research/verify/canon'
import type { ResearchDialect } from '../config/deskConfig'

export interface SavedSource {
  session: string
  kind: 'search' | 'page'
  request: SourceRecord['request']
  requestedAt: string
  text: string | null
  failure: string | null
  quotes: string[]
}
export interface Checkpoint { state: RunState; sources: SavedSource[] }

export function checkpoint(state: RunState, sources: readonly SourceRecord[]): Checkpoint {
  // Derived views, events and verdicts are deliberately omitted. Reconstruct
  // candidates from their bytes; source records from the original wire reply.
  return {
    state: { ...state, streaming: undefined, streamingId: undefined, events: [], verdicts: {}, registries: {}, citations: [],
      candidates: state.candidates.map(({ check, ...candidate }) => ({ ...candidate, ...(check ? { previousCheck: check } : {}) })),
      expectationIssues: state.expectationIssues.map(({ proposal: _proposal, ...issue }) => issue) },
    sources: sources.map(source => ({ session: source.session, kind: source.kind, request: source.request,
      requestedAt: source.requestedAt, text: source.response?.text ?? null,
      failure: source.failure, quotes: source.excerpts.map(excerpt => excerpt.text) }))
  }
}
const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every(item => typeof item === 'string')
function invalid(): never { throw new Error('Saved chat data is not supported. History was left untouched.') }

/** Validate display data before it reaches React; all trust is reset by restore. */
export function decodeCheckpoint(value: unknown): Checkpoint {
  if (!object(value) || !object(value.state) || !Array.isArray(value.sources)) return invalid()
  const state = value.state
  if (typeof state.brief !== 'string' || !strings(state.seedUrls) || !strings(state.unknowns) || !strings(state.sessions)
    || !Array.isArray(state.turns) || !Array.isArray(state.candidates) || !Array.isArray(state.cases)
    || !Array.isArray(state.expectationIssues) || !Number.isInteger(state.revisionsUsed) || (state.revisionsUsed as number) < 0) return invalid()
  const turns: Turn[] = state.turns.map((turn, index) => {
    if (!object(turn) || (turn.role !== 'user' && turn.role !== 'assistant') || typeof turn.text !== 'string'
      || (turn.input !== undefined && typeof turn.input !== 'string') || (turn.interrupted !== undefined && typeof turn.interrupted !== 'boolean') || typeof turn.at !== 'string' || !['brief', 'message', 'unknowns', 'note'].includes(String(turn.kind))) return invalid()
    return { id: readMessageId(turn.id) ?? `legacy-turn-${index}`, ...(turn.attachments !== undefined ? {attachments: readAttachments(turn.attachments, 4)} : {}), role: turn.role, text: turn.text, at: turn.at, kind: turn.kind as Turn['kind'], ...(typeof turn.input === 'string' ? { input: turn.input } : {}), ...(turn.interrupted === true ? { interrupted: true } : {}) }
  })
  if (new Set(turns.map(turn => turn.id)).size !== turns.length) return invalid()
  const candidates = state.candidates.map((candidate, index) => {
    if (!object(candidate) || typeof candidate.text !== 'string') return invalid()
    // This parser also bounds nesting and rejects duplicate JSON members.
    parseJsonText(candidate.text)
    return { responseId: readMessageId(candidate.responseId), text: candidate.text, document: JSON.parse(candidate.text) as unknown, digest: '', revision: index + 1, producedBy: 'conversation' as const, previousCheck: readPreviousCheck(candidate.previousCheck) }
  })
  const readCase = (row: unknown): RunState['cases'][number] => {
    if (!object(row) || typeof row.id !== 'string' || !object(row.facts) || typeof row.expectationSource !== 'string' || typeof row.rationale !== 'string') return invalid()
    return { id: row.id, facts: row.facts, expectationSource: row.expectationSource, rationale: row.rationale,
      expectedDisposition: row.expectedDisposition, ...(row.evidenceAvailability === undefined ? {} : { evidenceAvailability: row.evidenceAvailability }),
      ...(row.expectedHandoffTarget === undefined ? {} : { expectedHandoffTarget: row.expectedHandoffTarget }) }
  }
  const issues = state.expectationIssues.map(issue => {
    if (!object(issue) || typeof issue.id !== 'string' || typeof issue.message !== 'string') return invalid()
    // Historical approvals remain visible; pending tokens are never restored.
    let resolved: RunState['expectationIssues'][number]['resolved']
    if (issue.resolved !== undefined) {
      if (!object(issue.resolved) || typeof issue.resolved.approvedAt !== 'string' || typeof issue.resolved.rationale !== 'string') return invalid()
      resolved = { approvedAt: issue.resolved.approvedAt, rationale: issue.resolved.rationale, replacement: readCase(issue.resolved.replacement) }
    }
    return { id: issue.id, message: issue.message, original: readCase(issue.original), ...(resolved ? { resolved } : {}) }
  })
  const sources: SavedSource[] = value.sources.map(source => {
    if (!object(source) || typeof source.session !== 'string' || !['search', 'page'].includes(String(source.kind))
      || typeof source.requestedAt !== 'string' || (source.text !== null && typeof source.text !== 'string')
      || (source.failure !== null && typeof source.failure !== 'string') || !strings(source.quotes) || !object(source.request)
      || typeof source.request.source !== 'string' || typeof source.request.dialect !== 'string'
      || (source.request.url !== undefined && typeof source.request.url !== 'string')
      || (source.request.query !== undefined && typeof source.request.query !== 'string')) return invalid()
    return { session: source.session, kind: source.kind as SavedSource['kind'], requestedAt: source.requestedAt,
      text: source.text, failure: source.failure, quotes: source.quotes,
      request: { source: source.request.source, dialect: source.request.dialect,
        ...(source.request.url ? { url: source.request.url as string } : {}), ...(source.request.query ? { query: source.request.query as string } : {}) } }
  })
  return { state: { ...INITIAL_STATE, status: ['idle', 'running', 'complete', 'ready', 'needs-input', 'budget', 'stalled', 'stopped', 'failed'].includes(String(state.status)) ? state.status as RunState['status'] : 'idle', brief: state.brief, seedUrls: state.seedUrls, turns, candidates, responses: readResponseHistory(state.responses),
    probes: readProbes(state.probes), cases: state.cases.map(readCase), expectationIssues: issues, unknowns: state.unknowns,
    revisionsUsed: state.revisionsUsed as number, sessions: state.sessions }, sources }
}

export function restoreLedger(ledger: Ledger, sources: SavedSource[]): void {
  if (ledger.sources.length) throw new Error('Cannot replace an active source ledger')
  for (const source of sources) {
    ledger.openSession(source.session)
    const record = ledger.open(source.kind, source.request, new Date(source.requestedAt))
    if (source.text === null) { ledger.settle(record.id, { failure: source.failure ?? 'Interrupted before a response was saved.' }); continue }
    const parsed = parseJsonText(source.text)
    const result = memberOf(parsed, 'result'), receipt = memberOf(parsed, 'receipt'), salts = memberOf(parsed, 'salts')
    if (!result || !receipt) throw new Error('Saved source is missing its original result or receipt')
    const saltMap = Object.fromEntries(salts?.kind === 'object' ? salts.members.filter(member => member.value.kind === 'string').map(member => [member.name, member.value.kind === 'string' ? member.value.value : '']) : [])
    const dialect = source.request.dialect as ResearchDialect
    const document = source.kind === 'page' ? READ_DIALECTS[dialect]?.document(result) : undefined
    const hits = source.kind === 'search' ? SEARCH_DIALECTS[dialect]?.hits(result) : undefined
    if (source.kind === 'page' ? !document : !hits) throw new Error('The saved source dialect is no longer supported')
    ledger.settle(record.id, { response: { text: source.text, result, receipt, salts: saltMap }, document, hits })
    for (const quote of source.quotes) {
      if ('refused' in ledger.cite(record.id, quote)) throw new Error('A saved excerpt is not in its original source')
    }
  }
}
