// Frontend lifecycle fixtures only. No real writer, quota or production POST claim.
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {focusManager,onlineManager} from '@tanstack/react-query'
import {SavedChatRun} from './controller'
import type {ServerRunClient} from './client'
import type {DialectReadRecoveryView,RunSnapshot} from './types'
import type {Chat} from '../../chat/store'

const snapshot:RunSnapshot={id:'arun_'+'f'.repeat(32),revision:1,admissionId:'saved-admission',requestId:'saved-message',owner:{kind:'chat',id:'saved-chat',revision:'a'.repeat(64)},state:'failed',operations:[{id:'old-read',callId:'read-call',kind:'host',name:'read_dialect',intent:'{}',state:'uncertain',result:null}],events:[],cursor:'1:0'}
const chat:Chat={id:'saved-chat',title:'Retained work',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:'Unsent text',model:'model-one',mode:'draft',view:'chat',serverRequest:{requestId:snapshot.requestId,admissionId:snapshot.admissionId}}
const view=(recoveryRevision='1',backingState:'present-valid'|'absent'='present-valid'):DialectReadRecoveryView=>({runId:snapshot.id,runRevision:'9007199254740993',recoveryRevision,reads:[{operationId:'old-read',originalOutcome:'interrupted',backingState,ordinal:recoveryRevision,storageState:'unresolved',canInspect:true}]})
function deferred<T>(){let resolve!:(value:T)=>void,reject!:(cause:unknown)=>void;const promise=new Promise<T>((yes,no)=>{resolve=yes;reject=no});return {promise,resolve,reject}}
const owners:SavedChatRun[]=[]
function fixture(initial=view()){
 const get=vi.fn().mockResolvedValue(initial),post=vi.fn(),discover=vi.fn().mockResolvedValue([snapshot]),start=vi.fn(),action=vi.fn(),prepare=vi.fn(),reconcile=vi.fn(async()=>chat)
 const client={dialectReadRecovery:get,observeDialectRead:post,discover,start,action} as unknown as ServerRunClient
 const run=new SavedChatRun(chat.id,chat,{project:'/fixture-project',client:async()=>client,prepare,reconcile,pin:vi.fn(),problem:vi.fn()})
 owners.push(run)
 return {run,get,post,discover,start,action,prepare}
}
async function opened(h:ReturnType<typeof fixture>,revision='1'){
 await h.run.reconnect()
 await vi.waitFor(()=>expect(h.run.dialectReadRecovery?.recoveryRevision).toBe(revision))
}
beforeEach(()=>{
 focusManager.setFocused(true);onlineManager.setOnline(true)
 vi.stubGlobal('fetch',vi.fn().mockRejectedValue(Error('unexpected transport in lifecycle fixture')))
})
afterEach(()=>{
 owners.splice(0).forEach(run=>run.endSession())
 focusManager.setFocused(undefined);onlineManager.setOnline(true)
 vi.useRealTimers();vi.restoreAllMocks();vi.unstubAllGlobals()
})
it('retires inspection busy ownership on disconnect and ignores old completion during a newer explicit inspection',async()=>{
 const h=fixture(),old=deferred<DialectReadRecoveryView>(),next=deferred<DialectReadRecoveryView>()
 h.post.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
 await opened(h)
 const first=h.run.inspectDialectRead('old-read')
 await vi.waitFor(()=>expect(h.post).toHaveBeenCalledOnce())
 expect(h.run.dialectReadRecoveryBusy).toBe(true)
 h.run.disconnect()
 expect(h.run.dialectReadRecoveryBusy).toBe(false)
 expect(h.run.dialectReadRecovery).toEqual(view())
 h.get.mockResolvedValue(view('2'))
 await opened(h,'2')
 expect(h.post).toHaveBeenCalledOnce()
 const second=h.run.inspectDialectRead('old-read')
 await vi.waitFor(()=>expect(h.post).toHaveBeenCalledTimes(2))
 old.resolve(view('3','absent'));await first
 expect(h.run.dialectReadRecoveryBusy).toBe(true)
 expect(h.run.dialectReadRecovery).toEqual(view('2'))
 next.resolve(view('4'));await second
 expect(h.run.dialectReadRecoveryBusy).toBe(false)
 expect(h.run.dialectReadRecovery).toEqual(view('4'))
 expect(h.prepare).not.toHaveBeenCalled();expect(h.start).not.toHaveBeenCalled();expect(h.action).not.toHaveBeenCalled()
})
it('never lets a pending saved GET overwrite a newer POST with lossless uint64 identities',async()=>{
 const h=fixture(view('9007199254740993')),stale=deferred<DialectReadRecoveryView>()
 await opened(h,'9007199254740993')
 h.get.mockReturnValueOnce(stale.promise)
 const refresh=h.run.refreshDialectReadRecovery()
 await vi.waitFor(()=>expect(h.get).toHaveBeenCalledTimes(2))
 h.post.mockResolvedValue(view('9007199254740995'))
 await h.run.inspectDialectRead('old-read')
 stale.resolve(view('9007199254740994','absent'));await refresh
 expect(h.run.dialectReadRecovery).toEqual(view('9007199254740995'))
 expect(h.post).toHaveBeenCalledOnce()
})
it('never regresses when an older GET resolves after a newer invalidated GET',async()=>{
 const h=fixture(),old=deferred<DialectReadRecoveryView>(),next=deferred<DialectReadRecoveryView>()
 await opened(h)
 h.get.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
 const first=h.run.refreshDialectReadRecovery()
 await vi.waitFor(()=>expect(h.get).toHaveBeenCalledTimes(2))
 const second=h.run.refreshDialectReadRecovery()
 await vi.waitFor(()=>expect(h.get).toHaveBeenCalledTimes(3))
 next.resolve(view('18446744073709551615'));await second
 old.resolve(view('9007199254740993','absent'));await first
 expect(h.run.dialectReadRecovery).toEqual(view('18446744073709551615'))
 expect(h.post).not.toHaveBeenCalled()
})
it('refreshes separate saved status on invalidation focus and reconnect without changing or replaying the run',async()=>{
 const h=fixture();await opened(h)
 const turns=h.run.getSnapshot().turns
 h.get.mockResolvedValue(view('2'))
 await h.run.refreshDialectReadRecovery()
 expect(h.run.dialectReadRecovery?.recoveryRevision).toBe('2')
 h.get.mockResolvedValue(view('3'))
 focusManager.setFocused(false);focusManager.setFocused(true)
 await vi.waitFor(()=>expect(h.run.dialectReadRecovery?.recoveryRevision).toBe('3'))
 h.get.mockResolvedValue(view('4'))
 onlineManager.setOnline(false);onlineManager.setOnline(true)
 await vi.waitFor(()=>expect(h.run.dialectReadRecovery?.recoveryRevision).toBe('4'))
 expect(h.run.getSnapshot().turns).toEqual(turns)
 expect(h.discover).toHaveBeenCalledOnce()
 expect(h.post).not.toHaveBeenCalled();expect(h.prepare).not.toHaveBeenCalled();expect(h.start).not.toHaveBeenCalled();expect(h.action).not.toHaveBeenCalled()
})
it('retains last successful rows on saved-status failure and clears the error after focus refresh',async()=>{
 const h=fixture();await opened(h)
 h.get.mockRejectedValueOnce(Error('saved status unavailable'))
 await h.run.refreshDialectReadRecovery()
 await vi.waitFor(()=>expect(h.run.dialectReadRecoveryFailed).toBe(true))
 expect(h.run.dialectReadRecovery).toEqual(view())
 h.get.mockResolvedValue(view('2'))
 focusManager.setFocused(false);focusManager.setFocused(true)
 await vi.waitFor(()=>expect(h.run.dialectReadRecovery?.recoveryRevision).toBe('2'))
 expect(h.run.dialectReadRecoveryFailed).toBe(false)
 expect(h.post).not.toHaveBeenCalled()
})
it('does not add polling while saved status remains mounted and focused',async()=>{
 const h=fixture();await opened(h)
 vi.useFakeTimers()
 await vi.advanceTimersByTimeAsync(60000)
 expect(h.get).toHaveBeenCalledOnce();expect(h.post).not.toHaveBeenCalled();expect(h.discover).toHaveBeenCalledOnce()
})
it('retires the private resource on session end and ignores a late saved GET',async()=>{
 const h=fixture(),late=deferred<DialectReadRecoveryView>();await opened(h)
 h.get.mockReturnValueOnce(late.promise)
 const refresh=h.run.refreshDialectReadRecovery()
 await vi.waitFor(()=>expect(h.get).toHaveBeenCalledTimes(2))
 h.run.endSession();late.resolve(view('2'));await refresh
 focusManager.setFocused(false);focusManager.setFocused(true)
 expect(h.run.dialectReadRecovery).toBeUndefined();expect(h.run.dialectReadRecoveryBusy).toBe(false)
 expect(h.get).toHaveBeenCalledTimes(2);expect(h.post).not.toHaveBeenCalled()
})
