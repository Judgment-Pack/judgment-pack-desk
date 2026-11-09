import {useEffect,useSyncExternalStore} from 'react'
import {cleanup,fireEvent,screen,waitFor,within} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {ChatPanel} from './ChatPanel'
import {ChatStore,type Chat} from './store'
import {useSavedChatRun} from '../assistant/serverRuns/useSavedChatRun'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {connected,renderConnected,stubClient} from '../testing/harness'
import {INITIAL_STATE} from '../research/initialState'
import approval from '../assistant/serverRuns/fixtures/approval.json'
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
const hash='a'.repeat(64),reviewId='0123456789abcdef0123456789abcdef',approvalId='abcdef0123456789abcdef0123456789'
const at='2026-01-02T03:04:05Z'
// Handwritten current declaration schemas from assistant_{search,link,website}_tools.go,
// assistant_dialect_research_tools.go and input_preparations.go; not captured output.
const schemas=[
 {type:'object',properties:{query:{type:'string',maxLength:2000,description:'A concise public search query. Do not include private attachments or secrets.'}},required:['query'],additionalProperties:false},
 {type:'object',properties:{url:{type:'string',maxLength:4096},offset:{type:'integer',minimum:0,maximum:9007199254740991},find:{type:'string',maxLength:200}},required:['url'],additionalProperties:false},
 {type:'object',properties:{url:{type:'string',maxLength:4096}},required:['url'],additionalProperties:false},
 {type:'object',properties:{id:{type:'string',pattern:'^prep_[a-f0-9]{32}$'},revision:{type:'string',pattern:'^[a-f0-9]{64}$'}},required:['id','revision'],additionalProperties:false},
]
// Handwritten complete Desk singleton catalog; never captured from a run.
function catalog(approved=false){
 return {version:1,sources:[
  ['desk-search','search_sources','discover','read-external','read','research','none'],
  ['desk-link','read_link','read','read-external','read','read','none'],
  ['desk-website','explore_website','discover','read-external','read','research','none'],
  ['desk-research-workflow','request_dialect_research','act','act','act','','required'],
 ].map(([adapterId,name,operation,effect,effectClass,delegation,approval],index)=>{
  const ready=index!==1
  return {snapshot:{adapterId,adapterVersion:'1',kind:'custom',trust:'first-party',configurationDigest:hash,toolListDigest:hash,
   completeDeclarations:[{name,description:'<b>Complete declaration</b>',inputSchema:schemas[index],taxonomy:{operation,effect},effectClass,delegation,approval}],
   configuration:{target:index===0?'https://source.example.test':'',targetDigest:'',authorityDigest:'',publicKeyDigest:'',deskDigest:hash,preferenceDigest:'',connection:'',revision:'',provider:'',bundleDigest:'',verifierDigest:'',gatewayBuildDigest:'',ownerDigest:hash,catalogDigest:'',catalogOwnerDigest:''},
   status:ready?'review-required':'setup-required',...(ready?{identity:{kind:'connection',label:'Desk research '+(index===3?'workflow ':'HTTP ')+hash},identityDigest:hash}:{})},
   status:ready?approved&&index===0?'approved':'review-required':'setup-required',
   ...(approved&&index===0?{approval:{id:approvalId,revision:1,approvedAt:at}}:ready&&!approved?{reviewId:index===0?reviewId:String(index).repeat(32),reviewRevision:1}:{})}
 })}
}
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
async function mountChat(pendingApproval=false){
 const draft='Preserve the exact unsent draft.',chat:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:draft,model:'shared-model',mode:'draft',view:'chat',...(pendingApproval?{serverRequest:{requestId:approval.requestId,admissionId:approval.admissionId}}:{}),checkpoint:{state:INITIAL_STATE,sources:[]}}
 const document={version:1,chats:[chat]},write=vi.fn(async()=>{throw Error('No browser chat write expected')})
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'fixture',content:structuredClone(document)}),write},{read:async()=>({project:'/project',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write})
 fixture.store=store;stores.push(store);await store.load();expect(store.getSnapshot().error).toBe('')
 const fetch=vi.fn(async(path:unknown,_init?:RequestInit)=>{
  if(String(path).startsWith('/api/assistant-source-approvals/')){
   if(String(path).endsWith('/approve'))return json(catalog(true))
   if(String(path).endsWith('/revoke'))return json({version:1,status:'revoked',approval:{id:approvalId,revision:2,approvedAt:at,revokedAt:'2026-01-02T03:05:05Z'}})
   return json(catalog())
  }
  if(String(path).startsWith('/api/assistant-runs/owners/'))return Response.json({runs:[approval]})
  return Response.json({root:'/project',files:[],version:1,subjects:{},reviews:[],settings:{},suites:{},mode:'manual'})
 });vi.stubGlobal('fetch',fetch);giveThisPageASessionForTesting('mounted-source-review-session')
 const runtime=stubClient({});runtime.client.listTools=async()=>({tools:[]})
 renderConnected(<Mounted/>,connected({client:runtime.client}),{path:'/chat/chat-one'})
 await waitFor(()=>expect(store.getSnapshot().bindings.get(chat.id)?.run).toBeTruthy())
 return {store,write,fetch,runtime,draft,chat}
}
it('mounts source review/approval/revocation without changing draft, shared ownership or executing work',async()=>{
 const h=await mountChat(),run=h.store.getSnapshot().bindings.get(h.chat.id)!.run,composer=screen.getByRole('textbox',{name:'Message the assistant'})
 const calls=()=>h.fetch.mock.calls.filter(([path])=>String(path).startsWith('/api/assistant-source-approvals/'))
 expect(calls()).toEqual([]);fireEvent.click(screen.getByRole('button',{name:'Source approvals'}));expect(calls()).toEqual([])
 fireEvent.click(screen.getByRole('button',{name:'Review sources'}))
 const source=within(await screen.findByRole('region',{name:'desk-search'}))
 expect(calls()).toHaveLength(1);fireEvent.click(source.getByRole('button',{name:'Approve source'}))
 const revoke=await source.findByRole('button',{name:'Revoke source approval'});fireEvent.click(revoke)
 await screen.findByRole('region',{name:'Source approval receipt'})
 expect(calls().map(([path])=>path)).toEqual(['/api/assistant-source-approvals/chat-one','/api/assistant-source-approvals/chat-one/approve','/api/assistant-source-approvals/chat-one/revoke'])
 expect(JSON.parse(String(calls()[1]![1]?.body))).toEqual({reviewId,reviewRevision:1});expect(JSON.parse(String(calls()[2]![1]?.body))).toEqual({approvalId,approvalRevision:1})
 fireEvent.click(screen.getByRole('button',{name:'Close'}))
 expect(composer).toHaveProperty('value',h.draft);expect(h.store.getSnapshot().chats[0]!.composer).toBe(h.draft)
 expect(h.store.getSnapshot().bindings.get(h.chat.id)!.run).toBe(run);expect(run?.ownership).toBe('server')
 expect(h.write).not.toHaveBeenCalled();expect(h.runtime.calls).toEqual([])
 expect(h.fetch.mock.calls.filter(([path,options])=>!String(path).startsWith('/api/assistant-source-approvals/')&&options?.method==='POST')).toEqual([])
})
it('keeps an acknowledged pending host action distinct and blocks toolbar review with the composer',async()=>{
 const h=await mountChat(true),decision=within(await screen.findByRole('group',{name:'Approval required'}))
 expect(decision.getByText('write_file')).toBeTruthy();expect(decision.getByRole('button',{name:'Approve'})).toHaveProperty('disabled',false)
 expect(screen.getByRole('button',{name:'Source approvals'})).toHaveProperty('disabled',true)
 expect(screen.getByRole('textbox',{name:'Message the assistant'})).toHaveProperty('value',h.draft)
 expect(h.fetch.mock.calls.filter(([path])=>String(path).startsWith('/api/assistant-source-approvals/'))).toEqual([])
 expect(h.write).not.toHaveBeenCalled();expect(h.runtime.calls).toEqual([])
})
