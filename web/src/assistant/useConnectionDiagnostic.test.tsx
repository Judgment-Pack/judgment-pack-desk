import {act,cleanup,renderHook,waitFor} from '@testing-library/react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting,forgetSession} from '../mcp/session'
import {useConnectionDiagnostic} from './useConnectionDiagnostic'
import pending from './serverRuns/fixtures/diagnostic.json'
const target={connectionId:pending.owner.id,connectionRevision:'b'.repeat(64)}
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
const complete={...pending,state:'complete'}
beforeEach(()=>giveThisPageASessionForTesting('diagnostic-session'))
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
function fixture(options:{lost?:boolean;missing?:boolean;result?:unknown;hold?:boolean;captureHold?:boolean}={}){
 const calls:{path:string;body?:Record<string,unknown>}[]=[]
 let finish:()=>void=()=>{}
 vi.stubGlobal('fetch',async(path:string,init:RequestInit={})=>{
  const body=init.body?JSON.parse(String(init.body)):undefined;calls.push({path,body})
  const requestId=body?.requestId??String(calls.find(c=>c.path.endsWith('/reference'))?.body?.requestId)
  const start={admissionId:`diagnostic-${requestId}`,purpose:'connection-diagnostic',ownerId:pending.owner.id,revision:pending.owner.revision,requestId}
  const view={...complete,requestId,admissionId:start.admissionId}
  if(path.endsWith('/reference')){if(options.captureHold)await new Promise<void>(resolve=>{finish=resolve});return json({start})}
  if(path==='/api/assistant-runs'){
   if(options.hold)await new Promise<void>(resolve=>{finish=resolve})
   if(options.lost)throw new TypeError('lost acknowledgement')
   return json(view,201)
  }
  if(path.includes('/requests/'))return json(options.missing?{}:{run:view})
  if(path.endsWith('/connection-diagnostic'))return json(options.result??{succeeded:true})
  return json({...view,state:'stopped'})
 })
 const hook=renderHook(({identity})=>useConnectionDiagnostic(target,identity),{initialProps:{identity:'saved-one'}})
 return {...hook,calls,finish:()=>finish()}
}
it('uses only the fixed host reference and canonical run/result, without any browser prompt or model',async()=>{
 const f=fixture();act(()=>f.result.current.start());await waitFor(()=>expect(f.result.current.tested).toBe(true))
 expect(f.calls.map(c=>c.path)).toEqual(['/api/assistant-diagnostics/reference','/api/assistant-runs',`/api/assistant-runs/${pending.id}/connection-diagnostic`])
 expect(Object.keys(f.calls[0].body!)).toEqual(['requestId'])
 expect(Object.keys(f.calls[1].body!)).toEqual(['admissionId','purpose','ownerId','revision','requestId'])
 expect(f.result.current.status).toBe('finished')
})
it('reads a lost admission acknowledgement exactly once without a duplicate start',async()=>{
 const f=fixture({lost:true});act(()=>f.result.current.start());await waitFor(()=>expect(f.result.current.tested).toBe(true))
 expect(f.calls.filter(c=>c.path==='/api/assistant-runs')).toHaveLength(1);expect(f.calls.filter(c=>c.path.includes('/requests/'))).toHaveLength(1)
})
it('keeps missing uncertain admission unresolved without another dispatch',async()=>{
 const f=fixture({lost:true,missing:true});act(()=>f.result.current.start());await waitFor(()=>expect(f.result.current.status).toBe('finished'))
 expect(f.result.current.tested).toBe(false);expect(f.result.current.canStart).toBe(false)
 act(()=>f.result.current.start());expect(f.calls.filter(c=>c.path==='/api/assistant-runs')).toHaveLength(1)
})
it.each([{succeeded:false},{succeeded:'yes'},{message:'Connected'},{succeeded:true,proposal:{}}])('requires the exact host schema evidence response (%j)',async result=>{
 const f=fixture({result});act(()=>f.result.current.start());await waitFor(()=>expect(f.result.current.status).toBe('finished'));expect(f.result.current.tested).toBe(false)
})
it('withdraws success on each form/config/account/project revision and does not revive it when reverted',async()=>{
 const f=fixture();act(()=>f.result.current.start());await waitFor(()=>expect(f.result.current.tested).toBe(true))
 f.rerender({identity:'changed'});expect(f.result.current.tested).toBe(false);f.rerender({identity:'saved-one'});expect(f.result.current.tested).toBe(false)
})
it('stops an in-flight admission by cancelling its exact returned owner before accepting any success',async()=>{
 const f=fixture({hold:true});act(()=>f.result.current.start());await waitFor(()=>expect(f.calls.some(c=>c.path==='/api/assistant-runs')).toBe(true))
 act(()=>f.result.current.stop());act(()=>f.finish());await waitFor(()=>expect(f.calls.some(c=>c.path===`/api/assistant-runs/${pending.id}`)).toBe(true))
 expect(f.result.current.tested).toBe(false);expect(f.calls.some(c=>c.path.endsWith('/connection-diagnostic'))).toBe(false)
})
it('stops during model-free capture without starting work',async()=>{
 const f=fixture({captureHold:true});act(()=>f.result.current.start());await waitFor(()=>expect(f.calls).toHaveLength(1))
 act(()=>f.result.current.stop());act(()=>f.finish());await act(async()=>{})
 expect(f.calls).toHaveLength(1);expect(f.result.current.tested).toBe(false)
})
it('detaches on unmount without hidden cancellation or another start',async()=>{
 const f=fixture({hold:true});act(()=>f.result.current.start());await waitFor(()=>expect(f.calls).toHaveLength(2))
 f.unmount();act(()=>f.finish());await act(async()=>{});expect(f.calls).toHaveLength(2)
})

it('withdraws success on logout and cannot adopt observations into a replacement session',async()=>{
 const f=fixture();act(()=>f.result.current.start());await waitFor(()=>expect(f.result.current.tested).toBe(true))
 act(()=>forgetSession('signed out'));expect(f.result.current.tested).toBe(false);expect(f.result.current.canStart).toBe(false)
 giveThisPageASessionForTesting('replacement');act(()=>f.result.current.start());expect(f.calls.filter(c=>c.path==='/api/assistant-runs')).toHaveLength(1)
})
