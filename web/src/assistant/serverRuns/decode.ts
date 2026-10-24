import type { AssistantEvent } from '@judgment-pack/harness/contract'
import { readUserQuestion, validQuestionDraft, type UserQuestion } from '@judgment-pack/harness/questions'
import { recordDigest, sha256 } from '@judgment-pack/harness/records'
import { readExecution } from '@judgment-pack/harness/execution/state'
import { AIFailureError, failure } from '@judgment-pack/harness/failures'
import { parseJsonText } from '../../research/verify/canon'
import type { RunBinding, RunContinuation, RunDiscovery, RunEvent, RunOperation, RunProgress, RunReference, RunSnapshot } from './types'

export const MAX_RUN_BYTES = 16 * 1024 * 1024
export const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
export const exact = (v: Record<string, unknown>, keys: readonly string[]) => Object.keys(v).every(k => keys.includes(k))
export const text = (v: unknown, max: number): v is string => typeof v === 'string' && v.length > 0 && new TextEncoder().encode(v).length <= max && !/[\r\n\0\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v) && !/[\r\n\0]/.test(v)
export const digest = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v)
export const revision = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) > 0
export const runID = (v: unknown): v is string => typeof v === 'string' && /^arun_[a-f0-9]{32}$/.test(v)
export const ownerID = (v: unknown): v is string => typeof v === 'string' && /^[a-zA-Z0-9_-]{1,160}$/.test(v)
export const briefSubject = (v: unknown): v is string => text(v,1024) && (/^(pack|pack-draft|graph|graph-draft):.+$/.test(v) || /^case:.+:.+$/.test(v) || /^(job:job_|run:run_)[a-f0-9]{32}$/.test(v))
export const runOwner = (kind:unknown,id:unknown):boolean => kind === 'chat' ? ownerID(id) : kind === 'brief' && briefSubject(id)
export function invalid(): never { throw new AIFailureError(failure('invalid-response', 'desk', 'unknown')) }
export function parseRunJSON(raw: string): unknown {
  try { parseJsonText(raw); return JSON.parse(raw) as unknown } catch { return invalid() }
}
export function cursorParts(value: unknown): [number, number] {
  if (typeof value !== 'string' || !/^[1-9][0-9]*:(0|[1-9][0-9]*)$/.test(value)) return invalid()
  const [r, n] = value.split(':').map(Number)
  if (!revision(r) || !Number.isSafeInteger(n) || n > 1024) return invalid()
  return [r, n]
}
export const SEMANTIC_EVENT_TYPES = {
  approval_requested:true, steered:true, compacted:true, skill_loaded:true, skill_unloaded:true,
  retry:true, response_recovery:true, delegation_requested:true, delegated_task:true, user_question:true,
  reasoning:true, message:true, tool_call:true, tool_result:true, guardrail:true, thinking_unavailable:true,
  critic_decided:true, critique:true, proposal:true, error:true, end:true, policy_resolved:true,
  connection_switched:true, usage:true, recorded:true, memory_proposed:true, memory_recalled:true,
  edit_applied:true, edit_refused:true, export_written:true, output_guarded:true
} satisfies Record<Exclude<AssistantEvent['type'], 'message_progress' | 'execution_checkpoint' | 'budget_paused'>, true>
export function decodeEvent(value: unknown): RunEvent {
  if (!object(value) || !exact(value, ['id', 'event']) || !text(value.id, 128) || !object(value.event) || !text(value.event.type, 64) || !Object.hasOwn(SEMANTIC_EVENT_TYPES,value.event.type)) return invalid()
  return value as unknown as RunEvent
}
function operations(value: unknown): readonly RunOperation[] {
  if (value === null) return [] // Go nil slices are JSON null, never absent.
  if (!Array.isArray(value) || value.length > 512) return invalid()
  const ids = new Set<string>(), calls = new Set<string>()
  for (const op of value) {
    if (!object(op) || !exact(op, ['id','callId','kind','name','intent','state','result']) || !text(op.id,128) || ids.has(op.id) || !text(op.callId,256) || calls.has(op.callId) || !text(op.name,200) || typeof op.kind !== 'string' || !['model','runtime','host','action','callback'].includes(op.kind) || typeof op.state !== 'string' || !['not-started','dispatched','completed','refused','uncertain'].includes(op.state) || typeof op.intent !== 'string' || !base64(op.intent, false) || !(op.result === null || typeof op.result === 'string' && base64(op.result, true)) || (['completed','refused'].includes(op.state)) !== (op.result !== null)) return invalid()
    ids.add(op.id); calls.add(op.callId)
  }
  return value as RunOperation[]
}
function base64(value: string, empty: boolean): boolean { return (empty || value.length > 0) && /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value) }
export const questionDigest = (question: UserQuestion): string => recordDigest(question).slice(7)
export function pendingQuestion(snapshot: Pick<RunSnapshot,'events'|'continuations'>): {id:string; question:UserQuestion; digest:string} | undefined {
 const answered=new Set(snapshot.continuations?.flatMap(r=>r.kind==='answer'?[r.answer.questionId]:[])??[])
 const seen=new Set<string>(); let pending
 for(const event of snapshot.events) if(event.event.type==='user_question') {
  const id=event.event.id
  if(typeof id!=='string'||!/^[A-Za-z0-9_-]{1,100}$/.test(id)||seen.has(id))return invalid()
  seen.add(id)
  let question: UserQuestion;try{question=readUserQuestion(event.event.question)}catch{return invalid()}
  if(!answered.has(id)){if(pending)return invalid();pending={id,question,digest:questionDigest(question)}}
 }
 return pending
}
function continuations(value:unknown, events:readonly RunEvent[],checkpoint:RunSnapshot['checkpoint']): readonly RunContinuation[] | undefined {
 if(value===undefined)return undefined
 if(!Array.isArray(value)||!value.length||value.length>512)return invalid()
 const ids=new Set<string>(),questions=new Set<string>()
 for(const r of value){
  if(!object(r)||!text(r.id,128)||ids.has(r.id)||!digest(r.digest))return invalid();ids.add(r.id)
  if(r.kind==='answer'){
   const a=r.answer
   if(!exact(r,['id','digest','kind','answer'])||!object(a)||!exact(a,['questionId','questionDigest','selected','text','at'])||typeof a.questionId!=='string'||questions.has(a.questionId)||!digest(a.questionDigest)||typeof a.at!=='string'||!Number.isFinite(Date.parse(a.at)))return invalid()
   const matching=events.filter(e=>e.event.type==='user_question'&&e.event.id===a.questionId)
   if(matching.length!==1)return invalid()
   let q:UserQuestion;try{q=readUserQuestion(matching[0].event.question)}catch{return invalid()}
   if(questionDigest(q)!==a.questionDigest||!validQuestionDraft(q,{selected:a.selected,text:a.text})||!(a.selected as string[]).length&&!String(a.text).trim())return invalid()
   questions.add(a.questionId)
  }else if(r.kind==='extend-budget'){
   const g=r.grant
   if(!exact(r,['id','digest','kind','grant'])||!object(g)||!exact(g,['from','to','at'])||!checkpoint||checkpoint.unit!=='model-requests'||!checkpoint.grants.some(saved=>saved.from===g.from&&saved.to===g.to&&saved.at===g.at))return invalid()
  }else return invalid()
 }
 return value as RunContinuation[]
}
export function decodeSnapshot(value: unknown, expected: RunBinding & { readonly id?: string }): RunSnapshot {
  if (!object(value) || !exact(value,['id','revision','owner','requestId','state','historical','checkpoint','operations','events','cursor','chatProjection','projectionIssue','continuations','displayPreview']) || !runID(value.id) || expected.id !== undefined && value.id !== expected.id || !revision(value.revision) || !object(value.owner) || !exact(value.owner,['kind','id','revision']) || !runOwner(value.owner.kind,value.owner.id) || !digest(value.owner.revision) || value.owner.kind !== expected.owner.kind || value.owner.id !== expected.owner.id || value.owner.revision !== expected.owner.revision || !text(value.requestId,128) || value.requestId !== expected.requestId || typeof value.state !== 'string' || !['admitted','running','awaiting-input','awaiting-client','paused-budget','stopped','interrupted','complete','failed'].includes(value.state) || value.historical !== undefined && typeof value.historical !== 'boolean') return invalid()
  const [r, count] = cursorParts(value.cursor)
  const events = value.events === null ? [] : Array.isArray(value.events) ? value.events.map(decodeEvent) : invalid()
  if (events.length > 1024 || new Set(events.map(e => e.id)).size !== events.length || r !== value.revision || count !== events.length) return invalid()
  let checkpoint
  try { checkpoint = value.checkpoint === undefined ? undefined : readExecution(value.checkpoint) } catch { return invalid() }
  if (value.checkpoint !== undefined && !checkpoint) return invalid()
  if (value.chatProjection !== undefined && (!object(value.chatProjection) || !exact(value.chatProjection,['responseId','digest','acknowledged']) || value.chatProjection.responseId !== `response-${sha256(value.id).slice(0,32)}` || !digest(value.chatProjection.digest) || typeof value.chatProjection.acknowledged !== 'boolean')) return invalid()
  if (value.projectionIssue !== undefined && (typeof value.projectionIssue !== 'string' || !['proposal-target','pending-question','saved-chat-validation','unsupported-unicode'].includes(value.projectionIssue))) return invalid()
  const receipts = continuations(value.continuations,events,checkpoint)
  pendingQuestion({events,continuations:receipts})
  if (value.displayPreview !== undefined) decodePreview(value.displayPreview)
  const ops = operations(value.operations)
  if (value.state === 'complete' && ops.some(op=>!['completed','refused'].includes(op.state))) return invalid()
  return {...value, ...(receipts ? {continuations:receipts} : {}), operations: ops, events, ...(checkpoint ? {checkpoint} : {})} as unknown as RunSnapshot
}
export function reference(snapshot: RunSnapshot): RunReference { return {id:snapshot.id, owner:{...snapshot.owner}, requestId:snapshot.requestId} }

/** Discovery retains each run's source revision; a current journal revision is not its authority. */
export function decodeDiscovery(value: unknown, expected: RunDiscovery): readonly RunSnapshot[] {
  if (!object(value) || !exact(value,['runs']) || !Array.isArray(value.runs) || value.runs.length > 128) return invalid()
  const runs = value.runs.map(run=>{
    if (!object(run) || !object(run.owner) || !digest(run.owner.revision)) return invalid()
    return decodeSnapshot(run,{owner:{kind:expected.owner.kind,id:expected.owner.id,revision:run.owner.revision},requestId:expected.requestId})
  })
  if (new Set(runs.map(run=>run.id)).size !== runs.length) return invalid()
  return runs
}

export const MAX_PROGRESS_BYTES = 64 * 1024
export function decodeProgress(value: unknown, expected: RunReference): RunProgress {
  if (!object(value) || !exact(value,['id','owner','requestId','epoch','sequence','cursor','text','taskId']) || value.id !== expected.id || !object(value.owner) || !exact(value.owner,['kind','id','revision']) || value.owner.kind !== expected.owner.kind || value.owner.id !== expected.owner.id || value.owner.revision !== expected.owner.revision || value.requestId !== expected.requestId || typeof value.epoch !== 'string' || !/^[a-f0-9]{32}$/.test(value.epoch) || !revision(value.sequence) || typeof value.text !== 'string' || new TextEncoder().encode(value.text).length > MAX_PROGRESS_BYTES || /\0|[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value.text) || value.taskId !== undefined && !text(value.taskId,128)) return invalid()
  cursorParts(value.cursor)
  return value as unknown as RunProgress
}

export function decodePreview(value: unknown): void {
 if(!Array.isArray(value)||!value.length||value.length>17||new TextEncoder().encode(JSON.stringify(value)).length>1024*1024)return invalid()
 const keys=new Set<string>()
 for(const p of value){
  if(!object(p)||!exact(p,['text','taskId'])||typeof p.text!=='string'||!p.text||new TextEncoder().encode(p.text).length>MAX_PROGRESS_BYTES||/\0|[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(p.text)||p.taskId!==undefined&&!text(p.taskId,128))return invalid()
  const key=p.taskId??'';if(keys.has(key as string))return invalid();keys.add(key as string)
 }
}
