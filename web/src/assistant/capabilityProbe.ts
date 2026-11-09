import type { CapabilityFacts } from '../config/deskConfig'
import { chassisUrl, deskFetch } from '../files/client'
import { aiHeaders } from './aiConnections'
import { observeSettings } from './serverRuns/settingsObservation'

/** Bounded host-authored findings. Provider generation remains in canonical Observe. */
export interface ProbeFindings { at:string; facts:CapabilityFacts; unknown:string[] }
export type ProbeOutcome = {ok:true;findings:ProbeFindings} | {ok:false;problem:'refused'|'unreachable'|'unreadable';status?:number}
const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const whole = (value: unknown, low: number, high: number): value is number => Number.isInteger(value) && Number(value) >= low && Number(value) <= high

/** The names the common servers give a model's window and output cap in their listing. */
const WINDOW_NAMES = ['max_model_len', 'context_length', 'context_window', 'max_context_length', 'n_ctx', 'max_input_tokens'] as const
const OUTPUT_NAMES = ['max_output_tokens', 'max_completion_tokens'] as const

/** The window and output cap a listing states for one model, where it states them. */
export function readListing(listing: unknown, model: string): Pick<CapabilityFacts, 'contextWindow' | 'outputReserve'> {
  const rows = object(listing) && Array.isArray(listing.data) ? listing.data : Array.isArray(listing) ? listing : []
  const row = rows.find((item) => object(item) && item.id === model)
  if (!object(row)) return {}
  const named = (names: readonly string[], low: number, high: number) => {
    for (const name of names) if (whole(row[name], low, high)) return row[name] as number
    return undefined
  }
  const contextWindow = named(WINDOW_NAMES, 4096, 2_000_000)
  const output = named(OUTPUT_NAMES, 1024, 1_000_000)
  return {
    ...(contextWindow === undefined ? {} : { contextWindow }),
    // The reserve is at most half the window, the harness's own bound, so a larger cap is held to it.
    ...(contextWindow === undefined || output === undefined ? {} : { outputReserve: Math.min(output, Math.floor(contextWindow / 2)) })
  }
}

/** A fixed canonical observation plus independent model metadata; never a raw model call. */
export async function probeCapabilities(target:{connectionId:string;connectionRevision:string}|undefined,model:string,signal?:AbortSignal):Promise<ProbeOutcome> {
 const listing=(async()=>{
  try {
   const response=await deskFetch(chassisUrl('/api/assistant/models'),{method:'POST',headers:aiHeaders(target?.connectionId,target?.connectionRevision),signal})
   if(!response.ok){void response.body?.cancel().catch(()=>{});return {}}
   return readListing(await response.json(),model)
  } catch {return {}}
 })()
 try {
  const result=await observeSettings({kind:'capability',model},target,signal)
  if(result.kind!=='capability')return {ok:false,problem:'unreadable'}
  if(!result.capability.ok)return result.capability
  const facts={...result.capability.findings.facts,...await listing}
  return {ok:true,findings:{...result.capability.findings,facts,unknown:result.capability.findings.unknown.filter(item=>item!=='context window'||facts.contextWindow===undefined)}}
 }catch{return {ok:false,problem:'unreachable'}}
}
