import { approvalShownDigest } from '@judgment-pack/harness/contract'
import { serverContextDocument } from '../serverContexts'
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { privateQuery } from '../../data/privateQuery'
import { withCreation, type CreationAction } from './creation'
import { normalizeFailure } from '@judgment-pack/harness/failures'
import { sha256 } from '@judgment-pack/harness/records'
import { validQuestionDraft, type QuestionDraft } from '@judgment-pack/harness/questions'
import { INITIAL_STATE } from '../../research/initialState'
import type { Candidate, RunState, Turn } from '../../research/run'
import type { ResearchRunController } from '../../research/controller'
import type { Chat, ChatAttachment, ChatStore } from '../../chat/store'
import type { PackProposalBasis } from '../../chat/packProposal'
import { readTests } from '../../packs/test-workspace/client'
import { emptySuite } from '../../packs/test-workspace/model'
import { jsonIdentity } from '../../research/checkCandidate'
import { RunRequestError, serverRunClient, type ServerRunClient } from './client'
import { sessionBearer } from '../../mcp/session'
import { reference, pendingQuestion, questionDigest } from './decode'
import { RunObservations } from './stream'
import { presentRun, withPackChecks } from './presentation'
import type { DialectReadRecoveryView, RunAction, RunSnapshot } from './types'

// These identities are canonical uint64 decimals, never JS numbers.
const recoveryDecimalOrder=(a:string,b:string)=>a.length===b.length?(a===b?0:a<b?-1:1):a.length<b.length?-1:1
const newestRecovery=(old:DialectReadRecoveryView|undefined,next:DialectReadRecoveryView):DialectReadRecoveryView=>old?.runId===next.runId&&(recoveryDecimalOrder(next.recoveryRevision,old.recoveryRevision)<0||recoveryDecimalOrder(next.runRevision,old.runRevision)<0)?old:next

type ActionBody = RunAction extends infer A ? A extends RunAction ? Omit<A,'id'|'revision'> : never : never
interface Ports {
 project:string
 current?:()=>Chat|undefined
 draftScope?:()=>Promise<string>
 client:()=>Promise<ServerRunClient>
 prepare:(state:RunState,requestId:string,admissionId:string,signal:AbortSignal)=>Promise<string>
 reconcile:(release:boolean,projection?:{digest:string;responseId:string;requestId:string;admissionId:string})=>Promise<Chat|undefined>
 notSent?:(state:RunState,requestId:string)=>void
 pin:()=>void
 problem:(text:string)=>void
}
const restored=(chat:Chat):RunState=>{
 const saved=chat.checkpoint?.state??INITIAL_STATE
 return {...saved,restored:true,readiness:'',phase:saved.turns.length?'conversation':'idle',status:!saved.turns.length?'idle':saved.status==='running'?'stopped':saved.status==='ready'?'complete':saved.status,
  candidates:saved.candidates.map(c=>({...c,digest:sha256(c.text),document:JSON.parse(c.text)})),responses:saved.responses?.map(row=>({...row,work:{...row.work,items:row.work.items.map(item=>item.status==='working'?{...item,status:'interrupted' as const}:item)}})),events:[],streaming:undefined,taskProgress:undefined}
}
/** One saved owner. Lifecycle disconnect never sends a mutation. */
export class SavedChatRun implements ResearchRunController {
 readonly ownership='server' as const
 private state:RunState
 private base:RunState
 private listeners=new Set<()=>void>()
 private draftScope?:string
 private client?:ServerRunClient
 private snapshot?:RunSnapshot
 private recovery?:DialectReadRecoveryView
 private recoveryCache?:QueryClient
 private recoveryObserver?:QueryObserver<DialectReadRecoveryView,Error>
 private recoveryUnsubscribe?:()=>void
 private recoveryKey?:readonly string[]
 private recoveryReadGeneration=0
 private recoveryInspection=0
 private recoveryBusy=false
 private recoveryFailed=false
 private observations?:RunObservations
 private scope?:AbortController
 private epoch=0
 private ended=false
 private busy=false
 private knownNotSent=false
 private uncertain=false
 private cancelling=false
 private stopRequested=false
 private preparedAnswer?:{id:string;draft:QuestionDraft}
 private pendingCreation?:CreationAction
 private creationContext?:string
 // A terminal acknowledgement does not prove that the saved chat was reconciled.
 private unreconciled=false
 private marker?:Chat['serverRequest']
 constructor(private readonly ownerId:string,chat:Chat,private readonly ports:Ports) {this.base=restored(chat);this.state=this.base;this.marker=chat.serverRequest;this.unreconciled=!!this.marker}
 subscribe=(listener:()=>void)=>{this.listeners.add(listener);return()=>{this.listeners.delete(listener)}}
 getSnapshot=()=>this.state
 private set(patch:Partial<RunState>) {this.state={...this.state,...patch};this.listeners.forEach(l=>l())}
 /** Presentation mirrors the canonical edit guard; it grants no execution authority. */
 get inputBlocked(){return this.ended||this.uncertain||this.unreconciled}
 get running(){return this.busy||this.state.status==='running'}
 get canRetryResponse(){if(this.unreconciled||this.snapshot?.approvalResults?.some(result=>!result.result.available))return false;if(this.knownNotSent)return !this.ended&&!this.running&&!this.uncertain;return !this.ended&&!this.running&&!this.uncertain&&!!this.snapshot?.chatProjection?.acknowledged&&!this.snapshot.historical&&['failed','interrupted','stopped','complete'].includes(this.snapshot.state)}
 get needsReconnect(){if(this.ended)return false;return this.uncertain||this.unreconciled&&(!this.snapshot||!!this.snapshot.chatProjection?.acknowledged)||!!this.snapshot&&!this.snapshot.historical&&!this.snapshot.chatProjection?.acknowledged&&!['admitted','running','awaiting-input','awaiting-client','paused-budget'].includes(this.snapshot.state)}
 get dialectReadRecovery(){return this.recovery}
 get dialectReadRecoveryBusy(){return this.recoveryBusy}
 get dialectReadRecoveryFailed(){return this.recoveryFailed}
 refreshDialectReadRecovery=async()=>{
  if(!this.recoveryObserver||!this.recoveryKey){await this.loadDialectReadRecovery(this.epoch);return}
  await this.recoveryCache!.invalidateQueries({queryKey:this.recoveryKey,exact:true},{cancelRefetch:true})
 }
 private retireRecovery(){
  this.recoveryReadGeneration++;this.recoveryInspection++
  const busy=this.recoveryBusy;this.recoveryBusy=false
  this.recoveryUnsubscribe?.();this.recoveryUnsubscribe=undefined
  this.recoveryObserver?.destroy();this.recoveryObserver=undefined
  if(this.recoveryKey)this.recoveryCache?.unmount()
  this.recoveryKey=undefined
  if(busy)this.set({})
 }
 private async loadDialectReadRecovery(epoch:number){
  const snapshot=this.snapshot
  if(!this.active(epoch)||this.ended||!snapshot||!snapshot.operations.some(op=>op.kind==='host'&&op.name==='read_dialect'))return
  if(!this.recoveryObserver){
   const cache=this.recoveryCache??=new QueryClient({defaultOptions:{queries:{gcTime:0}}})
   const key=this.recoveryKey=['assistant-dialect-read-recovery',this.ports.project,this.ownerId,snapshot.id]
   cache.mount()
   this.recoveryObserver=new QueryObserver<DialectReadRecoveryView,Error>(cache,{
    ...privateQuery,refetchInterval:false,queryKey:key,
    initialData:this.recovery?.runId===snapshot.id?this.recovery:undefined,
    queryFn:async({signal})=>{
     const generation=++this.recoveryReadGeneration,client=await this.ready()
     if(!this.active(epoch)||this.snapshot?.id!==snapshot.id||signal.aborted)throw new RunRequestError('not-sent',undefined,'permission')
     try{
      const view=await client.dialectReadRecovery(reference(snapshot),signal)
      if(!this.active(epoch)||this.snapshot?.id!==snapshot.id||signal.aborted)throw new RunRequestError('not-sent',undefined,'permission')
      // A newer POST or invalidation may have completed while this GET waited.
      if(generation!==this.recoveryReadGeneration&&this.recovery)return this.recovery
      return newestRecovery(this.recovery,view)
     }catch(cause){
      if(generation!==this.recoveryReadGeneration&&this.recovery)return this.recovery
      throw cause
     }
    },
   })
   this.recoveryUnsubscribe=this.recoveryObserver.subscribe(result=>{
    if(!this.active(epoch)||this.snapshot?.id!==snapshot.id)return
    if(result.data)this.recovery=newestRecovery(this.recovery,result.data)
    this.recoveryFailed=result.isError;this.set({})
   })
  }
  // Mount, explicit run snapshots and reconnect read this resource only. The
  // observer also owns private-query invalidation/focus/online refresh; no timer.
  await this.recoveryObserver.refetch({cancelRefetch:false})
 }
 inspectDialectRead=async(operationId:string)=>{
  const snapshot=this.snapshot,view=this.recovery,epoch=this.epoch,signal=this.scope?.signal
  if(this.ended||this.running||this.recoveryBusy||!snapshot||!view||view.runId!==snapshot.id||!view.reads.some(row=>row.operationId===operationId&&row.canInspect))return
  const inspection=++this.recoveryInspection
  this.recoveryBusy=true;this.recoveryFailed=false;this.set({})
  try{
   const client=await this.ready()
   if(!this.active(epoch)||inspection!==this.recoveryInspection||signal?.aborted)return
   const observed=await client.observeDialectRead(reference(snapshot),view,operationId,crypto.randomUUID(),signal)
   if(this.active(epoch)&&inspection===this.recoveryInspection&&this.snapshot?.id===snapshot.id){
    this.recoveryReadGeneration++
    this.recovery=newestRecovery(this.recovery,observed)
    if(this.recoveryKey){
     this.recoveryCache!.setQueryData<DialectReadRecoveryView>(this.recoveryKey,old=>newestRecovery(old,this.recovery!))
     // Mark stale for explicit/focus/reconnect refresh without replaying POST.
     void this.recoveryCache!.invalidateQueries({queryKey:this.recoveryKey,exact:true,refetchType:'none'})
    }
   }
  }catch{if(this.active(epoch)&&inspection===this.recoveryInspection)this.recoveryFailed=true}
  finally{if(this.active(epoch)&&inspection===this.recoveryInspection){this.recoveryBusy=false;this.set({})}}
 }
 get canExtendResponse(){return !this.snapshot?.checkpoint?.planState?.pending&&!this.running&&!this.snapshot?.historical&&this.snapshot?.state==='paused-budget'}
 disconnect=()=>{this.epoch++;this.scope?.abort();this.scope=undefined;this.busy=false;this.retireRecovery()}
 private active(epoch:number){return epoch===this.epoch&&!this.scope?.signal.aborted}
 private failed(cause:unknown){this.busy=false;this.set({status:'failed',failure:normalizeFailure(cause,'desk'),detail:'The response is retained. Reconnect to check its current state; nothing restarted automatically.'})}
 private async ready(){
  if(this.ended)throw new RunRequestError('not-sent',undefined,'permission')
  if(!this.client){this.client=await this.ports.client();this.draftScope=await this.ports.draftScope?.()}
  return this.client
 }
 private draftKey(){return this.draftScope&&this.marker?`jpack.chat-question-draft:${this.draftScope}:${this.marker.requestId}`:undefined}
 private drafts():Record<string,{digest:string;draft:QuestionDraft}> {
  try {const key=this.draftKey(),value=key?JSON.parse(sessionStorage.getItem(key)??'{}'):{};return value&&typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).length<=32?value:{}}catch{return {}}
 }
 private clearDrafts(){try{const key=this.draftKey();if(key)sessionStorage.removeItem(key)}catch{/* UI drafts remain usable when browser storage is unavailable. */}}
 private present(snapshot:RunSnapshot,streaming='',children?:Record<string,string>){
  const state=presentRun(this.base,snapshot,streaming,children),drafts=this.drafts()
  return {...state,questions:state.questions?.map(q=>{const saved=drafts[q.id];return !q.resolution&&saved?.digest===questionDigest(q.prompt)&&validQuestionDraft(q.prompt,saved.draft)?{...q,draft:saved.draft}:q})}
 }

 endSession=()=>{this.clearDrafts();this.disconnect();this.ended=true;this.client=undefined;this.snapshot=undefined;this.recovery=undefined;this.recoveryFailed=false;this.recoveryBusy=false;this.recoveryCache?.clear();this.base=INITIAL_STATE;this.set({...INITIAL_STATE,status:'stopped',detail:'Sign in to read this response.'})}
 /** Safe discovery/read after mount or an explicit reconnect; never start here. */
 reconnect=async()=>{
  this.disconnect();const epoch=this.epoch;this.scope=new AbortController()
  if(!this.marker)return
  const inputBlocked=this.inputBlocked;this.unreconciled=true;this.ports.pin();if(inputBlocked!==this.inputBlocked)this.set({})
  try {
   const client=await this.ready(),runs=await client.discover({owner:{kind:'chat',id:this.ownerId},requestId:this.marker.requestId},this.scope.signal)
   if(!this.active(epoch))return
   const matches=runs.filter(r=>r.admissionId===this.marker!.admissionId)
   if(matches.length!==1) {this.uncertain=true;throw new RunRequestError('uncertain',undefined,'stale-state')}
   this.uncertain=false;await this.adopt(matches[0],epoch)
  }catch(cause){if(this.active(epoch)){this.uncertain=true;this.failed(cause)}}
 }
 private async adopt(snapshot:RunSnapshot,epoch:number) {
  if(!this.active(epoch))return
  const seen=new Set<string>()
  for(let depth=0;depth<128;depth++){
   if(seen.has(snapshot.id))throw new RunRequestError('uncertain',undefined,'invalid-response')
   seen.add(snapshot.id)
   const links=snapshot.continuations?.filter(r=>r.kind==='answer'&&r.successorRunId)??[]
   if(links.length>1)throw new RunRequestError('uncertain',undefined,'invalid-response')
   const link=links[0]
   if(!link||link.kind!=='answer'||!link.successorRunId)break
   const child=await (await this.ready()).read({...reference(snapshot),id:link.successorRunId},this.scope?.signal)
   if(!this.active(epoch))return
   const origin=child.answerHistory?.at(-1)
   if(!origin||origin.parentRunId!==snapshot.id||origin.admissionId!==snapshot.admissionId||origin.actionId!==link.id||JSON.stringify(origin.answer)!==JSON.stringify(link.answer)||child.answerHistory!.length!==(snapshot.answerHistory?.length??0)+1||JSON.stringify(child.answerHistory!.slice(0,-1))!==JSON.stringify(snapshot.answerHistory??[]))throw new RunRequestError('uncertain',undefined,'invalid-response')
   snapshot=child
   if(depth===127)throw new RunRequestError('uncertain',undefined,'invalid-response')
  }
  if((snapshot.answerHistory?.[0]?.admissionId??snapshot.admissionId)!==this.marker?.admissionId||snapshot.owner.id!==this.ownerId||snapshot.requestId!==this.marker?.requestId)throw new RunRequestError('uncertain',undefined,'stale-state')
  const inputBlocked=this.inputBlocked;this.unreconciled=true;if(this.snapshot?.id!==snapshot.id){this.retireRecovery();this.recovery=undefined;this.recoveryFailed=false}this.snapshot=snapshot;if(inputBlocked!==this.inputBlocked)this.set({});if(!['admitted','running'].includes(snapshot.state))void this.loadDialectReadRecovery(epoch)
  if(this.stopRequested&&!snapshot.historical&&['admitted','running','awaiting-input','awaiting-client','paused-budget'].includes(snapshot.state)){this.stopRequested=false;this.busy=false;await this.action({kind:'cancel'});return}
  this.observations=new RunObservations(reference(snapshot),snapshot)
  this.busy=false
  if(snapshot.chatProjection?.acknowledged) {
   const chat=await this.ports.reconcile(true,{digest:snapshot.chatProjection!.digest,responseId:snapshot.chatProjection!.responseId,requestId:snapshot.requestId,admissionId:snapshot.answerHistory?.[0]?.admissionId??snapshot.admissionId})
   if(!this.active(epoch))return
   if(chat){this.clearDrafts();this.base=withCreation(withPackChecks(restored(chat),snapshot.packChecks),snapshot.creation,!snapshot.historical&&snapshot.state==='complete',!snapshot.historical);this.creationContext=this.context(chat);this.unreconciled=false;this.set(this.base)}
   return
  }
  this.set(this.present(snapshot))
  if(this.needsReconnect)this.set({detail:'The response is retained on the server. Reconnect to reconcile its saved chat before sending again.'})
  if(snapshot.historical||!['admitted','running'].includes(snapshot.state))return
  await this.observe(epoch)
 }
 private async observe(epoch:number) {
  const observations=this.observations!,client=await this.ready()
  for await(const observation of client.events(observations,this.scope!.signal)) {
   if(!this.active(epoch))return
   if(observation.kind==='snapshot') {
    this.unreconciled=true;this.snapshot=observation.snapshot
    if(!['admitted','running'].includes(observation.snapshot.state))void this.loadDialectReadRecovery(epoch)
    this.set(this.present(observation.snapshot,observations.answer,Object.fromEntries([...observations.children].map(([k,p])=>[k,p.text]))))
    if(observation.snapshot.chatProjection?.acknowledged) {
     const chat=await this.ports.reconcile(true,{digest:observation.snapshot.chatProjection!.digest,responseId:observation.snapshot.chatProjection!.responseId,requestId:observation.snapshot.requestId,admissionId:observation.snapshot.answerHistory?.[0]?.admissionId??observation.snapshot.admissionId})
     if(this.active(epoch)&&chat){this.clearDrafts();this.base=withCreation(withPackChecks(restored(chat),observation.snapshot.packChecks),observation.snapshot.creation,!observation.snapshot.historical&&observation.snapshot.state==='complete',!observation.snapshot.historical);this.creationContext=this.context(chat);this.unreconciled=false;this.set(this.base)}
    }
   }else if(observation.kind==='progress'&&this.snapshot)this.set(this.present(this.snapshot,observations.answer,Object.fromEntries([...observations.children].map(([k,p])=>[k,p.text]))))
  }
 }
 start=(prompt:string,_seeds:string[],display=prompt,attachments:ChatAttachment[]=[],statement=display,choice?:Turn['researchChoice'],pack?:PackProposalBasis)=>{this.send(prompt,display,false,attachments,statement,choice,pack)}
 send=(input:string,display=input,_retry=false,attachments:ChatAttachment[]=[],statement=display,choice?:Turn['researchChoice'],packContext?:PackProposalBasis)=>{
  if(this.ended||this.running||this.uncertain||this.unreconciled)return
  const creation=this.pendingCreation;this.pendingCreation=undefined
  const turn:Turn={...(creation?{creation}:{}),id:crypto.randomUUID(),role:'user',kind:'message',text:display,input,statement,attachments,at:new Date().toISOString(),...(choice?{researchChoice:choice}:{}),...(packContext?{packContext}:{})}
  this.base={...this.state,restored:true,readiness:'',failure:undefined,detail:'',events:[],streaming:undefined,streamingId:undefined,taskProgress:undefined,turns:[...this.state.turns,turn]}
  this.unreconciled=true;this.marker={requestId:turn.id!,admissionId:turn.id!};this.snapshot=undefined;this.knownNotSent=false;this.stopRequested=false;this.busy=true;this.set({...this.base,phase:'conversation',status:'running',failure:undefined,detail:''})
  // The composer's synchronous selected controls/attachments update is part of this CAS.
  queueMicrotask(()=>{void this.begin(turn.id!)})
 }
 private async begin(requestId:string) {
  this.disconnect();this.busy=true;const epoch=this.epoch;this.scope=new AbortController()
  let postAttempted=false
  try {
   const revision=await this.ports.prepare(this.base,requestId,this.marker!.admissionId,this.scope.signal)
   if(!this.active(epoch))return
   if(this.stopRequested){this.stopRequested=false;this.ports.notSent?.(this.base,requestId);this.marker=undefined;this.unreconciled=false;this.knownNotSent=true;this.busy=false;this.set({status:'stopped'});return}
   const client=await this.ready()
   const context=await serverContextDocument().selector(client)
   if(!this.active(epoch))return
   if(this.stopRequested){this.stopRequested=false;this.ports.notSent?.(this.base,requestId);this.marker=undefined;this.unreconciled=false;this.knownNotSent=true;this.busy=false;this.set({status:'stopped'});return}
   postAttempted=true
   try {await this.adopt(await client.start({admissionId:this.marker!.admissionId,purpose:'chat',ownerId:this.ownerId,revision,requestId,context},this.scope.signal),epoch)}
   catch(cause) {
    if(!this.active(epoch))return
    if(cause instanceof RunRequestError&&cause.delivery==='uncertain') {
     this.uncertain=true
     const runs=await client.discover({owner:{kind:'chat',id:this.ownerId},requestId},this.scope.signal)
     if(!this.active(epoch))return
     const match=runs.filter(r=>r.admissionId===this.marker!.admissionId&&r.owner.revision===revision)
     if(match.length!==1)throw cause
     this.uncertain=false;await this.adopt(match[0],epoch)
    }else throw cause
   }
  }catch(cause){if(this.active(epoch)){
    const notSent=!postAttempted||!this.snapshot&&cause instanceof RunRequestError&&(cause.delivery==='not-sent'||cause.delivery==='refused')
    if(notSent){this.ports.notSent?.(this.base,requestId);this.marker=undefined;this.unreconciled=false;this.knownNotSent=true}
    this.uncertain=!notSent;this.failed(cause)
  }}
 }
 private async action(body:ActionBody) {
  if(!this.snapshot||this.snapshot.historical||this.busy)return
  const held=this.snapshot;this.disconnect();const epoch=this.epoch;this.scope=new AbortController();this.busy=true;this.set({status:'running'})
  try {const client=await this.ready();const next=await client.action(held,{...body,id:crypto.randomUUID(),revision:held.revision} as RunAction,this.scope?.signal);if(this.active(epoch))await this.adopt(next,epoch)}
  catch(cause){if(this.active(epoch)){this.uncertain=true;this.failed(cause)}}
 }
 stop=()=>{if(!this.snapshot){if(this.busy)this.stopRequested=true;return}if(this.cancelling||this.snapshot.historical)return;this.cancelling=true;void this.action({kind:'cancel'}).finally(()=>{this.cancelling=false})}
 keepExecution=()=>{void this.action({kind:'abandon'})}
 prepareBudgetContinuation=()=>this.canExtendResponse
 continueExecution=()=>{if(this.canExtendResponse)void this.action({kind:'extend-budget'})}
 retryResponse=()=>{if(this.canRetryResponse){const original=[...this.state.turns].reverse().find(t=>this.knownNotSent?t.role==='user':t.id===this.snapshot?.requestId);if(original?.creation){const action=original.creation;if(action.kind==='approve-correction'){this.approveExpectationCorrection(action.issueId,action.token)}else{this.requestCreation(action,original.text)}}else if(original)this.send(original.input??original.text,original.text,false,original.attachments,original.statement,original.researchChoice,original.packContext)}}
 updateQuestionDraft=(id:string,draft:QuestionDraft)=>{
  const q=this.state.questions?.find(q=>q.id===id)
  if(this.ended||this.running||this.snapshot?.checkpoint?.planState?.pending||this.snapshot?.historical||!q||q.resolution||!validQuestionDraft(q.prompt,draft))return
  this.set({questions:this.state.questions!.map(q=>q.id===id?{...q,draft:structuredClone(draft)}:q)})
  try{const key=this.draftKey(),saved=this.drafts();if(key){saved[id]={digest:questionDigest(q.prompt),draft:structuredClone(draft)};if(Object.keys(saved).length<=32)sessionStorage.setItem(key,JSON.stringify(saved))}}catch{/* A blocked browser store does not alter the pinned chat. */}
 }
 prepareQuestionAnswer=(id:string,kind:'answered'|'help')=>{const q=this.state.questions?.find(q=>q.id===id);if(this.ended||this.running||this.snapshot?.checkpoint?.planState?.pending||this.snapshot?.historical||!q||q.resolution||!validQuestionDraft(q.prompt,q.draft)||kind==='answered'&&!q.draft.selected.length&&!q.draft.text.trim())return false;this.preparedAnswer={id,draft:kind==='help'?{selected:[],text:['Help me decide. Explain the options without choosing for me.',q.draft.text].filter(Boolean).join('\n')}:structuredClone(q.draft)};return true}
 continueQuestion=(id:string)=>{const q=this.snapshot&&pendingQuestion(this.snapshot),answer=this.preparedAnswer;if(this.snapshot?.checkpoint?.planState?.pending||!q||q.id!==id||answer?.id!==id)return false;this.preparedAnswer=undefined;void this.action({kind:'answer',questionId:q.id,questionDigest:q.digest,answer:answer.draft} as ActionBody);return true}
 appendConversation=(turns:Turn[])=>{
  if(this.running||this.unreconciled)throw Error('Finish the active response before adding messages.')
  const byId=new Map(this.state.turns.filter(turn=>turn.id).map(turn=>[turn.id!,turn])),added:Turn[]=[]
  for(const turn of turns){
   if(!turn.id)throw Error('A retained message needs a stable identity.')
   const previous=byId.get(turn.id)
   if(previous){if(previous.role!==turn.role||previous.text!==turn.text||previous.at!==turn.at)throw Error('A retained message conflicts with this session.')}
   else{byId.set(turn.id,turn);added.push(structuredClone(turn))}
  }
  if(added.length){this.base={...this.state,turns:[...this.state.turns,...added].sort((a,b)=>a.at.localeCompare(b.at)),...(this.state.phase==='idle'?{phase:'conversation' as const,status:'complete' as const}:{})};this.set(this.base)}
  return this.state
 }
 get pendingPlanDecision(){
  const s=this.snapshot,p=s?.checkpoint?.planState?.pending,r=s?.planDecisionReference
  return !this.ended&&!this.uncertain&&!this.running&&!s?.historical&&s?.state==='awaiting-input'&&s.checkpoint?.status==='paused'&&s.checkpoint.approval?.state!=='asked'&&!s.approvalReference&&p&&r&&p.callId===r.callId&&p.revision===r.planRevision&&s.checkpoint.planState?.revision===r.planRevision&&p.candidate.planState.revision===r.candidateRevision?{...r,explanation:p.explanation,current:s.checkpoint.plan,candidate:p.candidate.plan,changes:p.candidate.planState.history.at(-1)?.diff??[]}:undefined
 }
 decidePlan=(approved:boolean)=>{const current=this.pendingPlanDecision;if(current)void this.action({kind:'plan-decision',callId:current.callId,planRevision:current.planRevision,candidateRevision:current.candidateRevision,approved})}
 get pendingApproval(){const snapshot=this.snapshot,a=snapshot?.checkpoint?.approval,r=snapshot?.approvalReference;return !this.ended&&!this.uncertain&&!this.running&&!snapshot?.historical&&snapshot?.state==='awaiting-input'&&a?.state==='asked'&&r&&a.callId===r.callId?{...r,name:a.name,approval:a,...(a.runtime?{runtimeOwner:{projectLabel:a.runtime.owner.projectLabel,principalLabel:a.runtime.owner.principalLabel,scopeLabel:a.runtime.owner.scopeLabel}}:{})}:undefined}
 decideApproval=(approved:boolean,shown?:{callId:string;shownDigest:string})=>{const current=this.pendingApproval;if(!current||approved&&(!shown||shown.callId!==current.callId||shown.shownDigest!==current.argsDigest||shown.shownDigest!==approvalShownDigest(current.approval)))return;void this.action({kind:'approval',callId:current.callId,argsDigest:current.argsDigest,approved,...(approved?{shownDigest:shown!.shownDigest}:{})})}
 recheck=()=>{if(this.state.candidates.at(-1)?.packReview){void this.check();return}const candidate=this.state.candidates.at(-1);if(candidate)this.requestCreation({kind:'check',candidateDigest:candidate.digest},'Run current draft checks.')}
 private context(chat:Chat){return JSON.stringify([chat.draftGeneration,chat.mode,chat.interaction,chat.aiConnection,chat.model,chat.documents,chat.checkpoint?.sources,chat.checkpoint?.state.cases,chat.checkpoint?.state.expectationIssues,chat.checkpoint?.state.turns])}
 basisNow=()=>{const current=this.ports.current?.();return this.snapshot?.creation&&this.creationContext&&current&&this.context(current)===this.creationContext&&!this.running&&!this.unreconciled&&!this.state.restored?this.snapshot.creation.sourceDigest:''}
 private requestCreation(action:CreationAction,label:string){if(this.ended||this.running||this.uncertain||this.unreconciled||this.needsReconnect)return false;this.pendingCreation=action;this.send(label,label,false,[],label,action.kind==='research-choice'?action.choice:undefined);return true}
 prepareCreation=async()=>{const candidate=this.state.candidates.at(-1);if(!candidate||candidate.packReview||!this.requestCreation({kind:'check',candidateDigest:candidate.digest},'Run current draft checks before finalizing.'))return false;return await new Promise<boolean>(resolve=>{const done=()=>{if(this.running)return;unsubscribe();resolve(this.state.status==='ready'&&!!this.basisNow())},unsubscribe=this.subscribe(done);done()})}
 recoverDraft=(text:string)=>{const turn=this.state.turns.find(t=>t.role==='assistant'&&t.kind==='message'&&t.text===text);if(turn?.id&&!this.state.candidates.length)this.requestCreation({kind:'recover',messageId:turn.id,messageDigest:sha256(text)},'Recover the complete proposal from the selected assistant message.')}
 establishCases=()=>{const candidate=this.state.candidates.at(-1);if(candidate&&!this.state.cases.length&&!this.state.expectationIssues.some(i=>!i.resolved)&&!this.state.heldProposal)this.requestCreation({kind:'establish-cases',candidateDigest:candidate.digest},'Establish independent cases from my statements and verified sources.')}
 retryExpectationValidation=()=>{const candidate=this.state.candidates.at(-1);if(candidate&&this.state.heldProposal?.candidateDigest===candidate.digest)this.requestCreation({kind:'retry-validation',candidateDigest:candidate.digest},'Retry validation of the exact retained case proposal.')}
 proposeExpectationCorrection=(issueId:string)=>{const candidate=this.state.candidates.at(-1);if(candidate&&this.state.expectationIssues.some(i=>i.id===issueId&&!i.resolved))this.requestCreation({kind:'propose-correction',candidateDigest:candidate.digest,issueId},'Propose an independently grounded expectation correction for '+issueId+'.')}
 approveExpectationCorrection=(issueId:string,token:string)=>{const candidate=this.state.candidates.at(-1),issue=this.state.expectationIssues.find(i=>i.id===issueId&&!i.resolved);if(candidate&&issue?.proposal?.token===token)this.requestCreation({kind:'approve-correction',candidateDigest:candidate.digest,issueId,token},'Approve the visible expectation correction for '+issueId+' and retest the exact unchanged draft.')}
 chooseResearch=(choice:'research'|'provided')=>{this.requestCreation({kind:'research-choice',choice},'Continue my previous request using this explicit read-only research choice. Do not repeat prior actions, propose artifacts, write packs or graphs, or send messages.')}

 /** Read-only presentation eligibility; checkProposal retains its own admission guard. */
 canCheckProposal=(candidate:Candidate)=>!this.ended&&!!this.snapshot&&!this.snapshot.historical&&!!candidate.packReview&&this.state.candidates.some(c=>c.digest===candidate.digest)&&!this.running
 checkProposal=async(candidate:Candidate)=>{
  const held=this.snapshot
  if(this.ended||!held||held.historical||!candidate.packReview||!this.state.candidates.some(c=>c.digest===candidate.digest)||this.running)throw new RunRequestError('not-sent',undefined,'permission')
  const epoch=this.epoch,saved=await readTests();if(saved.project!==this.ports.project)throw new RunRequestError('not-sent',undefined,'permission');const suite=saved.content.suites[candidate.packReview.basis.packId]??emptySuite(),client=await this.ready()
  const check=await client.recheckPack(reference(held),{id:crypto.randomUUID(),documentDigest:candidate.digest,suiteDigest:sha256(jsonIdentity(suite.cases))},this.scope?.signal)
  if(!this.active(epoch))throw new RunRequestError('uncertain',undefined,'permission')
  this.set({candidates:this.state.candidates.map(c=>c.digest===candidate.digest?{...c,packReview:{...c.packReview!,check}}:c)})
  return check
 }
 private async check(){
  const candidate=this.state.candidates.at(-1)
  if(!candidate)return
  try {await this.checkProposal(candidate)}catch(cause){this.failed(cause)}
 }

}
export function savedChatRun(store:ChatStore,chat:Chat):SavedChatRun {
 return new SavedChatRun(chat.id,chat,{project:store.projectRoot,current:()=>{const s=store.getSnapshot();return [...s.chats,...s.drafts].find(c=>c.id===chat.id)},draftScope:async()=>sha256((await sessionBearer())+'\0'+store.projectRoot+'\0'+chat.id),notSent:(state,requestId)=>store.releaseUnsentServerRequest(chat.id,requestId,state),client:serverRunClient,prepare:(state,id,admission,signal)=>store.prepareServerRequest(chat.id,state,id,admission,signal),reconcile:(release,projection)=>store.reconcileServer(chat.id,release,projection),pin:()=>store.pinServerChat(chat.id),problem:text=>store.problem(text)})
}
