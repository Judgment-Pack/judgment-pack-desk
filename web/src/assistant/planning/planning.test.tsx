// Exercise Desk's real document CAS owner; the production Plan tool is the Go host.
import {afterEach,describe,it,expect,vi} from 'vitest'
import {render,screen,fireEvent,cleanup,waitFor} from '@testing-library/react'
import {ChatStore,type Chat,type ChatPersistence} from '../../chat/store'
import {memoryDraftPersistence} from '../../testing/draftPersistence'
import {PlanCard} from './PlanCard'
import {deskPlanResolver} from './deskPlan'
import type {ChatPlan} from '@judgment-pack/harness/planning/model'
import type {PlanResolver} from '@judgment-pack/harness/planning/references'
vi.mock('../../files/client',async()=>({...await vi.importActual('../../files/client'),readFile:vi.fn()}))
vi.mock('../../dialects/client',()=>({dialectAPI:vi.fn()}))
import {readFile} from '../../files/client'
import {dialectAPI} from '../../dialects/client'
const plan:ChatPlan={id:'plan-one',revision:1,objective:'Map the dialect',steps:['Inspect saved columns','Review the mapping'],questions:[],references:[{kind:'file',id:'pack.json',revision:'a'.repeat(64)}],status:'proposed',createdAt:'2026-10-14T12:00:00Z'}
const chat:Chat={id:'chat-one',title:'Planning',interaction:'plan',plans:[plan],pinned:false,archived:false,updatedAt:plan.createdAt,composer:'',model:'fixture',mode:'draft',view:'chat'}
const stores:ChatStore[]=[]
const current=(store:ChatStore,id=chat.id)=>[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===id)!
const signal=()=>new AbortController().signal
function deferred(){let resolve!:()=>void;const promise=new Promise<void>(done=>{resolve=done});return {promise,resolve}}
function open(io:ChatPersistence){const store=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(store);return store}
async function setup(initial:Chat[]= [chat]){
 let doc={version:1 as const,chats:structuredClone(initial)},revision=1
 let beforeWrite:(()=>Promise<void>)|undefined,uncertain=false,rejected=false
 const reply=()=>({project:'/project',sha256:String(revision),content:structuredClone(doc)})
 const io:ChatPersistence={read:vi.fn(async()=>reply()),write:vi.fn(async(candidate,digest)=>{
  const hook=beforeWrite;beforeWrite=undefined;await hook?.()
  if(rejected||digest!==String(revision))throw Error('CAS refused')
  doc=structuredClone(candidate);revision++
  if(uncertain)throw Error('Acknowledgement lost')
  return reply()
 })}
 const store=open(io);await store.load()
 return {store,io,get saved(){return doc.chats},hold(){const entered=deferred(),release=deferred();beforeWrite=async()=>{entered.resolve();await release.promise};return {entered:entered.promise,release:release.resolve}},remote(rows:Chat[]){doc={version:1,chats:structuredClone(rows)};revision++},reject(){rejected=true},uncertain(){uncertain=true}}
}
async function accept(store:ChatStore,reviewed=plan,resolver=deskPlanResolver(reviewed),id=chat.id){return store.acceptReviewedPlan(id,reviewed,resolver,signal())}
afterEach(()=>{cleanup();stores.splice(0).forEach(s=>s.dispose());vi.clearAllMocks();vi.mocked(readFile).mockResolvedValue({sha256:'a'.repeat(64),path:'pack.json',bytes:2,content:'{}'});vi.mocked(dialectAPI).mockResolvedValue({sha256:'a'.repeat(64)})})
// Initialize readers before the first test too.
vi.mocked(readFile).mockResolvedValue({sha256:'a'.repeat(64),path:'pack.json',bytes:2,content:'{}'})
vi.mocked(dialectAPI).mockResolvedValue({sha256:'a'.repeat(64)})
describe('Desk durable Plan acceptance',()=>{
 it('does not publish approval or dispatch before exact durable acknowledgement',async()=>{
  const h=await setup(),gate=h.hold(),start=vi.fn(()=>true)
  render(<PlanCard chat={chat} store={h.store} onStart={start}/>)
  fireEvent.click(screen.getByRole('button',{name:'Start work'}));await gate.entered
  expect(current(h.store).interaction).toBe('plan');expect(current(h.store).plans?.at(-1)?.status).toBe('proposed');expect(start).not.toHaveBeenCalled()
  gate.release();await waitFor(()=>expect(start).toHaveBeenCalledOnce())
  await waitFor(()=>expect(h.saved[0].plans?.at(-1)?.status).toBe('started'))
 })
 it('persists legacy pins unchanged across acceptance and reload and requires current review',async()=>{
  const h=await setup()
  vi.mocked(readFile).mockResolvedValueOnce({sha256:'b'.repeat(64),path:'pack.json',bytes:2,content:'{}'})
  await expect(accept(h.store)).rejects.toThrow(/changed/)
  expect(h.io.write).not.toHaveBeenCalled();expect(current(h.store).interaction).toBe('plan')
  await accept(h.store)
  const restored=open(h.io);await restored.load()
  expect(current(restored)).toMatchObject({interaction:'work',plans:[{status:'accepted',references:plan.references,acceptedAt:expect.any(String)}]})
  await expect(accept(restored)).rejects.toThrow(/changed/)
 })
 it.each(['plans','interaction','pack'] as const)('rechecks %s after an awaited reference read',async field=>{
  const h=await setup(),entered=deferred(),release=deferred()
  const resolver:PlanResolver={resolve:async()=>{throw Error('unexpected')},legacy:async()=>{entered.resolve();await release.promise;return {kind:'artifact',id:'bound-object',digest:'a'.repeat(64)}}}
  const pending=accept(h.store,plan,resolver);await entered.promise
  if(field==='plans')h.store.update(chat.id,{plans:[{...plan,objective:'Changed scope'}]})
  if(field==='interaction')h.store.update(chat.id,{interaction:'work'})
  if(field==='pack')h.store.update(chat.id,{pack:{id:'new-pack',path:'other.json',digest:'b'.repeat(64)}})
  release.resolve();await expect(pending).rejects.toThrow(/changed/);expect(h.io.write).not.toHaveBeenCalled()
 })
 it('refuses remote changed Plan scope and same-field conflicts without a write',async()=>{
  const h=await setup(),entered=deferred(),release=deferred()
  const resolver:PlanResolver={resolve:async()=>{throw Error('unexpected')},legacy:async()=>{entered.resolve();await release.promise;return {kind:'artifact',id:'bound-object',digest:'a'.repeat(64)}}}
  const pending=accept(h.store,plan,resolver);await entered.promise
  h.store.update(chat.id,{composer:'Local text'})
  h.remote([{...chat,composer:'Remote text',plans:[{...plan,objective:'Remote revision'}]}])
  release.resolve();await expect(pending).rejects.toThrow(/same chat field/)
  expect(current(h.store).composer).toBe('Local text');expect(h.io.write).not.toHaveBeenCalled()
 })
 it('refuses remote Plan changes after reference resolution',async()=>{
  const h=await setup()
  const resolver:PlanResolver={resolve:async()=>{throw Error('unexpected')},legacy:async()=>{h.remote([{...chat,plans:[{...plan,objective:'Remote scope'}]}]);return {kind:'artifact',id:'bound-object',digest:'a'.repeat(64)}}}
  await expect(accept(h.store,plan,resolver)).rejects.toThrow(/changed/)
  expect(h.io.write).not.toHaveBeenCalled()
 })
 it('waits for an in-flight ordinary flush and refuses a changed reviewed target',async()=>{
  const h=await setup();h.store.update(chat.id,{composer:'Pending composer'})
  const gate=h.hold(),flushed=h.store.flush();await gate.entered
  const pending=accept(h.store);h.store.update(chat.id,{plans:[{...plan,questions:['New question?']}]})
  expect(h.io.write).toHaveBeenCalledOnce();gate.release();expect(await flushed).toBe(true)
  await expect(pending).rejects.toThrow(/changed/)
  expect(h.io.write).toHaveBeenCalledOnce();expect(h.saved[0].interaction).toBe('plan')
 })
 it('refuses a remote CAS race after the final read and leaves the remote revision intact',async()=>{
  const h=await setup(),gate=h.hold(),pending=accept(h.store);await gate.entered
  h.remote([{...chat,plans:[{...plan,objective:'Concurrent remote revision'}]}]);gate.release()
  await expect(pending).rejects.toThrow(/CAS refused/)
  expect(current(h.store).interaction).toBe('plan');expect(h.saved[0].plans?.at(-1)?.objective).toBe('Concurrent remote revision')
 })
 it('requires the exact acknowledged row and stops after cancellation during a write',async()=>{
  const h=await setup(),write=h.io.write
  h.io.write=vi.fn(async(...args:Parameters<ChatPersistence['write']>)=>{const saved=await write(...args);return {...saved,content:{version:1,chats:[chat]}}})
  await expect(accept(h.store)).rejects.toThrow(/acknowledgement/)
  expect(current(h.store).interaction).toBe('plan');expect(h.store.getSnapshot().error).toMatch(/uncertain/)
  const next=await setup(),gate=next.hold(),controller=new AbortController()
  const pending=next.store.acceptReviewedPlan(chat.id,plan,deskPlanResolver(plan),controller.signal);await gate.entered
  controller.abort();gate.release();await expect(pending).rejects.toThrow()
  expect(next.saved[0].plans?.at(-1)?.status).toBe('accepted')
 })
 it('preserves unrelated local and remote edits while the acceptance CAS is in flight',async()=>{
  const other={...chat,id:'chat-two',plans:undefined,interaction:undefined},h=await setup([chat,other])
  h.remote([chat,{...other,title:'Remote title'}])
  const gate=h.hold(),pending=accept(h.store);await gate.entered
  h.store.update(chat.id,{composer:'Unsent during save'});h.store.update(other.id,{composer:'Other local text'})
  const flushed=h.store.flush();gate.release();await pending;expect(await flushed).toBe(true)
  expect(current(h.store).composer).toBe('Unsent during save')
  expect(h.saved.find(c=>c.id===other.id)).toMatchObject({title:'Remote title',composer:'Other local text'})
  expect(h.saved[0].plans?.at(-1)?.status).toBe('accepted')
 })
 it('preserves changed local scope during acknowledgement and never dispatches it',async()=>{
  const h=await setup(),gate=h.hold(),start=vi.fn(()=>true)
  render(<PlanCard chat={chat} store={h.store} onStart={start}/>);fireEvent.click(screen.getByRole('button',{name:'Start work'}));await gate.entered
  h.store.update(chat.id,{plans:[{...plan,objective:'New local review'}]});gate.release()
  await screen.findByRole('alert');expect(start).not.toHaveBeenCalled()
  expect(current(h.store).plans?.at(-1)?.objective).toBe('New local review');expect(h.saved[0].plans?.at(-1)?.status).toBe('accepted')
 })
 it.each(['reject','uncertain'] as const)('%s CAS never publishes acceptance or automatically dispatches',async failure=>{
  const h=await setup(),start=vi.fn(()=>true);h[failure]()
  render(<PlanCard chat={chat} store={h.store} onStart={start}/>);fireEvent.click(screen.getByRole('button',{name:'Start work'}))
  await screen.findByRole('alert');expect(start).not.toHaveBeenCalled();expect(current(h.store).plans?.at(-1)?.status).toBe('proposed')
  expect(h.store.getSnapshot().error).toMatch(/uncertain|refused/)
  const restored=open(h.io);await restored.load()
  expect(current(restored).plans?.at(-1)?.status).toBe(failure==='uncertain'?'accepted':'proposed')
  expect(start).not.toHaveBeenCalled()
 })
 it('persists a draft proposed row before a separate acceptance CAS and restores it',async()=>{
  const h=await setup([]),draft=h.store.startChat(undefined,undefined,true)
  h.store.update(draft.id,{interaction:'plan',plans:[plan]})
  const gate=h.hold(),pending=accept(h.store,plan,deskPlanResolver(plan),draft.id);await gate.entered
  expect(current(h.store,draft.id).interaction).toBe('plan');gate.release();await pending
  const calls=vi.mocked(h.io.write).mock.calls
  expect(calls).toHaveLength(2);expect(calls[0][0].chats[0].plans?.at(-1)?.status).toBe('proposed');expect(calls[1][0].chats[0].plans?.at(-1)?.status).toBe('accepted')
  const restored=open(h.io);await restored.load();expect(current(restored,draft.id).interaction).toBe('work')
 })
 it('rechecks a draft changed during its proposed-row promotion',async()=>{
  const h=await setup([]),draft=h.store.startChat(undefined,undefined,true);h.store.update(draft.id,{interaction:'plan',plans:[plan]})
  const gate=h.hold(),pending=accept(h.store,plan,deskPlanResolver(plan),draft.id);await gate.entered
  h.store.update(draft.id,{plans:[{...plan,objective:'Changed draft review'}]});gate.release()
  await expect(pending).rejects.toThrow(/changed/);expect(h.io.write).toHaveBeenCalledOnce();expect(h.saved[0].plans?.at(-1)?.status).toBe('proposed')
 })
 it('refuses pinned rows, including ownership acquired during an acceptance write',async()=>{
  const h=await setup();h.store.pinServerChat(chat.id)
  await expect(accept(h.store)).rejects.toThrow(/active response/);expect(h.io.write).not.toHaveBeenCalled()
  const next=await setup(),gate=next.hold(),pending=accept(next.store);await gate.entered;next.store.pinServerChat(chat.id);gate.release()
  await expect(pending).rejects.toThrow(/changed|uncertain/);expect(next.saved[0].plans?.at(-1)?.status).toBe('accepted')
 })
 it.each(['false','throw'] as const)('preserves unsent revise text and durable acceptance when dispatch returns %s',async outcome=>{
  const h=await setup([{...chat,composer:'Keep the Canadian scope.'}]),start=vi.fn(()=>{if(outcome==='throw')throw Error('Dispatch refused');return false})
  const view=render(<PlanCard chat={chat} store={h.store} onStart={start}/>);fireEvent.click(screen.getByRole('button',{name:'Revise'}));expect(current(h.store).composer).toBe('Keep the Canadian scope.')
  fireEvent.click(screen.getByRole('button',{name:'Start work'}));await screen.findByRole('alert');expect(start).toHaveBeenCalledOnce()
  const restored=open(h.io);await restored.load();const saved=current(restored);view.rerender(<PlanCard chat={saved} store={restored} onStart={start}/>)
  expect(screen.getByText('Approved')).toBeTruthy();expect(screen.queryByText('Work started')).toBeNull();expect(saved.plans?.at(-1)?.status).toBe('accepted')
 })
 it.each(['pin','newer'] as const)('successful dispatch never overwrites %s response state',async change=>{
  const h=await setup(),start=vi.fn(()=>{if(change==='pin')h.store.pinServerChat(chat.id);else h.store.update(chat.id,{interaction:'plan',plans:[{...plan,id:'plan-two',revision:2,objective:'New revision'}]});return true})
  render(<PlanCard chat={chat} store={h.store} onStart={start}/>);fireEvent.click(screen.getByRole('button',{name:'Start work'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
  await waitFor(()=>expect(screen.queryByText('Checking plan…')).toBeNull())
  expect(current(h.store).plans?.at(-1)?.status).toBe(change==='pin'?'accepted':'proposed');expect(h.saved[0].plans?.at(-1)?.status).toBe('accepted');expect(h.io.write).toHaveBeenCalledOnce()
 })
 it.each(['pin','newer'] as const)('never publishes a started marker over %s state acquired during its CAS',async change=>{
  const h=await setup(),receipt=await accept(h.store),gate=h.hold()
  const pending=h.store.markReviewedPlanStarted(chat.id,receipt,signal());await gate.entered
  if(change==='pin')h.store.pinServerChat(chat.id)
  else h.store.update(chat.id,{interaction:'plan',plans:[{...plan,id:'plan-two',revision:2}]})
  gate.release();await expect(pending).rejects.toThrow(/changed/)
  expect(current(h.store).plans?.at(-1)?.status).toBe(change==='pin'?'accepted':'proposed')
  expect(h.store.getSnapshot().error).toMatch(/uncertain/)
 })
 it('requires a distinct click and refuses unresolved questions',async()=>{
  const h=await setup([{...chat,plans:[{...plan,questions:['Which pack?']}]}]),start=vi.fn(()=>true)
  render(<PlanCard chat={current(h.store)} store={h.store} onStart={start}/>);expect(start).not.toHaveBeenCalled();expect((screen.getByRole('button',{name:'Start work'}) as HTMLButtonElement).disabled).toBe(true)
  await expect(accept(h.store,{...plan,questions:['Which pack?']})).rejects.toThrow(/changed/);expect(h.io.write).not.toHaveBeenCalled()
 })
 it('checks explicit legacy file and Dialect mappings without rewriting saved pins',async()=>{
  const reviewed:ChatPlan={...plan,references:[...plan.references,{kind:'dialect',id:'dialect-one',revision:'a'.repeat(64)}]},h=await setup([{...chat,plans:[reviewed]}])
  const resolver=deskPlanResolver(reviewed),resolved=await resolver.legacy!(reviewed.references[0] as Parameters<NonNullable<PlanResolver['legacy']>>[0],signal())
  expect(resolved.id).toBe('desk-plan-legacy-1');expect(resolved.id).not.toBe('pack.json')
  await accept(h.store,reviewed,resolver);expect(h.saved[0].plans?.at(-1)?.references).toEqual(reviewed.references);expect(dialectAPI).toHaveBeenCalledWith('/dialect-one','GET',undefined,undefined,expect.any(AbortSignal))
 })
 it('refuses unsupported current IDs and missing or changed historical mappings',async()=>{
  const reviewed:ChatPlan={...plan,references:[{kind:'artifact',id:'opaque-current-id',digest:'a'.repeat(64)}]},h=await setup([{...chat,plans:[reviewed]}])
  await expect(accept(h.store,reviewed)).rejects.toThrow(/host must resolve/)
  await expect(deskPlanResolver(plan).legacy!({kind:'file',id:'deleted.json',revision:'a'.repeat(64)},signal())).rejects.toThrow(/host must resolve/)
  vi.mocked(readFile).mockRejectedValueOnce(Error('File no longer exists'))
  const legacy=await setup();await expect(accept(legacy.store)).rejects.toThrow(/no longer exists/)
  expect(h.io.write).not.toHaveBeenCalled();expect(legacy.io.write).not.toHaveBeenCalled()
 })
})
