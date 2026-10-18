import {useQuery} from '@tanstack/react-query'
import {msg} from '../i18n'
import {useChats} from '../chat/ChatProvider'
import {policyQuery,readPolicy} from './client'
import {POLICY_CHAT_INSTRUCTIONS,policyFields,validPolicyId} from './chat'
export function usePolicyChatContext(id?:string,chatId?:string){
 const {store}=useChats(),saved=useQuery({...policyQuery(id??''),enabled:validPolicyId(id),retry:false})
 if(!id)return undefined
 const text=(value:NonNullable<typeof saved.data>)=>JSON.stringify({workspace:'Policies',id,revision:value.revision,candidate:policyFields(value.document.candidate),reviewState:value.document.reviewState,instruction:POLICY_CHAT_INSTRUCTIONS})
 return {kind:'policy' as const,title:saved.data?.document.candidate.title||msg('Policy'),detail:msg('Saved policy draft'),text:saved.data?text(saved.data):'',busy:saved.isPending||saved.isError,prepare:async()=>{
  if(!chatId||!store)throw Error(msg('This conversation is no longer available.'))
  const current=await readPolicy(id)
  const policyRequest={revision:current.revision,candidate:policyFields(current.document.candidate)}
  // Preparation may overlap another Send. Only the admitted send owns the
  // request snapshot used by its tools; an abandoned read changes no chat.
  return {text:text(current),beforeSend:()=>store.update(chatId,{policyRequest})}
 }}
}
