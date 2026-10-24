import { AIFailureError, failure } from '@judgment-pack/harness/failures'
import type { ServerRunClient } from '../assistant/serverRuns/client'
import { RunObservations } from '../assistant/serverRuns/stream'
import type { RunSnapshot } from '../assistant/serverRuns/types'
import { briefText, type Reply } from './model'

/** Subscribe and read durable status only. Reconnection never sends generation. */
export async function observeBrief(client:ServerRunClient,initial:RunSnapshot,signal:AbortSignal,onSnapshot:(run:RunSnapshot)=>void):Promise<RunSnapshot> {
  let run=initial
  onSnapshot(run)
  if(['admitted','running'].includes(run.state)) {
    const observations=new RunObservations(run,run)
    for await(const observation of client.events(observations,signal)) {
      if(observation.kind==='snapshot'){run=observation.snapshot;onSnapshot(run)}
    }
    signal.throwIfAborted()
    run=await client.read(run,signal);onSnapshot(run)
  }
  return run
}

/** Readback binds acknowledgement to this run's exact token and source digest. */
export function savedBrief(value:unknown,run:RunSnapshot):Reply {
  if(!value||typeof value!=='object')throw new AIFailureError(failure('invalid-response','desk','unknown'))
  const reply=value as Reply
  const revision=reply.content?.version===1?reply.content.subjects?.[run.owner.id]?.revisions.find(r=>r.id===run.requestId):undefined
  if(!revision||revision.model.length===0)throw new AIFailureError(failure('invalid-response','desk','unknown'))
  briefText(revision.text)
  return reply
}
