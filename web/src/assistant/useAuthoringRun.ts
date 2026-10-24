import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { AssistantEvent } from '@judgment-pack/harness/engine'
import { failureEvent } from '@judgment-pack/harness/failures'
import type { AssistantEngine, ThinkingTier } from '../config/deskConfig'
import type { AITarget } from './aiConnections'
import { whenSessionEnds } from '../mcp/session'
import { canonicalProposal, frozen } from './proposalValue'
import { reportFromExecution } from './report'
import { serverRunClient, RunRequestError, type AuthoringPurpose, type ServerRunClient } from './serverRuns/client'
import { RunObservations } from './serverRuns/stream'
import type { RunSnapshot, RunStart } from './serverRuns/types'

export interface AuthoringRequest {
 requestId:string;purpose:AuthoringPurpose;prompt:string;arguments:Record<string,string>
 draft?:string;path?:string;graphId?:string;graphDigest?:string;packs?:string[]
 model:string;thinking:ThinkingTier;effort?:string;phase:'work'|'plan'
}
export type AuthoringInput=Omit<AuthoringRequest,'requestId'|'model'|'thinking'|'effort'|'phase'> & {phase?:'work'|'plan'}
interface Attempt {client?:ServerRunClient;start?:RunStart;stopped:boolean;ended:boolean;view:AbortController;phase:'work'|'plan';cancel?:Promise<unknown>}
function cancel(attempt:Attempt):Promise<unknown>{
 if(!attempt.client||!attempt.start)return Promise.resolve()
 return attempt.cancel??=attempt.client.cancelAuthoring(attempt.start)
}
const terminal=(run:RunSnapshot)=>['complete','stopped','interrupted','failed'].includes(run.state)
/** UI projection only: one explicit capture/start, model-free reconciliation,
 * retained server events, and explicit Stop. Detaching never cancels host work. */
export function useAuthoringRun(options:{target:AITarget;model:string;engine:AssistantEngine;thinking:ThinkingTier;effort?:string}) {
 const settings=useRef(options);settings.current=options
 const [status,setStatus]=useState<'idle'|'running'|'finished'>('idle')
 const [events,setEvents]=useState<AssistantEvent[]>([])
 const [failure,setFailure]=useState<string>()
 const [snapshot,setSnapshot]=useState<RunSnapshot>()
 const [streaming,setStreaming]=useState('')
 const [engineId,setEngineId]=useState(options.engine)
 const active=useRef<Attempt | undefined>(undefined);const mounted=useRef(true);const terminals=useRef({count:0})
 const finish=useCallback((attempt:Attempt)=>{
  if(attempt.ended)return;attempt.ended=true;terminals.current.count++
  if(mounted.current&&active.current===attempt){setStatus('finished');setStreaming('')}
 },[])
 const adopt=useCallback((attempt:Attempt,run:RunSnapshot)=>{
  if(!mounted.current||active.current!==attempt||attempt.stopped)return
  setSnapshot(run)
  const held=run.events.map(({event})=>event.type==='proposal'?canonicalProposal(event as Extract<AssistantEvent,{type:'proposal'}>):frozen(event as AssistantEvent))
  if(run.checkpoint)held.push(frozen({type:run.state==='paused-budget'?'budget_paused':'execution_checkpoint',checkpoint:run.checkpoint} as AssistantEvent))
  setEvents(held)
  setStreaming(run.displayPreview?.find(p=>!p.taskId)?.text??'')
  if(attempt.phase==='plan')setFailure('A Plan is retained for review and cannot be applied.')
  else if(run.historical||run.state!=='complete'&&!['admitted','running'].includes(run.state))setFailure('This retained run requires an explicit action before its proposal can be applied.')
  if(terminal(run)||!['admitted','running'].includes(run.state))finish(attempt)
 },[finish])
 const stop=useCallback(()=>{
  const attempt=active.current;if(!attempt||attempt.ended)return
  attempt.stopped=true;attempt.view.abort();finish(attempt)
  setFailure('The request was stopped. Its proposal cannot be applied.')
  // If capture is still in flight its continuation cancels the returned exact
  // reservation before ever calling start. No key alone authorizes cancellation.
  if(attempt.client&&attempt.start)void cancel(attempt).catch(cause=>{if(mounted.current&&active.current===attempt)setFailure(failureEvent(cause).message)})
 },[finish])
 const start=useCallback((input:AuthoringInput)=>{
  if(active.current&&!active.current.ended)return
  active.current?.view.abort()
  const attempt:Attempt={stopped:false,ended:false,view:new AbortController(),phase:input.phase??'work'};active.current=attempt
  const selected=settings.current
  setEvents([]);setFailure(undefined);setSnapshot(undefined);setStreaming('');setStatus('running');setEngineId(selected.engine)
  const request:AuthoringRequest={...input,requestId:crypto.randomUUID(),model:selected.model,thinking:selected.thinking,...(selected.effort?{effort:selected.effort}:{}),phase:input.phase??'work'}
  void(async()=>{
   try{
    const client=await serverRunClient();attempt.client=client
    if(attempt.stopped||!mounted.current)return
    if(!selected.target.connectionId||!selected.target.connectionRevision||!selected.model)throw new RunRequestError('not-sent',undefined,'configuration')
    let ref
    try {ref=await client.captureAuthoring(request,{connectionId:selected.target.connectionId,connectionRevision:selected.target.connectionRevision})}
    catch(cause){if(!(cause instanceof RunRequestError)||cause.delivery!=='uncertain')throw cause;ref=await client.authoringReference(request.requestId,request.purpose)}
    attempt.start=ref.start
    if(attempt.stopped){await cancel(attempt);return}
    if(!mounted.current)return
    if(ref.cancelled)throw new RunRequestError('refused',409,'stale-state')
    let run=ref.run
    if(!run){
     try {run=await client.start(ref.start,undefined)}
     catch(cause){
      if(!(cause instanceof RunRequestError)||cause.delivery!=='uncertain')throw cause
      // An uncertain admission is read, never resent. Missing admission is a
      // retained uncertain request, not permission to create another model run.
      const found=await client.authoringReference(request.requestId,request.purpose)
      if(!found.run)throw cause
      run=found.run
     }
    }
    if(attempt.stopped){await cancel(attempt);return}
    adopt(attempt,run)
    if(terminal(run)||!['admitted','running'].includes(run.state))return
    const observations=new RunObservations(run,run)
    try{
     for await(const observation of client.events(observations,attempt.view.signal)){
      if(observation.kind==='snapshot')adopt(attempt,observation.snapshot)
      else if(observation.kind==='progress'&&!observation.progress.taskId&&mounted.current&&!attempt.stopped)setStreaming(observation.progress.text)
     }
    }catch(cause){if(attempt.view.signal.aborted)return;adopt(attempt,await client.read(run,attempt.view.signal));throw cause}
    if(!attempt.view.signal.aborted)adopt(attempt,await client.read(run,attempt.view.signal))
   }catch(cause){if(mounted.current&&active.current===attempt&&!attempt.stopped)setFailure(failureEvent(cause).message)}
   finally{if(mounted.current&&active.current===attempt)finish(attempt)}
  })()
 },[adopt,finish])
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;active.current?.view.abort()}},[])
 useEffect(()=>whenSessionEnds(()=>{const attempt=active.current;if(attempt){attempt.stopped=true;attempt.view.abort();finish(attempt)};setFailure('The signed-in session ended. Its proposal cannot be applied.')}),[finish])
 const report=useMemo(()=>status==='running'?undefined:reportFromExecution(snapshot?.checkpoint),[snapshot,status])
 return {status,events,failure,streaming,report,engineId,terminals:terminals.current,start,stop}
}
