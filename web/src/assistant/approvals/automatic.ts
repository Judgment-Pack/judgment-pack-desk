import {useEffect,useRef} from 'react'
import type {ActionGrant} from './client'
/** Admission is server-owned. This dispatcher starts at most once for an exact
 * grant/candidate in this mount; commit consumes the grant before any write.
 * Adapters supply readiness and guarded execution, never permission booleans. */
export function useAutomaticAction({grant,action,path,revision,fingerprint,ready,execute,onError}:{grant?:ActionGrant;action:string;path?:string;revision?:string;fingerprint?:string;ready:boolean;execute:(grant:ActionGrant)=>Promise<void>;onError:(error:unknown)=>void}) {
 const attempted=useRef(new Set<string>()),inFlight=useRef(false),latest=useRef({execute,onError});latest.current={execute,onError}
 useEffect(()=>{
  if(!ready||!grant||grant.state!=='ready'||grant.action!==action||grant.path!==path||grant.revision!==revision||!fingerprint||inFlight.current||Date.parse(grant.expiresAt)<=Date.now())return
  const key=grant.id+':'+fingerprint
  if(attempted.current.has(key))return
  attempted.current.add(key);inFlight.current=true
  void latest.current.execute(grant).catch(e=>latest.current.onError(e)).finally(()=>{inFlight.current=false})
 },[grant?.id,grant?.state,grant?.action,grant?.path,grant?.revision,grant?.expiresAt,action,path,revision,fingerprint,ready])
}
