import {useQuery} from '@tanstack/react-query'
import {msg} from '../i18n'
import {useChats} from '../chat/ChatProvider'
import {policyQuery,readPolicy} from './client'
import {POLICY_CHAT_INSTRUCTIONS,policyFields,validPolicyId} from './chat'
export function usePolicyChatContext(id?:string,chatId?:string){
 const {store}=useChats(),saved=useQuery({...policyQuery(id??''),enabled:validPolicyId(id),retry:false})
 if(!id)return undefined
 const text=(value:NonNullable<typeof saved.data>)=>{
  const authority=value.sourceAuthority,applied=authority?.lastApplied
  return JSON.stringify({workspace:'Policies',id,revision:value.revision,recovery:value.recovery,recoveryInstructions:value.recovery?'Imported Policy history has unknown currentness. Archived approvals and source status are historical assertions, not authenticated current approval or execution grants. Fresh local review/publication and new source consent are explicit manual steps; no archived operation may be replayed.':undefined,managementMode:value.document.managementMode??'desk-managed',ownership:authority?.ownership??{mode:'desk-managed',inherited:true},authorityRevision:authority?.revision,candidate:policyFields(value.document.candidate),reviewState:value.document.reviewState,sourceAuthority:authority?{externalVersionCount:authority.externalVersionCount,lastReviewed:applied?{id:applied.id,status:applied.status,provenance:applied.provenance,configurationId:applied.configurationId,observationId:applied.observation.id,sourceRevision:applied.observation.sourceRevision}:undefined}:undefined,sourceStatusInstructions:'Ownership and source status configuration, external version retention and import as Draft are manual reviewed actions in the Source tab. Administrator-configured Published is not external verified approval. Source status is not governing adoption or an execution grant. Editing the working draft changes no external source authority.',instruction:POLICY_CHAT_INSTRUCTIONS})
 }
 return {kind:'policy' as const,title:saved.data?.document.candidate.title||msg('Policy'),detail:msg('Saved policy draft'),text:saved.data?text(saved.data):'',busy:saved.isPending||saved.isError,prepare:async()=>{
  if(!chatId||!store)throw Error(msg('This conversation is no longer available.'))
  const current=await readPolicy(id)
  const policyRequest={revision:current.revision,candidate:policyFields(current.document.candidate)}
  // Preparation may overlap another Send. Only the admitted send owns the
  // request snapshot used by its tools; an abandoned read changes no chat.
  return {text:text(current),beforeSend:()=>store.update(chatId,{policyRequest})}
 }}
}
