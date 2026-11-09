import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {EndpointCapabilitiesSection} from './EndpointCapabilitiesSection'
import {draftFrom,type EndpointDraft} from './endpointDraft'
import {probeCapabilities} from './capabilityProbe'
import {AIConnectionScope,type AIConnectionScopeValue} from './aiConnections'
vi.mock('./capabilityProbe',()=>({probeCapabilities:vi.fn()}))
const draft=()=>draftFrom({engine:'vercel',thinking:'off',endpoint:{kind:'openai-compatible',url:'https://fixture.invalid/v1',model:'selected',models:['selected'],tools:[]}} as never)
const scope={connection:{id:'ai-'+ 'a'.repeat(24),revision:'b'.repeat(64)},registry:{}} as AIConnectionScopeValue
const outcome={ok:true,findings:{at:'2026-10-28T12:00:00Z',facts:{nativeToolCalls:true,parallelToolCalls:false},unknown:['context window','vision','reasoning','tool-schemas','turn order']}} as const
afterEach(()=>{cleanup();vi.clearAllMocks()})
function view(form:EndpointDraft,change:(value:EndpointDraft)=>void,value=scope){return <AIConnectionScope value={value}><EndpointCapabilitiesSection draft={form} onChange={change} canProbe whyNot="" problems={{}} /></AIConnectionScope>}
it('applies findings manually to the current draft without reverting edits made while observing',async()=>{
 let finish:((value:unknown)=>void)|undefined
 vi.mocked(probeCapabilities).mockImplementation(()=>new Promise(resolve=>{finish=resolve as never}))
 const change=vi.fn(),form=draft(),mounted=render(view(form,change))
 fireEvent.click(screen.getByRole('button',{name:'Probe the endpoint'}))
 expect(probeCapabilities).toHaveBeenCalledWith({connectionId:scope.connection.id,connectionRevision:scope.connection.revision},'selected',expect.any(AbortSignal))
 const edited={...form,override:{...form.override,contextWindow:'65536'}};mounted.rerender(view(edited,change))
 await act(async()=>finish?.(structuredClone(outcome)))
 expect(change).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({override:edited.override,probe:{at:outcome.findings.at,facts:outcome.findings.facts,of:expect.stringContaining('selected')}}))
})
it.each(['model','revision','unmount'])('aborts and discards late observations after %s, including change back',async(kind)=>{
 let finish:((value:unknown)=>void)|undefined
 vi.mocked(probeCapabilities).mockImplementation(()=>new Promise(resolve=>{finish=resolve as never}))
 const change=vi.fn(),form=draft(),mounted=render(view(form,change))
 fireEvent.click(screen.getByRole('button',{name:'Probe the endpoint'}))
 const signal=vi.mocked(probeCapabilities).mock.calls[0]![2]!
 if(kind==='unmount')mounted.unmount()
 else if(kind==='revision')mounted.rerender(view(form,change,{...scope,connection:{...scope.connection,revision:'c'.repeat(64)}}))
 else mounted.rerender(view({...form,model:'other'},change))
 expect(signal.aborted).toBe(true)
 await act(async()=>finish?.(structuredClone(outcome)))
 expect(change).not.toHaveBeenCalled()
 if(kind!=='unmount'){mounted.rerender(view(form,change));await waitFor(()=>expect(screen.queryByText(/^Probed /)).toBeNull())}
})
