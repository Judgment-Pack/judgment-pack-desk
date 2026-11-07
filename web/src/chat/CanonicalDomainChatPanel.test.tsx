// Main-chat composition proof, reusing the same actual producer observations as
// CanonicalDomainProjections. No domain control is rendered outside ChatPanel.
import {readFileSync} from 'node:fs'
import {useEffect,useSyncExternalStore} from 'react'
import {useLocation} from 'react-router-dom'
import {cleanup,fireEvent,screen,waitFor,within} from '@testing-library/react'
import {afterEach,describe,expect,it,vi} from 'vitest'
import {ChatPanel} from './ChatPanel'
import {ChatStore,type Chat} from './store'
import {useSavedChatRun} from '../assistant/serverRuns/useSavedChatRun'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {connected,renderConnected,stubClient} from '../testing/harness'

const fixture=vi.hoisted(()=>({store:undefined as ChatStore|undefined}))
vi.mock('./ChatProvider',()=>({useChats:()=>{const store=fixture.store!;return {store,...useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot)}},useChatStore:()=>fixture.store}))
vi.mock('../assistant/useAssistantSlot',async original=>({...await original<object>(),useAssistantSlot:()=>({state:'configured',engine:'vercel',endpoint:{kind:'openai-compatible',url:'https://example.invalid',model:'shared-model',models:['shared-model'],tools:[]},thinking:'off',keyStatus:'success',keyPresent:true,retryKey:()=>{}})}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>({data:{value:{mode:'provided'}}}),useSearchConnections:()=>({loading:false,isError:false})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>({loading:false,isError:false,entries:[],web:false,discovery:false})}))
vi.mock('../packs/test-workspace/ChatTestProposals',()=>({useChatTestProposals:()=>[],ChatTestProposals:()=>null}))
vi.mock('../assistant/memory/MemorySettings',()=>({ChatMemoryReview:()=>null,MemorySettings:()=>null}))
const outputs=process.env.JPS_DOMAIN_OUTPUTS
if(process.env.JPS_REQUIRE_DOMAIN_OUTPUTS==='1'&&!outputs)throw Error('JPS_DOMAIN_OUTPUTS is required for mounted main-chat acceptance')
const read=(path:string)=>JSON.parse(readFileSync(`${outputs}/${path}.json`,'utf8'))
const stores:ChatStore[]=[]
afterEach(()=>{cleanup();stores.forEach(store=>store.dispose());stores.length=0;vi.unstubAllGlobals();sessionStorage.clear();localStorage.clear()})
function Worker({chat}:{chat:Chat}){
 const store=fixture.store!,binding=useSavedChatRun(store,chat)
 useEffect(()=>store.report(chat.id,binding),[store,chat.id,binding.run,binding.state,binding.ledger,binding.blocked])
 return null
}
function Mounted({locked}:{locked:boolean}){
 const store=fixture.store!,snapshot=useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot),chat=snapshot.chats[0]!,location=useLocation()
 return <><Worker chat={chat}/><ChatPanel chat={chat} locked={locked} context={{text:'Fixture context',title:'Retained domain proposal'}}/><output aria-label="Current route">{location.pathname+location.search}</output></>
}
async function setup(path:string,locked:boolean){
 const document=read(path)
 // The Graph host fixture omits unrelated display envelope fields. Domain
 // checkpoint and graphDrafts are never filled, changed or synthesized here.
 if(path==='graph/chat')document.chats=document.chats.map((chat:object)=>({title:'Graph review',composer:'',pinned:false,archived:false,mode:'draft',view:'chat',...chat}))
 const write=vi.fn(async()=>{throw Error('Mounting a domain chat must not persist')})
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'fixture',content:structuredClone(document)}),write},{read:async()=>({project:'/project',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write})
 stores.push(store);fixture.store=store;await store.load();expect(store.getSnapshot().error).toBe('')
 const loaded=structuredClone(store.getSnapshot().chats[0]),fetch=vi.fn(async(path:unknown,init?:RequestInit)=>{
  const url=String(path);expect(init?.method??'GET',`No mount or disabled action may dispatch ${url}`).toBe('GET')
  if(url.startsWith('/api/assistant-runs/owners/'))return Response.json({runs:[]})
  if(url.startsWith('/api/assistant-approvals/'))return Response.json({mode:'manual'})
  if(url.startsWith('/api/policies/')){
   const chat=document.chats[0]
   return Response.json({revision:chat.policyRequest.revision,document:{id:chat.policy.id,candidate:chat.policyRequest.candidate}})
  }
  return Response.json({root:'/project',files:[],version:1,subjects:{},reviews:[],settings:{},suites:{}})
 });vi.stubGlobal('fetch',fetch);giveThisPageASessionForTesting('domain-main-chat-session')
 const runtime=stubClient({});runtime.client.listTools=async()=>({tools:[]})
 renderConnected(<Mounted locked={locked}/>,connected({client:runtime.client}),{path:'/chat/chat-one'})
 await waitFor(()=>expect(store.getSnapshot().bindings.get('chat-one')?.run?.ownership).toBe('server'))
 const panel=screen.getByRole('region',{name:'Assistant chat'}),composer=within(panel).getByRole('textbox',{name:'Message the assistant'})
 await waitFor(()=>expect(composer).toHaveProperty('disabled',locked))
 const send=within(panel).getByRole('button',{name:'Send'});expect(send).toHaveProperty('disabled',true);fireEvent.click(send)
 const unchanged=()=>{expect(write).not.toHaveBeenCalled();expect(runtime.calls).toEqual([]);expect(fetch.mock.calls.filter(([,init])=>(init?.method??'GET')!=='GET')).toEqual([]);expect(store.getSnapshot().chats[0]).toEqual(loaded)}
 return {store,panel,composer,unchanged}
}

describe.skipIf(!outputs)('actual canonical domains inside production main ChatPanel',()=>{
 it('shows the exact retained Graph review beside its locked real composer without starting work',async()=>{
  const h=await setup('graph/chat',true),draft=h.store.getSnapshot().chats[0]!.graphDrafts![0]!
  expect(within(h.panel).getByText('flow',{exact:true})).toBeTruthy()
  expect(draft.content).toContain('"missing":null,"flag":false,"zero":0,"literal":"50","number":1.20')
  const link=within(h.panel).getByRole('link',{name:'Review graph'})
  expect(link.getAttribute('href')).toBe(`/graphs?chat=chat-one&draft=${draft.draftId}`)
  fireEvent.click(link);expect(screen.getByLabelText('Current route').textContent).toBe(link.getAttribute('href'))
  h.unchanged()
 })
 it('shows the actual stale Policy candidate while refusing its main-chat save and Auto controls',async()=>{
  const h=await setup('policy/stale',false),proposal=h.store.getSnapshot().chats[0]!.policyProposals![0]!
  expect(proposal.candidate).toEqual({title:'Changed',narrative:'Only approved staff may access records.\r\nRetain missing values, 中文, < and >.',artifactVersion:'1.0.0'})
  expect(within(h.panel).getByRole('region',{name:'Proposed Policy update'})).toBeTruthy()
  expect(within(h.panel).getByText(/The saved Policy changed/)).toBeTruthy()
  for(const name of ['Review and save draft','Auto within limits…']){const button=within(h.panel).getByRole('button',{name});expect(button).toHaveProperty('disabled',true);fireEvent.click(button)}
  expect(screen.queryByRole('dialog')).toBeNull();h.unchanged()
 })
 it('shows the exact typed Dialect recipe proposal and refuses review from its locked main-chat composer',async()=>{
  const h=await setup('dialect-recipe/chat',true),proposal=h.store.getSnapshot().chats[0]!.inputDrafts![0]!
  expect(proposal.name).toBe('Portable recipe');expect(proposal.intent).toBe('Preserve exact fields.\r\n中文 <record>&')
  expect(proposal.fields.map(field=>field.type)).toEqual(['text','integer','decimal','boolean','text-list','range','string','number'])
  expect(within(h.panel).getByText('Portable recipe',{exact:true})).toBeTruthy()
  const button=within(h.panel).getByRole('button',{name:'Review recipe'});expect(button).toHaveProperty('disabled',true);fireEvent.click(button)
  expect(screen.queryByRole('dialog')).toBeNull();h.unchanged()
 })
})
