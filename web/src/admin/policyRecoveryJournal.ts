import {useEffect,useState} from 'react'
import {activeDeskId} from '../desks/scope'
import {digestBytes} from '../dialect/canon'
import {sessionBearer,whenSessionEnds} from '../mcp/session'
import type {PolicyRestoreRequest} from './policyRecovery'

export async function createPolicyRecoveryJournal(projectId:string){
 const digest=await digestBytes(new TextEncoder().encode(await sessionBearer()))
 const key='jpack.policy-recovery.pending.v1:'+JSON.stringify([window.location.origin,activeDeskId,digest,projectId])
 const valid=(value:PolicyRestoreRequest)=>value?.projectId===projectId&&typeof value.operationId==='string'&&typeof value.archiveDigest==='string'&&Number.isSafeInteger(value.archiveBytes)&&value.archiveBytes>0&&typeof value.ledgerDigest==='string'&&typeof value.destination==='string'&&typeof value.destinationRevision==='string'
 return {read():PolicyRestoreRequest|undefined{try{const value=JSON.parse(sessionStorage.getItem(key)??'null');return valid(value)?value:undefined}catch{return undefined}},store(value:PolicyRestoreRequest){if(!valid(value))throw Error('Invalid Policy restore operation');sessionStorage.setItem(key,JSON.stringify(value))},clear(){sessionStorage.removeItem(key)}}
}
export function usePolicyRecoveryJournal(projectId?:string){
 const [journal,setJournal]=useState<Awaited<ReturnType<typeof createPolicyRecoveryJournal>>>(),[pending,setPending]=useState<PolicyRestoreRequest>()
 useEffect(()=>{let live=true;setJournal(undefined);setPending(undefined);if(projectId)void createPolicyRecoveryJournal(projectId).then(value=>{if(live){setPending(value.read());setJournal(value)}}).catch(()=>{/* No durable admission; restore stays disabled. */});const stop=whenSessionEnds(()=>{live=false;setJournal(undefined)});return()=>{live=false;stop()}},[projectId])
 return {ready:!!journal,pending,store(value:PolicyRestoreRequest){if(!journal)throw Error('Policy restore session unavailable');journal.store(value);setPending(value)},clear(){if(!journal)throw Error('Policy restore session unavailable');journal.clear();setPending(undefined)}}
}
