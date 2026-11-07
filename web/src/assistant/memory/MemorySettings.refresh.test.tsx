import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import {Tooltip} from 'radix-ui'
import {MemorySettings} from './MemorySettings'
import {memoryRequest,MEMORY_KEY,type MemoryReply} from '@judgment-pack/harness/memory/client'
vi.mock('@judgment-pack/harness/memory/client',async original=>({...await original<typeof import('@judgment-pack/harness/memory/client')>(),memoryRequest:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('does not silently rebase a memory edit when another chat updates memory',async()=>{
 const saved:MemoryReply={sha256:'a'.repeat(64),content:{version:1,settings:{use:true,suggest:true},entries:[{id:'one',key:'preference',text:'Old preference',state:'active',createdAt:'2026-10-14',updatedAt:'2026-10-14'}]}}
 vi.mocked(memoryRequest).mockResolvedValue(saved)
 const cache=new QueryClient()
 render(<QueryClientProvider client={cache}><Tooltip.Provider><MemorySettings/></Tooltip.Provider></QueryClientProvider>)
 fireEvent.click(await screen.findByRole('button',{name:'Edit'}))
 const dialog=screen.getByRole('dialog',{name:'Edit memory'})
 fireEvent.change(within(dialog).getByRole('textbox',{name:'Memory'}),{target:{value:'My unsaved preference'}})
 act(()=>cache.setQueryData(MEMORY_KEY,{...saved,sha256:'b'.repeat(64),content:{...saved.content,entries:[{...saved.content.entries[0],text:'Other chat'}]}}))
 expect((within(dialog).getByRole('textbox',{name:'Memory'}) as HTMLTextAreaElement).value).toBe('My unsaved preference')
 fireEvent.click(within(dialog).getByRole('button',{name:'Remember'}))
 await waitFor(()=>expect(memoryRequest).toHaveBeenCalledWith(expect.objectContaining({action:'edit',text:'My unsaved preference'}),'a'.repeat(64)))
})
