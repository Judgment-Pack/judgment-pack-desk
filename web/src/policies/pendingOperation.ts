import {useEffect,useState} from 'react'
import {activeDeskId} from '../desks/scope'
import {digestBytes} from '../dialect/canon'
import {sessionBearer,whenSessionEnds} from '../mcp/session'
import type {PolicyWrite} from './client'

type Family='source-sync'|'lifecycle'
export async function createPolicyPendingJournal(family:Family,id:string){
 // Use the same frozen workspace and current session as deskFetch. A token is
 // never stored in the journal key; a new sign-in cannot inherit an old grant.
 const sessionDigest=await digestBytes(new TextEncoder().encode(await sessionBearer()))
 const key='jpack.policy-pending.v2:'+JSON.stringify([window.location.origin,activeDeskId,sessionDigest,family,id])
 const path='/'+encodeURIComponent(id)+'/'+family
 return {key,
  read():PolicyWrite|undefined{try{const held=JSON.parse(sessionStorage.getItem(key)??'null');return held?.method==='POST'&&held.path===path&&typeof held.body?.operationId==='string'?held:undefined}catch{return undefined}},
  store(operation:PolicyWrite){if(operation.method!=='POST'||operation.path!==path)throw new Error('Invalid pending Policy operation');sessionStorage.setItem(key,JSON.stringify(operation))},
  clear(){sessionStorage.removeItem(key)},
 }
}
export function usePolicyPendingJournal(family:Family,id:string){
 const [journal,setJournal]=useState<Awaited<ReturnType<typeof createPolicyPendingJournal>>>(),[pending,setPending]=useState<PolicyWrite>()
 useEffect(()=>{
  let live=true
  void createPolicyPendingJournal(family,id).then(value=>{if(live){setPending(value.read());setJournal(value)}}).catch(()=>{/* SessionGate owns sign-in errors; writes stay disabled. */})
  const stop=whenSessionEnds(()=>{live=false;setJournal(undefined)})
  return()=>{live=false;stop()}
 },[family,id])
 return {ready:!!journal,pending,
  store(operation:PolicyWrite){if(!journal)throw new Error('Policy session unavailable');journal.store(operation);setPending(operation)},
  clear(){if(!journal)throw new Error('Policy session unavailable');journal.clear();setPending(undefined)},
 }
}
