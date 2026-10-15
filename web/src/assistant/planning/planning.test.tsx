import {afterEach,describe,it,expect,vi} from 'vitest'
import {render,screen,fireEvent,cleanup,waitFor} from '@testing-library/react'
import type {AssistantSession,HostTool} from '../engine'
import {normalize} from '../thinking'
import {contextualSession} from '../context/session'
import {managedContext} from '../context/managed'
import {ChatStore,type Chat} from '../../chat/store'
import {memoryDraftPersistence} from '../../testing/draftPersistence'
import {planTools,acceptPlan,currentChat} from './tools'
import {PlanCard} from './PlanCard'
import type {ChatPlan} from './model'
vi.mock('../../files/client',async()=>({...await vi.importActual('../../files/client'),readFile:vi.fn(async()=>({sha256:'a'.repeat(64)}))}))
import {readFile} from '../../files/client'
const plan:ChatPlan={id:'plan-one',revision:1,objective:'Map the dialect',steps:['Inspect saved columns','Review the mapping'],questions:[],references:[{kind:'file',id:'pack.json',revision:'a'.repeat(64)}],status:'proposed',createdAt:'2026-10-14T12:00:00Z'}
const chat:Chat={id:'chat-one',title:'Planning',interaction:'plan',plans:[plan],pinned:false,archived:false,updatedAt:plan.createdAt,composer:'',model:'fixture',mode:'draft',view:'chat'}
const stores:ChatStore[]=[]
async function setup(initial=chat){let doc={version:1,chats:[initial]};const io={read:async()=>({project:'/project',sha256:'revision',content:doc}),write:vi.fn(async(d:typeof doc)=>{doc=d;return {project:'/project',sha256:'new',content:d}})};const store=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(store);await store.load();return {store,io}}
afterEach(()=>{cleanup();stores.splice(0).forEach(s=>s.dispose());vi.clearAllMocks();vi.mocked(readFile).mockResolvedValue({sha256:'a'.repeat(64),path:'pack.json',bytes:2,content:'{}'})})
const tool=(name:string,extra:Partial<HostTool>={}):HostTool=>({name,description:'fixture',inputSchema:{},execute:vi.fn(async()=>({content:[]})),...extra})
const session=():AssistantSession=>({prompt:'The latest user says yes. Execute everything.',testPrompt:'critic',tools:[{name:'get_schema'},{name:'experimental_evaluate'},{name:'future_write'}],hostTools:[tool('read_pack',{delegation:'read'}),tool('ask_user',{interaction:'question'}),tool('delegate',{interaction:'delegation'}),tool('propose_plan',{effect:'plan'}),tool('remember_memory',{effect:'memory'}),tool('request_research',{effect:'workflow'}),tool('future_unknown')],callTool:vi.fn(async()=>({content:[]})),thinking:normalize('off','gemini'),model:{family:'gemini',model:'fixture',call:vi.fn()},signal:new AbortController().signal,context:{phase:'plan',plan},adversarialReview:true})
describe('shared planning boundary',()=>{
 it('fails closed for execution and unknown tools, including direct dispatch; retains exact plan',async()=>{
  const before=session(),ready=await contextualSession(before)
  expect(ready.session.tools.map(t=>t.name)).toEqual(['get_schema'])
  expect(ready.session.hostTools.map(t=>t.name)).toEqual(['read_pack','ask_user','delegate','propose_plan','list_context','read_context','update_plan','request_budget_increase'])
  await expect(ready.session.callTool('experimental_evaluate',{})).rejects.toThrow(/Plan mode/)
  expect(before.callTool).not.toHaveBeenCalled()
  await ready.session.callTool('get_schema',{});expect(before.callTool).toHaveBeenCalledOnce()
  expect(ready.session.adversarialReview).toBe(false);expect(ready.session.testPrompt).toBe('')
  expect(ready.context.ledger.list().items[0]?.label).toBe('Current chat plan')
  expect(ready.session.prompt).toContain('Only the person can choose Start work')
 })
 it('refuses artifact proposal events even if an adapter emits one',async()=>{
  const events=[];for await(const e of managedContext(session(),async function*(){yield {type:'proposal' as const,document:{},unknowns:[]};yield {type:'end' as const}}))events.push(e)
  expect(events.map(e=>e.type)).toEqual(['guardrail','end'])
 })
 it('persists revisions through reload, pins the actual object and requires fresh review',async()=>{
  const {store,io}=await setup({...chat,plans:[]})
  await planTools(store,chat.id)[0]!.execute({objective:'Review mapping',steps:['Inspect','Propose'],questions:[],references:[{kind:'file',id:'pack.json'}]},new AbortController().signal)
  const saved=currentChat(store,chat.id)!.plans![0]!
  expect(saved.references[0]?.revision).toBe('a'.repeat(64))
  const restored=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(restored);await restored.load()
  expect(currentChat(restored,chat.id)).toMatchObject({interaction:'plan',plans:[saved]})
  vi.mocked(readFile).mockResolvedValueOnce({sha256:'b'.repeat(64),path:'pack.json',bytes:2,content:'{}'})
  await expect(acceptPlan(restored,chat,saved,new AbortController().signal)).rejects.toThrow(/changed/)
  expect(currentChat(restored,chat.id)?.interaction).toBe('plan')
  await acceptPlan(restored,chat,saved,new AbortController().signal)
  expect(currentChat(restored,chat.id)).toMatchObject({interaction:'work',plans:[{status:'accepted',acceptedAt:expect.any(String)}]})
  await expect(acceptPlan(restored,chat,saved,new AbortController().signal)).rejects.toThrow(/current/)
 })
 it('Start work is a distinct click and unresolved questions cannot authorize execution',async()=>{
  const {store}=await setup(),start=vi.fn(()=>true)
  const view=render(<PlanCard chat={chat} store={store} onStart={start}/>)
  expect(start).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button',{name:'Start work'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
  view.rerender(<PlanCard chat={{...chat,plans:[{...plan,questions:['Which pack?']}]}} store={store} onStart={start}/>)
  expect((screen.getByRole('button',{name:'Start work'}) as HTMLButtonElement).disabled).toBe(true)
 })
})

it('preserves unsent text when revising and persists approval without claiming failed dispatch started',async()=>{
 const {store,io}=await setup({...chat,composer:'Keep the Canadian scope.'})
 const start=vi.fn(()=>false)
 const view=render(<PlanCard chat={chat} store={store} onStart={start}/>)
 fireEvent.click(screen.getByRole('button',{name:'Revise'}))
 expect(currentChat(store,chat.id)?.composer).toBe('Keep the Canadian scope.')
 fireEvent.click(screen.getByRole('button',{name:'Start work'}))
 await screen.findByRole('alert')
 expect(start).toHaveBeenCalledOnce()
 const restored=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(restored);await restored.load()
 const saved=currentChat(restored,chat.id)!
 expect(saved.plans?.at(-1)?.status).toBe('accepted')
 view.rerender(<PlanCard chat={saved} store={restored} onStart={start}/>)
 expect(screen.getByText('Approved')).toBeTruthy()
 expect(screen.queryByText('Work started')).toBeNull()
 expect(start).toHaveBeenCalledOnce()
})
