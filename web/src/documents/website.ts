import { sourceMessage } from '../i18n/source'
import type { ResearchConfig, ResearchGatewayConfig } from '../config/deskConfig'
import { answer, deskFetch } from '../files/client'
import { acquire, newResearchSession, registry, seal } from '../research/gatewayClient'
import { canonicalize, hexToBytes, memberOf, parseJsonText, stringMember } from '../research/verify/canon'
import { sha256Hex } from '../research/verify/receipt'
import { verifySession } from '../research/verify/session'
import { base64, readDocumentObject, type DocumentObject } from './client'
import { validWebURL } from './record'

export const WEB_DISCOVERY = 'web-discovery'
export interface WebsiteReference { id: string; digest: string; seed: string }
export interface WebsitePage { url: string; from: string; depth: number; status: 'discovered' | 'blocked' | 'failed' | 'skipped'; title: string; reason: string }
export interface WebsiteDiscovery {
 version: 1; seed: string; origin: string; pages: WebsitePage[]; stopReason: string; bytes: number; requests: number; externalLinks: number
 limits: { pages: number; depth: number; links: number; bytes: number; seconds: number }
}
export interface VerifiedWebsite { reference: WebsiteReference; discovery: WebsiteDiscovery }
const fail = () => new Error(sourceMessage('Website discovery could not be verified; no links were authorized.'))
const number = (n: unknown, max: number) => Number.isSafeInteger(n) && Number(n)>=0 && Number(n)<=max
export function validWebsiteReference(ref: unknown): ref is WebsiteReference {
 const v=ref as WebsiteReference|undefined
 return Boolean(v && typeof v==='object' && /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(v.id) && /^sha256:[a-f0-9]{64}$/.test(v.digest) && validWebURL(v.seed))
}
export function validateDiscovery(value: unknown, seed: string): WebsiteDiscovery {
 const d=value as WebsiteDiscovery
 if (!d || d.version!==1 || d.seed!==seed || !validWebURL(seed) || d.origin!==new URL(seed).origin || !Array.isArray(d.pages) || d.pages.length<1 || d.pages.length>100 || !d.limits || d.limits.pages!==10 || d.limits.depth!==2 || d.limits.links!==100 || d.limits.bytes!==8<<20 || d.limits.seconds!==45 || !number(d.bytes,8<<20) || !number(d.requests,30) || !number(d.externalLinks,10000) || !['finished','depth-limit','page-limit','byte-limit','time-limit','link-limit','request-limit','robots-unavailable'].includes(d.stopReason)) throw fail()
 const seen=new Set<string>(); let discovered=0
 for (const [i,p] of d.pages.entries()) {
  if (!p || !validWebURL(p.url) || new URL(p.url).origin!==d.origin || seen.has(p.url) || !number(p.depth,3) || typeof p.title!=='string' || p.title.length>1024 || typeof p.reason!=='string' || p.reason.length>200 || !['discovered','blocked','failed','skipped'].includes(p.status) || (p.depth>2 && p.status!=='skipped') || (i===0 ? p.url!==seed || p.from!=='' || p.depth!==0 : !seen.has(p.from) || p.depth!==(d.pages.find(parent=>parent.url===p.from)!.depth+1))) throw fail()
  if(p.status==='discovered')discovered++
  seen.add(p.url)
 }
 if(discovered>10)throw fail()
 return d
}
async function save(id:string,object:DocumentObject,digest:string,signal?:AbortSignal){
 return (await answer<{sha256:string}>(await deskFetch(`/api/attachments/${id}`,{method:'PUT',headers:{'Content-Type':'application/json','If-Match':digest},body:JSON.stringify(object),signal}))).sha256
}
export async function verifyWebsite(object:DocumentObject,ref:WebsiteReference,pin:ResearchGatewayConfig):Promise<VerifiedWebsite>{
 if(!validWebsiteReference(ref))throw fail()
 const p=object.proof,o=object.original
 if(object.version!==1 || !p || !o || p.source!==WEB_DISCOVERY || p.authority!==pin.authority || p.publicKey!==pin.signer.public || p.drive || p.gmail || p.connected || p.resource || p.web || o.mediaType!=='application/json' || o.bytes.length>1_400_000)throw fail()
 const parsed=parseJsonText(p.response),receipt=memberOf(parsed,'receipt'),result=memberOf(parsed,'result'),salts=memberOf(parsed,'salts')
 const acquisition=receipt&&memberOf(receipt,'acquisition')
 if(!receipt || !result || !salts || !acquisition || stringMember(receipt,'receiptVersion')!=='3' || stringMember(receipt,'kind')!=='acquisition' || stringMember(receipt,'source')!==WEB_DISCOVERY || stringMember(acquisition,'shape')!=='http' || stringMember(receipt,'resultDigest')!==ref.digest)throw fail()
 const verdict=await verifySession({sessionId:p.session,authority:pin.authority,publicKeyHex:pin.signer.public,receipts:[{receipt,result}],registryText:p.registry})
 if(!verdict.ok || !verdict.sealed)throw fail()
 const salt=stringMember(salts,'args');if(!salt || !/^[a-f0-9]{64}$/.test(salt))throw fail()
 const args=canonicalize(parseJsonText(JSON.stringify({url:ref.seed}))), committed=new Uint8Array(37+args.length)
 committed.set(hexToBytes(salt));committed.set(new TextEncoder().encode('args:'),32);committed.set(args,37)
 if(stringMember(receipt,'argumentsCommitment')!==`sha256:${await sha256Hex(committed)}`)throw fail()
 const original=Uint8Array.from(atob(o.bytes),c=>c.charCodeAt(0)),canonical=canonicalize(result)
 if(original.length>1<<20 || base64(original)!==o.bytes || o.sha256!==`sha256:${await sha256Hex(original)}` || base64(canonical)!==o.bytes)throw fail()
 return {reference:ref,discovery:validateDiscovery(JSON.parse(new TextDecoder().decode(original)),ref.seed)}
}
export async function discoverWebsite(seed:string,config:ResearchConfig,signal:AbortSignal):Promise<VerifiedWebsite>{
 if(!config.gateway || !config.documents?.enabled)throw fail()
 const session=newResearchSession(),id=crypto.randomUUID(),constraint=config.managedLocal?'local-documents':undefined
 const acquired=await acquire(session,WEB_DISCOVERY,{url:seed},1<<20,signal,constraint)
 signal.throwIfAborted()
 const original=canonicalize(acquired.result)
 if(original.length>1<<20)throw fail()
 const object:DocumentObject={version:1,original:{name:'website-discovery.json',mediaType:'application/json',bytes:base64(original),sha256:`sha256:${await sha256Hex(original)}`}}
 const stored=await save(id,object,'absent',signal)
 await seal(session,signal,constraint)
 const registryText=(await registry(signal,constraint)).split('\n').filter(line=>line.trim()&&stringMember(parseJsonText(line),'sessionId')===session).join('\n')+'\n'
 object.proof={session,source:WEB_DISCOVERY,authority:config.gateway.authority,publicKey:config.gateway.signer.public,response:acquired.text,registry:registryText}
 await save(id,object,stored,signal)
 signal.throwIfAborted()
 const ref={id,seed,digest:stringMember(acquired.receipt,'resultDigest')!}
 return verifyWebsite(object,ref,config.gateway)
}
export async function loadWebsite(ref:WebsiteReference,pin:ResearchGatewayConfig,signal?:AbortSignal){return verifyWebsite(await readDocumentObject(ref.id,signal),ref,pin)}
