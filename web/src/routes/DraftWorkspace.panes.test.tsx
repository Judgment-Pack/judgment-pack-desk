import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AppShell } from '../shell/AppShell'
import { DraftWorkspace } from './ChatWorkspace'
import { INITIAL_STATE } from '../research/run'
import { emptySuite } from '../packs/test-workspace/model'
import { connected, renderConnected, stubClient } from '../testing/harness'
import type { Chat } from '../chat/store'
import type { PackDraft } from '../packs/drafts/model'
import type { ResearchRunBinding } from '../research/useResearchRun'

const state = {...INITIAL_STATE, candidates: [{revision: 1, digest: 'digest', text: '{}', document: {title: 'Example', version: '1', rules: [], outcomes: []}}]} as unknown as typeof INITIAL_STATE
const suite = emptySuite()
const update = vi.fn(), activate = vi.fn()
const chats: any = { store:{update,activate,getSnapshot:()=>chats}, bindings: new Map(), chats: [{id:'chat',draftId:'draft',mode:'draft',composer:'Keep this question',updatedAt:'2026-10-13'}], drafts: [], packDrafts: [], deletedDrafts: [], ready: true }
vi.mock('../chat/ChatProvider', () => ({useChats: () => chats}))
vi.mock('../chat/ChatPanel', () => ({ChatPanel: () => <textarea aria-label="Pack conversation" defaultValue="Unsent message"/>}))
vi.mock('../packs/test-workspace/store', () => ({useTestStorage: () => ({suite, query: {isSuccess: true}, update: vi.fn(async () => suite)})}))
vi.mock('../packs/folders/FolderBrowser', () => ({ShowFolders: () => null, MovePackButton: () => null, MoveToFolderItem: () => null}))
const runtime = stubClient({list_packs: () => ({text: '{"status":"valid","packs":[]}'})})
beforeEach(() => {chats.store={update,activate,getSnapshot:()=>chats};delete suite.proposals;update.mockClear();activate.mockClear();runtime.client.listTools = async () => ({tools: []}); localStorage.clear(); vi.stubGlobal('fetch', async () => new Response('{"root":"/project","files":[]}', {status: 200}))})
afterEach(() => {cleanup(); vi.unstubAllGlobals(); localStorage.clear()})

it('keeps Assistant available and preserves the attached conversation across every draft tab, including an explicit collapse', async () => {
  renderConnected(<AppShell><DraftWorkspace chat={{id:'chat', mode:'draft'} as Chat}
    artifact={{id:'draft', title:'Example', generation:1, checkpoint:{state}} as PackDraft}
    fallback={{state, sources: [], run: {basisNow: () => ''}} as unknown as ResearchRunBinding}/></AppShell>, connected({client: runtime.client}), {path:'/packs/drafts/draft'})
  const packInput = screen.getByRole('textbox', {name:'Pack conversation'})
  fireEvent.change(packInput, {target:{value:'Keep this question'}})
  const tab = (name: string) => fireEvent.mouseDown(screen.getByRole('tab', {name}), {button:0})
  tab('Tests')
  expect(screen.getByRole('complementary', {name:'Assistant'})).toBeTruthy()
  expect(screen.getByRole('textbox', {name:'Pack conversation'})).toBe(packInput)
  expect(screen.queryByRole('textbox', {name:'Test conversation'})).toBeNull()
  fireEvent.keyDown(screen.getByRole('button',{name:'Add case'}),{key:'Enter'})
  fireEvent.click(await screen.findByRole('menuitem', {name:'Design with AI'}))
  expect(screen.getByRole('textbox', {name:'Pack conversation'})).toBe(packInput)
  expect(screen.queryByRole('textbox', {name:'Test conversation'})).toBeNull()
  expect(screen.queryByRole('button', {name:'Back to chat'})).toBeNull()
  expect(update).toHaveBeenCalledWith('chat', expect.objectContaining({composer:expect.stringContaining('Keep this question\n\nDesign test cases')}))
  expect(screen.getByRole('button', {name:'Add case'})).toBeTruthy()
  for (const name of ['Overview','Logic','Sources','Review','Usage','Overview']) {
    tab(name)
    expect(screen.getByRole('complementary', {name:'Assistant'})).toBeTruthy()
    expect(screen.getByRole('textbox', {name:'Pack conversation'})).toBe(packInput)
    expect(packInput).toHaveProperty('value','Keep this question')
    expect(screen.queryByRole('textbox', {name:'Test conversation'})).toBeNull()
  }
  tab('Tests')
  expect(screen.queryByRole('textbox', {name:'Test conversation'})).toBeNull()
  fireEvent.keyDown(screen.getByRole('button',{name:'Add case'}),{key:'Enter'})
  fireEvent.click(await screen.findByRole('menuitem', {name:'Design with AI'}))
  expect(screen.getByRole('textbox', {name:'Pack conversation'})).toBe(packInput)
  fireEvent.click(screen.getByRole('button', {name:'Collapse assistant'}))
  tab('Overview')
  expect(screen.queryByRole('complementary', {name:'Assistant'})).toBeNull()
  fireEvent.click(screen.getByRole('button', {name:'Assistant'}))
  await waitFor(() => expect(screen.getByRole('textbox', {name:'Pack conversation'})).toBe(packInput))
  expect(runtime.calls.filter(call => /test|evaluate/.test(call.name))).toEqual([])
})


it('keeps finalization gated and moves restored draft checks into Pack options',async()=>{
 chats.store=undefined
 const recheck=vi.fn(),restored={...state,restored:true,status:'needs-input' as const}
 renderConnected(<AppShell><DraftWorkspace chat={{id:'chat',mode:'draft'} as Chat}
  artifact={{id:'draft',title:'Example',generation:1,checkpoint:{state:restored}} as PackDraft}
  fallback={{state:restored,sources:[],run:{basisNow:()=>'',recheck}} as unknown as ResearchRunBinding}/></AppShell>,connected({client:runtime.client}),{path:'/packs/drafts/draft'})
 expect(screen.queryByRole('button',{name:/Recheck/})).toBeNull()
 expect(screen.getByRole('button',{name:'Review and finalize'})).toHaveProperty('disabled',true)
 expect(recheck).not.toHaveBeenCalled()
 fireEvent.keyDown(screen.getByRole('button',{name:'Pack options'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitem',{name:'Run checks'}))
 expect(recheck).toHaveBeenCalledOnce()
})

it('opens a draft test proposal from chat without replacing the session pane', async () => {
 suite.proposals=[{id:'proposal',chatId:'chat',at:'2026-10-13',request:'Design',packDigest:'digest',model:'fixture',contractVersion:'1',sources:[],caseSnapshots:{},state:'ready',message:'Review',attempts:[{document:{matrixVersion:'3',cases:[{id:'new-case',facts:{},expectedDisposition:{kind:'outcome',outcomeId:'yes',reasons:[],handoff:{state:'none'}}}]},unknowns:[],message:'Proposed',findings:[]}]}]
 renderConnected(<AppShell><DraftWorkspace chat={{id:'chat',mode:'draft'} as Chat}
  artifact={{id:'draft',title:'Example',generation:1,checkpoint:{state}} as PackDraft}
  fallback={{state,sources:[],run:{basisNow:()=>''}} as unknown as ResearchRunBinding}/></AppShell>,connected({client:runtime.client}),{path:'/packs/drafts/draft?chat=chat&testProposal=proposal'})
 await waitFor(()=>expect(screen.getByRole('button',{name:'Save selected cases (1)'})).toBeTruthy())
 expect(screen.getByRole('textbox',{name:'Pack conversation'})).toBeTruthy()
 expect(screen.queryByRole('textbox',{name:'Test conversation'})).toBeNull()
 expect(screen.queryByRole('button',{name:'Back to chat'})).toBeNull()
})
