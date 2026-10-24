import { normalizeFailure } from '@judgment-pack/harness/failures'
import { sha256 } from '@judgment-pack/harness/records'
import { validQuestionDraft, type QuestionDraft } from '@judgment-pack/harness/questions'
import { INITIAL_STATE } from '../../research/initialState'
import type { Candidate, RunState, Turn } from '../../research/run'
import type { ResearchRunController } from '../../research/controller'
import type { Chat, ChatStore } from '../../chat/store'
import { readTests } from '../../packs/test-workspace/client'
import { emptySuite } from '../../packs/test-workspace/model'
import { jsonIdentity } from '../../research/checkCandidate'
import { RunRequestError, serverRunClient, type ServerRunClient } from './client'
import { sessionBearer } from '../../mcp/session'
import { reference, pendingQuestion, questionDigest } from './decode'
import { RunObservations } from './stream'
import { presentRun, withPackChecks } from './presentation'
import type { RunAction, RunSnapshot } from './types'

type ActionBody = RunAction extends infer A ? A extends RunAction ? Omit<A,'id'|'revision'> : never : never
interface Ports {
 project:string
 draftScope?:()=>Promise<string>
 client:()=>Promise<ServerRunClient>
 prepare:(state:RunState,requestId:string,admissionId:string,signal:AbortSignal)=>Promise<string>
 reconcile:(release:boolean)=>Promise<Chat|undefined>
 notSent?:(state:RunState,requestId:string)=>void
 pin:()=>void
 problem:(text:string)=>void
}
const restored=(chat:Chat):RunState=>{
 const saved=chat.checkpoint?.state??INITIAL_STATE
 return {...saved,phase:saved.turns.length?'conversation':'idle',status:!saved.turns.length?'idle':saved.status==='running'?'stopped':saved.status==='ready'?'complete':saved.status,
  candidates:saved.candidates.map(c=>({...c,digest:sha256(c.text),document:JSON.parse(c.text)})),events:[],streaming:undefined,taskProgress:undefined}
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
 private marker?:Chat['serverRequest']
 constructor(private readonly ownerId:string,chat:Chat,private readonly ports:Ports) {this.base=restored(chat);this.state=this.base;this.marker=chat.serverRequest}
 subscribe=(listener:()=>void)=>{this.listeners.add(listener);return()=>{this.listeners.delete(listener)}}
 getSnapshot=()=>this.state
 private set(patch:Partial<RunState>) {this.state={...this.state,...patch};this.listeners.forEach(l=>l())}
 get running(){return this.busy||this.state.status==='running'}
 get canRetryResponse(){if(this.knownNotSent)return !this.ended&&!this.running&&!this.uncertain;return !this.ended&&!this.running&&!this.uncertain&&!!this.snapshot?.chatProjection?.acknowledged&&!this.snapshot.historical&&['failed','interrupted','stopped','complete'].includes(this.snapshot.state)}
 get needsReconnect(){return this.uncertain||!!this.snapshot&&!this.snapshot.historical&&!this.snapshot.chatProjection?.acknowledged&&!['admitted','running','awaiting-input','awaiting-client','paused-budget'].includes(this.snapshot.state)}
 get canExtendResponse(){return !this.running&&!this.snapshot?.historical&&this.snapshot?.state==='paused-budget'}
 disconnect=()=>{this.epoch++;this.scope?.abort();this.scope=undefined;this.busy=false}
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

 endSession=()=>{this.clearDrafts();this.disconnect();this.ended=true;this.client=undefined;this.snapshot=undefined;this.base=INITIAL_STATE;this.set({...INITIAL_STATE,status:'stopped',detail:'Sign in to read this response.'})}
 /** Safe discovery/read after mount or an explicit reconnect; never start here. */
 reconnect=async()=>{
  this.disconnect();const epoch=this.epoch;this.scope=new AbortController()
  if(!this.marker)return
  this.ports.pin()
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
  if(snapshot.admissionId!==this.marker?.admissionId||snapshot.owner.id!==this.ownerId||snapshot.requestId!==this.marker?.requestId)throw new RunRequestError('uncertain',undefined,'stale-state')
  this.snapshot=snapshot
  if(this.stopRequested&&!snapshot.historical&&['admitted','running','awaiting-input','awaiting-client','paused-budget'].includes(snapshot.state)){this.stopRequested=false;this.busy=false;await this.action({kind:'cancel'});return}
  this.observations=new RunObservations(reference(snapshot),snapshot)
  this.busy=false
  if(snapshot.chatProjection?.acknowledged) {
   const chat=await this.ports.reconcile(true)
   if(!this.active(epoch))return
   if(chat){this.clearDrafts();this.base=withPackChecks(restored(chat),snapshot.packChecks);this.set(this.base)}
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
    this.snapshot=observation.snapshot
    this.set(this.present(observation.snapshot,observations.answer,Object.fromEntries([...observations.children].map(([k,p])=>[k,p.text]))))
    if(observation.snapshot.chatProjection?.acknowledged) {
     const chat=await this.ports.reconcile(true)
     if(this.active(epoch)&&chat){this.clearDrafts();this.base=withPackChecks(restored(chat),observation.snapshot.packChecks);this.set(this.base)}
    }
   }else if(observation.kind==='progress'&&this.snapshot)this.set(this.present(this.snapshot,observations.answer,Object.fromEntries([...observations.children].map(([k,p])=>[k,p.text]))))
  }
 }
 start:ResearchRunController['start']=(prompt,_seeds,display,attachments,statement,choice,pack)=>{this.send(prompt,display,false,attachments,statement,choice,pack)}
 send:ResearchRunController['send']=(input,display=input,_retry=false,attachments=[],statement=display,choice,packContext)=>{
  if(this.ended||this.running||this.uncertain)return
  const turn:Turn={id:crypto.randomUUID(),role:'user',kind:'message',text:display,input,statement,attachments,at:new Date().toISOString(),...(choice?{researchChoice:choice}:{}),...(packContext?{packContext}:{})}
  this.base={...this.state,failure:undefined,detail:'',events:[],streaming:undefined,streamingId:undefined,taskProgress:undefined,turns:[...this.state.turns,turn]}
  this.marker={requestId:turn.id!,admissionId:turn.id!};this.snapshot=undefined;this.knownNotSent=false;this.stopRequested=false;this.busy=true;this.set({...this.base,phase:'conversation',status:'running',failure:undefined,detail:''})
  // The composer's synchronous selected controls/attachments update is part of this CAS.
  queueMicrotask(()=>{void this.begin(turn.id!)})
 }
 private async begin(requestId:string) {
  this.disconnect();this.busy=true;const epoch=this.epoch;this.scope=new AbortController()
  let postAttempted=false
  try {
   const revision=await this.ports.prepare(this.base,requestId,this.marker!.admissionId,this.scope.signal)
   if(!this.active(epoch))return
   if(this.stopRequested){this.stopRequested=false;this.ports.notSent?.(this.base,requestId);this.marker=undefined;this.knownNotSent=true;this.busy=false;this.set({status:'stopped'});return}
   const client=await this.ready()
   postAttempted=true
   try {await this.adopt(await client.start({admissionId:this.marker!.admissionId,purpose:'chat',ownerId:this.ownerId,revision,requestId},this.scope.signal),epoch)}
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
    if(notSent){this.ports.notSent?.(this.base,requestId);this.marker=undefined;this.knownNotSent=true}
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
 retryResponse=()=>{if(this.canRetryResponse){const original=[...this.state.turns].reverse().find(t=>this.knownNotSent?t.role==='user':t.id===this.snapshot?.requestId);if(original)this.send(original.input??original.text,original.text,false,original.attachments,original.statement,original.researchChoice,original.packContext)}}
 updateQuestionDraft=(id:string,draft:QuestionDraft)=>{
  const q=this.state.questions?.find(q=>q.id===id)
  if(this.ended||this.running||this.snapshot?.historical||!q||q.resolution||!validQuestionDraft(q.prompt,draft))return
  this.set({questions:this.state.questions!.map(q=>q.id===id?{...q,draft:structuredClone(draft)}:q)})
  try{const key=this.draftKey(),saved=this.drafts();if(key){saved[id]={digest:questionDigest(q.prompt),draft:structuredClone(draft)};if(Object.keys(saved).length<=32)sessionStorage.setItem(key,JSON.stringify(saved))}}catch{/* A blocked browser store does not alter the pinned chat. */}
 }
 prepareQuestionAnswer=(id:string,kind:'answered'|'help')=>{const q=this.state.questions?.find(q=>q.id===id);if(this.ended||this.running||this.snapshot?.historical||!q||q.resolution||!validQuestionDraft(q.prompt,q.draft)||kind==='answered'&&!q.draft.selected.length&&!q.draft.text.trim())return false;this.preparedAnswer={id,draft:kind==='help'?{selected:[],text:['Help me decide. Explain the options without choosing for me.',q.draft.text].filter(Boolean).join('\n')}:structuredClone(q.draft)};return true}
 continueQuestion=(id:string)=>{const q=this.snapshot&&pendingQuestion(this.snapshot),answer=this.preparedAnswer;if(!q||q.id!==id||answer?.id!==id)return false;this.preparedAnswer=undefined;void this.action({kind:'answer',questionId:q.id,questionDigest:q.digest,answer:answer.draft} as ActionBody);return true}
 appendConversation=(turns:Turn[])=>{if(this.running)throw Error('Finish the active response before adding messages.');this.base={...this.state,turns:[...this.state.turns,...turns]};this.set(this.base);return this.state}
 get pendingApproval(){const snapshot=this.snapshot,a=snapshot?.checkpoint?.approval,r=snapshot?.approvalReference;return !this.running&&!snapshot?.historical&&a?.state==='asked'&&r?{...r,name:a.name,args:a.args}:undefined}
 decideApproval=(approved:boolean)=>{const current=this.pendingApproval;if(current)void this.action({kind:'approval',callId:current.callId,argsDigest:current.argsDigest,approved})}
 recheck=()=>{void this.check()}
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
 return new SavedChatRun(chat.id,chat,{project:store.projectRoot,draftScope:async()=>sha256((await sessionBearer())+'\0'+store.projectRoot+'\0'+chat.id),notSent:(state,requestId)=>store.releaseUnsentServerRequest(chat.id,requestId,state),client:serverRunClient,prepare:(state,id,admission,signal)=>store.prepareServerRequest(chat.id,state,id,admission,signal),reconcile:release=>store.reconcileServer(chat.id,release),pin:()=>store.pinServerChat(chat.id),problem:text=>store.problem(text)})
}
