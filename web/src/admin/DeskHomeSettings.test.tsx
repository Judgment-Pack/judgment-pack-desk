import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import * as chatHooks from '../chat/ChatProvider'
import { ChatStore } from '../chat/store'
import { DeskHomeSettings, DeskHomeNotice } from './DeskHomeSettings'
import { DeskNotices, DeskNoticesProvider } from '../shell/DeskNotices'
import { UnsavedChangesProvider } from '../shell/UnsavedChanges'
vi.mock('../files/client',async original => ({...await original<typeof import('../files/client')>(),deskFetch:vi.fn()}))
import { deskFetch } from '../files/client'
const fetchMock=vi.mocked(deskFetch)
afterEach(() => { cleanup();vi.useRealTimers();vi.restoreAllMocks() })
let queued=false
beforeEach(() => {
 queued=false
 fetchMock.mockReset().mockImplementation(async (url,init) => {
  if(url==='/api/desk-home')return Response.json({path:'',defaultPath:'/data/jpack-desk',current:{id:'desk-a',name:'Research',folder:'/old/research'},desks:[],canMove:true,restartRequired:queued,destination:queued?'/data/jpack-desk':undefined})
  if(url.endsWith('/choose-folder'))return Response.json({destination:'/chosen/jpack-desk'})
  if(url.endsWith('/preview'))return Response.json({destination:JSON.parse(init!.body as string).destination,moves:[{label:'Research',from:'/old/research',to:'/data/jpack-desk/research'}],blockers:[],token:'preview-token'})
  if(url.endsWith('/move')){queued=true;return Response.json({restartRequired:true,destination:JSON.parse(init!.body as string).destination})}
  if(url.endsWith('/open-folder'))return Response.json({opened:true})
  throw new Error(url)
 })
})
function mount(){return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}><MemoryRouter><UnsavedChangesProvider><DeskNoticesProvider><DeskHomeSettings/><DeskHomeNotice/><DeskNotices/></DeskNoticesProvider></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)}
async function open(){await screen.findByText('Files use separate locations');fireEvent.click(screen.getByRole('button',{name:'Move…'}));return screen.findByRole('dialog',{name:'Move Desk home'})}
async function review(){const dialog=await open();fireEvent.click(within(dialog).getByRole('button',{name:'Use default location'}));fireEvent.click(within(dialog).getByRole('button',{name:'Review move'}));return screen.findByRole('dialog',{name:'Review Desk home move'})}
it('uses a native chooser and reviewed exact paths, with no free-text destination',async()=>{
 mount();const dialog=await open()
 expect(within(dialog).queryByRole('textbox')).toBeNull()
 fireEvent.click(within(dialog).getByRole('button',{name:'Choose folder…'}))
 await within(dialog).findByText('/chosen/jpack-desk')
 expect(fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))).toHaveLength(0)
 fireEvent.click(within(dialog).getByRole('button',{name:'Use default location'}))
 fireEvent.click(within(dialog).getByRole('button',{name:'Review move'}))
 const confirmation=await screen.findByRole('dialog',{name:'Review Desk home move'})
 expect(within(confirmation).getByText('/old/research')).toBeTruthy()
 expect(within(confirmation).getByText('→ /data/jpack-desk/research')).toBeTruthy()
 fireEvent.click(within(confirmation).getByRole('button',{name:'Move Desk home'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(await screen.findByRole('button',{name:'Restart required · Desk home move'})).toBeTruthy()
 const submitted=fetchMock.mock.calls.find(([url])=>url.endsWith('/move'))![1]!
 expect(JSON.parse(submitted.body as string)).toEqual({destination:'/data/jpack-desk',token:'preview-token'})
 expect(fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))).toHaveLength(1)
})
it('keeps blockers next to the review and prevents confirmation',async()=>{
 mount();const dialog=await open();fireEvent.click(within(dialog).getByRole('button',{name:'Use default location'}))
 fetchMock.mockImplementationOnce(async()=>Response.json({destination:'/data/jpack-desk',moves:[],blockers:['The destination already contains a Desk.'],token:'blocked'}))
 fireEvent.click(within(dialog).getByRole('button',{name:'Review move'}))
 const confirmation=await screen.findByRole('dialog',{name:'Review Desk home move'})
 expect(within(confirmation).getByText('The destination already contains a Desk.')).toBeTruthy()
 expect((within(confirmation).getByRole('button',{name:'Move Desk home'}) as HTMLButtonElement).disabled).toBe(true)
 expect(fetchMock.mock.calls.some(([url])=>url.endsWith('/move'))).toBe(false)
})
it('does not retry an uncertain move',async()=>{
 mount();const dialog=await review()
 fetchMock.mockImplementationOnce(async()=>{queued=true;throw new TypeError('connection closed')})
 fireEvent.click(within(dialog).getByRole('button',{name:'Move Desk home'}))
 const status=await screen.findByRole('button',{name:'Check move status'})
 expect((screen.getByRole('button',{name:'Review move'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(status)
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))).toHaveLength(1)
})
it('keeps the selected default when the file explorer is canceled',async()=>{
 mount();const dialog=await open();fireEvent.click(within(dialog).getByRole('button',{name:'Use default location'}))
 fetchMock.mockImplementationOnce(async()=>Response.json({canceled:true}))
 fireEvent.click(within(dialog).getByRole('button',{name:'Choose folder…'}))
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Choosing…'})).toBeNull())
 expect(within(dialog).getByText('/data/jpack-desk')).toBeTruthy()
 expect(screen.queryByRole('alert')).toBeNull()
})

it('bounds a hung confirmation and checks status without submitting a second move',async()=>{
 mount();const dialog=await review()
 const normal=fetchMock.getMockImplementation()!
 fetchMock.mockImplementation((url,init)=>url.endsWith('/move')?new Promise<Response>(()=>{}):normal(url,init))
 vi.useFakeTimers()
 fireEvent.click(within(dialog).getByRole('button',{name:'Move Desk home'}))
 expect(screen.getByRole('button',{name:'Working…'})).toBeTruthy()
 await act(async()=>{await vi.advanceTimersByTimeAsync(30_000)})
 vi.useRealTimers()
 const recovery=await screen.findByRole('button',{name:'Check move status'})
 const submitted=fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))
 expect(submitted).toHaveLength(1)
 expect(submitted[0][1]?.signal?.aborted).toBe(true)
 expect((screen.getByRole('button',{name:'Review move'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(recovery)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Review move'}) as HTMLButtonElement).disabled).toBe(false))
 expect(fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))).toHaveLength(1)
})
it('bounds a stalled move-status check while preserving uncertain state',async()=>{
 mount();const dialog=await review()
 fetchMock.mockImplementationOnce(async()=>{throw new TypeError('lost response')})
 fireEvent.click(within(dialog).getByRole('button',{name:'Move Desk home'}))
 const recovery=await screen.findByRole('button',{name:'Check move status'})
 fetchMock.mockImplementation(()=>new Promise<Response>(()=>{}))
 vi.useFakeTimers();fireEvent.click(recovery)
 await act(async()=>{await vi.advanceTimersByTimeAsync(30_000)})
 vi.useRealTimers()
 expect(await screen.findByText('Desk is unavailable. Check the local Desk process before restarting or attempting another move.')).toBeTruthy()
 expect((screen.getByRole('button',{name:'Check move status'}) as HTMLButtonElement).disabled).toBe(false)
 expect((screen.getByRole('button',{name:'Review move'}) as HTMLButtonElement).disabled).toBe(true)
})

it('confirms from a clean chat snapshot without waiting for a redundant flush',async()=>{
 const store=new ChatStore('/old/research')
 const snapshot={...store.getSnapshot(),ready:true}
 vi.spyOn(store,'getSnapshot').mockReturnValue(snapshot)
 const flush=vi.spyOn(store,'flush').mockImplementation(()=>new Promise<boolean>(()=>{}))
 vi.spyOn(chatHooks,'useChats').mockReturnValue({store,...snapshot})
 mount();const dialog=await review()
 fireEvent.click(within(dialog).getByRole('button',{name:'Move Desk home'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(flush).not.toHaveBeenCalled()
 expect(fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))).toHaveLength(1)
})
it('rechecks chat changes before posting a reviewed move',async()=>{
 const store=new ChatStore('/old/research')
 const snapshot={...store.getSnapshot(),ready:true}
 const read=vi.spyOn(store,'getSnapshot').mockReturnValue(snapshot)
 vi.spyOn(chatHooks,'useChats').mockReturnValue({store,...snapshot})
 mount();const dialog=await review()
 read.mockReturnValue({...snapshot,dirty:true})
 fireEvent.click(within(dialog).getByRole('button',{name:'Move Desk home'}))
 expect(await screen.findByText('Save or recover unsaved chat changes before moving.')).toBeTruthy()
 expect(fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))).toHaveLength(0)
})

it('shows a confirmed trigger refusal and leaves Working without uncertain recovery',async()=>{
 mount();const dialog=await review()
 const message='Sales demo: Pause "Evidence refresh" in Jobs → Evidence readiness → Triggers before moving Desk home.'
 fetchMock.mockImplementationOnce(async()=>Response.json({code:'stale',error:message},{status:409}))
 fireEvent.click(within(dialog).getByRole('button',{name:'Move Desk home'}))
 await screen.findByText(message)
 expect(screen.queryByRole('button',{name:'Working…'})).toBeNull()
 expect(screen.queryByRole('button',{name:'Check move status'})).toBeNull()
 expect((screen.getByRole('button',{name:'Review move'}) as HTMLButtonElement).disabled).toBe(false)
 expect(fetchMock.mock.calls.filter(([url])=>url.endsWith('/move'))).toHaveLength(1)
})
