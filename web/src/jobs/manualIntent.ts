import {sourceMessage} from '../i18n/source'
import {activeDeskId} from '../desks/scope'
import {digestBytes} from '../dialect/canon'
import {sessionBearer} from '../mcp/session'

export interface PendingManualIntent {key:string;requestDigest:string}
/** A recovery reminder only. The anchored host intent is the authority; this
 * journal never causes a retry and retains no facts, evidence or credential. */
export async function manualIntentJournal(jobId:string){
 const sessionDigest=await digestBytes(new TextEncoder().encode(await sessionBearer()))
 const name='jpack.manual-intent.v1:'+JSON.stringify([window.location.origin,activeDeskId,sessionDigest,jobId])
 return {read():PendingManualIntent|undefined{
  const raw=sessionStorage.getItem(name)
  if(raw===null)return
  const v=JSON.parse(raw)
  if(!v||Object.keys(v).sort().join(',')!=='key,requestDigest'||typeof v.key!=='string'||!/^[A-Za-z0-9_-]{1,128}$/.test(v.key)||typeof v.requestDigest!=='string'||!/^sha256:[a-f0-9]{64}$/.test(v.requestDigest))throw Error(sourceMessage('The submission reminder is unavailable. No run was sent.'))
  return v
 },async begin(payload:string):Promise<PendingManualIntent>{
  if(this.read())throw Error(sourceMessage('An earlier submission is unresolved; inspect its history before choosing a new intent.'))
  const value={key:crypto.randomUUID(),requestDigest:await digestBytes(new TextEncoder().encode(payload))}
  // Recheck after the digest await: concurrent clicks cannot allocate two
  // keys while each believes the reminder is empty.
  if(this.read())throw Error(sourceMessage('An earlier submission is unresolved; inspect its history before choosing a new intent.'))
  // digestBytes already returns the canonical sha256: spelling.
  sessionStorage.setItem(name,JSON.stringify(value));return value
 },clear(){sessionStorage.removeItem(name)}}
}
