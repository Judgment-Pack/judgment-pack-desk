import type {Chat,ChatStore} from '../chat/store'
import {msg} from '../i18n'
import {readPolicyAuthoringSource} from './client'
import {policyEditorBlocked} from './chatEditing'
import {validPolicyId} from './chat'
import {validPolicyRevisionId,validPolicyPublicationId,type PolicyAuthoringSource,type PolicySourceSelection} from './authoringSource'
export interface PolicyInterpretationProposal {version:1;id:string;policyId:string;revisionId:string;publicationId?:string;createdAt:string;afterTurnId?:string;openedChatId?:string}
/** Persisted handoffs use the server's RFC3339Nano wire shape, including real calendar dates. */
function validInterpretationTime(value:unknown):value is string {
 if(typeof value!=='string')return false
 const parts=/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(value)
 if(!parts)return false
 const year=Number(parts[1]),month=Number(parts[2]),day=Number(parts[3]),leap=year%4===0&&(year%100!==0||year%400===0)
 const days=[31,leap?29:28,31,30,31,30,31,31,30,31,30,31]
 return month>=1&&month<=12&&day>=1&&day<=days[month-1]!&&Number(parts[4])<=23&&Number(parts[5])<=59&&Number(parts[6])<=59&&(parts[7]===undefined||Number(parts[7])<=23&&Number(parts[8])<=59)
}
export function validPolicyInterpretationProposal(value:unknown):value is PolicyInterpretationProposal {
 if(!value||typeof value!=='object'||Array.isArray(value))return false
 const p=value as PolicyInterpretationProposal
 return Object.keys(p).every(key=>['version','id','policyId','revisionId','publicationId','createdAt','afterTurnId','openedChatId'].includes(key))&&p.version===1&&typeof p.id==='string'&&/^[A-Za-z0-9-]{1,80}$/.test(p.id)&&validPolicyId(p.policyId)&&validPolicyRevisionId(p.revisionId)&&(p.publicationId===undefined||validPolicyPublicationId(p.publicationId))&&validInterpretationTime(p.createdAt)&&(p.afterTurnId===undefined||typeof p.afterTurnId==='string'&&new TextEncoder().encode(p.afterTurnId).length<=256)&&(p.openedChatId===undefined||typeof p.openedChatId==='string'&&/^[A-Za-z0-9-]{1,80}$/.test(p.openedChatId))
}
export async function preparePolicyInterpretation(selection:PolicySourceSelection,signal?:AbortSignal):Promise<PolicyAuthoringSource>{
 if(policyEditorBlocked(selection.policyId))throw Error(msg('Save or discard your Policy changes before designing a pack.'))
 const source=await readPolicyAuthoringSource(selection,signal)
 signal?.throwIfAborted()
 if(policyEditorBlocked(selection.policyId))throw Error(msg('Save or discard your Policy changes before designing a pack.'))
 return source
}
/** Opening a composer is model-free. A retained ordinary chat also survives reload. */
export function openPolicyInterpretation(store:ChatStore,source:PolicyAuthoringSource,openedChatId?:string):Chat{
 if(policyEditorBlocked(source.policyId))throw Error(msg('Save or discard your Policy changes before designing a pack.'))
 if(!store.getSnapshot().ready||store.getSnapshot().error)throw Error(msg('Save or recover this conversation before continuing.'))
 if(openedChatId){const existing=[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===openedChatId);if(existing&&existing.policySources?.some(held=>JSON.stringify(held)===JSON.stringify(source)))return existing;throw Error(msg('The opened conversation was deleted. Design a pack again from the Policy history.'))}
 const chat=store.create(undefined,'draft')
 store.update(chat.id,{policySources:[source],title:msg('Interpret {{title}}',{title:source.title||msg('Policy')}),composer:msg('Design a pack interpretation of {{title}}, version {{version}}, using the exact selected Policy source. Identify ambiguities and missing facts for review.',{title:source.title||msg('Policy'),version:source.artifactVersion})})
 return [...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===chat.id)!
}
