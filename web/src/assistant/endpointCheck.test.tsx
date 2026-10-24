import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useEndpointCheck } from './endpointCheck'
import { probeAssistantEndpoint } from './client'
import type { EndpointDraft } from './endpointDraft'
import type { AssistantEndpointConfig } from '../config/deskConfig'

const fixture = vi.hoisted(() => ({ revision: 'saved-one', signals: [] as AbortSignal[], resolve: undefined as undefined | ((rows: {id:string;label:string}[])=>void) }))
vi.mock('./aiConnections', () => ({useAIConnectionScope:()=>({connection:{id:'ai-fixture',revision:fixture.revision}})}))
vi.mock('./modelListing', () => ({listModels:vi.fn((_kind, _id, _revision, signal:AbortSignal)=>{
 fixture.signals.push(signal)
 return new Promise(resolve=>{fixture.resolve=resolve})
})}))
vi.mock('./client', () => ({DIAGNOSTIC_SAYS:{},probeAssistantEndpoint:vi.fn(async()=>({reachable:true,status:200,latencyMs:1,diagnostic:''}))}))
const saved = {kind:'openai-compatible',url:'https://fixture.invalid',model:'fixture'} as AssistantEndpointConfig
const draft = {...saved} as unknown as EndpointDraft
afterEach(()=>{vi.clearAllMocks();fixture.revision='saved-one';fixture.signals=[];fixture.resolve=undefined})

it('cancels and discards a late listing when the form changes, including change back', async()=>{
 const {result,rerender,unmount}=renderHook(({form})=>useEndpointCheck(form,saved),{initialProps:{form:draft}})
 act(()=>result.current.run())
 await waitFor(()=>expect(fixture.signals).toHaveLength(1))
 expect(probeAssistantEndpoint).toHaveBeenLastCalledWith(fixture.signals[0], 'ai-fixture', 'saved-one')
 rerender({form:{...draft,url:'https://changed.invalid'}})
 expect(fixture.signals[0]?.aborted).toBe(true)
 await act(async()=>fixture.resolve?.([{id:'late',label:'Late'}]))
 rerender({form:draft})
 expect(result.current.answer).toBeUndefined()
 unmount()
})

it('drops observations and cancels on saved connection revision changes', async()=>{
 const {result,rerender,unmount}=renderHook(()=>useEndpointCheck(draft,saved))
 act(()=>result.current.run())
 await waitFor(()=>expect(fixture.signals).toHaveLength(1))
 expect(probeAssistantEndpoint).toHaveBeenLastCalledWith(fixture.signals[0], 'ai-fixture', 'saved-one')
 fixture.revision='saved-two'
 rerender()
 expect(fixture.signals[0]?.aborted).toBe(true)
 await act(async()=>fixture.resolve?.([{id:'old',label:'Old'}]))
 expect(result.current.answer).toBeUndefined()
 act(()=>result.current.run())
 expect(fixture.signals).toHaveLength(2)
 unmount()
 expect(fixture.signals[1]?.aborted).toBe(true)
})
