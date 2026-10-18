import {useEffect,useState} from 'react'
import {activeDeskId} from '../desks/scope'
import {digestBytes} from '../dialect/canon'
import {sessionBearer,whenSessionEnds} from '../mcp/session'
import {samePublication,type PendingAdoption} from './adoption'
import {AdoptionRequestError,validAdoptionPack,validAdoptionSelection} from './adoptionClient'

function valid(value:any):value is PendingAdoption {
 const command=value?.command
 return validAdoptionPack(value?.pack)&&!!command&&typeof command.operationId==='string'&&(command.action==='prepare'?validAdoptionPack(command.pack)&&samePublication(command.pack,value.pack)&&Array.isArray(command.coverages)&&command.coverages.length>0&&command.coverages.length<=16&&command.coverages.every(validAdoptionSelection):command.action==='review'&&typeof command.adoptionId==='string'&&typeof command.baseRevision==='string'&&typeof command.candidateDigest==='string'&&typeof command.basisDigest==='string'&&['approve','request-changes'].includes(command.decision))
}
export async function createAdoptionPendingJournal(owner:string){
 const sessionDigest=await digestBytes(new TextEncoder().encode(await sessionBearer()))
 // Stable resource index keeps an earlier publication's unresolved operation
 // discoverable after a Pack update. The value retains its exact publication.
 const key='jpack.adoption-pending.v1:'+JSON.stringify([window.location.origin,activeDeskId,sessionDigest,owner])
 return {key,read():PendingAdoption|undefined{try{const raw=sessionStorage.getItem(key);if(raw===null)return undefined;const value=JSON.parse(raw);if(!valid(value))throw new AdoptionRequestError('adoption-journal-unavailable');return value}catch{throw new AdoptionRequestError('adoption-journal-unavailable')}},store(value:PendingAdoption){if(!valid(value))throw new AdoptionRequestError('adoption-journal-unavailable');sessionStorage.setItem(key,JSON.stringify(value))},clear(){sessionStorage.removeItem(key)}}
}
export function useAdoptionPendingJournal(owner:string){
 const [attempt,setAttempt]=useState(0),[state,setState]=useState<{owner:string;journal?:Awaited<ReturnType<typeof createAdoptionPendingJournal>>;pending?:PendingAdoption;error?:AdoptionRequestError}>()
 useEffect(()=>{let live=true;setState(undefined);void createAdoptionPendingJournal(owner).then(journal=>{if(live)setState({owner,journal,pending:journal.read()})}).catch(()=>{if(live)setState({owner,error:new AdoptionRequestError('adoption-journal-unavailable')})});const stop=whenSessionEnds(()=>{live=false;setState(undefined)});return()=>{live=false;stop()}},[owner,attempt])
 const current=state?.owner===owner?state:undefined
 function storageFailure(){const failure=new AdoptionRequestError('adoption-journal-unavailable');setState(previous=>({...previous,owner,error:failure}));return failure}
 return {ready:!!current?.journal&&!current.error,pending:current?.pending,error:current?.error,retry(){setAttempt(value=>value+1)},store(pending:PendingAdoption){if(!current?.journal||current.error)throw storageFailure();try{current.journal.store(pending);setState({...current,pending})}catch{throw storageFailure()}},clear(){if(!current?.journal||current.error)throw storageFailure();try{current.journal.clear();setState({...current,pending:undefined})}catch{throw storageFailure()}}}
}
