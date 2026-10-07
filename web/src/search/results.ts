import { SearchFailure } from './failures'
import type { ResearchConfig, ResearchGatewayConfig } from '../config/deskConfig'
import { answer, deskFetch } from '../files/client'
import { acquire, newResearchSession, registry, seal } from '../research/gatewayClient'
import { canonicalize, hexToBytes, memberOf, parseJsonText, stringMember } from '../research/verify/canon'
import { sha256Hex } from '../research/verify/receipt'
import { verifySession } from '../research/verify/session'
import { base64, readDocumentObject, type DocumentObject } from '../documents/client'
import { validWebURL } from '../documents/record'

export const WEB_SEARCH = 'web-search'
export interface SearchRequest { connection:string; revision:string; query:string; maxResults:number }
export interface SearchReference { id:string; digest:string; request:SearchRequest }
export interface SearchResult {
 version:1; connection:string; revision:string; provider:string; query:string; retrievedAt:string
 kind:'search-results'|'grounded-answer'; hits:{title:string;url:string;snippet:string}[]
 generatedAnswer?:string; attributionHtml?:string; queries?:string[]
}
export interface VerifiedSearch { reference:SearchReference; result:SearchResult }
const fail = () => new SearchFailure('search-verification-failed')
const identifier=(s:unknown)=>typeof s==='string'&&/^[a-z][a-z0-9-]{0,47}$/.test(s)
export function validSearchReference(ref:unknown):ref is SearchReference {
 const v=ref as SearchReference|undefined,r=v?.request
 return Boolean(v && typeof v.id==='string' && /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(v.id)
  && typeof v.digest==='string' && /^sha256:[a-f0-9]{64}$/.test(v.digest) && r && identifier(r.connection)
  && typeof r.revision==='string' && /^[a-f0-9]{64}$/.test(r.revision) && typeof r.query==='string' && r.query.trim().length>0
  && r.query.length<=2000 && Number.isSafeInteger(r.maxResults) && r.maxResults>=1 && r.maxResults<=10
  && Object.keys(r).every(k=>['connection','revision','query','maxResults'].includes(k)))
}
export function validateSearch(value:unknown,request:SearchRequest):SearchResult {
 const d=value as SearchResult
 if(!d || d.version!==1 || d.connection!==request.connection || d.revision!==request.revision || d.query!==request.query
  || !identifier(d.provider) || !['search-results','grounded-answer'].includes(d.kind) || typeof d.retrievedAt!=='string' || !Number.isFinite(Date.parse(d.retrievedAt))
  || !Array.isArray(d.hits) || d.hits.length>request.maxResults || new Set(d.hits.map(h=>h?.url)).size!==d.hits.length)throw fail()
 for(const h of d.hits)if(!h || !validWebURL(h.url) || !h.url.startsWith('https://') || h.url.length>4096 || typeof h.title!=='string' || h.title.length>512 || typeof h.snippet!=='string' || h.snippet.length>2000)throw fail()
 if(d.generatedAnswer!==undefined && (d.kind!=='grounded-answer' || typeof d.generatedAnswer!=='string' || d.generatedAnswer.length>12000))throw fail()
 if(d.attributionHtml!==undefined && (d.kind!=='grounded-answer' || typeof d.attributionHtml!=='string' || d.attributionHtml.length>32768))throw fail()
 if(d.queries!==undefined && (!Array.isArray(d.queries) || d.queries.length>20 || d.queries.some(q=>typeof q!=='string'||q.length>2000)))throw fail()
 return d
}
async function save(id:string,object:DocumentObject,digest:string,signal?:AbortSignal){
 try {
  return (await answer<{sha256:string}>(await deskFetch(`/api/attachments/${id}`,{method:'PUT',headers:{'Content-Type':'application/json','If-Match':digest},body:JSON.stringify(object),signal}))).sha256
 } catch { signal?.throwIfAborted(); throw new SearchFailure('search-storage-failed') }
}
export async function verifySearch(object:DocumentObject,ref:SearchReference,pin:ResearchGatewayConfig):Promise<VerifiedSearch>{
 if(!validSearchReference(ref))throw fail()
 const p=object.proof,o=object.original
 if(object.version!==1 || !p || !o || p.source!==WEB_SEARCH || p.authority!==pin.authority || p.publicKey!==pin.signer.public || p.drive || p.gmail || p.connected || p.resource || p.web || o.mediaType!=='application/json' || o.bytes.length>1_400_000)throw fail()
 const parsed=parseJsonText(p.response),receipt=memberOf(parsed,'receipt'),result=memberOf(parsed,'result'),salts=memberOf(parsed,'salts')
 const acquisition=receipt&&memberOf(receipt,'acquisition')
 if(!receipt || !result || !salts || !acquisition || stringMember(receipt,'receiptVersion')!=='3' || stringMember(receipt,'kind')!=='acquisition' || stringMember(receipt,'source')!==WEB_SEARCH || stringMember(acquisition,'shape')!=='http' || stringMember(receipt,'resultDigest')!==ref.digest)throw fail()
 const verdict=await verifySession({sessionId:p.session,authority:pin.authority,publicKeyHex:pin.signer.public,receipts:[{receipt,result}],registryText:p.registry})
 if(!verdict.ok || !verdict.sealed)throw fail()
 const salt=stringMember(salts,'args');if(!salt || !/^[a-f0-9]{64}$/.test(salt))throw fail()
 const args=canonicalize(parseJsonText(JSON.stringify(ref.request))), committed=new Uint8Array(37+args.length)
 committed.set(hexToBytes(salt));committed.set(new TextEncoder().encode('args:'),32);committed.set(args,37)
 if(stringMember(receipt,'argumentsCommitment')!==`sha256:${await sha256Hex(committed)}`)throw fail()
 const original=Uint8Array.from(atob(o.bytes),c=>c.charCodeAt(0)),canonical=canonicalize(result)
 if(original.length>1<<20 || base64(original)!==o.bytes || o.sha256!==`sha256:${await sha256Hex(original)}` || base64(canonical)!==o.bytes)throw fail()
 return {reference:ref,result:validateSearch(JSON.parse(new TextDecoder().decode(original)),ref.request)}
}
export async function searchWeb(request:SearchRequest,config:ResearchConfig,signal:AbortSignal):Promise<VerifiedSearch>{
 if(!config.gateway || !config.documents?.enabled)throw fail()
 const session=newResearchSession(),id=crypto.randomUUID(),constraint=config.managedLocal?'local-documents':undefined
 const acquired=await acquire(session,WEB_SEARCH,{...request},1<<20,signal,constraint)
 signal.throwIfAborted()
 const original=canonicalize(acquired.result)
 if(original.length>1<<20)throw fail()
 const object:DocumentObject={version:1,original:{name:'web-search.json',mediaType:'application/json',bytes:base64(original),sha256:`sha256:${await sha256Hex(original)}`}}
 const stored=await save(id,object,'absent',signal)
 let registryText:string
 try {
  await seal(session,signal,constraint)
  registryText=(await registry(signal,constraint)).split('\n').filter(line=>line.trim()&&stringMember(parseJsonText(line),'sessionId')===session).join('\n')+'\n'
 } catch { signal.throwIfAborted(); throw new SearchFailure('search-proof-failed') }
 object.proof={session,source:WEB_SEARCH,authority:config.gateway.authority,publicKey:config.gateway.signer.public,response:acquired.text,registry:registryText}
 await save(id,object,stored,signal)
 signal.throwIfAborted()
 const ref={id,request,digest:stringMember(acquired.receipt,'resultDigest')!}
 try { return await verifySearch(object,ref,config.gateway) } catch { throw fail() }
}
export async function loadSearch(ref:SearchReference,pin:ResearchGatewayConfig,signal?:AbortSignal){return verifySearch(await readDocumentObject(ref.id,signal),ref,pin)}
