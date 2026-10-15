import {afterEach,expect,it,vi} from 'vitest'
import {render,screen,fireEvent,cleanup,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemorySettings} from './MemorySettings'
import {projectMemoryTools} from './tools'
import {memoryRequest} from './client'
vi.mock('./client',async()=>{const original=await vi.importActual<typeof import('./client')>('./client');return {...original,memoryRequest:vi.fn()}})
afterEach(()=>{cleanup();vi.clearAllMocks()})
const initial={sha256:'revision-one',content:{version:1 as const,settings:{use:false,suggest:true},entries:[{id:'m1',key:'outreach',text:'Prefer sourced outreach answers.',state:'pending' as const,createdAt:'2026-10-14T10:00:00Z',updatedAt:'2026-10-14T10:00:00Z'}]}}
it('reviews memory independently from recall settings and protects writes with a revision',async()=>{
 vi.mocked(memoryRequest).mockResolvedValue(initial)
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemorySettings/></QueryClientProvider>)
 expect(await screen.findByText('Prefer sourced outreach answers.')).toBeTruthy()
 expect((screen.getByLabelText('Use saved memory') as HTMLInputElement).checked).toBe(false)
 fireEvent.click(screen.getByRole('button',{name:'Remember'}))
 await waitFor(()=>expect(memoryRequest).toHaveBeenCalledWith({action:'approve',id:'m1'},'revision-one'))
})
it('shows a stale source and disables accepting it',async()=>{
 vi.mocked(memoryRequest).mockResolvedValue({...initial,content:{...initial.content,entries:[{...initial.content.entries[0],stale:true}]}})
 render(<QueryClientProvider client={new QueryClient()}><MemorySettings/></QueryClientProvider>)
 await screen.findByText(/Source changed/)
 expect((screen.getByRole('button',{name:'Remember'}) as HTMLButtonElement).disabled).toBe(true)
})
it('binds source to the current user message and saves it before the AI suggestion',async()=>{
 const order:string[]=[]
 vi.mocked(memoryRequest).mockImplementation(async()=>{order.push('request');return {saved:true,reviewRequired:true} as never})
 const tools=projectMemoryTools(()=>({chatId:'own-chat',turnId:'user-turn'}),async()=>{order.push('checkpoint')},()=>{order.push('changed')})
 await tools[1]!.execute({key:'preference',text:'Concise answers',source:{chatId:'forged'}},new AbortController().signal)
 expect(order).toEqual(['checkpoint','request','changed'])
 expect(memoryRequest).toHaveBeenCalledWith({action:'suggest',key:'preference',text:'Concise answers',source:{chatId:'own-chat',turnId:'user-turn'}},undefined,'/suggest',expect.any(AbortSignal))
 expect(tools[1]!.delegation).toBeUndefined()
 expect(tools[1]!.effect).toBe('memory')
})
