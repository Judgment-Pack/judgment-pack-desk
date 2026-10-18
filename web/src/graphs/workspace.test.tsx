import source from '../../../internal/desk/testdata/policy-authoring-source.json'
import { act,cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter,createMemoryRouter,RouterProvider } from 'react-router-dom'
import { GraphEditor,GraphWorkspace } from './GraphWorkspace'
import { ChatStore } from '../chat/store'
import { deskFetch,readFile } from '../files/client'
import type { GraphOffer } from './author'
const state = vi.hoisted(() => ({store:undefined as ChatStore|undefined, navigate:vi.fn(), invalidate:vi.fn(),setData:vi.fn(), clear:vi.fn(), guard:vi.fn(),records:[] as import('./drafts').GraphDraft[]}))
vi.mock('../chat/ChatProvider', () => ({useChats:() => ({store:state.store,...state.store?.getSnapshot()})}))
vi.mock('../shell/useDirtyGuard', () => ({useDirtyGuard:(...args:unknown[]) => {state.guard(...args);return state.clear}}))
vi.mock('@tanstack/react-query', async original => ({...await original(),useQuery:()=>({data:{content:{version:1,drafts:state.records,deleted:[]}},isPending:false,error:null}), useQueryClient:() => ({invalidateQueries:state.invalidate,setQueryData:state.setData})}))
vi.mock('react-router-dom', async original => ({...await original(), useNavigate:() => state.navigate}))
vi.mock('../files/queries',()=>({useFileContent:()=>({data:{content:'{\"packs\":{\"a\":{\"path\":\"a.json\"}}}'}})}))
vi.mock('../files/client', async original => ({...await original(), deskFetch:vi.fn(),readFile:vi.fn()}))
vi.mock('./GraphComposition', () => ({GraphComposition:({content,onChange,tools}:{content:string;onChange:(s:string)=>void;tools:import('react').ReactNode}) => <>{tools}<textarea aria-label="Composition document" value={content} onChange={e=>onChange(e.target.value)}/></>}))
vi.mock('../files/CodeEditor', () => ({default:() => null}))
const content='{"formatVersion":"1","id":"flow","version":"0.1.0","nodes":{"a":{"pack":"a"}},"edges":[],"result":"a"}\n'
const initial={id:'flow',path:'flow.graph.json',content}
const offer:GraphOffer={...initial,before:'{}',configContent:'{"graphs":{"flow":{"path":"flow.graph.json"}}}',configSha256:'a'.repeat(64),token:'token',nonce:'nonce',hasLock:false,findings:'{"status":"valid"}',plan:'{"status":"planned"}'}
const write=vi.fn()
beforeEach(async()=>{
  vi.clearAllMocks();state.records=[]
  vi.mocked(readFile).mockImplementation(async path=>({path,content:path==='jpack.json'?offer.configContent:initial.content,sha256:'a'.repeat(64),bytes:initial.content.length}))
  state.store=new ChatStore('project', {read:async()=>({project:'project',content:{version:1,chats:[]},sha256:'absent'}),write}, {read:async()=>({project:'project',content:{version:1,drafts:[],deleted:[]},sha256:'absent'}),write:async()=>({project:'project',sha256:'d',content:{version:1,drafts:[],deleted:[]}})})
  await state.store.load();write.mockResolvedValue({project:'project',sha256:'b'})
  let graphRecord={version:1,drafts:[],deleted:[]} as import('./drafts').GraphDraftDocument
  vi.mocked(deskFetch).mockImplementation(async(route,init)=>{if(String(route)==='/api/draft-graphs'){if(init?.method==='PUT')graphRecord=JSON.parse(init.body as string);return Response.json({project:'project',sha256:'graph-revision',content:graphRecord})}return Response.json(String(route).endsWith('/proposal')?offer:{graphWritten:true,declared:true})})
})
afterEach(()=>{cleanup();state.store?.dispose()})
const draw=()=>render(<MemoryRouter><GraphEditor initial={initial} packs={['a']}/></MemoryRouter>)
async function keepDraft() {
 fireEvent.keyDown(screen.getByRole('button',{name:'Graph options'}),{key:'Enter'})
 const command=await screen.findByRole('menuitem',{name:'Keep draft'})
 await waitFor(()=>expect(command.getAttribute('data-disabled')).toBeNull())
 fireEvent.click(command)
}
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
  const normal=vi.mocked(deskFetch).getMockImplementation()!;vi.mocked(deskFetch).mockImplementation((route,init)=>String(route).endsWith('/write')?Promise.resolve(Response.json({error:'Graph written; configuration write failed'},{status:500})):normal(route,init))
  fireEvent.click(screen.getByRole('button',{name:'Confirm graph write'}));await screen.findByRole('alert')
  expect(state.invalidate).toHaveBeenCalled();expect(state.navigate).not.toHaveBeenCalled();expect(screen.queryByRole('button',{name:'Confirm graph write'})).toBeNull()
})
it('persists a recoverable draft before navigation without project file writes',async()=>{
  draw();await keepDraft();await waitFor(()=>expect(state.navigate).toHaveBeenCalled())
  expect(write.mock.calls[0]![0].chats[0].graphDrafts[0]).toMatchObject({...initial,saved:false})
  expect(state.setData).toHaveBeenCalledWith(['graph-drafts'],expect.objectContaining({content:expect.objectContaining({drafts:[expect.objectContaining({...initial,originChatId:expect.any(String)})]})}))
  expect(state.navigate.mock.calls[0]![0]).toContain('/graphs?chat=');expect(state.clear).toHaveBeenCalled();expect(vi.mocked(deskFetch).mock.calls.every(([url])=>String(url)==='/api/draft-graphs')).toBe(true)
})
it('stays after persistence fails and retains one draft on retry',async()=>{
  write.mockRejectedValueOnce(new Error('History disk unavailable'));draw();await keepDraft();await screen.findByRole('alert')
  expect(state.navigate).not.toHaveBeenCalled();expect(state.store!.getSnapshot().chats).toHaveLength(1)
  await keepDraft();await waitFor(()=>expect(state.navigate).toHaveBeenCalled())
  expect(state.store!.getSnapshot().chats).toHaveLength(1);expect(state.store!.getSnapshot().chats[0]!.graphDrafts).toHaveLength(1)
})
it('rejects an invalid draft id before creating a conversation',async()=>{
  draw();fireEvent.keyDown(screen.getByRole('button',{name:'Graph options'}),{key:'Enter'});fireEvent.click(await screen.findByRole('menuitem',{name:'Graph settings…'}));fireEvent.change(screen.getByLabelText('Configured graph id'),{target:{value:'INVALID ID'}});fireEvent.click(screen.getByRole('button',{name:'Done'}));await keepDraft();await screen.findByRole('alert')
  expect(state.store!.getSnapshot().chats).toHaveLength(0);expect(write).not.toHaveBeenCalled()
})

it('keeps private draft metadata off the closed proposal request wire',async()=>{
  const retained={...initial,draftId:'draft-1',createdAt:'2026-10-09T12:00:00Z',saved:false}
  render(<MemoryRouter><GraphEditor initial={retained} draft={retained} packs={['a']}/></MemoryRouter>)
  fireEvent.click(screen.getByRole('button',{name:'Review & save'}));await screen.findByRole('button',{name:'Confirm graph write'})
  expect(JSON.parse(vi.mocked(deskFetch).mock.calls[0]![1]!.body as string)).toEqual(initial)
})

it('keeps document commands in the graph menu without duplicating navigation tabs', async()=>{
 draw();fireEvent.keyDown(screen.getByRole('button',{name:'Graph options'}),{key:'Enter'})
 const commands=await screen.findAllByRole('menuitem')
 expect(commands.map(item=>item.textContent)).toEqual(['View JSON','Graph settings…','Edit JSON…','Keep draft'])
 expect(screen.queryByRole('menuitem',{name:'Library'})).toBeNull()
 expect(screen.queryByRole('menuitem',{name:'Settings'})).toBeNull()
 expect(screen.queryByRole('menuitem',{name:'Source'})).toBeNull()
 fireEvent.keyDown(screen.getByRole('menu'),{key:'Escape'})
 await waitFor(()=>expect(document.activeElement).toBe(screen.getByRole('button',{name:'Graph options'})))
 expect(screen.getByRole('button',{name:'Review & save'})).toBeTruthy()
})


it('offers deletion for retained graph drafts only',async()=>{
 const retained={...initial,draftId:'draft-1',createdAt:'2026-10-09T12:00:00Z',saved:false}
 const view=render(<MemoryRouter><GraphEditor initial={retained} draft={retained} draftChatId="owner" packs={['a']}/></MemoryRouter>)
 fireEvent.keyDown(screen.getByRole('button',{name:'Graph options'}),{key:'Enter'})
 const remove=await screen.findByRole('menuitem',{name:'Delete draft…'})
 expect(remove.getAttribute('data-destructive')).toBe('true')
 fireEvent.keyDown(screen.getByRole('menu'),{key:'Escape'})
 view.rerender(<MemoryRouter><GraphEditor initial={retained} draft={{...retained,saved:true}} draftChatId="owner" packs={['a']}/></MemoryRouter>)
 fireEvent.keyDown(screen.getByRole('button',{name:'Graph options'}),{key:'Enter'})
 await screen.findByRole('menu');expect(screen.queryByRole('menuitem',{name:'Delete draft…'})).toBeNull()
})

it('keeps draft conversations attached when their graph is saved', async () => {
  const retained = {...initial, draftId: 'draft-flow', createdAt: '2026-10-10T12:00:00Z', saved: false}
  const first = state.store!.create(), second = state.store!.create(), unrelated = state.store!.create()
  for (const chat of [first, second]) state.store!.update(chat.id, {graph: {id: 'flow', path: initial.path, workspace: retained.draftId}})
  state.store!.update(first.id, {graphDrafts: [retained]})
  state.store!.update(unrelated.id, {graph: {id: 'other', path: 'other.json', workspace: 'other'}})
  render(<MemoryRouter><GraphEditor initial={retained} draft={retained} chatId={second.id} draftChatId={first.id} packs={['a']}/></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', {name: 'Review & save'}))
  fireEvent.click(await screen.findByRole('button', {name: 'Confirm graph write'}))
  await waitFor(() => expect(state.navigate).toHaveBeenCalledWith('/graphs/flow?chat=' + second.id))
  const chats = state.store!.getSnapshot().chats
  for (const id of [first.id, second.id]) expect(chats.find(c => c.id === id)?.graph).toEqual({id: 'flow', path: initial.path, workspace: 'flow'})
  expect(chats.find(c => c.id === unrelated.id)?.graph?.workspace).toBe('other')
})

it('keeps settings edits in the draft after closing and retains one shared tab structure', async()=>{
 draw();expect(screen.getAllByRole('tab').map(tab=>tab.textContent)).toEqual(['Overview','Logic','Sources','Tests','Review','Brief','History','Usage'])
 const opener=screen.getByRole('button',{name:'Graph options'})
 fireEvent.keyDown(opener,{key:'Enter'});fireEvent.click(await screen.findByRole('menuitem',{name:'Graph settings…'}))
 fireEvent.change(screen.getByLabelText('Description'),{target:{value:'Changed description'}})
 fireEvent.click(screen.getByRole('button',{name:'Done'}));await waitFor(()=>expect(document.activeElement).toBe(opener))
 expect(deskFetch).not.toHaveBeenCalled()
 fireEvent.keyDown(opener,{key:'Enter'});fireEvent.click(await screen.findByRole('menuitem',{name:'Graph settings…'}))
 expect(screen.getByLabelText('Description')).toHaveProperty('value','Changed description')
})

it('switches duplicate draft ids by original conversation and protects dirty owner changes',async()=>{
 const first={...initial,draftId:'shared-id',originChatId:'first-owner',createdAt:'2026-10-18T12:00:00Z',policySources:[source as import('../policies/authoringSource').PolicyAuthoringSource]}
 const second={...first,originChatId:'second-owner',content:'{"formatVersion":"1","nodes":{},"edges":[]}',policySources:[{...source,narrative:'The second exact Policy source.'} as import('../policies/authoringSource').PolicyAuthoringSource]}
 state.records=[first,second]
 const router=createMemoryRouter([{path:'/graphs',element:<GraphWorkspace/>}],{initialEntries:['/graphs?chat=first-owner&draft=shared-id']})
 render(<RouterProvider router={router}/>)
 expect(screen.getByLabelText('Composition document')).toHaveProperty('value',first.content)
 fireEvent.change(screen.getByLabelText('Composition document'),{target:{value:first.content+' '}})
 const guard=state.guard.mock.lastCall![2] as {shouldBlock:(value:any)=>boolean}
 expect(guard.shouldBlock({currentLocation:{pathname:'/graphs',search:'?chat=first-owner&draft=shared-id'},nextLocation:{pathname:'/graphs',search:'?chat=second-owner&draft=shared-id'}})).toBe(true)
 expect(guard.shouldBlock({currentLocation:{pathname:'/graphs',search:'?draftChat=first-owner&chat=helper-a&draft=shared-id'},nextLocation:{pathname:'/graphs',search:'?draftChat=first-owner&chat=helper-b&draft=shared-id'}})).toBe(false)
 await act(()=>router.navigate('/graphs?chat=second-owner&draft=shared-id'))
 expect(screen.getByLabelText('Composition document')).toHaveProperty('value',second.content)
 fireEvent.mouseDown(screen.getByRole('tab',{name:'Sources'}),{button:0,ctrlKey:false});fireEvent.click((await screen.findAllByText(source.title))[0]!)
 expect(screen.getByText('The second exact Policy source.')).toBeTruthy();expect(screen.queryByText(source.narrative)).toBeNull()
})

it.each(['before-commit','after-commit'] as const)('preserves a committed graph when private finalization fails %s and retries only its history',async failure=>{
 const normal=vi.mocked(deskFetch).getMockImplementation()!;let failed=false
 vi.mocked(deskFetch).mockImplementation(async(url,init)=>{
  if(String(url)==='/api/draft-graphs'&&init?.method==='PUT'&&JSON.parse(init.body as string).drafts.some((d:any)=>d.saved)&&!failed){failed=true;if(failure==='after-commit')await normal(url,init);throw Error('Private finalization response interrupted')}
  return normal(url,init)
 })
 draw();fireEvent.click(screen.getByRole('button',{name:'Review & save'}));fireEvent.click(await screen.findByRole('button',{name:'Confirm graph write'}))
 await screen.findByText('The graph file was saved. Finish retaining its private history.')
 expect(state.navigate).not.toHaveBeenCalled()
 const writes=()=>vi.mocked(deskFetch).mock.calls.filter(([url])=>String(url).endsWith('/graphs/write'))
 expect(writes()).toHaveLength(1)
 expect(state.setData.mock.calls.some(([,reply])=>reply.content.drafts.some((d:any)=>d.writePending))).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Finish saving history'}));await waitFor(()=>expect(state.navigate).toHaveBeenCalled())
 expect(writes()).toHaveLength(1);expect(readFile).toHaveBeenCalledWith('flow.graph.json',expect.any(AbortSignal));expect(readFile).toHaveBeenCalledWith('jpack.json',expect.any(AbortSignal))
})
it('blocks public dispatch after uncertain intent persistence and recovers the retained intent explicitly',async()=>{
 const normal=vi.mocked(deskFetch).getMockImplementation()!;let interrupted=false
 vi.mocked(deskFetch).mockImplementation(async(url,init)=>{
  if(String(url)==='/api/draft-graphs'&&init?.method==='PUT'&&JSON.parse(init.body as string).drafts.some((d:any)=>d.writePending)&&!interrupted){interrupted=true;await normal(url,init);throw Error('Intent acknowledgement lost')}
  return normal(url,init)
 })
 draw();fireEvent.click(screen.getByRole('button',{name:'Review & save'}));fireEvent.click(await screen.findByRole('button',{name:'Confirm graph write'}))
 await screen.findByRole('button',{name:'Recover save intent'})
 expect(vi.mocked(deskFetch).mock.calls.some(([url])=>String(url).endsWith('/graphs/write'))).toBe(false)
 fireEvent.click(screen.getByRole('button',{name:'Recover save intent'}));await screen.findByRole('button',{name:'Create revised draft'})
 expect(vi.mocked(deskFetch).mock.calls.some(([url])=>String(url).endsWith('/graphs/write'))).toBe(false)
})
it('restores pending history with no automatic graph write and refuses mismatched registration',async()=>{
 const retained={...initial,draftId:'pending',originChatId:'owner',createdAt:'2026-10-18T12:00:00Z',writePending:true}
 state.records=[retained]
 vi.mocked(readFile).mockImplementation(async path=>({path,content:path==='jpack.json'?'{"graphs":{"flow":{"path":"flow.graph.json","description":"changed"}}}':content,sha256:'a'.repeat(64),bytes:content.length}))
 render(<MemoryRouter><GraphEditor initial={retained} draft={retained} persistedDraft={retained} packs={['a']}/></MemoryRouter>)
 expect(screen.getByRole('button',{name:'Review & save'})).toHaveProperty('disabled',true)
 expect(deskFetch).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Finish saving history'}));await screen.findByRole('alert')
 expect(state.navigate).not.toHaveBeenCalled();expect(deskFetch).not.toHaveBeenCalled()
})

it('offers a revised private copy after a proven precommit refusal without replaying the public write',async()=>{
 const normal=vi.mocked(deskFetch).getMockImplementation()!
 vi.mocked(deskFetch).mockImplementation((url,init)=>String(url).endsWith('/graphs/write')?Promise.resolve(Response.json({error:'The reviewed confirmation changed.',outcome:'not-committed'},{status:409})):normal(url,init))
 draw();fireEvent.click(screen.getByRole('button',{name:'Review & save'}));fireEvent.click(await screen.findByRole('button',{name:'Confirm graph write'}))
 fireEvent.click(await screen.findByRole('button',{name:'Create revised draft'}));await waitFor(()=>expect(state.navigate).toHaveBeenCalled())
 expect(vi.mocked(deskFetch).mock.calls.filter(([url])=>String(url).endsWith('/graphs/write'))).toHaveLength(1)
 const final=state.setData.mock.lastCall![1].content.drafts
 expect(final).toHaveLength(2);expect(final.some((d:any)=>d.writePending)).toBe(true);expect(final.some((d:any)=>!d.writePending&&!d.saved)).toBe(true)
})
