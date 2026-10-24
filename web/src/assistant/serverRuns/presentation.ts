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
 const events=snapshot.events.map(e=>e.event) as AssistantEvent[]
 const turns:Turn[]=[...base.turns],questions=[...(base.questions??[])],candidates=[...base.candidates]
 let messageId:string|undefined,failure=base.failure
 for(const held of snapshot.events) {
  const e=held.event
  if(e.type==='message'&&typeof e.text==='string') {
   messageId=`${responseId}-message-${sha256(held.id).slice(0,16)}`
   turns.push({id:messageId,role:'assistant',kind:'message',text:e.text,at,...(!running&&snapshot.state!=='complete'?{interrupted:true}:{})})
  } else if(e.type==='user_question'&&typeof e.id==='string') {
   const prompt=readUserQuestion(e.question),answer=snapshot.continuations?.find(r=>r.kind==='answer'&&r.answer.questionId===e.id)
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
 for(const row of work.items)if(row.task&&childPreviews[row.id])row.progress=childPreviews[row.id]
 return {...base,phase:'conversation',status:running?'running':snapshot.state==='paused-budget'?'budget':snapshot.state==='awaiting-input'||snapshot.state==='awaiting-client'?'needs-input':snapshot.state==='complete'?'complete':snapshot.state==='failed'?'failed':'stopped',
  detail:snapshot.historical?'Historical response. Nothing restarted automatically.':snapshot.state==='awaiting-client'?'Waiting for explicit approval.':snapshot.projectionIssue?'The response is retained on the server. Its saved chat projection needs recovery.':'',
  turns,questions,candidates,events,execution:snapshot.checkpoint,failure,streaming:streaming||snapshot.displayPreview?.find(p=>!p.taskId)?.text,streamingId:responseId,taskProgress:childPreviews,
  responses:[...(base.responses??[]),{id:responseId,...(messageId?{messageId}:{afterTurnId:snapshot.requestId}),documents:[],websites:[],sourceIds:[],work}]}
}

/** Latest completed receipt is display evidence, including failure/mismatch; never a Save grant. */
export function withPackChecks(state:RunState,checks:RunSnapshot['packChecks']):RunState {
 if(!checks)return state
 for(const check of checks)if(!state.candidates.some(c=>c.digest===check.documentDigest&&c.packReview))throw new RunRequestError('uncertain',undefined,'invalid-response')
 return {...state,candidates:state.candidates.map(c=>{const check=checks.find(check=>check.documentDigest===c.digest);return check?{...c,packReview:{...c.packReview!,check}}:c})}
}
