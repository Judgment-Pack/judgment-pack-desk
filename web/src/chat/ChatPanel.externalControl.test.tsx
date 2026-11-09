import {useEffect,useSyncExternalStore} from 'react'
import {cleanup,fireEvent,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {ChatPanel} from './ChatPanel'
import {ChatStore,type Chat} from './store'
import {useSavedChatRun} from '../assistant/serverRuns/useSavedChatRun'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {connected,renderConnected,stubClient} from '../testing/harness'
import {INITIAL_STATE} from '../research/initialState'
import type {ExternalControl} from '../search/step'
const fixture=vi.hoisted(()=>({store:undefined as ChatStore|undefined}))
vi.mock('./ChatProvider',()=>({useChats:()=>{const store=fixture.store!;return {store,...useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot)}},useChatStore:()=>fixture.store}))
vi.mock('../assistant/useAssistantSlot',async original=>({...await original<object>(),useAssistantSlot:()=>({state:'configured',engine:'vercel',endpoint:{kind:'openai-compatible',url:'https://example.invalid',model:'shared-model',models:['shared-model'],tools:[]},thinking:'off',keyStatus:'success',keyPresent:true,retryKey:()=>{}})}))
vi.mock('../search/connections',()=>({useSearchPreference:()=>({data:{value:{mode:'auto'}}}),useSearchConnections:()=>({loading:false,isError:false})}))
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
async function mount(kind:ExternalControl['kind']){
 const at='2026-10-20T00:00:00Z',control:ExternalControl={version:1,kind,hostOperationId:'host-one',sourceBindingDigest:'a'.repeat(64),requestId:'request-one',query:'public policy',reason:kind==='research-choice'?'choice-required':kind==='provided-only'?'provided-only':'configuration-required'}
 // Handwritten saved display. Actual backend association/zero acquisition is
 // exercised by TestAssistantExternalControlActualSearchEarlyExits in Go.
 const state={...INITIAL_STATE,phase:'conversation' as const,status:'stopped' as const,turns:[{id:'request-one',role:'user' as const,kind:'message' as const,text:'Review public policy.',at,researchChoice:'ask' as const},{id:'assistant-one',role:'assistant' as const,kind:'message' as const,text:'I will review the request.',at}],responses:[{id:'response-one',messageId:'assistant-one',documents:[],websites:[],sourceIds:[],work:{items:[{id:'call-one',name:'search_sources',status:'failed' as const,externalControl:control,...(kind==='research-choice'?{researchSuggestion:true as const}:{}),...(kind==='setup-required'?{failure:'setup-required' as const}:{})}],notices:[]}}]}
 const chat:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:at,composer:'Keep the unsent draft.',model:'shared-model',mode:'draft',view:'chat',checkpoint:{state,sources:[]}}
 const document={version:1,chats:[chat]}
 const write=vi.fn(async(_document:unknown)=>new Promise<never>(()=>{}))
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'fixture',content:structuredClone(document)}),write},{read:async()=>({project:'/project',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write})
 fixture.store=store;stores.push(store);await store.load();expect(store.getSnapshot().error).toBe('')
 const fetch=vi.fn(async()=>Response.json({root:'/project',files:[],version:1,subjects:{},reviews:[],settings:{},suites:{},mode:'manual'}))
 vi.stubGlobal('fetch',fetch);giveThisPageASessionForTesting('external-control-session')
 const runtime=stubClient({});runtime.client.listTools=async()=>({tools:[]})
 renderConnected(<Mounted/>,connected({client:runtime.client}),{path:'/chat/chat-one'})
 await waitFor(()=>expect(store.getSnapshot().bindings.get(chat.id)?.run).toBeTruthy())
 return {store,write,fetch,runtime}
}
it('keeps reloaded choice controls actionable only through a newly saved explicit user choice',async()=>{
 const h=await mount('research-choice')
 const button=await screen.findByRole('button',{name:'Use Research'})
 expect(button).toHaveProperty('disabled',false)
 expect(h.write).not.toHaveBeenCalled();expect(h.runtime.calls).toEqual([])
 fireEvent.click(button)
 await waitFor(()=>expect(h.write).toHaveBeenCalledTimes(1))
 const document=h.write.mock.calls[0]![0] as {chats:Chat[]},turn=document.chats[0].checkpoint!.state.turns.at(-1)!
 expect(turn.id).not.toBe('request-one')
 expect(turn).toMatchObject({role:'user',researchChoice:'research',creation:{kind:'research-choice',choice:'research'}})
 expect(h.runtime.calls).toEqual([])
})
it.each(['provided-only','setup-required'] as const)('preserves %s without creating a source approval or research choice',async kind=>{
 const h=await mount(kind)
 expect(screen.queryByRole('button',{name:'Use Research'})).toBeNull()
 expect(screen.getByRole('button',{name:'Source approvals'})).toHaveProperty('disabled',false)
 expect(h.write).not.toHaveBeenCalled();expect(h.runtime.calls).toEqual([])
})
