import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting,forgetSession} from '../../mcp/session'
import {serverRunClient} from './client'
import {decodeSnapshot} from './decode'
import {decodeSettingsResult,observeSettings,settingsSelector} from './settingsObservation'
const target={connectionId:'ai-'+ 'a'.repeat(24),connectionRevision:'b'.repeat(64)}
const selector={kind:'ping' as const,model:'selected'}
const start=(requestId:string)=>({admissionId:`settings-observation-${requestId}`,purpose:'settings-observation' as const,ownerId:target.connectionId,revision:'c'.repeat(64),requestId,observation:selector})
const snapshot=(requestId:string,state='complete')=>({admissionId:`settings-observation-${requestId}`,id:'arun_'+ 'd'.repeat(32),owner:{kind:'settings-observation' as const,id:target.connectionId,revision:'c'.repeat(64)},requestId,revision:1,state,operations:[],events:[],cursor:'1:0'})
const result={kind:'ping',observeSucceeded:false,ping:{reachable:true,status:200,latencyMs:1,diagnostic:'',observationComplete:false},failure:{version:1,code:'output-limit',source:'model',outcome:'refused',completion:{finishReason:'length'}}}
const json=(value:unknown)=>Response.json(value)
beforeEach(()=>giveThisPageASessionForTesting('settings-test'))
afterEach(()=>vi.unstubAllGlobals())
it('uses exact reference/start/result and keeps retained reachability distinct from completion',async()=>{
 let id='';const sent=vi.fn(async(path:string,init?:RequestInit)=>{
  if(path.endsWith('/reference')){id=JSON.parse(String(init?.body)).requestId;return json({start:start(id)})}
  if(path==='/api/assistant-runs')return json(snapshot(id,'stopped'))
  return json(result)
 });vi.stubGlobal('fetch',sent)
 expect(await observeSettings(selector,target)).toEqual(result)
 expect(sent.mock.calls.map(([path])=>path)).toEqual(['/api/assistant-observations/reference','/api/assistant-runs',`/api/assistant-runs/${snapshot(id).id}/settings-observation`])
 expect(JSON.parse(String(sent.mock.calls[1]?.[1]?.body))).toEqual(start(id))
})
it('looks up uncertain admission once, preserving the same request and never replacing start',async()=>{
 let id='';const sent=vi.fn(async(path:string,init?:RequestInit)=>{
  if(path.endsWith('/reference')){id=JSON.parse(String(init?.body)).requestId;return json({start:start(id)})}
  if(path==='/api/assistant-runs')throw Error('raw secret')
  return json({})
 });vi.stubGlobal('fetch',sent)
 await expect(observeSettings(selector,target)).rejects.toMatchObject({delivery:'uncertain'})
 expect(sent.mock.calls.map(([path])=>path)).toEqual(['/api/assistant-observations/reference','/api/assistant-runs',`/api/assistant-observations/requests/${id}`])
})
it('observes one redacted stream and refuses to reconnect or fetch results after abort',async()=>{
 let id='';const controller=new AbortController();const sent=vi.fn(async(path:string,init?:RequestInit)=>{
  if(path.endsWith('/reference')){id=JSON.parse(String(init?.body)).requestId;return json({start:start(id)})}
  if(path==='/api/assistant-runs')return json(snapshot(id,'running'))
  controller.abort();return new Response('',{headers:{'Content-Type':'text/event-stream'}})
 });vi.stubGlobal('fetch',sent)
 await expect(observeSettings(selector,target,controller.signal)).rejects.toBeDefined()
 expect(sent.mock.calls.filter(([p])=>p==='/api/assistant-runs')).toHaveLength(1)
 expect(sent.mock.calls.some(([p])=>p.endsWith('/settings-observation'))).toBe(false)
})
it('does not follow a replacement session',async()=>{
 const sent=vi.fn(async(_path:string,init?:RequestInit)=>{const id=JSON.parse(String(init?.body)).requestId;forgetSession();return json({start:start(id)})});vi.stubGlobal('fetch',sent)
 await expect(observeSettings(selector,target)).rejects.toBeDefined()
 expect(sent).toHaveBeenCalledTimes(1)
})
it('closes selectors, normalized results and private snapshot members',async()=>{
 for(const value of [{...selector,kind:'future'},{...selector,prompt:'text'},{...selector,model:' '}])expect(settingsSelector(value)).toBe(false)
 for(const value of [{...result,kind:'future'},{...result,raw:'secret'},{...result,ping:{...result.ping,text:'secret'}},{...result,ping:{...result.ping,diagnostic:'future'}},{...result,failure:{...result.failure,detail:'secret'}}])expect(()=>decodeSettingsResult(value,'ping')).toThrow()
 const run=snapshot('press');for(const extra of [{checkpoint:{}},{operations:[{name:'secret'}]},{events:[{}]},{displayPreview:[]}])expect(()=>decodeSnapshot({...run,...extra},run)).toThrow()
 const client=await serverRunClient(),sent=vi.fn();vi.stubGlobal('fetch',sent)
 await expect(client.start({...start('press'),purpose:'chat'} as never,undefined,target)).rejects.toBeDefined()
 expect(()=>client.action(decodeSnapshot(run,run),{id:'next',kind:'extend-budget',revision:1})).toThrow()
 expect(sent).not.toHaveBeenCalled()
})
it('rejects reference selector substitution before start',async()=>{
 const sent=vi.fn(async(_path:string,init?:RequestInit)=>json({start:{...start(JSON.parse(String(init?.body)).requestId),observation:{kind:'ping',model:'other'}}}));vi.stubGlobal('fetch',sent)
 await expect(observeSettings(selector,target)).rejects.toBeDefined()
 expect(sent).toHaveBeenCalledTimes(1)
})
