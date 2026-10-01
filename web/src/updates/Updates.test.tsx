import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { testQueryClient } from '../testing/harness'
import { newer, componentMatch, Updates } from './Updates'
import type { ComponentBuilds } from '../config/deskConfig'
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


it('compares exact commits and does not label unknown, modified or digest-approved builds as matching',()=>{
 expect(componentMatch({revision:'abc'},'abcdef')).toBe('different')
 expect(componentMatch({revision:'abcdef',modified:true},'abcdef')).toBe('different')
 expect(componentMatch(undefined,'abcdef')).toBe('unknown')
 expect(componentMatch({},'abcdef')).toBe('unknown')
 expect(componentMatch({revision:'abcdef',unverified:true},'abcdef')).toBe('unknown')
 expect(componentMatch({revision:'abcdef'},'abcdef')).toBe('matching')
})

const lock={runtime:{version:'v0.24.0',revision:'a'.repeat(40),channel:'stable'},runner:{version:'v0.2.0',revision:'b'.repeat(40),channel:'stable'},gateway:{version:'v0.8.0',revision:'c'.repeat(40),channel:'stable'}}
const running={desk:{},runtime:{revision:'a'.repeat(40)},runner:{revision:'b'.repeat(40)},sourceWorker:{revision:'b'.repeat(40)}}
function versions(builds:ComponentBuilds|undefined,gateway?:{version?:string;revision:string;unverified?:boolean}){
 io.fetch.mockResolvedValue({development:true,managed:false,installedVersion:'development',components:lock})
 return render(<QueryClientProvider client={testQueryClient()}><Updates builds={builds} gateway={gateway}/></QueryClientProvider>)
}
const row=(label:string)=>screen.getByText(label,{selector:'th'}).closest('tr')!

it('shows an older worker separately even when Runner matches the release',async()=>{
 versions({...running,sourceWorker:{revision:'old-commit'}})
 expect((await screen.findByRole('alert')).textContent).toContain('Some components differ')
 expect(within(row('Source worker')).getByText('Different build')).toBeTruthy()
 expect(within(row('Source worker')).getByText('old-commit')).toBeTruthy()
 expect(within(row('Runner')).getByText('Matches release')).toBeTruthy()
})

it('names the pinned version only for a matching build, and raises no alert when all match',async()=>{
 versions(running,{version:'v0.8.0',revision:'c'.repeat(40)})
 await screen.findByText('Component versions')
 for(const label of ['Runtime','Runner','Source worker','Gateway']) expect(within(row(label)).getByText('Matches release')).toBeTruthy()
 expect(within(row('Runtime')).getAllByText('v0.24.0')).toHaveLength(2)
 expect(screen.queryByRole('alert')).toBeNull()
})

it('reports a modified Runtime as different, by commit, never by the pinned version',async()=>{
 versions({...running,runtime:{revision:'a'.repeat(40),modified:true}})
 expect((await screen.findByRole('alert')).textContent).toContain('Some components differ')
 expect(within(row('Runtime')).getByText('Different build')).toBeTruthy()
 expect(within(row('Runtime')).getByText('a'.repeat(12))).toBeTruthy()
 expect(within(row('Runtime')).getAllByText('v0.24.0')).toHaveLength(1)
})

it('leaves a digest-approved or stopped Gateway unknown rather than matching',async()=>{
 versions(running,{version:'v0.8.0',revision:'c'.repeat(40),unverified:true})
 await screen.findByText('Component versions')
 expect(within(row('Gateway')).getAllByText('Unknown')).toHaveLength(1)
 expect(within(row('Gateway')).queryByText('Matches release')).toBeNull()
 expect(screen.queryByRole('alert')).toBeNull()
 cleanup()
 versions(running)
 await screen.findByText('Component versions')
 expect(within(row('Gateway')).getAllByText('Unknown')).toHaveLength(2)
})

it('omits Runner rows when Desk reports Runner as not configured',async()=>{
 versions({desk:{},runtime:{revision:'a'.repeat(40)}})
 await screen.findByText('Component versions')
 expect(screen.queryByText('Runner',{selector:'th'})).toBeNull()
 expect(screen.queryByText('Source worker',{selector:'th'})).toBeNull()
 cleanup()
 versions(undefined)
 await screen.findByText('Component versions')
 expect(within(row('Runner')).getAllByText('Unknown')).toHaveLength(2)
})

it('calls a development pin a pinned commit, never a release, and says when identities were recorded',async()=>{
 io.fetch.mockResolvedValue({development:true,managed:false,installedVersion:'development',components:{...lock,runner:{version:'v0.3.0-dev+bbbbbbb',revision:'b'.repeat(40),channel:'development'}}})
 render(<QueryClientProvider client={testQueryClient()}><Updates builds={running}/></QueryClientProvider>)
 await screen.findByText('Component versions')
 expect(screen.getByText(/Build information recorded when Desk started/)).toBeTruthy()
 for(const label of ['Runner','Source worker']){
  expect(within(row(label)).getByText('Matches pinned commit')).toBeTruthy()
  expect(within(row(label)).queryByText('Matches release')).toBeNull()
  expect(within(row(label)).getByText('b'.repeat(12))).toBeTruthy()
 }
 expect(within(row('Runtime')).getByText('Matches release')).toBeTruthy()
})

it('shows no comparison when the chassis does not report its lock',async()=>{
 io.fetch.mockResolvedValue({development:true,managed:false,installedVersion:'development'})
 render(<QueryClientProvider client={testQueryClient()}><Updates builds={running}/></QueryClientProvider>)
 await screen.findByText('Local development build')
 expect(screen.queryByText('Component versions')).toBeNull()
 expect(screen.queryByText('Matches release')).toBeNull()
})
