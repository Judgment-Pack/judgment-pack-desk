import {describe,expect,it,vi,afterEach} from 'vitest'
import {probeCapabilities,readListing} from './capabilityProbe'
import {observeSettings} from './serverRuns/settingsObservation'
vi.mock('./serverRuns/settingsObservation',()=>({observeSettings:vi.fn()}))
vi.mock('../files/client',()=>({chassisUrl:(v:string)=>v,deskFetch:(...args:unknown[])=>fetch(...args as Parameters<typeof fetch>)}))
afterEach(()=>{vi.unstubAllGlobals();vi.clearAllMocks()})
describe('what the listing shows', () => {
  it('reads the window and the output cap the common servers name, for the model asked about', () => {
    expect(readListing({ data: [{ id: 'other', max_model_len: 4096 }, { id: 'a-model', max_model_len: 32768, max_output_tokens: 4096 }] }, 'a-model')).toEqual({ contextWindow: 32768, outputReserve: 4096 })
    expect(readListing({ data: [{ id: 'a-model', context_length: 131072 }] }, 'a-model')).toEqual({ contextWindow: 131072 })
    expect(readListing({ data: [{ id: 'a-model', n_ctx: 8192 }] }, 'a-model')).toEqual({ contextWindow: 8192 })
  })

  it('holds the output cap to half the window, as the harness does', () => {
    expect(readListing({ data: [{ id: 'a-model', context_length: 8192, max_completion_tokens: 8192 }] }, 'a-model')).toEqual({ contextWindow: 8192, outputReserve: 4096 })
  })

  it('reads no number that is not a whole one in the harness\'s range, nor a cap with no window', () => {
    expect(readListing({ data: [{ id: 'a-model', max_model_len: 100 }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'a-model', max_model_len: '32768' }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'a-model', max_model_len: 5000000 }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'a-model', max_output_tokens: 4096 }] }, 'a-model')).toEqual({})
    expect(readListing({ data: [{ id: 'another', max_model_len: 32768 }] }, 'a-model')).toEqual({})
    expect(readListing(null, 'a-model')).toEqual({})
  })
})


const target={connectionId:'ai-'+ 'a'.repeat(24),connectionRevision:'b'.repeat(64)}
const outcome={kind:'capability',observeSucceeded:true,capability:{ok:true,findings:{at:'2026-10-28T12:00:00Z',facts:{nativeToolCalls:true,parallelToolCalls:false},unknown:['context window','vision','reasoning','tool-schemas','turn order']}}} as const
it('uses canonical capability observation and fixed independent metadata, preserving host timestamp',async()=>{
 vi.mocked(observeSettings).mockResolvedValue(structuredClone(outcome) as never)
 const sent=vi.fn(async(_path:string,_init?:RequestInit)=>Response.json({data:[{id:'other',context_length:99999},{id:'selected',context_length:32768,max_output_tokens:4096}]}));vi.stubGlobal('fetch',sent)
 const controller=new AbortController()
 expect(await probeCapabilities(target,'selected',controller.signal)).toEqual({ok:true,findings:{at:'2026-10-28T12:00:00Z',facts:{nativeToolCalls:true,parallelToolCalls:false,contextWindow:32768,outputReserve:4096},unknown:['vision','reasoning','tool-schemas','turn order']}})
 expect(observeSettings).toHaveBeenCalledExactlyOnceWith({kind:'capability',model:'selected'},target,controller.signal)
 expect(sent).toHaveBeenCalledExactlyOnceWith('/api/assistant/models',expect.objectContaining({method:'POST',signal:controller.signal}))
 expect(sent.mock.calls[0]?.[1]).not.toHaveProperty('body')
})
it('retains findings on metadata failure and never interprets raw generation replies',async()=>{
 vi.mocked(observeSettings).mockResolvedValue(structuredClone(outcome) as never)
 vi.stubGlobal('fetch',vi.fn(async()=>new Response('',{status:502})))
 expect(await probeCapabilities(target,'selected')).toEqual(outcome.capability)
 vi.mocked(observeSettings).mockRejectedValue(new Error('raw provider secret'))
 expect(await probeCapabilities(target,'selected')).toEqual({ok:false,problem:'unreachable'})
})
