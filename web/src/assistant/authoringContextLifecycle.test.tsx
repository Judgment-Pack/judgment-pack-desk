import {act,cleanup,renderHook,waitFor} from '@testing-library/react'
import {StrictMode,type ReactNode} from 'react'
import {afterEach,expect,it,vi} from 'vitest'
import {useAuthoringRun} from './useAuthoringRun'
import type {RunSnapshot,RunStart} from './serverRuns/types'
const f=vi.hoisted(()=>({capture:vi.fn(),start:vi.fn(),reference:vi.fn(),cancel:vi.fn(),document:vi.fn(),pane:vi.fn(),release:vi.fn()}))
vi.mock('./serverRuns/client',async original=>({...await original<typeof import('./serverRuns/client')>(),serverRunClient:async()=>({captureAuthoring:f.capture,start:f.start,authoringReference:f.reference,cancelAuthoring:f.cancel,contextDocument:f.document,contextPane:f.pane,releaseContext:f.release,events:async function*(){}})}))
afterEach(cleanup)
const start:RunStart={admissionId:'author-admit-'+'b'.repeat(32),purpose:'describe-pack',ownerId:'author_'+'a'.repeat(32),revision:'c'.repeat(64),requestId:'request-one'}
const complete:RunSnapshot={admissionId:start.admissionId,id:'arun_'+'d'.repeat(32),owner:{kind:'describe-pack',id:start.ownerId,revision:start.revision},requestId:start.requestId,revision:1,state:'complete',operations:[],events:[],cursor:'1:0'}
const options={target:{connectionId:'selected-ai',connectionRevision:'e'.repeat(64)},model:'chosen-model',engine:'vercel' as const,thinking:'off' as const}
const input={purpose:'describe-pack' as const,prompt:'author_pack',arguments:{policy:'Exact immutable request'}}
it('actual hook effect replay keeps one pane; real remount is distinct; recovered admission needs no new registration',async()=>{
 let panes=0
 f.document.mockResolvedValue({document:'a'.repeat(64)})
 f.pane.mockImplementation(async(document:string)=>({document,pane:String(++panes).padStart(64,'0')}))
 f.release.mockResolvedValue(undefined);f.capture.mockResolvedValue({start});f.start.mockResolvedValue(complete)
 const wrapper=({children}:{children:ReactNode})=><StrictMode>{children}</StrictMode>
 const first=renderHook(()=>useAuthoringRun(options),{wrapper})
 act(()=>first.result.current.start(input));await waitFor(()=>expect(first.result.current.status).toBe('finished'))
 expect(f.document).toHaveBeenCalledOnce();expect(f.pane).toHaveBeenCalledOnce()
 expect(f.capture.mock.calls[0][0]).not.toHaveProperty('context')
 const firstStart=f.start.mock.calls[0][0]
 expect(firstStart).toEqual({...start,context:{document:'a'.repeat(64),pane:'1'.padStart(64,'0')}})
 first.unmount();await waitFor(()=>expect(f.release).toHaveBeenCalledOnce());expect(f.cancel).not.toHaveBeenCalled()
 const second=renderHook(()=>useAuthoringRun(options),{wrapper})
 act(()=>second.result.current.start(input));await waitFor(()=>expect(second.result.current.status).toBe('finished'))
 expect(f.document).toHaveBeenCalledOnce();expect(f.pane).toHaveBeenCalledTimes(2)
 expect(f.start.mock.calls[1][0].context).not.toEqual(firstStart.context)
 second.unmount();await waitFor(()=>expect(f.release).toHaveBeenCalledTimes(2))
 f.capture.mockResolvedValue({start,run:complete})
 const recovered=renderHook(()=>useAuthoringRun(options),{wrapper})
 act(()=>recovered.result.current.start(input));await waitFor(()=>expect(recovered.result.current.status).toBe('finished'))
 expect(f.pane).toHaveBeenCalledTimes(2);expect(f.start).toHaveBeenCalledTimes(2)
})
