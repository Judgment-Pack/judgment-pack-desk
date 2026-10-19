import {actionApprovalAPI} from '../assistant/approvals/client'
import type {ChatStore} from '../chat/store'
import type {DialectChatAction} from './chatActions'
import type {DialectPublicationAnswer} from './DialectPublication'
import {publicationRequest,readPublicationResult} from './publicationProposal'
import {dialectRecipeIsEditing} from './recipeEditing'
import {msg} from '../i18n'

export function publicationSaveGuard(store:ChatStore,chatId:string,action:DialectChatAction){
 const snapshot=store.getSnapshot(),chat=snapshot.chats.find(c=>c.id===chatId),binding=snapshot.bindings.get(chatId),state=binding?.run?.getSnapshot()??binding?.state??chat?.checkpoint?.state
 const current=chat?.inputActions?.find(a=>a.id===action.id)
 if(!chat||chat.interaction==='plan'||binding?.run?.running||!state||!['complete','ready'].includes(state.status)||state.unknowns.length||state.questions?.some(q=>!q.resolution)||!current||current.applied||JSON.stringify(current)!==JSON.stringify(action)||dialectRecipeIsEditing(action.inputId))throw Error(msg('Finish the response and review open questions before publishing.'))
}
export async function retainPublishedAction(store:ChatStore,chatId:string,action:DialectChatAction){
 const current=store.getSnapshot().chats.find(c=>c.id===chatId)
 if(!current)throw Error(msg('This conversation is no longer available.'))
 store.update(chatId,{inputActions:current.inputActions?.map(a=>a.id===action.id?{...a,applied:{at:new Date().toISOString()}}:a)})
 await store.checkpoint(new AbortController().signal)
}
/** Both Ask me and Auto checkpoint the reviewed candidate, use the host adapter,
 * and verify the immutable receipt. A failed write is never automatically retried. */
export async function savePublication({store,chatId,action,grantId,guard=()=>{}}:{store:ChatStore;chatId:string;action:DialectChatAction;grantId?:string;guard?:()=>void}){
 const assert=()=>{guard();publicationSaveGuard(store,chatId,action)}
 assert()
 const binding=store.getSnapshot().bindings.get(chatId)
 if(binding)store.report(chatId,binding)
 await store.checkpoint(new AbortController().signal)
 assert()
 const existing=await readPublicationResult(action)
 if(existing){await retainPublishedAction(store,chatId,action);return existing}
 assert()
 const saved=await actionApprovalAPI<DialectPublicationAnswer>(chatId,grantId?'commit':'apply',{action:'dialect-publish',target:action.id,revision:action.revision,reviewed:true,...(grantId?{id:grantId}:{}),content:JSON.stringify(publicationRequest(action))})
 const verified=await readPublicationResult(action)
 if(!verified||verified.publication.id!==saved.publication.id)throw Error(msg('The publication could not be verified. Check its saved result before retrying.'))
 await retainPublishedAction(store,chatId,action)
 return verified
}
