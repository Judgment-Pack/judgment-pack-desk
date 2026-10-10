import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { GraphEditor } from './GraphWorkspace'
import { ChatStore } from '../chat/store'
import { deskFetch } from '../files/client'
import type { GraphOffer } from './author'
const state = vi.hoisted(() => ({store:undefined as ChatStore|undefined, navigate:vi.fn(), invalidate:vi.fn(), clear:vi.fn(), guard:vi.fn()}))
vi.mock('../chat/ChatProvider', () => ({useChats:() => ({store:state.store})}))
vi.mock('../shell/useDirtyGuard', () => ({useDirtyGuard:(...args:unknown[]) => {state.guard(...args);return state.clear}}))
vi.mock('@tanstack/react-query', async original => ({...await original(), useQueryClient:() => ({invalidateQueries:state.invalidate})}))
vi.mock('react-router-dom', async original => ({...await original(), useNavigate:() => state.navigate}))
vi.mock('../files/client', async original => ({...await original(), deskFetch:vi.fn()}))
vi.mock('./GraphComposition', () => ({GraphComposition:({content,onChange,tools}:{content:string;onChange:(s:string)=>void;tools:import('react').ReactNode}) => <>{tools}<textarea aria-label="Composition document" value={content} onChange={e=>onChange(e.target.value)}/></>}))
vi.mock('../files/CodeEditor', () => ({default:() => null}))
const content='{"formatVersion":"1","id":"flow","version":"0.1.0","nodes":{"a":{"pack":"a"}},"edges":[],"result":"a"}\n'
const initial={id:'flow',path:'flow.graph.json',content}
const offer:GraphOffer={...initial,before:'{}',configContent:'{"graphs":{"flow":{"path":"flow.graph.json"}}}',configSha256:'a'.repeat(64),token:'token',nonce:'nonce',hasLock:false,findings:'{"status":"valid"}',plan:'{"status":"planned"}'}
const write=vi.fn()
beforeEach(async()=>{
  vi.clearAllMocks()
  state.store=new ChatStore('project', {read:async()=>({project:'project',content:{version:1,chats:[]},sha256:'absent'}),write}, {read:async()=>({project:'project',content:{version:1,drafts:[],deleted:[]},sha256:'absent'}),write:async()=>({project:'project',sha256:'d',content:{version:1,drafts:[],deleted:[]}})})
  await state.store.load();write.mockResolvedValue({project:'project',sha256:'b'})
  vi.mocked(deskFetch).mockImplementation(async route=>new Response(JSON.stringify(String(route).endsWith('/proposal')?offer:{graphWritten:true,declared:true}),{status:200}))
})
afterEach(()=>{cleanup();state.store?.dispose()})
const draw=()=>render(<MemoryRouter><GraphEditor initial={initial} packs={['a']}/></MemoryRouter>)
it('withdraws a reviewed offer on editing and keeps undo/redo local', async()=>{
  draw();expect(deskFetch).not.toHaveBeenCalled();fireEvent.click(screen.getByRole('button',{name:'Review & save'}))
  await screen.findByRole('button',{name:'Confirm graph write'})
  fireEvent.change(screen.getByLabelText('Composition document'),{target:{value:content+' '}})
  expect(screen.queryByRole('button',{name:'Confirm graph write'})).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Undo'}));expect(screen.getByLabelText('Composition document')).toHaveProperty('value',content)
  fireEvent.click(screen.getByRole('button',{name:'Redo'}));expect(screen.getByLabelText('Composition document')).toHaveProperty('value',content+' ')
  expect(deskFetch).toHaveBeenCalledTimes(1)
})
it('invalidates project reads even on a partial graph write failure',async()=>{
  draw();fireEvent.click(screen.getByRole('button',{name:'Review & save'}));await screen.findByRole('button',{name:'Confirm graph write'})
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response('{"error":"Graph written; configuration write failed"}',{status:500}))
  fireEvent.click(screen.getByRole('button',{name:'Confirm graph write'}));await screen.findByRole('alert')
  expect(state.invalidate).toHaveBeenCalled();expect(state.navigate).not.toHaveBeenCalled();expect(screen.queryByRole('button',{name:'Confirm graph write'})).toBeNull()
})
it('persists a recoverable draft before navigation without project file writes',async()=>{
  draw();fireEvent.mouseDown(screen.getByRole('tab',{name:'Settings'}),{button:0});fireEvent.click(screen.getByRole('button',{name:'Keep draft'}));await waitFor(()=>expect(state.navigate).toHaveBeenCalled())
  expect(write.mock.calls[0]![0].chats[0].graphDrafts[0]).toMatchObject({...initial,saved:false})
  expect(state.navigate.mock.calls[0]![0]).toContain('/graphs?chat=');expect(state.clear).toHaveBeenCalled();expect(deskFetch).not.toHaveBeenCalled()
})
it('stays after persistence fails and retains one draft on retry',async()=>{
  write.mockRejectedValueOnce(new Error('History disk unavailable'));draw();fireEvent.mouseDown(screen.getByRole('tab',{name:'Settings'}),{button:0});fireEvent.click(screen.getByRole('button',{name:'Keep draft'}));await screen.findByRole('alert')
  expect(state.navigate).not.toHaveBeenCalled();expect(state.store!.getSnapshot().chats).toHaveLength(1)
  fireEvent.click(screen.getByRole('button',{name:'Keep draft'}));await waitFor(()=>expect(screen.getByRole('button',{name:'Keep draft'})).toHaveProperty('disabled',false))
  expect(state.store!.getSnapshot().chats).toHaveLength(1);expect(state.store!.getSnapshot().chats[0]!.graphDrafts).toHaveLength(1);await waitFor(()=>expect(state.navigate).toHaveBeenCalled())
})
it('rejects an invalid draft id before creating a conversation',async()=>{
  draw();fireEvent.mouseDown(screen.getByRole('tab',{name:'Settings'}),{button:0});fireEvent.change(screen.getByLabelText('Configured graph id'),{target:{value:'INVALID ID'}});fireEvent.click(screen.getByRole('button',{name:'Keep draft'}));await screen.findByRole('alert')
  expect(state.store!.getSnapshot().chats).toHaveLength(0);expect(write).not.toHaveBeenCalled()
})

it('keeps private draft metadata off the closed proposal request wire',async()=>{
  const retained={...initial,draftId:'draft-1',createdAt:'2026-10-09T12:00:00Z',saved:false}
  render(<MemoryRouter><GraphEditor initial={retained} draft={retained} packs={['a']}/></MemoryRouter>)
  fireEvent.click(screen.getByRole('button',{name:'Review & save'}));await screen.findByRole('button',{name:'Confirm graph write'})
  expect(JSON.parse(vi.mocked(deskFetch).mock.calls[0]![1]!.body as string)).toEqual(initial)
})
