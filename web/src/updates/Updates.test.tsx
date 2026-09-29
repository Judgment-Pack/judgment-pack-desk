import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { testQueryClient } from '../testing/harness'
import { newer, Updates } from './Updates'
const io=vi.hoisted(()=>({fetch:vi.fn()}))
vi.mock('../files/client',()=>({deskFetch:io.fetch,answer:async(value:unknown)=>value}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
function show(value:unknown){io.fetch.mockResolvedValue(value);return render(<QueryClientProvider client={testQueryClient()}><Updates/></QueryClientProvider>)}
it('shows development status without installation controls',async()=>{
 show({development:true,managed:false,installedVersion:'development',latest:{version:'1.0.0'}})
 expect(await screen.findByText('Local development build')).toBeTruthy()
 expect(screen.queryByRole('button',{name:'Prepare update'})).toBeNull()
 expect(screen.queryByRole('checkbox')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Check for updates'}))
 await waitFor(()=>expect(io.fetch).toHaveBeenCalledWith('/api/updates',expect.objectContaining({method:'POST',body:JSON.stringify({action:'check'})})))
})
it('stages a managed update and shows that it waits for the next launch',async()=>{
 const status={development:false,managed:true,installedVersion:'1.0.0',current:{version:'1.0.0'},latest:{version:'1.1.0',asset:'archive'}}
 show(status)
 await screen.findByRole('button',{name:'Prepare update'})
 io.fetch.mockResolvedValue({...status,pending:{version:'1.1.0'}})
 fireEvent.click(screen.getByRole('button',{name:'Prepare update'}))
 expect(await screen.findByText('Version 1.1.0 is ready for the next launch.')).toBeTruthy()
 expect(screen.queryByRole('button',{name:'Prepare update'})).toBeNull()
 expect(screen.getByRole('button',{name:'Cancel prepared update'})).toBeTruthy()
 expect(io.fetch).toHaveBeenLastCalledWith('/api/updates',expect.objectContaining({body:'{"action":"stage"}'}))
})
it('never offers an older release or an unchecked failed download',async()=>{
 expect(newer('1.10.0','1.9.0')).toBe(true);expect(newer('1.2.0','1.10.0')).toBe(false)
 show({development:false,managed:true,installedVersion:'1.0.0',latest:{version:'1.1.0',asset:'archive'},error:'offline'})
 await screen.findByRole('alert')
 expect(screen.queryByRole('button',{name:'Prepare update'})).toBeNull()
})

it('reflects an automatic-update change while saving and restores it on failure',async()=>{
 show({development:false,managed:true,installedVersion:'1.0.0',autoInstall:false})
 const option=await screen.findByRole('checkbox',{name:'Automatically update on launch'})
 let rejectSave!:(error:Error)=>void
 io.fetch.mockReturnValue(new Promise((_resolve,reject)=>{rejectSave=reject}))
 fireEvent.click(option)
 await waitFor(()=>{expect((option as HTMLInputElement).checked).toBe(true);expect((option as HTMLInputElement).disabled).toBe(true)})
 rejectSave(new Error('network failure'))
 await screen.findByRole('alert')
 expect((option as HTMLInputElement).checked).toBe(false)
 expect((option as HTMLInputElement).disabled).toBe(false)
})
