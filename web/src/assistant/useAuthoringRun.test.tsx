import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useAuthoringRun } from './useAuthoringRun'
import { RunRequestError } from './serverRuns/client'
import type { RunSnapshot } from './serverRuns/types'
const fixture=vi.hoisted(()=>({capture:vi.fn(),reference:vi.fn(),start:vi.fn(),cancel:vi.fn(),read:vi.fn(),events:vi.fn(),endSession:()=>{}}))
vi.mock('./serverRuns/client',async original=>({...await original<typeof import('./serverRuns/client')>(),serverRunClient:async()=>({captureAuthoring:fixture.capture,authoringReference:fixture.reference,start:fixture.start,cancelAuthoring:fixture.cancel,read:fixture.read,events:fixture.events})}))
vi.mock('../mcp/session',async original=>({...await original<typeof import('../mcp/session')>(),whenSessionEnds:(fn:()=>void)=>{fixture.endSession=fn;return()=>{}}}))
const ref={admissionId:'author-admit-'+'b'.repeat(32),purpose:'describe-pack' as const,ownerId:'author_'+'a'.repeat(32),revision:'c'.repeat(64),requestId:'request-one'}
const complete=():RunSnapshot=>({admissionId:ref.admissionId,id:'arun_'+'d'.repeat(32),owner:{kind:'describe-pack',id:ref.ownerId,revision:ref.revision},requestId:ref.requestId,revision:1,state:'complete',operations:[],events:[{id:'proposal',event:{type:'proposal',document:{title:'Retained'},unknowns:[]}},{id:'end',event:{type:'end'}}],cursor:'1:2'})
const options={target:{connectionId:'selected-ai',connectionRevision:'e'.repeat(64)},model:'chosen-model',engine:'vercel' as const,thinking:'off' as const}
const input={purpose:'describe-pack' as const,prompt:'author_pack',arguments:{policy:'Exact description\n<&> 한글'}}
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return{promise,resolve}}
beforeEach(()=>{vi.clearAllMocks();fixture.capture.mockResolvedValue({start:ref});fixture.start.mockResolvedValue(complete());fixture.cancel.mockResolvedValue({start:ref,cancelled:true});fixture.reference.mockResolvedValue({start:ref,run:complete()});fixture.read.mockResolvedValue(complete());fixture.events.mockImplementation(async function*(){})})
afterEach(cleanup)
it('sends closed request data, selected controls and host-issued reference once, then projects frozen canonical events',async()=>{
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input))
 await waitFor(()=>expect(result.current.status).toBe('finished'))
 expect(fixture.capture).toHaveBeenCalledOnce();const [request,target]=fixture.capture.mock.calls[0]
 expect(request).toMatchObject({...input,model:'chosen-model',thinking:'off',phase:'work'});expect(request).not.toHaveProperty('tools');expect(request).not.toHaveProperty('endpoint');expect(target).toEqual(options.target)
 expect(fixture.start).toHaveBeenCalledExactlyOnceWith(ref,undefined)
 expect(result.current.events.map(e=>e.type)).toEqual(['proposal','end']);expect(result.current.terminals.count).toBe(1)
 const proposal=result.current.events[0];expect(Object.isFrozen(proposal)).toBe(true)
})
it('ingests a proposal document once and freezes its detached snapshot before review',async()=>{
 let reads=0
 const nested={facts:{literal:'50',flag:false,zero:0,missing:null}}
 const retained:RunSnapshot={...complete(),events:[{id:'proposal',event:{type:'proposal',get document(){reads++;return{title:`Read ${reads}`,nested}},unknowns:[]}}]}
 fixture.start.mockResolvedValue(retained)
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input))
 await waitFor(()=>expect(result.current.status).toBe('finished'))
 const event=result.current.events[0]
 expect(event.type).toBe('proposal');if(event.type!=='proposal')throw Error('proposal required')
 expect(reads).toBe(1);expect(event.document).toEqual({title:'Read 1',nested})
 expect(Object.isFrozen(event.document)).toBe(true)
 expect(Object.isFrozen((event.document as {nested:typeof nested}).nested.facts)).toBe(true)
 nested.facts.literal='changed upstream'
 expect((event.document as {nested:typeof nested}).nested.facts.literal).toBe('50')
})
it('reconciles a lost capture acknowledgement before its sole start',async()=>{
 fixture.capture.mockRejectedValue(new RunRequestError('uncertain'));fixture.reference.mockResolvedValue({start:ref})
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(result.current.status).toBe('finished'))
 expect(fixture.reference).toHaveBeenCalledOnce();expect(fixture.start).toHaveBeenCalledOnce()
})
it('never resends start after a lost admission acknowledgement, including a missing retained run',async()=>{
 fixture.start.mockRejectedValue(new RunRequestError('uncertain'));fixture.reference.mockResolvedValue({start:ref})
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(result.current.status).toBe('finished'))
 expect(fixture.start).toHaveBeenCalledOnce();expect(result.current.failure).toBeTruthy();expect(result.current.events.some(e=>e.type==='proposal')).toBe(false)
})
it('Stop during capture cancels the exact returned reservation and never starts',async()=>{
 const capture=deferred<{start:typeof ref}>();fixture.capture.mockReturnValue(capture.promise)
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(fixture.capture).toHaveBeenCalledOnce());act(()=>result.current.stop())
 await act(async()=>capture.resolve({start:ref}));await waitFor(()=>expect(fixture.cancel).toHaveBeenCalledExactlyOnceWith(ref))
 expect(fixture.start).not.toHaveBeenCalled();expect(result.current.terminals.count).toBe(1)
})
it('Stop racing an uncertain start cancels only the captured owner and retains no actionable proposal',async()=>{
 const admission=deferred<RunSnapshot>();fixture.start.mockReturnValue(admission.promise)
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(fixture.start).toHaveBeenCalledOnce());act(()=>result.current.stop())
 await act(async()=>admission.resolve(complete()));expect(fixture.cancel).toHaveBeenCalledWith(ref);expect(result.current.events.some(e=>e.type==='proposal')).toBe(false);expect(result.current.failure).toBeTruthy()
})
it('route unmount detaches without canceling or replaying host execution',async()=>{
 const pending=deferred<void>();fixture.start.mockResolvedValue({...complete(),state:'running',events:[],cursor:'1:0'});fixture.events.mockImplementation(async function*(){await pending.promise})
 const {result,unmount}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(fixture.events).toHaveBeenCalledOnce());unmount();pending.resolve()
 expect(fixture.cancel).not.toHaveBeenCalled();expect(fixture.start).toHaveBeenCalledOnce()
})
it('a stopped/interrupted/historical read never offers its retained proposal for Apply',async()=>{
 fixture.start.mockResolvedValue({...complete(),state:'interrupted',historical:true})
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(result.current.status).toBe('finished'));expect(result.current.failure).toBeTruthy()
})
it('session termination withdraws a finished proposal',async()=>{
 const {result}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(result.current.status).toBe('finished'));act(()=>fixture.endSession());expect(result.current.failure).toBeTruthy()
})
it('a paused question or Plan never grants Apply authority from a retained proposal',async()=>{
 fixture.start.mockResolvedValue({...complete(),state:'awaiting-input'})
 const {result,unmount}=renderHook(()=>useAuthoringRun(options));act(()=>result.current.start(input));await waitFor(()=>expect(result.current.status).toBe('finished'));expect(result.current.failure).toBeTruthy();unmount()
 fixture.start.mockResolvedValue(complete());const plan=renderHook(()=>useAuthoringRun(options));act(()=>plan.result.current.start({...input,phase:'plan'}));await waitFor(()=>expect(plan.result.current.status).toBe('finished'));expect(plan.result.current.failure).toContain('Plan')
})
