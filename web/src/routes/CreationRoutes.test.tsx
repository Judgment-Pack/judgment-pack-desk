import {act,cleanup,fireEvent,screen,waitFor} from '@testing-library/react'
import {useEffect,useSyncExternalStore} from 'react'
import {Route,Routes,useLocation} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {readFileSync} from 'node:fs'
import {sha256} from '@judgment-pack/harness/records'
import {ChatStore,type Chat} from '../chat/store'
import {decodeCheckpoint} from '../chat/checkpoint'
import {memoryDraftPersistence} from '../testing/draftPersistence'
import {fromChat} from '../packs/drafts/model'
import {INITIAL_STATE} from '../research/initialState'
import {useSavedChatRun} from '../assistant/serverRuns/useSavedChatRun'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {AppShell} from '../shell/AppShell'
import {connected,renderConnected,stubClient} from '../testing/harness'
import {NewPackView,DraftPackView} from './DraftPackView'
import {emptySuite} from '../packs/test-workspace/model'

const fixture=vi.hoisted(()=>({store:undefined as ChatStore|undefined,snapshot:undefined as unknown}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>{const store=fixture.store!;return {store,...useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot)}},useChatStore:()=>fixture.store}))
vi.mock('../research/useResearchRun',()=>({useResearchRun:()=>{throw Error('Legacy authoring must not mount.')}}))
vi.mock('../assistant/useAssistantSlot',async original=>({...await original<object>(),useAssistantSlot:()=>({state:'configured',engine:'vercel',endpoint:{kind:'openai-compatible',url:'https://example.invalid',model:'shared-model',models:['shared-model'],tools:[]},thinking:'off',keyStatus:'success',keyPresent:true,retryKey:()=>{}})}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>({data:{value:{mode:'provided'}}}),useSearchConnections:()=>({loading:false,isError:false})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>({loading:false,isError:false,entries:[],web:false,discovery:false})}))
const suite=emptySuite()
vi.mock('../packs/test-workspace/store',()=>({useTestStorage:()=>({suite,query:{isSuccess:true},update:vi.fn(async()=>suite)})}))
vi.mock('../packs/test-workspace/ChatTestProposals',()=>({useChatTestProposals:()=>[],ChatTestProposals:()=>null}))
vi.mock('../packs/folders/FolderBrowser',()=>({ShowFolders:()=>null,MovePackButton:()=>null,MoveToFolderItem:()=>null}))
vi.mock('../assistant/memory/MemorySettings',()=>({ChatMemoryReview:()=>null,MemorySettings:()=>null}))

const stores:ChatStore[]=[]
afterEach(()=>{cleanup();stores.forEach(store=>store.dispose());stores.length=0;vi.restoreAllMocks();vi.unstubAllGlobals();sessionStorage.clear();localStorage.clear()})
function Worker({chat}:{chat:Chat}){const store=fixture.store!,binding=useSavedChatRun(store,chat);useEffect(()=>store.report(chat.id,binding),[store,chat.id,binding.run,binding.state,binding.ledger,binding.blocked]);return null}
function Mounted(){const store=fixture.store!,snapshot=useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot),location=useLocation();return <>{snapshot.active.map(id=>{const chat=[...snapshot.chats,...snapshot.drafts].find(chat=>chat.id===id);return chat?<Worker key={id} chat={chat}/>:null})}<AppShell><Routes><Route path="/packs/new" element={<NewPackView/>}/><Route path="/packs/drafts/:draftId" element={<DraftPackView/>}/></Routes></AppShell><output aria-label="Current route">{location.pathname}</output></>}
async function setup(content:unknown={version:1,chats:[]},drafts=memoryDraftPersistence('/project'),revision='a'.repeat(64)){
 let saved=content
 const read=async()=>({project:'/project',sha256:'saved',content:structuredClone(saved),chatRevisions:Object.fromEntries((saved as {chats:Chat[]}).chats.map(chat=>[chat.id,revision]))})
 const store=new ChatStore('/project',{read,write:async document=>{saved=structuredClone(document);return read()}},drafts);fixture.store=store;stores.push(store);await store.load()
 const fetch=vi.fn(async(path:unknown)=>String(path).startsWith('/api/assistant-runs/owners/')?new Response(JSON.stringify({runs:fixture.snapshot?[fixture.snapshot]:[]}),{headers:{'Content-Type':'application/json'}}):new Response(JSON.stringify({root:'/project',files:[],version:1,subjects:{},reviews:[],settings:{},suites:{}}),{headers:{'Content-Type':'application/json'}}));vi.stubGlobal('fetch',fetch)
 giveThisPageASessionForTesting('creation-route-session')
 const runtime=stubClient({list_packs:()=>({text:'{"status":"valid","packs":[]}'})});runtime.client.listTools=async()=>({tools:[]})
 return {store,fetch,runtime,remote:(value:unknown,digest:string)=>{saved=value;revision=digest}}
}
it('mounts NewPackView through real ChatStore and ChatPanel, then navigates only after exact acknowledged proposal reconciliation',async()=>{
 fixture.snapshot=undefined
 const h=await setup();renderConnected(<Mounted/>,connected({client:h.runtime.client}),{path:'/packs/new'})
 await screen.findByRole('textbox',{name:'Message the assistant'})
 const chat=h.store.getSnapshot().drafts[0]!
 expect(h.store.getSnapshot().packDrafts).toEqual([])
 const request={id:'request-one',role:'user' as const,kind:'message' as const,text:'Always accept.',at:'2026-10-23T00:00:00Z'}
 await act(async()=>{h.store.update(chat.id,{model:'shared-model'});await h.store.prepareServerRequest(chat.id,{...INITIAL_STATE,turns:[request]},request.id,'admit-one',new AbortController().signal)})
 const saved=h.store.getSnapshot().chats[0]!,response={id:'response-one',messageId:request.id,documents:[],websites:[],sourceIds:[],work:{items:[],notices:[]}}
 const proposal={text:'{"title":"Actual route draft","version":"1","rules":[],"outcomes":[]}',revision:1,digest:'',document:{title:'Actual route draft'},producedBy:'conversation' as const}
 h.remote({version:1,chats:[{...saved,checkpoint:{sources:[],state:{...saved.checkpoint!.state,candidates:[proposal],responses:[response]}}}]},'b'.repeat(64))
 await act(async()=>{await h.store.reconcileServer(chat.id,true)})
 expect(screen.getByLabelText('Current route').textContent).toBe('/packs/new')
 await act(async()=>{await h.store.reconcileServer(chat.id,true,{digest:'b'.repeat(64),responseId:response.id,requestId:request.id,admissionId:'admit-one'})})
 await waitFor(()=>expect(screen.getByLabelText('Current route').textContent).toContain('/packs/drafts/'))
 expect(screen.getByRole('textbox',{name:'Message the assistant'})).toBeTruthy()
 expect(screen.getByRole('button',{name:'Review and finalize'})).toHaveProperty('disabled',true)
 expect(h.runtime.calls.filter(call=>/test|evaluate/.test(call.name))).toEqual([])
})

const outputs=process.env.JPS_CREATION_OUTPUTS
it.skipIf(!outputs)('mounts the actual API-produced approved draft with current canonical readiness and sends an explicit fresh check before finalization',async()=>{
 const before=JSON.parse(readFileSync(`${outputs}/api-04-propose-correction.json`,'utf8')),approved=JSON.parse(readFileSync(`${outputs}/api-05-approve-correction.json`,'utf8'))
 const old=before.chat.chats[0],chat=approved.chat.chats[0]
 const draftIO=memoryDraftPersistence('/project'),artifact={...fromChat(old,{...old.checkpoint,state:decodeCheckpoint(old.checkpoint).state}),id:old.draftId,generation:chat.draftGeneration}
 await draftIO.write({version:1,drafts:[artifact],deleted:[]},'absent')
 fixture.snapshot=approved.run
 const h=await setup(approved.chat,draftIO,approved.run.chatProjection.digest)
 renderConnected(<Mounted/>,connected({client:h.runtime.client}),{path:`/packs/drafts/${chat.draftId}`})
 await waitFor(()=>{const snapshot=h.store.getSnapshot(),binding=snapshot.bindings.get(chat.id);expect({error:snapshot.error,dirty:snapshot.dirty,status:binding?.state.status,detail:binding?.state.detail,restored:binding?.state.restored,basis:!!binding?.run?.basisNow?.()}).toMatchObject({error:'',dirty:false,status:'ready',restored:false,basis:true})})
 await waitFor(()=>expect(screen.getByRole('button',{name:'Review and finalize'})).toHaveProperty('disabled',false))
 expect(h.store.getSnapshot().packDrafts[0]!.checkpoint.state.cases).toHaveLength(2)
 expect(h.store.getSnapshot().packDrafts[0]!.checkpoint.state.cases[0]!.facts).toEqual({zero:0,flag:false,nil:null,numeric:'1'})
 expect(h.store.getSnapshot().bindings.get(chat.id)!.run?.ownership).toBe('server')
 expect(screen.getByRole('textbox',{name:'Message the assistant'})).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('button',{name:'Pack options'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitem',{name:'Run checks'}))
 await waitFor(()=>expect(h.fetch.mock.calls.some(([path])=>path==='/api/assistant-runs')).toBe(true))
 const retained=h.store.getSnapshot().chats.find(c=>c.id===chat.id)!
 expect(retained.checkpoint!.state.turns.at(-1)?.creation).toEqual({kind:'check',candidateDigest:sha256(approved.run.creation.candidate)})
 expect(h.store.getSnapshot().bindings.get(chat.id)!.state.readiness).toBe('')
})

it.skipIf(!outputs)('keeps the actual partial corrected draft readable and blocks finalization while another issue remains',async()=>{
 const partial=JSON.parse(readFileSync(`${outputs}/last/04-approve-correction.json`,'utf8')),chat=partial.chat.chats[0]
 const draftIO=memoryDraftPersistence('/project'),saved={...chat.checkpoint,state:decodeCheckpoint(chat.checkpoint).state}
 const artifact={...fromChat(chat,saved),id:chat.draftId,generation:chat.draftGeneration}
 await draftIO.write({version:1,drafts:[artifact],deleted:[]},'absent')
 fixture.snapshot=partial.run
 const h=await setup(partial.chat,draftIO,partial.run.chatProjection.digest)
 renderConnected(<Mounted/>,connected({client:h.runtime.client}),{path:`/packs/drafts/${chat.draftId}`})
 await waitFor(()=>{
  const snapshot=h.store.getSnapshot(),binding=snapshot.bindings.get(chat.id)
  expect(snapshot.error).toBe('');expect(binding?.state.status).toBe('needs-input')
  expect(binding?.state.restored).toBe(false)
  expect(binding?.state.candidates.at(-1)?.check?.cases.every(c=>c.passed)).toBe(true)
  expect(binding?.state.expectationIssues.filter(i=>!i.resolved)).toHaveLength(1)
  expect(binding?.state.readiness).toBe('')
 })
 expect(screen.getByRole('button',{name:'Review and finalize'})).toHaveProperty('disabled',true)
 expect(h.runtime.calls.filter(call=>/test|evaluate|save|apply/.test(call.name))).toEqual([])
})
