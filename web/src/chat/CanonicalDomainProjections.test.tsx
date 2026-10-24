// Handwritten assertions over temporary, actual Go producer observations. These
// are shared mounted controls; generated observations are never golden vectors.
import {readFileSync} from 'node:fs'
import {useSyncExternalStore} from 'react'
import {useLocation} from 'react-router-dom'
import {cleanup,fireEvent,screen,waitFor} from '@testing-library/react'
import {afterEach,describe,expect,it,vi} from 'vitest'
import {ChatStore} from './store'
import {GraphDraftNotice} from '../graphs/GraphArtifacts'
import {DialectRecipeNotice} from '../dialects/DialectRecipeArtifacts'
import {DialectActionReference} from '../dialects/DialectActionArtifacts'
import {PolicyChatProposals} from '../policies/PolicyChatProposals'
import {PolicyInterpretationProposals} from '../policies/PolicyInterpretation'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {connected,renderConnected,stubClient} from '../testing/harness'

const fixture=vi.hoisted(()=>({store:undefined as ChatStore|undefined}))
// Only provider plumbing is substituted: every document, strict reader, store,
// proposal control, dialog and admission guard below is the production module.
vi.mock('./ChatProvider',()=>({useChats:()=>{const store=fixture.store!;return {store,...useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot)}},useChatStore:()=>fixture.store}))
const outputs=process.env.JPS_DOMAIN_OUTPUTS
if(process.env.JPS_REQUIRE_DOMAIN_OUTPUTS==='1'&&!outputs)throw Error('JPS_DOMAIN_OUTPUTS is required for mounted domain acceptance')
const read=(path:string)=>JSON.parse(readFileSync(`${outputs}/${path}.json`,'utf8'))
const stores:ChatStore[]=[]
afterEach(()=>{cleanup();stores.forEach(store=>store.dispose());stores.length=0;vi.unstubAllGlobals();sessionStorage.clear();localStorage.clear()})
function Mounted({locked=false}:{locked?:boolean}){
 const store=fixture.store!,snapshot=useSyncExternalStore(store.subscribe,store.getSnapshot,store.getSnapshot),chat=snapshot.chats[0]!,location=useLocation()
 return <><GraphDraftNotice chatId={chat.id} drafts={chat.graphDrafts??[]}/><DialectRecipeNotice chatId={chat.id} drafts={chat.inputDrafts??[]} disabled={locked}/>
 <PolicyChatProposals chatId={chat.id} disabled={locked} history/><PolicyInterpretationProposals chatId={chat.id} disabled={locked} history/>
 {chat.inputActions?.map(action=><DialectActionReference key={action.id} chatId={chat.id} action={action} disabled={locked}/>)}
 <output aria-label="Current route">{location.pathname+location.search}</output></>
}
async function setup(path:string,{locked=false,source,retained}:{locked?:boolean;source?:unknown;retained?:unknown}={}){
 const document=read(path)
 // The Graph host fixture seeds only session identity. Supply unrelated UI
 // envelope fields; leave its produced checkpoint and graphDrafts untouched.
 if(path==='graph/chat')document.chats=document.chats.map((chat:object)=>({title:'Graph review',composer:'',pinned:false,archived:false,mode:'draft',view:'chat',...chat}))
 const write=vi.fn(async()=>{throw Error('Mount/review must not persist chat')})
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'fixture',content:structuredClone(document)}),write},{read:async()=>({project:'/project',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write})
 stores.push(store);fixture.store=store;await store.load();expect(store.getSnapshot().error).toBe('');expect(store.getSnapshot().chats).toHaveLength(1)
 const loaded=structuredClone(store.getSnapshot().chats[0])
 const fetch=vi.fn(async(path:unknown,init?:RequestInit)=>{
  const url=String(path),method=init?.method??'GET'
  if(method==='POST'&&url==='/api/dialect-drafts'&&retained)return Response.json(retained)
  expect(method,`Unexpected dispatch to ${url}`).toBe('GET')
  if(url.startsWith('/api/assistant-approvals/'))return Response.json({mode:'manual'})
  if(url.includes('/publications?request='))return Response.json({result:null})
  if(url.startsWith('/api/input-preparations/')&&source)return Response.json(source)
  throw Error(`Unexpected read ${url}`)
 });vi.stubGlobal('fetch',fetch);giveThisPageASessionForTesting('domain-mounted-session')
 const runtime=stubClient({}),view=renderConnected(<Mounted locked={locked}/>,connected({client:runtime.client}),{path:'/chat/chat-one'})
 const unchanged=()=>{expect(write).not.toHaveBeenCalled();expect(runtime.calls).toEqual([]);expect(store.getSnapshot().chats[0]).toEqual(loaded);expect(fetch.mock.calls.filter(([,init])=>(init?.method??'GET')!=='GET')).toEqual([])}
 return {store,document,loaded,fetch,view,unchanged}
}

describe.skipIf(!outputs)('actual canonical domain output in mounted shared controls',()=>{
 it('opens only the retained exact Graph review route and preserves candidate bytes on reload',async()=>{
  const h=await setup('graph/chat'),draft=h.store.getSnapshot().chats[0]!.graphDrafts![0]!
  expect(draft.id).toBe('flow');expect(draft.content).toContain('"missing":null,"flag":false,"zero":0,"literal":"50","number":1.20')
  const link=screen.getByRole('link',{name:'Review graph'})
  expect(link.getAttribute('href')).toBe(`/graphs?chat=chat-one&draft=${draft.draftId}`)
  fireEvent.click(link);expect(screen.getByLabelText('Current route').textContent).toBe(link.getAttribute('href'))
  await h.store.load();h.unchanged()
 })
 it.each(['passed','stale'])('renders the %s Policy candidate and keeps review admission current',async(status)=>{
  const h=await setup(`policy/${status}`),proposal=h.store.getSnapshot().chats[0]!.policyProposals![0]!
  expect(proposal.check?.status).toBe(status)
  expect(proposal.candidate).toEqual(status==='passed'?{title:' Exact title ',narrative:'Preserve missing, false, zero, 1.20 and "50" exactly.\r\n中文 <record>.',artifactVersion:'1.1.0'}:{title:'Changed',narrative:'Only approved staff may access records.\r\nRetain missing values, 中文, < and >.',artifactVersion:'1.0.0'})
  const review=screen.getByRole('button',{name:'Review and save draft'})
  expect(review).toHaveProperty('disabled',status==='stale')
  if(status==='passed'){
   expect(screen.getByRole('region',{name:'Proposed pack interpretation'})).toBeTruthy();expect(screen.getByRole('region',{name:'Proposed graph interpretation'})).toBeTruthy()
   fireEvent.click(review);expect(await screen.findByRole('dialog',{name:'Review Policy draft update'})).toBeTruthy();expect(screen.getByRole('button',{name:'Save draft update'})).toHaveProperty('disabled',false)
  }else{fireEvent.click(review);expect(screen.queryByRole('dialog')).toBeNull()}
  h.unchanged()
 })
 it('locks the passed Policy update and both interpretation actions without writes',async()=>{
  const h=await setup('policy/passed',{locked:true})
  for(const name of ['Review and save draft','Auto within limits…','Design pack','Design graph']){const button=screen.getByRole('button',{name});expect(button).toHaveProperty('disabled',true);fireEvent.click(button)}
  expect(screen.queryByRole('dialog')).toBeNull();await h.store.load();h.unchanged()
 })
 it('retains typed recipe fields, blocks locked review, then opens only its independent exact draft',async()=>{
  const retained=read('dialect-recipe/draft');let h=await setup('dialect-recipe/chat',{locked:true,retained});const proposal=h.store.getSnapshot().chats[0]!.inputDrafts![0]!
  expect(proposal.fields.map(field=>field.type)).toEqual(['text','integer','decimal','boolean','text-list','range','string','number'])
  expect(proposal.intent).toBe('Preserve exact fields.\r\n中文 <record>&')
  const button=screen.getByRole('button',{name:'Review recipe'});expect(button).toHaveProperty('disabled',true);fireEvent.click(button);h.unchanged()
  h.view.unmount();h=await setup('dialect-recipe/chat',{retained});fireEvent.click(screen.getByRole('button',{name:'Review recipe'}))
  await waitFor(()=>expect(screen.getByLabelText('Current route').textContent).toBe(`/dialects/drafts/${retained.document.id}?chat=chat-one`))
  const calls=h.fetch.mock.calls.filter(([,init])=>init?.method==='POST');expect(calls).toHaveLength(1)
  expect(JSON.parse(calls[0]![1]!.body as string)).toMatchObject({origin:{chatId:'chat-one',draftId:proposal.draftId},name:'Portable recipe',recipe:{intent:proposal.intent,research:{fields:proposal.fields}}})
  expect(h.store.getSnapshot().chats[0]).toEqual(h.loaded)
 })
 it('keeps checked mapping/publication identities and blocks locked publication confirmation',async()=>{
  const h=await setup('dialect-mapping-publication/chat',{locked:true}),actions=h.store.getSnapshot().chats[0]!.inputActions!
  expect(actions.map(action=>action.kind)).toEqual(['update-dialect','publish-dialect'])
  expect(actions[0]!.dialect!.facts).toEqual([{target:'/company/country',column:'Country',conversion:{kind:'text'}}])
  expect(screen.getByRole('button',{name:'Review dialect'})).toHaveProperty('disabled',true)
  fireEvent.click(screen.getByRole('button',{name:'Review publication'}))
  expect(await screen.findByRole('dialog',{name:'Review publication'})).toBeTruthy()
  await waitFor(()=>expect(h.fetch.mock.calls.some(([path])=>String(path).includes('/publications?request='))).toBe(true))
  await waitFor(()=>expect(screen.queryByText('Checking saved result…')).toBeNull())
  const publish=screen.getByRole('button',{name:'Publish version'});expect(publish).toHaveProperty('disabled',true);fireEvent.click(publish)
  expect(screen.getByText('Review original asserted fields <record>& 中文')).toBeTruthy();h.unchanged()
 })
 it('renders all six record/action reviews, retaining selected IDs and typed original observations while locked',async()=>{
  const h=await setup('dialect-record-actions/chat',{locked:true}),chat=h.store.getSnapshot().chats[0]!,actions=chat.inputActions!,records=read('dialect-record-actions/records'),source=read('dialect-record-actions/source')
  expect(actions.map(a=>a.kind)).toEqual(['update-recipe','remove-recipe','research','reprocess','stop-research','evaluate'])
  expect(actions[3]!.recordIds).toEqual([source.document.records[1].id]);expect(actions[5]!.recordIds).toEqual([source.document.records[1].id]);expect(actions[4]!.runId).toBe('research_11111111111111111111111111111111')
  expect(records.items[1].research.fields.flag.value).toBe(false);expect(records.items[1].research.fields.zero.value).toBe(0);expect(records.items[1].research.fields.empty.value).toBe('')
  expect(screen.getAllByRole('button',{name:/^Review (changes|action|research|reprocessing)$/})).toHaveLength(6)
  for(const button of screen.getAllByRole('button',{name:/^Review (changes|action|research|reprocessing)$/})){expect(button).toHaveProperty('disabled',true);fireEvent.click(button)}
  expect(screen.queryByRole('dialog')).toBeNull();await h.store.load();h.unchanged()
 })
 it.each(['dialect-mapping-publication','dialect-record-actions'])('refuses stale %s reviews before any domain dispatch',async(domain)=>{
  const source={...read(`${domain}/source`),sha256:'f'.repeat(64)},h=await setup(`${domain}/chat`,{source})
  const controls=domain==='dialect-mapping-publication'?[['Review dialect',0] as const]:[['Review changes',0] as const,['Review action',0] as const,['Review research',0] as const,['Review reprocessing',0] as const,['Review action',1] as const,['Review action',2] as const]
  for(const [name,index] of controls){fireEvent.click(screen.getAllByRole('button',{name})[index]!);expect(await screen.findByText('Dialects changed. Request a new proposal against the current recipe.')).toBeTruthy();expect(screen.queryByRole('button',{name:/^(Save recipe|Save dialect|Start research|Evaluate)$/})).toBeNull();fireEvent.click(screen.getByRole('button',{name:'Close'}))}
  h.unchanged()
 })
})
