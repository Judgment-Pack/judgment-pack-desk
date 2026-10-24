import {useCallback,useEffect,useRef,useState} from 'react'
import {whenSessionEnds} from '../mcp/session'
import {serverRunClient,RunRequestError,type ServerRunClient} from './serverRuns/client'
import {RunObservations} from './serverRuns/stream'
import type {RunSnapshot} from './serverRuns/types'
import type {AITarget} from './aiConnections'

interface Attempt {stopped:boolean;view:AbortController;client?:ServerRunClient;run?:RunSnapshot;resolved:boolean;sentStart?:boolean;cancel?:Promise<void>}
const terminal=(run:RunSnapshot)=>['complete','failed','stopped','interrupted'].includes(run.state)
/** A fixed host diagnostic. No prompt, model, provider transport or tool capability enters the browser. */
export function useConnectionDiagnostic(target:AITarget,identity:string){
 const selected=useRef(target);selected.current=target
 const mounted=useRef(true),closed=useRef(false),active=useRef<Attempt|undefined>(undefined)
 const [status,setStatus]=useState<'idle'|'running'|'finished'>('idle'),[tested,setTested]=useState(false),[canStart,setCanStart]=useState(true)
 const current=(attempt:Attempt)=>mounted.current&&!closed.current&&active.current===attempt&&!attempt.view.signal.aborted
 const cancelOwner=useCallback((attempt:Attempt):Promise<void>=>attempt.cancel??=(async()=>{
  if(!attempt.run||!attempt.client)return
  let run=await attempt.client.read(attempt.run)
  if(!terminal(run)){
   const id=crypto.randomUUID()
   try{run=await attempt.client.action(run,{id,kind:'cancel',revision:run.revision})}
   catch(cause){
    if(!(cause instanceof RunRequestError))throw cause
    run=await attempt.client.read(run)
    if(cause.delivery==='refused'&&cause.status===409&&!terminal(run))run=await attempt.client.action(run,{id,kind:'cancel',revision:run.revision})
    else if(!terminal(run))throw cause
   }
  }
  if(!terminal(run))throw new RunRequestError('uncertain')
  attempt.resolved=true
  if(mounted.current&&!closed.current&&active.current===attempt)setCanStart(true)
 })().finally(()=>{if(!attempt.resolved)attempt.cancel=undefined}),[])
 const stop=useCallback(()=>{
  const attempt=active.current;if(!attempt||attempt.resolved)return
  attempt.stopped=true;attempt.view.abort();setTested(false);setStatus('finished')
  if(attempt.run)void cancelOwner(attempt).catch(()=>{})
 },[cancelOwner])
 const start=useCallback(()=>{
  if(closed.current||active.current&&!active.current.resolved)return
  const target=selected.current
  if(!target.connectionId||!target.connectionRevision)return
  const bound={connectionId:target.connectionId,connectionRevision:target.connectionRevision}
  const attempt:Attempt={stopped:false,view:new AbortController(),resolved:false};active.current=attempt
  setStatus('running');setTested(false);setCanStart(false)
  void(async()=>{
   try{
    const client=await serverRunClient();attempt.client=client
    if(attempt.stopped||!current(attempt)){attempt.resolved=true;return}
    const ref=await client.diagnosticReference(crypto.randomUUID(),bound)
    if(attempt.stopped||!current(attempt)){attempt.resolved=true;return}
    let run:RunSnapshot
    attempt.sentStart=true
    try{run=await client.start(ref,undefined,bound)}
    catch(cause){
     if(!(cause instanceof RunRequestError)||cause.delivery!=='uncertain')throw cause
     const retained=await client.diagnosticLookup(ref)
     if(!retained)throw cause
     run=retained
    }
    attempt.run=run
    if(attempt.stopped){await cancelOwner(attempt);return}
    if(!current(attempt))return
    if(!terminal(run)){
     const observations=new RunObservations(run,run)
     try{for await(const observation of client.events(observations,attempt.view.signal))if(observation.kind==='snapshot'){run=observation.snapshot;attempt.run=run}}
     catch(cause){if(attempt.view.signal.aborted)return;run=await client.read(run,attempt.view.signal);attempt.run=run;if(!terminal(run))throw cause}
     if(!attempt.view.signal.aborted){run=await client.read(run,attempt.view.signal);attempt.run=run}
    }
    attempt.resolved=terminal(run)
    if(current(attempt)&&run.state==='complete'){
     const succeeded=await client.diagnosticResult(run,attempt.view.signal)
     if(current(attempt))setTested(succeeded)
    }
   }catch(cause){if(!attempt.sentStart||cause instanceof RunRequestError&&cause.delivery!=='uncertain'&&!attempt.run)attempt.resolved=true}
   finally{if(current(attempt)){setStatus('finished');setCanStart(attempt.resolved)}else if(attempt.stopped&&attempt.resolved&&mounted.current&&!closed.current&&active.current===attempt)setCanStart(true)}
  })()
 },[cancelOwner])
 // Detachment withdraws private observations; it never issues a hidden Stop.
 useEffect(()=>{active.current?.view.abort();active.current=undefined;setStatus('idle');setTested(false);setCanStart(!closed.current)},[identity])
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;active.current?.view.abort()}},[])
 useEffect(()=>whenSessionEnds(()=>{closed.current=true;active.current?.view.abort();setTested(false);setStatus('idle');setCanStart(false)}),[])
 return {status,tested,canStart,canStop:!!active.current&&!active.current.resolved&&!closed.current,start,stop}
}
