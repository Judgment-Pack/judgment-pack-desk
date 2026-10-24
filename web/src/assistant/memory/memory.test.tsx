// The Desk's cases of the harness's memory tests: the memory settings screen is the Desk's.
// The rest of the file moved with the harness to @judgment-pack/harness (memory/memory.test.ts).
import {afterEach,expect,it,vi} from 'vitest'
import {render,screen,fireEvent,cleanup,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemorySettings} from './MemorySettings'
import {memoryRequest} from '@judgment-pack/harness/memory/client'
vi.mock('@judgment-pack/harness/memory/client',async()=>{const original=await vi.importActual<typeof import('@judgment-pack/harness/memory/client')>('@judgment-pack/harness/memory/client');return {...original,memoryRequest:vi.fn()}})
afterEach(()=>{cleanup();vi.clearAllMocks()})
const initial={sha256:'a'.repeat(64),content:{version:1 as const,settings:{use:false,suggest:true},entries:[{id:'m1',key:'outreach',text:'Prefer sourced outreach answers.',state:'pending' as const,createdAt:'2026-10-14T10:00:00Z',updatedAt:'2026-10-14T10:00:00Z'}]}}
it('reviews memory independently from recall settings and protects writes with a revision',async()=>{
 vi.mocked(memoryRequest).mockResolvedValue(initial)
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemorySettings/></QueryClientProvider>)
 expect(await screen.findByText('Prefer sourced outreach answers.')).toBeTruthy()
 expect((screen.getByLabelText('Use saved memory') as HTMLInputElement).checked).toBe(false)
 fireEvent.click(screen.getByRole('button',{name:'Remember'}))
 await waitFor(()=>expect(memoryRequest).toHaveBeenCalledWith({action:'approve',id:'m1'},'a'.repeat(64)))
})
it('shows a stale source and disables accepting it',async()=>{
 vi.mocked(memoryRequest).mockResolvedValue({...initial,content:{...initial.content,entries:[{...initial.content.entries[0],stale:true}]}})
 render(<QueryClientProvider client={new QueryClient()}><MemorySettings/></QueryClientProvider>)
 await screen.findByText(/Source changed/)
 expect((screen.getByRole('button',{name:'Remember'}) as HTMLButtonElement).disabled).toBe(true)
})
it('keeps private canonical metadata out of management text and edit requests',async()=>{
 const entry={...initial.content.entries[0]!,state:'active' as const,canonical:{id:'proposed-outreach',kind:'preference',source:{run:'private-agent-run',proposedBy:'model'},digest:'sha256:c5b283a91e324d8aaa63a3c03bdee031f0a5f17cc68534a61f916ce73319bd3c',expiresAt:'2099-01-01T00:00:00Z'}}
 vi.mocked(memoryRequest).mockResolvedValue({...initial,content:{...initial.content,entries:[entry]}})
 render(<QueryClientProvider client={new QueryClient()}><MemorySettings/></QueryClientProvider>)
 await screen.findByText(entry.text)
 expect(screen.queryByText('private-agent-run')).toBeNull()
 expect(screen.queryByText('proposed-outreach')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Edit'}))
 fireEvent.click(screen.getByRole('button',{name:'Remember'}))
 await waitFor(()=>expect(memoryRequest).toHaveBeenCalledWith({action:'edit',id:'m1',key:'outreach',text:entry.text,dependencies:undefined},'a'.repeat(64)))
})
