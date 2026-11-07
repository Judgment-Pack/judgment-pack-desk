import { readFailure, type AIFailure } from '@judgment-pack/harness/failures'
import { modelIdProblem } from '../../config/deskConfig'
import { PROBE_DIAGNOSTICS, type ProbeResult } from '../client'
import type { ProbeOutcome } from '../capabilityProbe'
import { digest, exact, invalid, object, runOwner } from './decode'
import { RunRequestError, serverRunClient } from './client'
import { RunObservations } from './stream'
import type { RunSnapshot, RunStart, SettingsObservationSelector } from './types'

export type SettingsObservationResult = { readonly kind:'capability'; readonly observeSucceeded:boolean; readonly capability:ProbeOutcome; readonly failure?:AIFailure } | { readonly kind:'ping'; readonly observeSucceeded:boolean; readonly ping:ProbeResult; readonly failure?:AIFailure }
export const settingsSelector=(value:unknown):value is SettingsObservationSelector=>object(value)&&exact(value,['kind','model'])&&(value.kind==='capability'||value.kind==='ping')&&typeof value.model==='string'&&value.model===value.model.trim()&&modelIdProblem(value.model)===undefined
export function settingsStart(value:unknown):value is RunStart {
 return object(value)&&exact(value,['admissionId','purpose','ownerId','revision','requestId','observation'])&&value.purpose==='settings-observation'&&runOwner(value.purpose,value.ownerId)&&digest(value.revision)&&typeof value.requestId==='string'&&/^[A-Za-z0-9-]{1,100}$/.test(value.requestId)&&value.admissionId===`settings-observation-${value.requestId}`&&settingsSelector(value.observation)
}
const whole=(v:unknown,min:number,max:number)=>Number.isSafeInteger(v)&&Number(v)>=min&&Number(v)<=max
/** Closed normalized findings only. No provider response, tool call, prompt or caught text is accepted. */
export function decodeSettingsResult(value:unknown,kind:SettingsObservationSelector['kind']):SettingsObservationResult {
 if(!object(value)||!exact(value,['kind','observeSucceeded','capability','ping','failure'])||value.kind!==kind||typeof value.observeSucceeded!=='boolean')return invalid()
 if(value.failure!==undefined&&(!object(value.failure)||!exact(value.failure,['version','code','source','outcome','status','completion'])||!readFailure(value.failure)))return invalid()
 if(value.observeSucceeded&&value.failure!==undefined)return invalid()
 if(object(value.failure)&&value.failure.completion!==undefined&&(!object(value.failure.completion)||!exact(value.failure.completion,['finishReason','inputTokens','outputTokens','reasoningTokens','cacheReadTokens','cacheWriteTokens'])))return invalid()
 if(kind==='ping'){
  const p=value.ping
  if(value.capability!==undefined||!object(p)||!exact(p,['reachable','status','latencyMs','diagnostic','observationComplete'])||typeof p.reachable!=='boolean'||!whole(p.status,0,599)||Number(p.status)>0&&Number(p.status)<100||!whole(p.latencyMs,0,Number.MAX_SAFE_INTEGER)||typeof p.diagnostic!=='string'||p.diagnostic!==''&&!PROBE_DIAGNOSTICS.includes(p.diagnostic as typeof PROBE_DIAGNOSTICS[number])||typeof p.observationComplete!=='boolean'||p.observationComplete!==value.observeSucceeded||p.reachable&&(Number(p.status)<200||Number(p.status)>=300||p.diagnostic!==''))return invalid()
 }else{
  const c=value.capability
  if(value.ping!==undefined||!object(c)||!exact(c,['ok','findings','problem','status'])||typeof c.ok!=='boolean'||c.status!==undefined&&!whole(c.status,100,599))return invalid()
  if(c.ok){
   const f=c.findings
   if(c.problem!==undefined||!object(f)||!exact(f,['at','facts','unknown'])||typeof f.at!=='string'||f.at.length>64||!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/.test(f.at)||!Number.isFinite(Date.parse(f.at))||!object(f.facts)||!exact(f.facts,['nativeToolCalls','parallelToolCalls','reasoningField'])||typeof f.facts.nativeToolCalls!=='boolean'||typeof f.facts.parallelToolCalls!=='boolean'||f.facts.parallelToolCalls&&!f.facts.nativeToolCalls||f.facts.reasoningField!==undefined&&!['reasoning','reasoning_content'].includes(String(f.facts.reasoningField))||!Array.isArray(f.unknown)||JSON.stringify(f.unknown)!==JSON.stringify(['context window','vision','reasoning','tool-schemas','turn order']))return invalid()
  }else if(c.findings!==undefined||!['refused','unreachable','unreadable'].includes(String(c.problem)))return invalid()
 }
 return value as SettingsObservationResult
}
const terminal=(run:RunSnapshot)=>['complete','failed','stopped','interrupted'].includes(run.state)
/** One explicit press: one admission, one optional uncertainty lookup, one stream. Never replays. */
export async function observeSettings(selector:SettingsObservationSelector,target:{connectionId:string;connectionRevision:string}|undefined,signal?:AbortSignal):Promise<SettingsObservationResult> {
 const client=await serverRunClient()
 const start=await client.settingsReference(crypto.randomUUID(),selector,target,signal)
 let run:RunSnapshot
 try{run=await client.start(start,signal,target)}catch(cause){
  if(!(cause instanceof RunRequestError)||cause.delivery!=='uncertain')throw cause
  const retained=await client.settingsLookup(start)
  if(!retained)throw cause
  run=retained
 }
 if(signal?.aborted)throw new RunRequestError('not-sent')
 if(!terminal(run)){
  const observations=new RunObservations(run,run)
  for await(const observation of client.events(observations,signal))if(observation.kind==='snapshot')run=observation.snapshot
 }
 if(signal?.aborted||!terminal(run))throw new RunRequestError('uncertain')
 return client.settingsResult(run,selector.kind,signal)
}
