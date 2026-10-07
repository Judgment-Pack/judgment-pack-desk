import tavilyMark from './assets/tavily-mark-black.svg'
import { StrictMode, useCallback, useState, type ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { testQueryClient } from '../testing/harness'
import { WorkSummary } from '../chat/RunPresentation'
import type { WorkRecord } from '../chat/responseHistory'
import { DetailsSlotContext } from '../shell/DetailsSlot'
import { SearchSources } from './SearchSources'
import type { SearchReference } from './results'
const load=vi.hoisted(()=>vi.fn())
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{research:{gateway:{url:'http://localhost:9876',authority:'test',signer:{algorithm:'ed25519',public:'ab'.repeat(32)}}}}})}))
vi.mock('./results',async original=>({...await original<typeof import('./results')>(),loadSearch:load}))
vi.mock('./AttributionFrame',()=>({AttributionFrame:()=> <iframe title="Search provider attribution"/>}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
const reference:SearchReference={id:'12345678-1234-1234-1234-123456789012',digest:'sha256:'+'a'.repeat(64),request:{connection:'google',revision:'b'.repeat(64),query:'NASA missions',maxResults:5}}
const result={version:1,provider:'google-grounding',query:'NASA missions',retrievedAt:'2026-10-06T12:00:00Z',kind:'grounded-answer',hits:[{title:'NASA',url:'https://www.nasa.gov/',snippet:'Official mission information'}],queries:['NASA official mission updates'],attributionHtml:'<div>Google</div>'}
function Harness({work}:{work:WorkRecord}){
 const [node,setNode]=useState<ReactNode>(null)
 const onRead=useCallback((next:ReactNode)=>setNode(next),[])
 return <DetailsSlotContext value={{target:null,open:!!node,claim:()=>()=>{},reveal:()=>{},dismissInspection:()=>setNode(null)}}><WorkSummary work={work} onRead={onRead}/><aside>{node}</aside></DetailsSlotContext>
}
const wrap=(node:ReactNode)=> <StrictMode><QueryClientProvider client={testQueryClient()}>{node}</QueryClientProvider></StrictMode>
it('opens exact per-call details with provider marks and keeps attribution out of the transcript',async()=>{
 load.mockResolvedValue({reference,result})
 const work:WorkRecord={items:[
  {id:'a',name:'search_sources',status:'failed',failure:'search-timeout',search:{query:'failed Tavily query',provider:'tavily',submitted:true}},
  {id:'b',name:'search_sources',status:'complete',search:{query:reference.request.query,provider:'google-grounding',submitted:true,reference}},
  {id:'old',name:'search_sources',status:'complete'}
 ],notices:[]}
 const view=render(wrap(<Harness work={work}/>))
 expect(screen.queryByTitle('Search provider attribution')).toBeNull()
 expect(load).not.toHaveBeenCalled()
 expect(view.container.querySelector('details')?.open).toBe(false)
 fireEvent.click(screen.getByText('Work · 3 steps'))
 const failed=screen.getByRole('button',{name:'Search sources · Tavily · Failed'})
 expect(failed.querySelector('img')?.getAttribute('src')).toBe(tavilyMark)
 fireEvent.click(failed)
 await screen.findByText('failed Tavily query')
 await waitFor(()=>expect(failed.getAttribute('aria-pressed')).toBe('true'))
 expect(screen.getByText(/took too long/).closest('aside')).not.toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Search sources · Google Search · Done'}))
 await screen.findByText('NASA official mission updates')
 expect(screen.getByText('Query sent')).toBeTruthy()
 expect(screen.getByTitle('Search provider attribution').closest('aside')).not.toBeNull()
 const readsBeforeInspecting=load.mock.calls.length
 expect(screen.getByRole('link',{name:'NASA Open source'}).getAttribute('href')).toBe('https://www.nasa.gov/')
 expect(screen.queryByText('failed Tavily query')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Search sources · Web search · Done'}))
 await screen.findByText('Details were not recorded for this step.')
 expect(view.container.querySelector('aside')?.textContent).not.toContain('NASA missions')
 fireEvent.keyDown(within(view.container.querySelector('aside')!).getByRole('region'),{key:'Escape'})
 await waitFor(()=>expect(view.container.querySelector('button[aria-pressed="true"]')).toBeNull())
 // Opening several inspectors reads the same retained artifact; it never starts another search.
 expect(load).toHaveBeenCalledTimes(readsBeforeInspecting)
})
it('updates the open pane when its in-flight call fails',async()=>{
 load.mockResolvedValue({reference,result})
 const pending:WorkRecord={items:[{id:'a',name:'search_sources',status:'working',search:{query:'pending',provider:'google-grounding'}}],notices:[]}
 const client=testQueryClient()
 const content=(work:WorkRecord)=><StrictMode><QueryClientProvider client={client}><Harness work={work}/></QueryClientProvider></StrictMode>
 const view=render(content(pending));fireEvent.click(screen.getByText('Work · 1 step'))
 const working=screen.getByRole('button',{name:'Search sources · Google Search · Working…'})
 const icon=working.querySelector('img')!.getAttribute('src')
 expect(icon).toBeTruthy()
 fireEvent.click(working)
 await screen.findByText('Requested query')
 view.rerender(content({items:[{...pending.items[0]!,status:'failed',failure:'search-timeout',search:{query:'pending',provider:'google-grounding',submitted:true}}],notices:[]}))
 await screen.findByText(/took too long/)
 expect(screen.getByText('Query sent')).toBeTruthy()
 expect(screen.getByRole('button',{name:'Search sources · Google Search · Failed'}).getAttribute('aria-pressed')).toBe('true')
 expect(screen.getByRole('button',{name:'Search sources · Google Search · Failed'}).querySelector('img')!.getAttribute('src')).toBe(icon)
})
it('does not expose result links or provider-supplied queries when verification fails',async()=>{
 load.mockRejectedValue(new Error('signature mismatch'))
 render(wrap(<SearchSources reference={reference}/>))
 await screen.findByRole('alert')
 expect(screen.queryByRole('link')).toBeNull()
 expect(screen.queryByText('Google queries')).toBeNull()
})
