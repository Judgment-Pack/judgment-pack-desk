import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {useEffect,useRef,useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {useChats} from '../chat/ChatProvider'
import {ChatAction,ChatActionLink} from '../chat/ChatActions'
import {ProposalDiffView} from '../assistant/ProposalDiffView'
import {diffProposal} from '../assistant/proposalDiff'
import {useAutomaticAction} from '../assistant/approvals/automatic'
import {actionApprovalAPI,actionApprovalKey,useActionApproval,type ActionApproval,type ActionGrant} from '../assistant/approvals/client'
import {Disclosure} from '../ui/Disclosure'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Notice} from '../ui/Notice'
import {msg} from '../i18n'
import {deskFetch} from '../files/client'
import {POLICIES_KEY,PolicyRequestError,readPolicy,recoverPolicyOperation,policyHref,type PolicyMutationReply} from './client'
import {policyFields,policyProposalBody,type PolicyProposal} from './chat'
import {policyEditorBlocked,usePolicyEditorBlocked} from './chatEditing'

// Pane and Related work may render the same proposal concurrently. One live
// admission prevents a refused duplicate from overwriting its completed state.
const activeSaves=new Set<string>()
async function saveProposal(chatId:string,p:PolicyProposal,grant?:ActionGrant):Promise<PolicyMutationReply>{
 let response:Response
 try{response=await deskFetch('/api/assistant-approvals/'+encodeURIComponent(chatId)+(grant?'/commit':'/apply'),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'policy-update',target:p.id,revision:p.revision,content:JSON.stringify(policyProposalBody(p)),...(grant?{id:grant.id}:{reviewed:true})})})}catch{throw new PolicyRequestError('delivery-unknown',true)}
 let body:unknown;try{body=await response.json()}catch{throw new PolicyRequestError('delivery-unknown',true)}
 if(!response.ok){const failure=body as {code?:string;outcome?:string};throw new PolicyRequestError(failure.code??'unavailable',failure.outcome==='unknown'||response.status>=500&&failure.outcome!=='not-committed')}
 return body as PolicyMutationReply
}
export function PolicyChatProposals({chatId,disabled=false,history=false}:{chatId:string;disabled?:boolean;history?:boolean}){
 const {chats,drafts}=useChats(),chat=[...chats,...drafts].find(c=>c.id===chatId)
 const proposals=chat?.policyProposals??[]
 return <>{(history?proposals:proposals.slice(-1)).map(p=><PolicyProposalCard key={p.id} chatId={chatId} proposal={p} disabled={disabled}/>)}</>
}
function PolicyProposalCard({chatId,proposal:p,disabled=false}:{chatId:string;proposal:PolicyProposal;disabled?:boolean}){
 const {store,bindings,chats,drafts}=useChats(),cache=useQueryClient(),status=useActionApproval(chatId),editorBlocked=usePolicyEditorBlocked(p.policyId)
 const [review,setReview]=useState<'manual'|'auto'|undefined>(),[busy,setBusy]=useState(false),[error,setError]=useState(''),flight=useRef(false)
 const mounted=useRef(true);useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
 useRegisteredChanges(false,'',{kind:'activity',busy})
 const chat=[...chats,...drafts].find(c=>c.id===chatId),state=bindings.get(chatId)?.state??chat?.checkpoint?.state,grant=status.isError?undefined:status.data?.grant
 const finished=!!state&&['complete','ready'].includes(state.status)&&!state.unknowns.length&&!state.questions?.some(q=>!q.resolution)
 const uncertain=p.save?.state==='pending'||p.save?.state==='uncertain',complete=p.save?.state==='complete'
 const blocked=disabled||busy||editorBlocked||!finished||chat?.interaction==='plan'||p.check?.status!=='passed'||!!store?.running
 const update=(patch:Partial<PolicyProposal>)=>{if(!store)return;const current=[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===chatId);if(current)store.update(chatId,{policyProposals:current.policyProposals?.map(item=>item.id===p.id?{...item,...patch}:item)})}
 const checkpoint=()=>store?.checkpoint(new AbortController().signal)??Promise.reject(Error(msg('This conversation is no longer available.')))
 const completed=async(reply:PolicyMutationReply)=>{
  if(reply.policy.document.id!==p.policyId||JSON.stringify(policyFields(reply.policy.document.candidate))!==JSON.stringify(p.candidate))throw new PolicyRequestError('delivery-unknown',true)
  update({save:{state:'complete',revision:reply.policy.revision,candidateDigest:reply.policy.document.candidate.digest}})
  await checkpoint()
  await readPolicy(p.policyId) // Current authorization and read-back; never replace a frozen editor.
  await cache.invalidateQueries({queryKey:POLICIES_KEY});await cache.invalidateQueries({queryKey:actionApprovalKey(chatId)})
 }
 const saveKey=chatId+':'+p.operationId
 const save=async(allowance?:ActionGrant)=>{
  if(activeSaves.has(saveKey)||!mounted.current||flight.current||blocked||uncertain||complete||policyEditorBlocked(p.policyId))return
  activeSaves.add(saveKey);flight.current=true;setBusy(true);setError('')
  let dispatched=false
  try{
   const current=await readPolicy(p.policyId)
   if(current.revision!==p.revision)throw Error(msg('The saved Policy changed. Keep this proposal and send a new request against the latest draft.'))
   if(!mounted.current||policyEditorBlocked(p.policyId))throw Error(msg('Save or discard your editor changes before saving this proposal.'))
   update({save:{state:'pending'}});await checkpoint()
   if(!mounted.current||policyEditorBlocked(p.policyId))throw Error(msg('Save or discard your editor changes before saving this proposal.'))
   dispatched=true;await completed(await saveProposal(chatId,p,allowance));setReview(undefined)
  }catch(cause){if(dispatched&&(!(cause instanceof PolicyRequestError)||cause.uncertain))update({save:{state:'uncertain'}});else update({save:undefined});setError(cause instanceof Error?cause.message:msg('The Policy update could not be saved.'));try{await checkpoint()}catch{/* Shared persistence status retains the failure. */}}
  finally{activeSaves.delete(saveKey);flight.current=false;setBusy(false)}
 }
 useAutomaticAction({grant,action:'policy-update',path:'policy:'+p.policyId,revision:p.revision,fingerprint:JSON.stringify(policyProposalBody(p)),ready:!blocked&&!uncertain&&!complete&&grant?.target===p.id,canStart:()=>!activeSaves.has(saveKey)&&mounted.current&&!policyEditorBlocked(p.policyId),execute:save,onError:()=>setError(msg('The Policy update could not be saved.'))})
 const allow=async()=>{
  if(blocked||flight.current||policyEditorBlocked(p.policyId))return
  flight.current=true;setBusy(true);setError('')
  try{await checkpoint();if(!mounted.current||policyEditorBlocked(p.policyId))throw Error(msg('Save or discard your editor changes before saving this proposal.'));cache.setQueryData(actionApprovalKey(chatId),await actionApprovalAPI<ActionApproval>(chatId,'grant',{action:'policy-update',target:p.id,revision:p.revision}));setReview(undefined)}catch(cause){setError((cause as Error).message)}finally{flight.current=false;setBusy(false)}
 }
 const recover=async()=>{if(flight.current)return;flight.current=true;setBusy(true);setError('');try{await completed(await recoverPolicyOperation(p.operationId))}catch(cause){setError(cause instanceof PolicyRequestError&&cause.code==='not-found'?new PolicyRequestError('operation-unresolved').message:(cause as Error).message)}finally{flight.current=false;setBusy(false)}}
 const diff=<ProposalDiffView diff={diffProposal(JSON.stringify(p.before),p.candidate)}/>
 return <section aria-label={msg('Proposed Policy update')}>
  <p>{complete?msg('Policy draft saved'):msg('Proposed Policy update')} · {p.candidate.title||msg('Untitled policy')}</p>
  <p>{p.check?.status==='passed'?msg('Draft shape checked. Policy correctness and approval require human review.'):msg('The saved Policy changed. Keep this proposal and send a new request against the latest draft.')}</p>
  <Disclosure title={msg('Review changes')}>{diff}</Disclosure>
  {editorBlocked&&<p>{msg('Save or discard your editor changes before saving this proposal.')}</p>}
  {error&&<Notice tone="warning">{error}</Notice>}
  {complete?<ChatActionLink to={policyHref(p.policyId)}>{msg('Open policy')}</ChatActionLink>:uncertain?<ChatAction disabled={busy} onClick={()=>void recover()}>{busy?msg('Recovering…'):msg('Recover exact save result')}</ChatAction>:<>
   <ChatAction disabled={blocked} onClick={()=>setReview('manual')}>{msg('Review and save draft')}</ChatAction>
   <ChatAction disabled={blocked||status.isError} onClick={()=>setReview('auto')}>{grant?.state==='ready'&&grant.target===p.id?msg('Auto'):msg('Auto within limits…')}</ChatAction>
   {grant?.state==='ready'&&grant.target===p.id&&<ChatAction disabled={busy} onClick={()=>{void actionApprovalAPI<ActionApproval>(chatId,'revoke',{id:grant.id}).then(reply=>cache.setQueryData(actionApprovalKey(chatId),reply),()=>setError(msg('Approvals unavailable')))}}>{msg('Ask me')}</ChatAction>}
  </>}
  <Dialog open={!!review} onOpenChange={open=>{if(!busy&&!open)setReview(undefined)}} title={review==='auto'?msg('Allow one Policy draft update'):msg('Review Policy draft update')} footer={<DialogActions><ChatAction disabled={busy} onClick={()=>setReview(undefined)}>{msg('Cancel')}</ChatAction><ChatAction disabled={blocked} onClick={()=>void(review==='auto'?allow():save())}>{review==='auto'?msg('Allow one draft update'):msg('Save draft update')}</ChatAction></DialogActions>}>
   {diff}<p>{msg('This saves only the shown draft update to this exact Policy revision. Reviews, sign-offs and publication remain separate manual actions.')}</p>
   {review==='auto'&&<p>{msg('Allow this exact proposal once within 15 minutes in this signed-in window. Changing the Policy or proposal invalidates the allowance. Plan mode and unsaved editor changes block saving.')}</p>}
   {error&&<Notice tone="warning">{error}</Notice>}
  </Dialog>
 </section>
}
