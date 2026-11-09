import { RunRequestError, type AuthoringPurpose, type ServerRunClient } from './serverRuns/client'
import type { RunContextSelector } from './serverRuns/types'

type ContextClient=Pick<ServerRunClient,'contextDocument'|'contextPane'|'releaseContext'>
// Registration is model-free and idempotent. One lost acknowledgement permits
// one replay of exactly the same registration, never a replacement selector.
async function register<T>(work:()=>Promise<T>):Promise<T>{
 try{return await work()}catch(cause){
  if(!(cause instanceof RunRequestError)||cause.delivery!=='uncertain')throw cause
  return work()
 }
}
export class ServerContextDocument {
 private readonly registration=crypto.randomUUID()
 private client?:ContextClient
 private pending?:Promise<{document:string}>
 private retired=false
 selector(client:ContextClient):Promise<RunContextSelector>{
  if(this.retired)return Promise.reject(new RunRequestError('not-sent',undefined,'permission'))
  this.client??=client
  this.pending??=register(()=>this.client!.contextDocument(this.registration))
  return this.pending.then(value=>{
   if(this.retired)throw new RunRequestError('not-sent',undefined,'permission')
   return {...value}
  })
 }
 release():void{
  if(this.retired)return
  this.retired=true
  // An in-flight registration can still be retired after its acknowledgement.
  // This sends no run action and never allocates a document just to release it.
  if(this.pending)void this.pending.then(value=>this.client!.releaseContext(value)).catch(()=>{})
 }
 pane():ServerContextPane{return new ServerContextPane(this)}
}
export class ServerContextPane {
 private readonly registration=crypto.randomUUID()
 private purpose?:AuthoringPurpose
 private pending?:Promise<RunContextSelector>
 private client?:ContextClient
 private retired=false
 private mounts=0
 private epoch=0
 constructor(private readonly document:ServerContextDocument){}
 // Effect replay reattaches before this microtask. A real remount has a new
 // object/registration; cleanup of the prior mount cannot retire that object.
 attach():()=>void{
  if(this.retired)throw new RunRequestError('not-sent',undefined,'permission')
  this.mounts++;this.epoch++
  let detached=false
  return ()=>{
   if(detached)return;detached=true;this.mounts--
   const epoch=++this.epoch
   queueMicrotask(()=>{if(this.mounts===0&&this.epoch===epoch)this.release()})
  }
 }
 selector(client:ContextClient,purpose:AuthoringPurpose):Promise<RunContextSelector>{
  if(this.retired||this.purpose!==undefined&&this.purpose!==purpose)return Promise.reject(new RunRequestError('not-sent',undefined,'permission'))
  this.purpose??=purpose;this.client??=client
  this.pending??=this.document.selector(client).then(({document})=>{
   if(this.retired)throw new RunRequestError('not-sent',undefined,'permission')
   return register(()=>this.client!.contextPane(document,this.registration,purpose))
  })
  return this.pending.then(value=>{
   if(this.retired)throw new RunRequestError('not-sent',undefined,'permission')
   return {...value}
  })
 }
 private release(){
  if(this.retired)return
  this.retired=true
  if(this.pending)void this.pending.then(value=>this.client!.releaseContext(value)).catch(()=>{})
 }
}
let documentScope:ServerContextDocument|undefined
export function serverContextDocument():ServerContextDocument{
 if(!documentScope){
  documentScope=new ServerContextDocument()
  if(typeof window!=='undefined')window.addEventListener('pagehide',event=>{
   // A document placed in the back/forward cache remains the same mounted
   // document. No timer, heartbeat or visibility change retires it.
   if(!event.persisted)documentScope?.release()
  },{once:false})
 }
 return documentScope
}
