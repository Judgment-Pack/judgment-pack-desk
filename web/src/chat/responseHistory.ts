import {questionFailureMessage,type QuestionFailureCode} from '@judgment-pack/harness/questionFailure'
import { validDelegatedTask, type DelegatedTask } from '@judgment-pack/harness/delegationContract'
import {dialectRecipeFailureMessage,type DialectRecipeFailureCode} from '../dialects/proposalFailure'
import { validLinkFailure, type LinkFailureCode } from './linkFailure'
import { GRAPH_TOOLS, graphWork, graphWorkResult, validGraphWork, type GraphWork } from '../graphs/work'
import { linkStepFromCall, linkStepFromResult, validLinkStep, type LinkStep } from './linkStep'
import { validSearchStep, type SearchStep } from '../search/step'
import { searchFailureMessage, type SearchFailureCode } from '../search/failures'
import { validSearchReference, type SearchReference } from '../search/results'
import type { AssistantEvent } from '@judgment-pack/harness/engine'
import { criticRecord, validCriticRecord, type CriticRecord } from '../assistant/critic'
import { reportFromExecution, validRunReport, type RunReport } from '../assistant/report'
import type { ExecutionCheckpoint } from '@judgment-pack/harness/execution/state'
import type { RunTiming } from '../assistant/runMeter'
import type { ChatAttachment } from './store'
import { validWebURL } from '../documents/record'
import { validWebsiteReference, type WebsiteReference } from '../documents/website'

export interface WorkItem { /** A running delegated task's prose so far, streamed under its task by id; never saved. */ progress?: string; questionFailure?: QuestionFailureCode; questionRecovered?: true; task?: DelegatedTask; inputRecipeFailure?: DialectRecipeFailureCode; redirectPolicy?: 'google-grounding'; linkFailure?: LinkFailureCode; researchSuggestion?: true; graph?: GraphWork; id: string; name: string; status: 'working' | 'complete' | 'failed' | 'interrupted'; failure?: SearchFailureCode; search?: SearchStep; link?: LinkStep }
// Saved work keeps the tool name used at execution, including names before the Dialects rename.
const isRecipeProposalTool = (name: unknown) => name === 'propose_dialect_recipe' || name === 'propose_input_recipe'

/** Pair by invocation identity, never by a tool name (parallel calls may repeat). */
export function workItems(events: readonly AssistantEvent[], running: boolean, progress?: Readonly<Record<string, string>>): WorkItem[] {
  const rows: WorkItem[] = []
  const pending = new Map<string, WorkItem>()
  events.forEach((event, index) => {
    if (event.type === 'user_question') {
      for (const row of rows) if (row.name === 'ask_user' && row.status === 'failed') { row.status = 'complete'; row.questionRecovered = true }
    } else if (event.type === 'delegated_task') {
      const task=structuredClone(event.task)
      if(!running && (task.state==='running'||task.state==='queued')) {
        task.state='interrupted'
        task.problem='This task was interrupted. Nothing restarted automatically.'
        task.steps=task.steps.map(step=>step.state==='running'||step.state==='queued'?{...step,state:'interrupted'}:step)
      }
      const status=task.state==='queued'||task.state==='running'?'working':task.state
      const existing=rows.find(row=>row.id===task.id)
      // A running task's streamed prose (`message_progress` with its task id) goes under it, until it ends.
      const streamed=running&&(task.state==='running'||task.state==='queued')&&progress?.[task.id]?{progress:progress[task.id]!}:{}
      if(existing){Object.assign(existing,{task,status});delete existing.progress;Object.assign(existing,streamed)}
      else rows.push({id:task.id,name:'delegate_task',task,status,...streamed})
    } else if (event.type === 'tool_call' && event.callId) {
      const row: WorkItem = { id: event.callId, name: event.name, status: running ? 'working' : 'interrupted' }
      const query=(event.args as {query?:unknown}|undefined)?.query
      if(event.name==='search_sources' && validSearchStep({query})) {
       row.search={query:query as string}
       if(validSearchStep({...row.search,provider:event.presentation?.provider}))row.search.provider=event.presentation?.provider
      }
      if(event.name==='read_link')row.link=linkStepFromCall(event.args)
      if(GRAPH_TOOLS.has(event.name))row.graph=graphWork(event.args)
      rows.push(row); pending.set(event.callId, row)
    } else if (event.type === 'tool_result') {
      const row = event.callId ? pending.get(event.callId) : undefined
      const researchSuggestion = ['search_sources','explore_website'].includes(event.name) && !event.isError && (event.structured as {researchSuggestion?:unknown}|undefined)?.researchSuggestion === true
      const status = event.isError ? 'failed' : 'complete'
      const questionCode=event.name==='ask_user'&&event.isError?(event.structured as {questionFailure?:unknown}|undefined)?.questionFailure:undefined
      const questionFailure=questionFailureMessage(questionCode)?questionCode as QuestionFailureCode:undefined
      const proposedFailure=isRecipeProposalTool(event.name)&&event.isError?(event.structured as {inputRecipeFailure?:unknown}|undefined)?.inputRecipeFailure:undefined
      const inputRecipeFailure=dialectRecipeFailureMessage(proposedFailure)?proposedFailure as DialectRecipeFailureCode:undefined
      const code=event.name==='search_sources'&&event.isError?(event.structured as {searchFailure?:unknown}|undefined)?.searchFailure:undefined
      const failure=searchFailureMessage(code)?code as SearchFailureCode:undefined
      const detail=event.name==='search_sources'?(event.structured as {searchStep?:unknown}|undefined)?.searchStep:undefined
      const search=validSearchStep(detail)?structuredClone(detail):undefined
      const readFailure = event.name==='read_link'&&event.isError ? (event.structured as {linkFailure?:unknown}|undefined)?.linkFailure : undefined
      const linkFailure = event.name==='read_link'&&event.isError ? validLinkFailure(readFailure) ? readFailure : 'unknown' : undefined
      const redirectPolicy = linkFailure==='web-redirect-origin' && (event.structured as {redirectPolicy?:unknown}|undefined)?.redirectPolicy==='google-grounding' ? 'google-grounding' as const : undefined
      if(row&&row.name===event.name&&linkFailure){row.linkFailure=linkFailure;if(redirectPolicy)row.redirectPolicy=redirectPolicy}
      const link=event.name==='read_link'&&!event.isError?linkStepFromResult(event.structured):undefined
      if (row && researchSuggestion) row.researchSuggestion = true
      if (row && row.name === event.name && GRAPH_TOOLS.has(event.name)) row.graph = graphWorkResult(row.graph, event.text)
      if (row) { if(questionFailure&&row.name===event.name)row.questionFailure=questionFailure; if(inputRecipeFailure&&row.name===event.name)row.inputRecipeFailure=inputRecipeFailure; if(link && row.name==='read_link')row.link=link; row.status = status; if(failure)row.failure=failure; if(search && row.name==='search_sources')row.search=search; pending.delete(event.callId!) }
      else rows.push({ id: `result-${index}`, name: event.name, status, ...(questionFailure?{questionFailure}:{}), ...(inputRecipeFailure?{inputRecipeFailure}:{}), ...(researchSuggestion ? {researchSuggestion:true as const} : {}), ...(failure?{failure}:{}), ...(linkFailure?{linkFailure}:{}), ...(redirectPolicy?{redirectPolicy}:{}), ...(search?{search}:{}), ...(link?{link}:{}) })
    }
  })
  return rows
}


export interface WorkRecord { items: WorkItem[]; notices: string[]; critique?: string; critic?: CriticRecord; report?: RunReport }
export interface ResponseHistory {
  id: string
  /** The final prose message of this engine response, or its preceding turn if interrupted before prose. */
  messageId?: string
  afterTurnId?: string
  documents: ChatAttachment[]
  websites: WebsiteReference[]
  searches?: SearchReference[]
  sourceIds: string[]
  work: WorkRecord
}
/** What a summary is made of beyond the events: the model the run used, running tasks' streamed prose, and the execution checkpoint. */
export interface WorkContext { runModel?: string; progress?: Readonly<Record<string, string>>; execution?: ExecutionCheckpoint; timing?: RunTiming }
export function summarizeWork(events: readonly AssistantEvent[], running: boolean, context: WorkContext = {}): WorkRecord {
  const { runModel, progress, execution, timing } = context
  const report = reportFromExecution(execution, timing)
  const critique = [...events].reverse().find((event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique')
  const critic = critique && criticRecord(critique, runModel)
  return { items: workItems(events, running, progress), notices: [...new Set(events.flatMap(event =>
    event.type === 'guardrail' && event.action !== 'narrowed' ? [event.detail]
    : event.type === 'thinking_unavailable' ? [event.detail]
    : event.type === 'tool_result' && event.name === 'search_sources' && event.isError
      ? [searchFailureMessage((event.structured as {searchFailure?:unknown}|undefined)?.searchFailure)].filter((text):text is string=>!!text) : []))],
    critique: critique?.text, ...(critic ? { critic } : {}), ...(report ? { report } : {}) }
}
export const attachmentKey = (file: ChatAttachment) => `${file.id}/${file.document?.digest ?? ''}`
export function mergeReferences(previous: ChatAttachment[], next: ChatAttachment[]): ChatAttachment[] {
  const files = new Map(previous.map(file => [attachmentKey(file), file]))
  for (const file of next) {
    const before = files.get(attachmentKey(file))
    files.set(attachmentKey(file), before?.document && file.document ? {...file, document: {...file.document,
      pages: [...new Set([...before.document.pages, ...file.document.pages])].sort((a,b)=>a-b)}} : file)
  }
  return [...files.values()]
}
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const id = (v: unknown): v is string => typeof v === 'string' && /^[a-zA-Z0-9-]{1,100}$/.test(v)
function invalid(): never { throw new Error('Saved message details are invalid. History was left untouched.') }
export function readMessageId(value: unknown): string | undefined { if (value === undefined) return; return id(value) ? value : invalid() }
export function readAttachments(value: unknown, limit = 256): ChatAttachment[] {
  if (!Array.isArray(value) || value.length > limit) return invalid()
  const files = value.map(file => {
    if (!object(file) || typeof file.id !== 'string' || typeof file.name !== 'string' || typeof file.text !== 'string' || file.text.length > 200_000) return invalid()
    const ref = file.document
    if (ref !== undefined && (!object(ref) || ref.id !== file.id || !/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(file.id) || typeof ref.digest !== 'string' || !/^sha256:[a-f0-9]{64}$/.test(ref.digest) || !Array.isArray(ref.pages) || ref.pages.length > 500 || ref.pages.some(n => !Number.isSafeInteger(n) || n < 1) || new Set(ref.pages).size !== ref.pages.length || typeof ref.allowPartial !== 'boolean' || file.text !== '')) return invalid()
    if (file.link !== undefined && (!ref || !object(file.link) || !validWebURL(file.link.url) || file.link.resolvedUrl!==undefined&&!validWebURL(file.link.resolvedUrl) || file.link.anchor !== undefined && (typeof file.link.anchor !== 'string' || new TextEncoder().encode(file.link.anchor).length > 4096 || /[\x00-\x1f\x7f]/.test(file.link.anchor)))) return invalid()
    return structuredClone(file) as unknown as ChatAttachment
  })
  if (new Set(files.map(attachmentKey)).size !== files.length) return invalid()
  return files
}
export function readResponseHistory(value: unknown): ResponseHistory[] {
  if (value === undefined) return []
  if (!Array.isArray(value)) return invalid()
  const records = value.map(row => {
    if (!object(row) || row.searches!==undefined && (!Array.isArray(row.searches)||row.searches.length>64||!row.searches.every(validSearchReference)) || !id(row.id) || !Array.isArray(row.websites) || row.websites.length > 16 || !row.websites.every(validWebsiteReference)
      || !Array.isArray(row.sourceIds) || row.sourceIds.some(s => typeof s !== 'string' || !/^src-\d+$/.test(s)) || !object(row.work)
      || !Array.isArray(row.work.items) || !Array.isArray(row.work.notices) || row.work.notices.some(n => typeof n !== 'string')
      || row.work.critique !== undefined && typeof row.work.critique !== 'string'
      || row.work.critic !== undefined && !validCriticRecord(row.work.critic)
      || row.work.report !== undefined && !validRunReport(row.work.report)) return invalid()
    for (const item of row.work.items) if (!object(item) || item.questionRecovered!==undefined&&(item.questionRecovered!==true||item.name!=='ask_user'||item.status!=='complete') || item.questionFailure!==undefined&&(!questionFailureMessage(item.questionFailure)||item.name!=='ask_user'||(item.status!=='failed'&&!item.questionRecovered)) || item.task!==undefined&&(item.name!=='delegate_task'||!validDelegatedTask(item.task)||item.task.id!==item.id) || item.inputRecipeFailure!==undefined&&(!dialectRecipeFailureMessage(item.inputRecipeFailure)||!isRecipeProposalTool(item.name)||item.status!=='failed') || item.redirectPolicy!==undefined&&(item.redirectPolicy!=='google-grounding'||item.linkFailure!=='web-redirect-origin'||item.status!=='failed'||item.name!=='read_link') || item.linkFailure!==undefined&&(item.name!=='read_link'||item.status!=='failed'||!validLinkFailure(item.linkFailure)) || item.researchSuggestion!==undefined&&(item.researchSuggestion!==true||!['search_sources','explore_website'].includes(String(item.name))) || item.graph!==undefined&&(!GRAPH_TOOLS.has(String(item.name))||!validGraphWork(item.graph)) || typeof item.id !== 'string' || typeof item.name !== 'string' || !['working','complete','failed','interrupted'].includes(String(item.status)) || item.failure!==undefined&&(!searchFailureMessage(item.failure)||item.status!=='failed'||item.name!=='search_sources') || item.search!==undefined&&(item.name!=='search_sources'||!validSearchStep(item.search)) || item.link!==undefined&&(item.name!=='read_link'||!validLinkStep(item.link))) return invalid()
    return {id: row.id, messageId: readMessageId(row.messageId), afterTurnId: readMessageId(row.afterTurnId), documents: readAttachments(row.documents),
      ...(row.searches ? {searches:structuredClone(row.searches) as SearchReference[]} : {}), websites: structuredClone(row.websites), sourceIds: [...row.sourceIds], work: {items: (structuredClone(row.work.items) as WorkItem[]).map(({progress:_streamed,...kept}):WorkItem=>kept).map((item):WorkItem=>item.task && (item.task.state==='queued'||item.task.state==='running') ? {...item,status:'interrupted',task:{...item.task,state:'interrupted',problem:'This task was interrupted. Nothing restarted automatically.',steps:item.task.steps.map(step=>step.state==='running'?{...step,state:'interrupted'}:step)}} : item), notices: [...row.work.notices] as string[], ...(typeof row.work.critique === 'string' ? {critique: row.work.critique} : {}), ...(row.work.critic !== undefined ? {critic: structuredClone(row.work.critic) as CriticRecord} : {}), ...(row.work.report !== undefined ? {report: structuredClone(row.work.report) as RunReport} : {})}}
  })
  if (new Set(records.map(r => r.id)).size !== records.length) return invalid()
  return records
}
