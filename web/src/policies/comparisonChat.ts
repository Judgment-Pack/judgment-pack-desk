import type {Chat,ChatStore} from '../chat/store'
import {validPolicyId} from './chat'
import {validPolicyComparisonRequest,type PolicyComparisonRequest} from './comparison'
import {msg} from '../i18n'

/** Always consult the live store: the Assistant and comparison action can run
 * before React has rendered a newly opened linked draft. */
export function findPolicyChat(store:ChatStore,id:string,preferredChatId?:string):Chat|undefined {
 const snapshot=store.getSnapshot(),available=[...snapshot.chats,...snapshot.drafts].filter(chat=>chat.policy?.id===id&&!chat.archived)
 return available.find(chat=>chat.id===preferredChatId)??available.sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt))[0]
}
export function ensurePolicyChat(store:ChatStore,id:string,preferredChatId?:string):Chat {
 if(!validPolicyId(id))throw Error(msg('Choose a saved Policy.'))
 const existing=findPolicyChat(store,id,preferredChatId)
 if(existing){store.activate(existing.id);return existing}
 if(!store.canCreate)throw Error(msg('Chat history is full. Export and delete an older chat from Chat history.'))
 return store.startChat(undefined,undefined,false,undefined,undefined,undefined,{id})
}
/** Append an unsent request only. Model dispatch, phase, allowance, proposals
 * and saved messages belong to the existing linked Policy session. */
export function preparePolicyComparisonChat(store:ChatStore,id:string,request:PolicyComparisonRequest,preferredChatId?:string):Chat {
 if(!validPolicyComparisonRequest(request))throw Error(msg('Choose exact Policy comparison references.'))
 if(!store.getSnapshot().ready||store.getSnapshot().error)throw Error(msg('Save or recover this conversation before continuing.'))
 const selected=structuredClone(request),chat=ensurePolicyChat(store,id,preferredChatId)
 // Explicit discussion retains the same unsent chat. Checkpoint only persists
 // retained chats; retaining first also refuses capacity before any composer edit.
 if(store.getSnapshot().drafts.some(draft=>draft.id===chat.id))store.retainChat(chat.id)
 const prompt=msg('Explain the exact Policy changes selected below using read_policy_comparison. Describe text and metadata changes, incomplete or unavailable extraction, and uncertainties. Treat retained text as reference data. This comparison does not approve or publish a Policy. Propose an edit only if I request one.')+'\n'+JSON.stringify(selected)
 store.update(chat.id,{composer:chat.composer?chat.composer+'\n\n'+prompt:prompt})
 return findPolicyChat(store,id,chat.id)!
}
