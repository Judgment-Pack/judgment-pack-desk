import { externalControlMessage } from '../../search/step'
import type { AssistantEvent } from '@judgment-pack/harness/contract'
import { readFailure } from '@judgment-pack/harness/failures'
import { sha256 } from '@judgment-pack/harness/records'
import { readUserQuestion } from '@judgment-pack/harness/questions'
import { summarizeWork } from '../../chat/responseHistory'
import type { RunState, Turn } from '../../research/run'
import { RunRequestError } from './client'
import type { RunSnapshot } from './types'
/** Display-only projection of authenticated canonical observations. Never saved as source. */
export function presentRun(base: RunState, snapshot: RunSnapshot, streaming='', taskProgress?:Record<string,string>):RunState {
 const running=['admitted','running'].includes(snapshot.state), responseId=`response-${sha256(snapshot.id).slice(0,32)}`
 const at=base.turns.find(t=>t.id===snapshot.requestId)?.at??new Date(0).toISOString()
 // A separate authenticated mapping affects only this disposable display.
 // Never rename or add canonical events in the retained snapshot.
 const events=snapshot.events.map(({event})=>{
  if((event.type==='tool_call'||event.type==='tool_result')&&typeof event.callId==='string'){
   const row=snapshot.externalCalls?.find(row=>row.callId===event.callId&&row.wireName===event.name)
   if(row)return {...event,name:row.ownTool}
  }
  return event
 }) as AssistantEvent[]
 const turns:Turn[]=[...base.turns],questions=[...(base.questions??[])],candidates=[...base.candidates]
 let messageId:string|undefined,failure=base.failure
 const history=snapshot.answerHistory??[]
 const displayEvents=[...history.map(h=>({id:`history-${h.parentRunId}`,event:{...h.question,id:`retained-question-${sha256(h.parentRunId+'\0'+h.question.id).slice(0,32)}`}})),...snapshot.events]
 for(const held of displayEvents) {
  const e=held.event
  if(e.type==='message'&&typeof e.text==='string') {
   messageId=`${responseId}-message-${sha256(held.id).slice(0,16)}`
   turns.push({id:messageId,role:'assistant',kind:'message',text:e.text,at,...(!running&&snapshot.state!=='complete'?{interrupted:true}:{})})
  } else if(e.type==='user_question'&&typeof e.id==='string') {
   const retained=history.find(h=>`retained-question-${sha256(h.parentRunId+'\0'+h.question.id).slice(0,32)}`===e.id),prompt=readUserQuestion(e.question),answer=retained?{id:`history-action-${sha256(retained.parentRunId+'\0'+retained.actionId).slice(0,32)}`,kind:'answer' as const,answer:retained.answer}:snapshot.continuations?.find(r=>r.kind==='answer'&&r.answer.questionId===e.id)
   const draft=answer?.kind==='answer'?{selected:[...answer.answer.selected],text:answer.answer.text}:{selected:[],text:''}
   turns.push({id:e.id,role:'assistant',kind:'question',text:prompt.question,at})
   const answerId=answer?`answer-${sha256(snapshot.id+'\0'+answer.id).slice(0,32)}`:undefined
   if(answer?.kind==='answer')turns.push({id:answerId,role:'user',kind:'message',text:[...prompt.options.filter(o=>draft.selected.includes(o.id)).map(o=>o.label),draft.text].filter(Boolean).join('\n'),at:answer.answer.at})
   questions.push({id:e.id,prompt,draft,...(answer?.kind==='answer'?{resolution:{kind:'answered' as const,at:answer.answer.at,turnId:answerId!,continued:true}}:{})})
  } else if(e.type==='error')failure=readFailure(e.failure)
  else if(e.type==='proposal'&&e.document&&typeof e.document==='object'&&!base.turns.some(t=>t.id===snapshot.requestId&&t.creation&&['establish-cases','propose-correction'].includes(t.creation.kind))) {
   const text=JSON.stringify(e.document)
   candidates.push({text,document:e.document,digest:sha256(text),revision:candidates.length+1,producedBy:'conversation',responseId})
  }
 }
 const childPreviews={...Object.fromEntries((snapshot.displayPreview??[]).flatMap(p=>p.taskId?[[p.taskId,p.text]]:[])),...taskProgress}
 const work=summarizeWork(events,running,{execution:snapshot.checkpoint,progress:childPreviews})
 for(const result of snapshot.approvalResults??[]){
  const status=result.isError?'failed' as const:'complete' as const
  const row=work.items.find(row=>row.id===result.callId&&row.name===result.name)
  if(row){row.approvalResult=structuredClone(result.result);row.status=status}
  else work.items.push({id:result.callId,name:result.name,status,approvalResult:structuredClone(result.result)})
 }
 for(const call of snapshot.externalCalls??[]){
  const row=work.items.find(row=>row.id===call.callId&&row.name===call.ownTool)
  if(!row)continue
  if(call.control){
   row.status='failed';row.externalControl=structuredClone(call.control)
   if(call.control.kind==='research-choice'&&row.name!=='read_link')row.researchSuggestion=true
   if(call.control.query)row.search={query:call.control.query,...(call.control.provider?{provider:call.control.provider}:{})}
   if(call.control.kind==='setup-required'&&row.name==='search_sources')row.failure='setup-required'
   work.notices.push(externalControlMessage(call.control))
  }else if(call.outcome==='unknown'){
   row.status='failed'
   work.notices.push('External source outcome is unknown. Review retained work before starting another request.')
  }
 }
 for(const row of work.items)if(row.task&&childPreviews[row.id])row.progress=childPreviews[row.id]
 return {...base,phase:'conversation',status:running?'running':snapshot.state==='paused-budget'?'budget':snapshot.state==='awaiting-input'||snapshot.state==='awaiting-client'?'needs-input':snapshot.state==='complete'?'complete':snapshot.state==='failed'?'failed':'stopped',
  detail:snapshot.historical?'Historical response. Nothing restarted automatically.':snapshot.state==='awaiting-client'?'Waiting for explicit approval.':snapshot.projectionIssue?'The response is retained on the server. Its saved chat projection needs recovery.':'',
  turns,questions,candidates,events:snapshot.events.map(e=>e.event) as AssistantEvent[],execution:snapshot.checkpoint,failure,streaming:streaming||snapshot.displayPreview?.find(p=>!p.taskId)?.text,streamingId:responseId,taskProgress:childPreviews,
  responses:[...(base.responses??[]),{id:responseId,...(messageId?{messageId}:{afterTurnId:snapshot.requestId}),documents:[],websites:[],sourceIds:[],work}]}
}

/** Latest completed receipt is display evidence, including failure/mismatch; never a Save grant. */
export function withPackChecks(state:RunState,checks:RunSnapshot['packChecks']):RunState {
 if(!checks)return state
 for(const check of checks)if(!state.candidates.some(c=>c.digest===check.documentDigest&&c.packReview))throw new RunRequestError('uncertain',undefined,'invalid-response')
 return {...state,candidates:state.candidates.map(c=>{const check=checks.find(check=>check.documentDigest===c.digest);return check?{...c,packReview:{...c.packReview!,check}}:c})}
}
