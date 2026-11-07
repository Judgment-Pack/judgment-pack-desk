import {useEffect,useSyncExternalStore} from 'react'
import {act,cleanup,fireEvent,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {sha256} from '@judgment-pack/harness/records'
import {ChatPanel} from './ChatPanel'
import {ChatStore,type Chat} from './store'
import {useSavedChatRun} from '../assistant/serverRuns/useSavedChatRun'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {connected,renderConnected,stubClient} from '../testing/harness'
import {INITIAL_STATE} from '../research/initialState'
import pending from '../assistant/serverRuns/fixtures/pending.json'
const fixture=vi.hoisted(()=>({store:undefined as ChatStore|undefined}))
vi.mock('./ChatProvider',()=>({useChats:()=>{const store=fixture.store!;return {store,...useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot)}},useChatStore:()=>fixture.store}))
vi.mock('../assistant/useAssistantSlot',async original=>({...await original<object>(),useAssistantSlot:()=>({state:'configured',engine:'vercel',endpoint:{kind:'openai-compatible',url:'https://example.invalid',model:'shared-model',models:['shared-model'],tools:[]},thinking:'off',keyStatus:'success',keyPresent:true,retryKey:()=>{}})}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>({data:{value:{mode:'provided'}}}),useSearchConnections:()=>({loading:false,isError:false})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>({loading:false,isError:false,entries:[],web:false,discovery:false})}))
vi.mock('../packs/test-workspace/ChatTestProposals',()=>({useChatTestProposals:()=>[],ChatTestProposals:()=>null}))
vi.mock('../assistant/memory/MemorySettings',()=>({ChatMemoryReview:()=>null,MemorySettings:()=>null}))

const stores:ChatStore[]=[]
afterEach(()=>{cleanup();stores.forEach(store=>store.dispose());stores.length=0;vi.unstubAllGlobals();sessionStorage.clear();localStorage.clear()})
const deferred=<T,>()=>{let resolve!:(value:T)=>void,reject!:(cause:unknown)=>void;const promise=new Promise<T>((r,j)=>{resolve=r;reject=j});return {promise,resolve,reject}}
function Worker({chat}:{chat:Chat}){
 const store=fixture.store!,binding=useSavedChatRun(store,chat)
 useEffect(()=>store.report(chat.id,binding),[store,chat.id,binding.run,binding.state,binding.ledger,binding.blocked])
 return null
}
function Mounted(){
 const store=fixture.store!,snapshot=useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot),chat=snapshot.chats[0]!
 return <><Worker chat={chat}/><ChatPanel chat={chat}/></>
}
it.each(['success','rejected'] as const)('protects the mounted composer throughout deferred discovery and %s reconciliation',async outcome=>{
 const draft='Keep this exact unsent draft. 中文\nSecond line',chat:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:draft,model:'shared-model',mode:'draft',view:'chat',serverRequest:{requestId:pending.requestId,admissionId:pending.admissionId},checkpoint:{state:INITIAL_STATE,sources:[]}}
 const document={version:1,chats:[chat]},reply=()=>({project:'/project',sha256:'fixture',content:structuredClone(document)})
 const discovery=deferred<Response>(),readback=deferred<ReturnType<typeof reply>>(),read=vi.fn().mockResolvedValueOnce(reply()).mockImplementation(()=>readback.promise),write=vi.fn(async()=>{throw Error('No write expected')})
 const store=new ChatStore('/project',{read,write},{read:async()=>({project:'/project',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write})
 fixture.store=store;stores.push(store);await store.load();expect(store.getSnapshot().error).toBe('')
 const fetch=vi.fn(async(path:unknown,init?:RequestInit)=>{
  expect(init?.method??'GET').toBe('GET')
  if(String(path).startsWith('/api/assistant-runs/owners/'))return discovery.promise
  return Response.json({root:'/project',files:[],version:1,subjects:{},reviews:[],settings:{},suites:{},mode:'manual'})
 });vi.stubGlobal('fetch',fetch);giveThisPageASessionForTesting('composer-reconnect-session')
 const runtime=stubClient({});runtime.client.listTools=async()=>({tools:[]})
 renderConnected(<Mounted/>,connected({client:runtime.client}),{path:'/chat/chat-one'})
 await waitFor(()=>expect(fetch.mock.calls.some(([path])=>String(path).startsWith('/api/assistant-runs/owners/'))).toBe(true))
 const composer=screen.getByRole('textbox',{name:'Message the assistant'}),send=screen.getByRole('button',{name:'Send'})
 // Causal proof: ownership already refuses edits while discovery is pending.
 act(()=>store.update(chat.id,{composer:'Rejected while pinned'}))
 expect(store.getSnapshot().chats[0].composer).toBe(draft)
 expect(composer).toHaveProperty('disabled',true)
 expect(composer).toHaveProperty('value',draft);expect(send).toHaveProperty('disabled',true)
 expect(screen.getAllByText('This chat is owned by an active server response. Reconnect or stop it before editing.').length).toBeGreaterThan(0)
 const terminal={...pending,state:'complete',chatProjection:{responseId:`response-${sha256(pending.id).slice(0,32)}`,digest:'b'.repeat(64),acknowledged:true}}
 await act(async()=>discovery.resolve(Response.json({runs:[terminal]})))
 await waitFor(()=>expect(read).toHaveBeenCalledTimes(2))
 expect(composer).toHaveProperty('disabled',true);expect(composer).toHaveProperty('value',draft)
 await act(async()=>{if(outcome==='success')readback.resolve(reply());else readback.reject(Error('private read failed'))})
 await waitFor(()=>expect(store.getSnapshot().bindings.get(chat.id)?.run?.needsReconnect).toBe(outcome!=='success'))
 expect(composer).toHaveProperty('disabled',outcome!=='success');expect(composer).toHaveProperty('value',draft)
 if(outcome==='rejected'){
  act(()=>store.update(chat.id,{composer:'Still pinned'}));expect(store.getSnapshot().chats[0].composer).toBe(draft)
  expect(screen.getByRole('button',{name:'Reconnect'})).toHaveProperty('disabled',false)
 }else{
  fireEvent.change(composer,{target:{value:draft+' Edit'}});expect(store.getSnapshot().chats[0].composer).toBe(draft+' Edit')
 }
 expect(write).not.toHaveBeenCalled();expect(runtime.calls).toEqual([])
 expect(fetch.mock.calls.filter(([,init])=>(init?.method??'GET')!=='GET')).toEqual([])
})
