import {answer,deskFetch,FileRequestError} from '../files/client'
import {privateQuery} from '../data/privateQuery'
export interface ImportRecord {id:string;entryId:string;location:string;tag:string;commit:string;at:string;previousCommit?:string;reviewer:{kind:string;name?:string;subject?:string}}
export interface Candidate {row:{id:string;entryDigest:string;dialectDigest:string};entry:{kind?:string;pack?:{id:string;versions?:string;digests?:string[]};artifact?:{id:string;version:string};title:string;description?:string;licence:string;author:string;sourceKinds:string[];compatibility?:Record<string,string>};files:Record<string,string>;document:string;digest:string;fixtureReports?:Record<string,unknown>}
export interface CatalogReview {previous:Record<string,string>;review:{id:string;base:string;release:{location:string;tag:string;commit:string;candidates:Candidate[];unavailable:{id:string;reason:string}[]}};changes:Record<string,{withdrawn:boolean;files:string[];upgradeNote?:string;compatibility?:Record<string,string>}>}
export const EXCHANGE_KEY=['exchange-imports']
export async function exchangeAPI<T>(path:string,body?:unknown,signal?:AbortSignal):Promise<T>{
 const response=await deskFetch('/api/exchange/'+path,{method:body===undefined?'GET':'POST',signal,headers:{'Content-Type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})})
 const result=await answer<T>(response)
 if(path==='imports'&&body!==undefined){
  const request=body as {reviewId:string;entryId:string},receipt=result as {id?:string;entryId?:string}|null
  const digest=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(request.reviewId+'\n'+request.entryId))
  const expected=Array.from(new Uint8Array(digest),b=>b.toString(16).padStart(2,'0')).join('')
  if(!receipt||receipt.id!==expected||receipt.entryId!==request.entryId)throw new FileRequestError(response.status,'The import response does not match the reviewed request.','desk')
 }
 return result
}

export const exchangeImportsQuery=()=>({...privateQuery,queryKey:EXCHANGE_KEY,queryFn:({signal}:{signal:AbortSignal})=>exchangeAPI<{items:ImportRecord[];revision:string}>('imports',undefined,signal)})
export const exchangeImportQuery=(id:string)=>({...privateQuery,queryKey:[...EXCHANGE_KEY,id],queryFn:({signal}:{signal:AbortSignal})=>exchangeAPI<{import:ImportRecord;candidate:Candidate}>('imports/'+id,undefined,signal)})
