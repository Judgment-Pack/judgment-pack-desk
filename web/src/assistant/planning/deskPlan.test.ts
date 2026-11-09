import {afterEach,describe,expect,it,vi} from 'vitest'
import {ChatStore,type Chat} from '../../chat/store'
import {memoryDraftPersistence} from '../../testing/draftPersistence'
import {deskPlanResolver,validDeskPlans} from './deskPlan'
import {conversationWire} from '../../chat/wire'
import {INITIAL_STATE} from '../../research/initialState'
import type {ChatPlan} from '@judgment-pack/harness/planning/model'
import vectors from '../../../../internal/desk/testdata/assistant-plan-v1.json'
vi.mock('../../files/client',async()=>({...await vi.importActual('../../files/client'),deskFetch:vi.fn()}))
import {deskFetch} from '../../files/client'

const plan:ChatPlan={id:'current-plan',revision:1,objective:'Review saved dependencies',steps:['Read the saved pack'],questions:[],references:[{kind:'artifact',id:'opaque-one',digest:'a'.repeat(64)}],status:'proposed',createdAt:'2026-10-24T12:00:00Z'}
const chat:Chat={id:'chat-one',title:'Current Plan',interaction:'plan',plans:[plan],pinned:false,archived:false,updatedAt:plan.createdAt,composer:'',model:'fixture',mode:'draft',view:'chat'}
const stores:ChatStore[]=[]
afterEach(()=>{stores.splice(0).forEach(store=>store.dispose());vi.clearAllMocks()})
const signal=()=>new AbortController().signal
async function setup(initial:Chat[]=[chat]) {
 let doc={version:1 as const,chats:structuredClone(initial)},revision=1
 const reply=()=>({project:'fixture',sha256:String(revision),content:structuredClone(doc),chatRevisions:Object.fromEntries(doc.chats.map(chat=>[chat.id,'b'.repeat(64)]))})
 const io={read:vi.fn(async()=>reply()),write:vi.fn(async(content:typeof doc,digest:string)=>{expect(digest).toBe(String(revision));revision++;doc=structuredClone(content);return reply()})}
 const store=new ChatStore('fixture',io,memoryDraftPersistence('fixture'));stores.push(store);await store.load()
 const resolver=deskPlanResolver(plan,{chat:chat.id,revision:signal=>store.reviewedPlanRevision(chat.id,plan,signal)})
 return {store,io,resolver,get saved(){return doc},remote(changed:Chat){doc={version:1,chats:[changed]};revision++}}
}

describe('Desk current Plan references',()=>{
 it('reads handwritten historical/current forms and refuses closed-shape, duplicate and bounded mutations',()=>{
  for(const key of ['plans','accepted','started','mixed'] as const)expect(validDeskPlans(vectors[key]),key).toBe(true)
  for(const raw of vectors.invalidReferences){
   let rejected=false
   try{conversationWire(`{"content":{"chats":[{"id":"chat-one","references":[${raw}]}]}}`);rejected=!validDeskPlans([{...plan,references:[JSON.parse(raw)]}])}catch{rejected=true}
   expect(rejected,raw).toBe(true)
  }
  for(const length of [256,257])expect(validDeskPlans([{...plan,references:[{kind:'artifact',id:'x'.repeat(length),digest:'a'.repeat(64)}]}])).toBe(length===256)
  for(const refs of [[plan.references[0],plan.references[0]],[plan.references[0],{kind:'dialect',id:'opaque-one',digest:'a'.repeat(64)}]])expect(validDeskPlans([{...plan,references:refs}])).toBe(false)
 })
 it('uses the acknowledged saved proposal and exact chat CAS for production lookup during acceptance',async()=>{
  const h=await setup()
  vi.mocked(deskFetch).mockImplementation(async(path,init)=>{
   expect(path).toBe('/api/conversations/chat-one/plans/current-plan/references/opaque-one')
   expect(init?.headers).toEqual({'If-Match':'b'.repeat(64),'X-Plan-Revision':'1'})
   expect(h.saved.chats[0].plans?.[0].status).toBe('proposed')
   return Response.json(plan.references[0])
  })
  await h.store.acceptReviewedPlan(chat.id,plan,h.resolver,signal())
  expect(deskFetch).toHaveBeenCalledOnce();expect(h.saved.chats[0]).toMatchObject({interaction:'work',plans:[{status:'accepted',references:plan.references}]})
 })
 it('promotes an unsaved draft before requesting its opaque reference',async()=>{
  const h=await setup([]),draft=h.store.startChat(undefined,undefined,true)
  h.store.update(draft.id,{interaction:'plan',plans:[plan]})
  const resolver=deskPlanResolver(plan,{chat:draft.id,revision:signal=>h.store.reviewedPlanRevision(draft.id,plan,signal)})
  vi.mocked(deskFetch).mockImplementation(async(path)=>{
   expect(path).toBe(`/api/conversations/${draft.id}/plans/current-plan/references/opaque-one`)
   expect(h.io.write).toHaveBeenCalledOnce();expect(h.saved.chats[0].plans).toEqual([plan])
   return Response.json(plan.references[0])
  })
  await h.store.acceptReviewedPlan(draft.id,plan,resolver,signal())
  expect(h.io.write).toHaveBeenCalledTimes(2)
 })
 it('compares saved checkpoints through the same message-ownership projection as load',async()=>{
  const h=await setup([{...chat,checkpoint:{state:{...INITIAL_STATE,turns:[{id:'turn-one',role:'user',kind:'brief',at:plan.createdAt,text:'Inspect this dependency'}]},sources:[]}}])
  vi.mocked(deskFetch).mockResolvedValue(Response.json(plan.references[0]))
  await h.store.acceptReviewedPlan(chat.id,plan,h.resolver,signal())
  expect(h.saved.chats[0].plans?.[0].status).toBe('accepted')
 })
 it.each(['digest','deleted','scope','malformed'] as const)('refuses %s lookup without saving acceptance',async(mode)=>{
  const h=await setup()
  vi.mocked(deskFetch).mockResolvedValue(mode==='deleted'?Response.json({error:'Reference no longer exists',code:'not-found'},{status:404}):Response.json({...plan.references[0],...(mode==='digest'?{digest:'f'.repeat(64)}:mode==='scope'?{id:'other'}:{locator:'one.json'})}))
  await expect(h.store.acceptReviewedPlan(chat.id,plan,h.resolver,signal())).rejects.toThrow()
  expect(h.io.write).not.toHaveBeenCalled();expect(h.saved.chats[0].interaction).toBe('plan')
 })
 it('refuses a changed saved Plan and an unknown handle before HTTP lookup',async()=>{
  const h=await setup()
  h.remote({...chat,plans:[{...plan,objective:'Changed saved review'}]})
  await expect(h.resolver.resolve({kind:'artifact',id:'opaque-one'},signal())).rejects.toThrow(/changed/)
  await expect(h.resolver.resolve({kind:'artifact',id:'unknown'},signal())).rejects.toThrow(/host must resolve/)
  expect(deskFetch).not.toHaveBeenCalled()
 })
 it('rechecks local review identity after saved-revision lookup waits',async()=>{
  const h=await setup()
  let release!:()=>void
  const wait=new Promise<void>(resolve=>{release=resolve})
  const original=h.io.read.getMockImplementation()!
  h.io.read.mockImplementationOnce(async()=>{await wait;return original()})
  const pending=h.resolver.resolve({kind:'artifact',id:'opaque-one'},signal())
  h.store.update(chat.id,{pack:{id:'changed',path:'other.json',digest:'c'.repeat(64)}})
  release();await expect(pending).rejects.toThrow(/changed/);expect(deskFetch).not.toHaveBeenCalled()
 })
 it('real document reader refuses duplicate raw members before JSON.parse can collapse them',async()=>{
  const raw=`{"project":"fixture","sha256":"1","content":{"version":1,"chats":[${JSON.stringify(chat).replace('"id":"opaque-one"','"id":"opaque-one","id":"other"')}]}}`
  vi.mocked(deskFetch).mockResolvedValue(new Response(raw))
  const store=new ChatStore('fixture',undefined,memoryDraftPersistence('fixture'));stores.push(store);await store.load()
  expect(store.getSnapshot().error).toMatch(/duplicate/);expect(store.getSnapshot().chats).toEqual([])
 })
 it('preserves mixed imported references on display, reload and unrelated saves without custody',async()=>{
  const mixed=vectors.mixed as ChatPlan[],h=await setup([{...chat,plans:mixed}])
  expect(h.store.getSnapshot().error).toBe('')
  h.store.update(chat.id,{title:'Unrelated edit'});expect(await h.store.flush()).toBe(true)
  expect(h.saved.chats[0].plans).toEqual(mixed)
  const restored=new ChatStore('fixture',h.io,memoryDraftPersistence('fixture'));stores.push(restored);await restored.load()
  expect(restored.getSnapshot().chats[0].plans).toEqual(mixed)
 })
})
