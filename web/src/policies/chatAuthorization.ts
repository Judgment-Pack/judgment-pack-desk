import {deskFetch} from '../files/client'
import {failureError} from '@judgment-pack/harness/failures'
/** Local Policy access requires a current authenticated administrator. Keep
 * availability of the current Policy blobs separate from retained chat access. */
export async function authorizePolicyChat(signal:AbortSignal){
 let response:Response
 try{response=await deskFetch('/api/session',{signal})}catch{throw failureError('authentication','desk','not-started')}
 if(!response.ok)throw failureError(response.status===401?'authentication':'permission','desk','not-started')
 let body:unknown;try{body=await response.json()}catch{throw failureError('unavailable','desk','not-started')}
 const actor=body as {subject?:unknown;issuer?:unknown}|null
 if(!actor||typeof actor.subject!=='string'||!actor.subject||new TextEncoder().encode(actor.subject).length>1024||(actor.issuer!==null&&(typeof actor.issuer!=='string'||!actor.issuer||new TextEncoder().encode(actor.issuer).length>4096)))throw failureError('permission','desk','not-started')
}
