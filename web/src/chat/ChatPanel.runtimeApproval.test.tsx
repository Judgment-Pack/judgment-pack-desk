import {useEffect,useSyncExternalStore} from 'react'
import {cleanup,fireEvent,screen,waitFor,within} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {ChatPanel} from './ChatPanel'
import {ChatStore,type Chat} from './store'
import {useSavedChatRun} from '../assistant/serverRuns/useSavedChatRun'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {connected,renderConnected,stubClient} from '../testing/harness'
import {INITIAL_STATE} from '../research/initialState'
import approvalTexts from '../assistant/serverRuns/fixtures/approval-text.json'
import approval from '../assistant/serverRuns/fixtures/approval.json'
import {approvalShownArguments,approvalShownDigest} from '@judgment-pack/harness/contract'
import {readExecution} from '@judgment-pack/harness/execution/state'
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
const runtimeOwner={
 artifactDigest:'sha256:'+'1'.repeat(64),processId:'runtime-process',projectId:'project-one',projectLabel:'<b>Runtime project</b>',
 sessionId:'runtime-session',principalDigest:'sha256:'+'2'.repeat(64),principalLabel:'https://principal.invalid/',
 scopeDigest:'sha256:'+'3'.repeat(64),scopeLabel:'Reviewed scope',
}
it.each([{runtimeApproval:true,row:undefined},{runtimeApproval:false,row:undefined},...approvalTexts.map(row=>({runtimeApproval:false,row}))])('renders the stored text and returns its proof from the card (%j)',async ({runtimeApproval,row})=>{
 const approve=runtimeApproval||!!row
 const retained={...approval,checkpoint:{...approval.checkpoint,...(row?{version:5,status:'paused'}:runtimeApproval?{version:3,status:'paused'}:{}),approval:{...approval.checkpoint.approval,...(row?{args:row.args,request:row.request}:{}),...(runtimeApproval?{source:{kind:'runtime',id:'runtime',trust:'first-party'},runtime:{owner:runtimeOwner,admissionDigest:'sha256:'+'4'.repeat(64)}}:{})}},...(row?{approvalReference:{callId:approval.approvalReference.callId,argsDigest:row.digest}}:{})}
 // Runtime approvals remain v3 without request; kept-text source approvals use v5.
 if(row){expect(retained.checkpoint.approval.source.kind).toBe('custom');expect('runtime' in retained.checkpoint.approval).toBe(false)}
 expect(readExecution(retained.checkpoint)).toEqual(retained.checkpoint)
 const draft='Keep this exact unsent draft.',chat:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:draft,model:'shared-model',mode:'draft',view:'chat',serverRequest:{requestId:retained.requestId,admissionId:retained.admissionId},checkpoint:{state:INITIAL_STATE,sources:[]}}
 const document={version:1,chats:[chat]},write=vi.fn(async()=>{throw Error('No browser write expected')})
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'fixture',content:structuredClone(document)}),write},{read:async()=>({project:'/project',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write})
 fixture.store=store;stores.push(store);await store.load();expect(store.getSnapshot().error).toBe('')
 const fetch=vi.fn(async(path:unknown,init?:RequestInit)=>{
  if((init?.method??'GET')==='POST')return new Promise<Response>(()=>{})
  if(String(path).startsWith('/api/assistant-runs/owners/'))return Response.json({runs:[retained]})
  return Response.json({root:'/project',files:[],version:1,subjects:{},reviews:[],settings:{},suites:{},mode:'manual'})
 });vi.stubGlobal('fetch',fetch);giveThisPageASessionForTesting('runtime-approval-session')
 const runtime=stubClient({});runtime.client.listTools=async()=>({tools:[]})
 renderConnected(<Mounted/>,connected({client:runtime.client}),{path:'/chat/chat-one'})
 const group=await screen.findByRole('group',{name:'Approval required'}),decision=within(group)
 expect(decision.getByText('write_file')).toBeTruthy()
 const code=group.querySelector('pre')!
 expect(code.textContent).toBe(approvalShownArguments(retained.checkpoint.approval))
 if(row){expect(code.textContent).toBe(row.request);expect(approvalShownDigest(retained.checkpoint.approval)).toBe(row.digest)}
 if(runtimeApproval){
  for(const label of ['Project','Principal','Scope',runtimeOwner.projectLabel,runtimeOwner.principalLabel,runtimeOwner.scopeLabel])expect(decision.getByText(label)).toBeTruthy()
  expect(group.querySelector('b')).toBeNull();expect(decision.queryByRole('link')).toBeNull()
 }else expect(decision.queryByText('Principal')).toBeNull()
 expect(fetch.mock.calls.filter(([,init])=>(init?.method??'GET')!=='GET')).toEqual([])
 const button=decision.getByRole('button',{name:approve?'Approve':'Decline'});expect(button).toHaveProperty('disabled',false)
 fireEvent.click(button);fireEvent.click(button)
 await waitFor(()=>expect(fetch.mock.calls.filter(([,init])=>init?.method==='POST')).toHaveLength(1))
 const [path,init]=fetch.mock.calls.find(([,init])=>init?.method==='POST')!
 expect(path).toBe('/api/assistant-runs/'+retained.id+'/actions');expect(JSON.parse(String(init?.body))).toEqual({id:expect.any(String),revision:retained.revision,kind:'approval',...retained.approvalReference,approved:approve,...(approve?{shownDigest:approvalShownDigest(retained.checkpoint.approval)}:{})})
 expect(screen.getByRole('textbox',{name:'Message the assistant'})).toHaveProperty('value',draft)
 expect(write).not.toHaveBeenCalled();expect(runtime.calls).toEqual([])
})
