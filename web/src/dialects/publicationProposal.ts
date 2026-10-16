import {msg} from '../i18n'
import type {ArtifactImpact} from '../artifacts/review'
import type {DialectPublicationAnswer} from './DialectPublication'
import type {DialectChatAction} from './chatActions'
import {dialectAPI} from './client'

export interface DialectPublicationProposal {variant:'asserted'|'generated';version:string;impact:ArtifactImpact;answer:DialectPublicationAnswer}
const object=(x:unknown):x is Record<string,unknown>=>!!x&&typeof x==='object'&&!Array.isArray(x)
const digest=(x:unknown)=>typeof x==='string'&&/^[a-f0-9]{64}$/.test(x)
const reference=(x:unknown)=>object(x)&&digest(x.sha256)&&Number.isSafeInteger(x.bytes)&&(x.bytes as number)>=0&&(x.bytes as number)<=32*1024*1024
export function validPublicationProposal(x:unknown):x is DialectPublicationProposal {
 if(!object(x)||Object.keys(x).some(k=>!['variant','version','impact','answer'].includes(k))||!['asserted','generated'].includes(String(x.variant))||typeof x.version!=='string'||x.version.length>256||!object(x.impact)||!['initial','patch','minor','major'].includes(String(x.impact.classification))||typeof x.impact.rationale!=='string'||!x.impact.rationale.trim()||x.impact.rationale.length>4000)return false
 const a=x.answer
 if(!object(a)||typeof a.document!=='string'||a.document.length>2*1024*1024||typeof a.digest!=='string'||!/^sha256:[a-f0-9]{64}$/.test(a.digest)||a.saved!==false||typeof a.basePublication!=='string'||a.basePublication!==''&&!digest(a.basePublication)||!reference(a.review)||!object(a.publication))return false
 const p=a.publication,d=p.dialect
 return digest(p.id)&&typeof p.at==='string'&&Number.isFinite(Date.parse(p.at))&&object(d)&&typeof d.preparationId==='string'&&/^prep_[a-f0-9]{32}$/.test(d.preparationId)&&digest(d.revision)&&d.variant===x.variant&&digest(d.request)&&Array.isArray(p.documents)&&p.documents.length===1&&object(p.documents[0])&&p.documents[0].version===x.version&&p.documents[0].id===`urn:judgmentpack:dialect:${d.preparationId}:${x.variant}`
}
export function publicationRequest(action:DialectChatAction){
 if(action.kind!=='publish-dialect'||!action.publication)throw Error('Choose a checked Dialect publication.')
 const {variant,version,impact,answer}=action.publication
 return {action:'publish',revision:action.revision,variant,version,impact,reviewed:true,checkDigest:answer.digest,basePublication:answer.basePublication,review:answer.review}
}
export const publicationPath=(a:DialectChatAction)=>`urn:judgmentpack:dialect:${a.inputId}:${a.publication?.variant}`
export const publicationFingerprint=(a:DialectChatAction)=>JSON.stringify(publicationRequest(a))
export function pendingPublication(actions:DialectChatAction[]|undefined,inputId?:string){return [...(actions??[])].reverse().find(a=>a.kind==='publish-dialect'&&!a.applied&&(!inputId||a.inputId===inputId))}
/** Receipt lookup is read-only and works even after a recipe changes or disappears. */
export async function readPublicationResult(action:DialectChatAction,signal?:AbortSignal){
 const p=action.publication
 if(!p)throw Error('Choose a checked Dialect publication.')
 const {result}=await dialectAPI<{result:DialectPublicationAnswer|null}>(`/${action.inputId}/publications?request=${p.answer.publication.dialect.request}`,'GET',undefined,undefined,signal)
 if(result&&(!result.saved||result.digest!==p.answer.digest||result.document!==p.answer.document||result.publication.documents[0]?.id!==publicationPath(action)||result.publication.documents[0]?.version!==p.version||result.publication.dialect.preparationId!==action.inputId))throw Error(msg('The saved publication does not match this proposal.'))
 return result
}
