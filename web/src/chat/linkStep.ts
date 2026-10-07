import { normalizeLink } from '../documents/link'

/** Only the URL and exact retained read window, never raw tool text. */
export interface LinkStep { url:string; documentId?:string; digest?:string; pages?:number[] }
export function linkStepFromCall(args:unknown):LinkStep|undefined {
 const raw=(args as {url?:unknown}|undefined)?.url
 if(typeof raw!=='string'||raw.length>8192)return
 const parsed=normalizeLink(raw)
 return 'refused' in parsed?undefined:{url:parsed.displayUrl}
}
export function validLinkStep(value:unknown):value is LinkStep {
 const v=value as LinkStep|undefined
 if(!v||!linkStepFromCall(v)||Object.keys(v).some(k=>!['url','documentId','digest','pages'].includes(k)))return false
 if(v.documentId===undefined&&v.digest===undefined&&v.pages===undefined)return true
 return typeof v.documentId==='string'&&/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(v.documentId)
  && typeof v.digest==='string'&&/^sha256:[a-f0-9]{64}$/.test(v.digest)
  && Array.isArray(v.pages)&&v.pages.length>0&&v.pages.length<=500&&v.pages.every(n=>Number.isSafeInteger(n)&&n>0)&&new Set(v.pages).size===v.pages.length
}
export function linkStepFromResult(value:unknown):LinkStep|undefined {
 const v=value as {link?:unknown;documentId?:unknown;digest?:unknown;pages?:unknown}|undefined
 if(!v)return
 const detail={...linkStepFromCall({url:v.link}),documentId:v.documentId,digest:v.digest,pages:v.pages}
 return validLinkStep(detail)?structuredClone(detail):undefined
}
