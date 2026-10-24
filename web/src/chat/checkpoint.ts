import { copyWireValue, retainWireValue } from './wire'
import {readPackBasis,readPackReview} from './packProposal'
import { readCreationAction } from '../assistant/serverRuns/creation'
import {readFailure} from '@judgment-pack/harness/failures'
import {readExecution} from '@judgment-pack/harness/execution/state'
import { readSavedQuestions } from '@judgment-pack/harness/questions'
import { validAIConnectionId } from '../assistant/aiConnections'
import { readAttachments, readMessageId, readResponseHistory } from './responseHistory'
import { readPreviousCheck, readProbes } from '../research/runtimeProbes'
import type { RunState, Turn } from '../research/run'
import { INITIAL_STATE } from '../research/initialState'
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
    state: { ...state, checkRecovery:undefined, checkIssue:undefined, streaming: undefined, streamingId: undefined, taskProgress: undefined, events: [], verdicts: {}, registries: {}, citations: [], tracing: undefined, tracedBasis: undefined,
      candidates: state.candidates.map(({ check, ...candidate }) => ({ ...candidate, ...(check ? { previousCheck: check } : {}) })),
      expectationIssues: state.expectationIssues.map(original => { const { proposal: _proposal, ...issue } = original; return copyWireValue(original,issue) }) },
    sources: sources.map(source => ({ session: source.session, kind: source.kind, request: source.request,
      requestedAt: source.requestedAt, text: source.response?.text ?? null,
      failure: source.failure, quotes: source.excerpts.map(excerpt => excerpt.text) }))
  }
}
const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every(item => typeof item === 'string')
function invalid(): never { throw new Error('Saved chat data is not supported. History was left untouched.') }

/** Validate display data before it reaches React; all trust is reset by restore. */
export function decodeCheckpoint(value: unknown,raw?:string): Checkpoint {
  if (!object(value) || !object(value.state) || !Array.isArray(value.sources)) return invalid()
  const state = value.state
  if (typeof state.brief !== 'string' || !strings(state.seedUrls) || !strings(state.unknowns) || !strings(state.sessions)
    || !Array.isArray(state.turns) || !Array.isArray(state.candidates) || !Array.isArray(state.cases)
    || !Array.isArray(state.expectationIssues) || !Number.isInteger(state.revisionsUsed) || (state.revisionsUsed as number) < 0) return invalid()
  if (state.inheritedStatements !== undefined && (!Array.isArray(state.inheritedStatements) || state.inheritedStatements.length > 256 ||
    state.inheritedStatements.some(row => !Array.isArray(row) || row.length !== 2 || !strings(row)) ||
    new Set(state.inheritedStatements.map(row => row[0])).size !== state.inheritedStatements.length)) return invalid()
  const turns: Turn[] = state.turns.map((turn, index) => {
    if (!object(turn) || (turn.role !== 'user' && turn.role !== 'assistant') || typeof turn.text !== 'string'
      || (turn.statement !== undefined && typeof turn.statement !== 'string') || (turn.input !== undefined && typeof turn.input !== 'string') || (turn.interrupted !== undefined && typeof turn.interrupted !== 'boolean') || typeof turn.at !== 'string' || !['brief', 'message', 'unknowns', 'note', 'question'].includes(String(turn.kind))) return invalid()
    if(turn.researchChoice!==undefined && (turn.role!=='user' || !['ask','research','provided'].includes(String(turn.researchChoice))))return invalid()
	if(turn.creation!==undefined&&turn.role!=='user')return invalid()
    if (turn.testDesign !== undefined && (!object(turn.testDesign) || typeof turn.testDesign.owner !== 'string' || !turn.testDesign.owner || turn.testDesign.owner.length > 256 || (turn.testDesign.proposalId !== undefined && typeof turn.testDesign.proposalId !== 'string'))) return invalid()
    if(turn.target!==undefined&&(!object(turn.target)||!validAIConnectionId(turn.target.connectionId)||typeof turn.target.connectionName!=='string'||turn.target.connectionName.length>128||typeof turn.target.model!=='string'||!turn.target.model||turn.target.model.length>128))return invalid()
    return { ...(turn.creation===undefined?{}:{creation:readCreationAction(turn.creation)}), ...(readPackBasis(turn.packContext) ? {packContext:readPackBasis(turn.packContext)} : {}), ...(turn.testDesign ? {testDesign: turn.testDesign as Turn['testDesign']} : {}), ...(turn.researchChoice ? {researchChoice:turn.researchChoice as Turn['researchChoice']} : {}), ...(turn.target?{target:turn.target as unknown as NonNullable<Turn['target']>}:{}), id: readMessageId(turn.id) ?? `legacy-turn-${index}`, ...(turn.attachments !== undefined ? {attachments: readAttachments(turn.attachments, 4)} : {}), role: turn.role, text: turn.text, at: turn.at, kind: turn.kind as Turn['kind'], ...(typeof turn.statement === 'string' ? { statement: turn.statement } : {}), ...(typeof turn.input === 'string' ? { input: turn.input } : {}), ...(turn.interrupted === true ? { interrupted: true } : {}) }
  })
  if (new Set(turns.map(turn => turn.id)).size !== turns.length) return invalid()
  const questions = readSavedQuestions(state.questions)
  if (questions.some(q => !turns.some(t => t.id === q.id && t.role === 'assistant' && t.kind === 'question') || q.resolution && !turns.some(t => t.id === q.resolution!.turnId && t.role === 'user'))
    || turns.some(t => t.kind === 'question' && !questions.some(q => q.id === t.id))) return invalid()
  const candidates = state.candidates.map((candidate, index) => {
    if (!object(candidate) || typeof candidate.text !== 'string') return invalid()
    // This parser also bounds nesting and rejects duplicate JSON members.
    parseJsonText(candidate.text)
    return { packReview:readPackReview(candidate.packReview), responseId: readMessageId(candidate.responseId), text: candidate.text, document: JSON.parse(candidate.text) as unknown, digest: '', revision: index + 1, producedBy: 'conversation' as const, previousCheck: readPreviousCheck(candidate.previousCheck) }
  })
  const readCase = (row: unknown): RunState['cases'][number] => {
    if (!object(row) || typeof row.id !== 'string' || !object(row.facts) || typeof row.expectationSource !== 'string' || typeof row.rationale !== 'string') return invalid()
    return { id: row.id, facts: row.facts, expectationSource: row.expectationSource, rationale: row.rationale,
      expectedDisposition: row.expectedDisposition, ...(row.evidenceAvailability === undefined ? {} : { evidenceAvailability: row.evidenceAvailability }),
      ...(row.expectedHandoffTarget === undefined ? {} : { expectedHandoffTarget: row.expectedHandoffTarget }) }
  }
	let heldProposal:RunState['heldProposal']=null
	if(state.heldProposal!==undefined&&state.heldProposal!==null){
		const held=state.heldProposal
		if(!object(held)||typeof held.candidateDigest!=='string'||!/^[a-f0-9]{64}$/.test(held.candidateDigest)||!Array.isArray(held.admitted)||held.admitted.length>256||!Array.isArray(held.dropped)||held.dropped.some(row=>!object(row)||typeof row.id!=='string'||typeof row.reason!=='string')||!strings(held.unknowns))return invalid()
		heldProposal={candidateDigest:held.candidateDigest,admitted:held.admitted.map(readCase),dropped:held.dropped as {id:string;reason:string}[],unknowns:held.unknowns}
		if(new Set(heldProposal.admitted.map(row=>row.id)).size!==heldProposal.admitted.length)return invalid()
	}
  const issues = state.expectationIssues.map(issue => {
    if (!object(issue) || typeof issue.id !== 'string' || typeof issue.message !== 'string') return invalid()
    // Historical approvals remain visible; pending tokens are never restored.
    let resolved: RunState['expectationIssues'][number]['resolved']
    if (issue.resolved !== undefined) {
      if (!object(issue.resolved) || typeof issue.resolved.approvedAt !== 'string' || typeof issue.resolved.rationale !== 'string') return invalid()
      resolved = { approvedAt: issue.resolved.approvedAt, rationale: issue.resolved.rationale, replacement: readCase(issue.resolved.replacement) }
    }
		let finding:RunState['expectationIssues'][number]['finding']
		if(issue.finding!==undefined){const f=issue.finding;if(!object(f)||f.status!=='invalid'||typeof f.message!=='string'||!f.message.trim()||f.admitted!==undefined&&typeof f.admitted!=='boolean'||f.unreachable!==undefined&&typeof f.unreachable!=='boolean')return invalid();finding=f as typeof finding}
    if(issue.position!==undefined&&(!Number.isSafeInteger(issue.position)||(issue.position as number)<0||(issue.position as number)>255))return invalid()
    return { ...(issue.position===undefined?{}:{position:issue.position as number}), id: issue.id, message: issue.message, original: readCase(issue.original), ...(finding?{finding}:{}), ...(resolved ? { resolved } : {}) }
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
  const wireState=raw&&memberOf(parseJsonText(raw),'state')
  const cases=state.cases.map(readCase)
  for(const [key,value] of [['cases',cases],['heldProposal',heldProposal],['expectationIssues',issues]] as const){
    const node=wireState&&memberOf(wireState,key)
    if(node)retainWireValue(value,node)
  }
  // Retain only the interrupted-conversation recovery hint. Artifact/check
  // phases are reconstructed by restore, never resumed from a checkpoint.
  return { state: { ...INITIAL_STATE, phase: state.phase==='conversation' && ['failed','running','stopped','budget'].includes(String(state.status)) ? 'conversation' : INITIAL_STATE.phase, detail: typeof state.detail === 'string' && state.detail.length <= 4096 ? state.detail : '', status: ['idle', 'running', 'complete', 'ready', 'needs-input', 'budget', 'stalled', 'stopped', 'failed'].includes(String(state.status)) ? state.status as RunState['status'] : 'idle', execution:readExecution(state.execution), failure:readFailure(state.failure), brief: state.brief, seedUrls: state.seedUrls, turns, questions, candidates, responses: readResponseHistory(state.responses),
    inheritedStatements: state.inheritedStatements as RunState['inheritedStatements'],
    checkRun: ['running','complete','failed','stopped'].includes(String(state.checkRun)) ? state.checkRun as RunState['checkRun'] : undefined,
    probes: readProbes(state.probes), cases, heldProposal, expectationIssues: issues, unknowns: state.unknowns,
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
