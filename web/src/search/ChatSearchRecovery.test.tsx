import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { testQueryClient } from '../testing/harness'
import type { Chat } from '../chat/store'
import type { WorkItem } from '../chat/responseHistory'
import { ChatSearchRecovery } from './ChatSearchRecovery'
const m = vi.hoisted(() => ({
 status: {available:true,loading:false,isError:false,data:{version:1,providers:[{id:'tavily',name:'Tavily',fields:['api-key'],docs:'https://docs.tavily.com'}],connections:[] as Record<string,unknown>[]},refetch:vi.fn()},
 preference:{data:{value:{mode:'auto',connection:null as string|null}},isError:false},
 call:vi.fn(async()=>({saved:true})),change:vi.fn(),resume:vi.fn()
}))
vi.mock('./connections',()=>({SEARCH_CONNECTIONS_KEY:['web-search-connections'],useSearchConnections:()=>m.status,useSearchPreference:()=>m.preference}))
vi.mock('../connections/client',async original=>({...await original<typeof import('../connections/client')>(),connectionCall:m.call}))
const saved = {id:'public',revision:'ab'.repeat(32),name:'Public sources',provider:'tavily',dailyLimit:100}
const failure:WorkItem={id:'lookup',name:'search_sources',status:'failed',failure:'setup-required',search:{query:'JPS launch'}}
const chat={id:'chat',composer:'Keep this unsent draft'} as Chat
const view=(item=failure,c=chat)=><MemoryRouter><QueryClientProvider client={testQueryClient()}><ChatSearchRecovery chat={c} failure={item} disabled={false} onChange={m.change} onContinue={m.resume}/></QueryClientProvider></MemoryRouter>
beforeEach(()=>{m.status.available=true;m.status.loading=false;m.status.isError=false;m.status.data.connections=[];m.preference.data.value.mode='auto';m.preference.data.value.connection=null;m.preference.isError=false})
afterEach(()=>{cleanup();vi.clearAllMocks()})
it('opens the shared credential editor in place and selects only the new search connection for this chat',async()=>{
 render(view())
 fireEvent.click(screen.getByRole('button',{name:'Set up web search'}))
 expect(screen.getByRole('dialog',{name:'Web search'})).toBeTruthy()
 fireEvent.change(screen.getByLabelText('Name'),{target:{value:'Public research'}})
 fireEvent.change(screen.getByLabelText('API key'),{target:{value:'test-only-secret'}})
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await waitFor(()=>expect(m.change).toHaveBeenCalledWith({searchConnection:expect.stringMatching(/^search-/)}))
 expect(m.call).toHaveBeenCalledWith('configure',expect.objectContaining({provider:'tavily',credential:'test-only-secret'}),undefined,'web-search')
 expect(m.change.mock.calls[0]?.[0]).not.toHaveProperty('composer')
 expect(JSON.stringify(m.change.mock.calls)).not.toContain('test-only-secret')
 expect(m.resume).not.toHaveBeenCalled()
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(document.body.textContent).not.toContain('test-only-secret')
})
it('offers existing connections without asking for another key and continues only on an explicit click',async()=>{
 m.status.data.connections=[saved]
 const result=render(view())
 fireEvent.click(screen.getByRole('button',{name:'Set up web search'}))
 fireEvent.click(screen.getByRole('button',{name:'Use Public sources'}))
 expect(m.change).toHaveBeenCalledWith({searchConnection:'public'})
 expect(m.call).not.toHaveBeenCalled();expect(m.resume).not.toHaveBeenCalled()
 result.rerender(view(failure,{...chat,searchConnection:'public'}))
 fireEvent.click(screen.getByRole('button',{name:'Continue request'}))
 expect(m.resume).toHaveBeenCalledExactlyOnceWith('JPS launch')
})
it('shows connection repair for authentication failures rather than replaying the failed request',()=>{
 m.status.data.connections=[saved]
 render(view({...failure,failure:'credentials-required'},{...chat,searchConnection:'public'}))
 fireEvent.click(screen.getByRole('button',{name:'Check connection'}))
 expect(screen.getByLabelText('Name').getAttribute('value')).toBe('Public sources')
 expect(screen.getByText('Leave credentials blank to keep the saved value.')).toBeTruthy()
 expect(m.resume).not.toHaveBeenCalled()
})
it('does not mislabel a failed status read as missing configuration',()=>{
 m.status.isError=true
 render(view())
 expect(screen.queryByRole('button',{name:'Set up web search'})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Retry'}))
 expect(m.status.refetch).toHaveBeenCalledOnce()
})
it('does not offer configuration while connections are loading or policy says provided sources only',()=>{
 m.status.loading=true
 const result=render(view())
 expect(screen.queryByRole('button',{name:'Set up web search'})).toBeNull()
 m.status.loading=false;m.preference.data.value.mode='provided'
 result.rerender(view())
 expect(screen.queryByRole('status')).toBeNull()
 expect(screen.queryByRole('button')).toBeNull()
})
it('closes a clean editor with Escape and restores keyboard focus to its opener',async()=>{
 render(view())
 const opener=screen.getByRole('button',{name:'Set up web search'})
 fireEvent.click(opener)
 fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'})
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 await waitFor(()=>expect(document.activeElement).toBe(opener))
 expect(m.call).not.toHaveBeenCalled()
})

it('allows continuation after a repaired existing connection passes its test, without resending automatically',async()=>{
 m.status.data.connections=[saved]
 render(view({...failure,failure:'credentials-required'},{...chat,searchConnection:'public'}))
 fireEvent.click(screen.getByRole('button',{name:'Check connection'}))
 fireEvent.click(screen.getByRole('button',{name:'Test connection'}))
 await waitFor(()=>expect(m.change).toHaveBeenCalledWith({searchConnection:'public'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(screen.getByRole('button',{name:'Continue request'})).toBeTruthy()
 expect(m.resume).not.toHaveBeenCalled()
})
