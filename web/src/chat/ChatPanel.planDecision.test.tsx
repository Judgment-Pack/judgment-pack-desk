import {useEffect,useSyncExternalStore} from 'react'
import {cleanup,fireEvent,screen,waitFor,within} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {ChatPanel} from './ChatPanel'
import {ChatStore,type Chat} from './store'
import {useSavedChatRun} from '../assistant/serverRuns/useSavedChatRun'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {connected,renderConnected,stubClient} from '../testing/harness'
import {INITIAL_STATE} from '../research/initialState'
import plan from '../assistant/serverRuns/fixtures/plan-decision.json'
const fixture=vi.hoisted(()=>({store:undefined as ChatStore|undefined}))
vi.mock('./ChatProvider',()=>({useChats:()=>{const store=fixture.store!;return {store,...useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot)}},useChatStore:()=>fixture.store}))
vi.mock('../assistant/useAssistantSlot',async original=>({...await original<object>(),useAssistantSlot:()=>({state:'configured',engine:'vercel',endpoint:{kind:'openai-compatible',url:'https://example.invalid',model:'shared-model',models:['shared-model'],tools:[]},thinking:'off',keyStatus:'success',keyPresent:true,retryKey:()=>{}})}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>({data:{value:{mode:'provided'}}}),useSearchConnections:()=>({loading:false,isError:false})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>({loading:false,isError:false,entries:[],web:false,discovery:false})}))
vi.mock('../packs/test-workspace/ChatTestProposals',()=>({useChatTestProposals:()=>[],ChatTestProposals:()=>null}))
vi.mock('../assistant/memory/MemorySettings',()=>({ChatMemoryReview:()=>null,MemorySettings:()=>null}))

const stores:ChatStore[]=[]
afterEach(()=>{cleanup();stores.forEach(store=>store.dispose());stores.length=0;vi.unstubAllGlobals();sessionStorage.clear();localStorage.clear()})
function Worker({chat}:{chat:Chat}){
 const store=fixture.store!,binding=useSavedChatRun(store,chat)
 useEffect(()=>store.report(chat.id,binding),[store,chat.id,binding.run,binding.state,binding.ledger,binding.blocked])
 return null
}
function Mounted(){
 const store=fixture.store!,snapshot=useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot),chat=snapshot.chats[0]!
 return <><Worker chat={chat}/><ChatPanel chat={chat}/></>
}
it.each([true,false])('mounts the retained Plan change and transmits one explicit decision (%s)',async approved=>{
 const draft='Keep this exact unsent draft.',chat:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:draft,model:'shared-model',mode:'draft',view:'chat',serverRequest:{requestId:plan.requestId,admissionId:plan.admissionId},checkpoint:{state:INITIAL_STATE,sources:[]}}
 const document={version:1,chats:[chat]},write=vi.fn(async()=>{throw Error('No browser write expected')})
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'fixture',content:structuredClone(document)}),write},{read:async()=>({project:'/project',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write})
 fixture.store=store;stores.push(store);await store.load();expect(store.getSnapshot().error).toBe('')
 const fetch=vi.fn(async(path:unknown,init?:RequestInit)=>{
  if((init?.method??'GET')==='POST')return new Promise<Response>(()=>{})
  if(String(path).startsWith('/api/assistant-runs/owners/'))return Response.json({runs:[plan]})
  return Response.json({root:'/project',files:[],version:1,subjects:{},reviews:[],settings:{},suites:{},mode:'manual'})
 });vi.stubGlobal('fetch',fetch);giveThisPageASessionForTesting('plan-decision-session')
 const runtime=stubClient({});runtime.client.listTools=async()=>({tools:[]})
 renderConnected(<Mounted/>,connected({client:runtime.client}),{path:'/chat/chat-one'})
 const decision=within(await screen.findByRole('group',{name:'Approval required'}))
 expect(decision.getByText('Remove reviewed second step')).toBeTruthy()
 expect(decision.getByRole('heading',{name:'Current'})).toBeTruthy();expect(decision.getByRole('heading',{name:'Proposed'})).toBeTruthy()
 const lists=decision.getAllByRole('list');expect(lists).toHaveLength(2)
 expect(within(lists[0]!).getAllByRole('listitem').map(item=>item.textContent)).toEqual(['Inspect source','Validate result'])
 expect(within(lists[1]!).getAllByRole('listitem').map(item=>item.textContent)).toEqual(['Inspect source'])
 expect(fetch.mock.calls.filter(([,init])=>(init?.method??'GET')!=='GET')).toEqual([])
 const button=decision.getByRole('button',{name:approved?'Approve':'Decline'});expect(button).toHaveProperty('disabled',false)
 fireEvent.click(button);fireEvent.click(button)
 await waitFor(()=>expect(fetch.mock.calls.filter(([,init])=>init?.method==='POST')).toHaveLength(1))
 const [path,init]=fetch.mock.calls.find(([,init])=>init?.method==='POST')!
 expect(path).toBe('/api/assistant-runs/'+plan.id+'/actions');expect(JSON.parse(String(init?.body))).toEqual({id:expect.any(String),revision:plan.revision,kind:'plan-decision',...plan.planDecisionReference,approved})
 expect(screen.getByRole('textbox',{name:'Message the assistant'})).toHaveProperty('value',draft)
 expect(write).not.toHaveBeenCalled();expect(runtime.calls).toEqual([])
})
