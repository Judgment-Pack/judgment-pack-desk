import {answer,deskFetch} from '../files/client'
import {sha256Hex} from '../research/verify/receipt'
import type {ResearchActivity,ResearchLimits} from '../dialects/research'
import type {ProfileEntry} from './dialectTypes'
import {jobsRequest,RetainedJSON} from './wire'

export interface PortableResearchChoice {connection:string;revision:string;model:string;effort?:string}
export interface PortableResearchReference {id:string;context:string}
export interface PortableResearchRun {
 id:string;revision:string;state:ResearchActivity['state'];createdAt:string
 choice:PortableResearchChoice;limits:ResearchLimits
 plan:{name:string;kind:'research';arguments:{question:string;constraints:unknown;fields:unknown;capabilities:string[]}}
 inputText:string;responseText:string;activity:ResearchActivity
}
export function validResearchReferences(value:unknown):value is Record<string,PortableResearchReference> {
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length>16)return false
 return Object.entries(value).every(([name,ref])=>/^[a-z][a-zA-Z0-9_-]{0,63}$/.test(name)&&ref&&typeof ref==='object'&&Object.keys(ref).every(k=>['id','context'].includes(k))&&/^research_[a-f0-9]{32}$/.test(ref.id)&&/^[a-f0-9]{64}$/.test(ref.context))
}
export function researchContextKey(text:string,caseText:string,importText?:string,sourceRevision?:string){return sha256Hex(new TextEncoder().encode(JSON.stringify([text,caseText,importText??'',sourceRevision??''])))}
export async function portableResearchAPI<T=PortableResearchRun>(path='',body?:unknown,revision?:string,signal?:AbortSignal):Promise<T> {
 return answer<T>(await deskFetch('/api/portable-research'+path,{method:body===undefined?'GET':'POST',headers:body===undefined?{}:{'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body===undefined?{}:{body:jobsRequest(body)}),signal}))
}
export const registerResearchModel=(choice:PortableResearchChoice)=>portableResearchAPI<ProfileEntry>('/profiles',{choice,reviewed:true})
export const reviewPortableResearch=(input:RetainedJSON,choice:PortableResearchChoice,limits:ResearchLimits,signal:AbortSignal)=>portableResearchAPI('',{input,choice,limits},undefined,signal)
