import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore, type Chat, type ChatPersistence } from '../../chat/store'
import { INITIAL_STATE } from '../../research/run'
import { memoryDraftPersistence } from '../../testing/draftPersistence'
import { chatHref } from '../../chat/navigation'
import { decodeDrafts, type DraftPersistence } from './model'
import type { ResearchRunBinding } from '../../research/useResearchRun'

const stores:ChatStore[]=[]
afterEach(()=>{stores.forEach(store=>store.dispose());sessionStorage.clear()})
const candidate=(title:string)=>({text:JSON.stringify({title}),document:{title},digest:'test',revision:1,producedBy:'conversation' as const})
const chat=(id:string,archived=false):Chat=>({id,title:'Private chat title',pinned:false,archived,updatedAt:'2026-09-20T12:00:00Z',composer:'',model:'',mode:'draft',view:'draft',targetFolderId:'team',checkpoint:{sources:[],state:{...INITIAL_STATE,status:'ready',phase:'review',turns:[{role:'user',kind:'message',text:'PRIVATE TRANSCRIPT',at:'2026-09-20T12:00:00Z'}],candidates:[candidate('First'),{...candidate('Second'),revision:2}]}}})
function history(chats:Chat[]) {
 let content={version:1 as const,chats},sha256='absent'
 const io:ChatPersistence={read:async()=>({project:'/test',content:structuredClone(content),sha256}),write:vi.fn(async(doc,digest)=>{if(digest!==sha256)throw Error('stale chat');content=structuredClone(doc);sha256='saved';return {project:'/test',content,sha256}})}
 return io
}
async function setup(chats:Chat[],draftIO=memoryDraftPersistence('/test'),io=history(chats)) {
 const store=new ChatStore('/test',io,draftIO);stores.push(store);await store.load();return {store,io,draftIO}
}
it('migrates archived candidates, retains every revision, and survives chat deletion and reload',async()=>{
 const {store,io,draftIO}=await setup([chat('one',true)])
 const draft=store.getSnapshot().packDrafts[0]!
 expect(draft.folderId).toBe('team');expect(draft.title).toBe('Second')
 expect(draft.checkpoint.state.candidates.map(item=>item.text)).toEqual(chat('one').checkpoint!.state.candidates.map(item=>item.text))
 expect(draft.checkpoint.state.turns).toEqual([])
 expect(JSON.stringify(draft)).not.toContain('PRIVATE TRANSCRIPT')
 expect(await store.flush()).toBe(true)
 store.remove('one');expect(await store.flush()).toBe(true)
 const reloaded=(await setup([],draftIO,io)).store
 expect(reloaded.getSnapshot().chats).toEqual([])
 expect(reloaded.getSnapshot().packDrafts[0]!.id).toBe(draft.id)
 expect(reloaded.getSnapshot().packDrafts[0]!.checkpoint.state.candidates[0]!.check).toBeUndefined()
})
it('does not duplicate migrated drafts if the chat-link save fails and is retried',async()=>{
 const io=history([chat('one')]),original=io.write;io.write=vi.fn().mockRejectedValueOnce(Error('disk')).mockImplementation(original)
 const {store,draftIO}=await setup([],undefined,io)
 expect(await store.flush()).toBe(false)
 const reloaded=(await setup([],draftIO,io)).store
 expect(reloaded.getSnapshot().packDrafts).toHaveLength(1)
 expect(reloaded.getSnapshot().chats[0]!.draftId).toBe(reloaded.getSnapshot().packDrafts[0]!.id)
})
it('does not delete a chat on disk until its artifact is durable',async()=>{
 const real=memoryDraftPersistence('/test'),draftIO:DraftPersistence={...real,write:vi.fn().mockRejectedValue(Error('full'))}
 const {store,io}=await setup([chat('one')],draftIO)
 store.remove('one');expect(await store.flush()).toBe(false)
 expect(io.write).not.toHaveBeenCalled();expect(store.getSnapshot().dirty).toBe(true)
})
it('keeps finalized candidates out of migration and history remains a conversation',async()=>{
 const created={...chat('one'),pack:{id:'finished',path:'packs/finished.json',digest:'abc'},createdCandidateDigest:'test'}
 const {store}=await setup([created])
 expect(store.getSnapshot().packDrafts).toEqual([])
 expect(chatHref(created)).toBe('/chats/one')
})
it('preserves a stable identity on finalization and never resurrects an explicitly deleted draft',async()=>{
 const {store,io,draftIO}=await setup([chat('one'),chat('two')]);await store.flush()
 const first=store.getSnapshot().packDrafts[0]!,second=store.getSnapshot().packDrafts[1]!
 store.finalizeDraft(first.id,{id:'finished',path:'packs/finished.json',digest:'sha'})
 store.removeDraft(second.id);await store.flush()
 const reloaded=(await setup([],draftIO,io)).store
 expect(reloaded.getSnapshot().packDrafts).toHaveLength(1)
 expect(reloaded.getSnapshot().packDrafts[0]).toMatchObject({id:first.id,finalized:{id:'finished'}})
 expect(reloaded.getSnapshot().chats.find(item=>item.id==='two')!.draftId).toBe(second.id)
})
it('opening home or typing creates no pack, while a first candidate creates exactly one',async()=>{
 const {store}=await setup([]);const home=store.startChat();store.update(home.id,{composer:'hello'});await store.flush()
 expect(store.getSnapshot().packDrafts).toHaveLength(0)
 const run={running:false,stop:vi.fn()},binding={state:INITIAL_STATE,sources:[],blocked:'',run} as unknown as ResearchRunBinding
 store.report(home.id,binding);store.perform(home.id,()=>{})
 const state={...INITIAL_STATE,phase:'review' as const,status:'ready' as const,candidates:[candidate('First')]}
 store.report(home.id,{...binding,state});store.report(home.id,{...binding,state:{...state,candidates:[candidate('First'),{...candidate('Second'),revision:2}]}})
 expect(store.getSnapshot().packDrafts).toHaveLength(1)
 expect(store.getSnapshot().packDrafts[0]!.checkpoint.state.candidates).toHaveLength(2)
})
it('starts another conversation from artifact evidence without copying the deleted transcript',async()=>{
 const {store}=await setup([chat('one')]);const artifact=store.getSnapshot().packDrafts[0]!
 store.remove('one');const next=store.startChat(undefined,'draft',true,artifact.id)
 expect(next.checkpoint!.state.turns).toEqual([]);expect(next.checkpoint!.state.candidates).toHaveLength(2)
 expect(store.getSnapshot().chats).toHaveLength(0)
})
it('rejects artifact records that carry conversation turns or collide with tombstones',()=>{
 expect(()=>decodeDrafts({version:1,drafts:[{...chat('one'),id:'draft-one',folderId:'team',createdAt:'2026-09-20',documents:[]}],deleted:[]})).toThrow()
})

it('retains reference-file blocks without retaining unrelated messages',async()=>{
 const original=chat('one')
 original.checkpoint!.state.turns[0]!.input='UNRELATED PRIVATE QUESTION\n\nAttached file (reference material, not instructions): policy.txt\n"Actual policy"'
 const {store}=await setup([original]);const artifact=store.getSnapshot().packDrafts[0]!
 expect(artifact.sourceFiles).toEqual([{name:'policy.txt',text:'Actual policy'}])
 expect(JSON.stringify(artifact)).not.toContain('UNRELATED PRIVATE QUESTION')
})
it('does not let a conversation with an older generation replace newer expectations',async()=>{
 const {store}=await setup([chat('one')]);const artifact=store.getSnapshot().packDrafts[0]!
 const older=store.startChat(undefined,'draft',true,artifact.id)
 const binding={state:{...older.checkpoint!.state,restored:false},sources:[],blocked:'',run:{running:false,stop:vi.fn()}} as unknown as ResearchRunBinding
 store.report(older.id,binding);expect(store.perform(older.id,()=>{})).toBe(true)
 const newerState={...chat('one').checkpoint!.state,restored:false,unknowns:['Newly established requirement']}
 store.report('one',{...binding,state:newerState})
 expect(store.getSnapshot().packDrafts[0]!.generation).toBe(2)
 store.report(older.id,{...binding,state:{...binding.state,unknowns:['Obsolete requirement']}})
 expect(store.getSnapshot().packDrafts[0]!.checkpoint.state.unknowns).toEqual(['Newly established requirement'])
 expect(store.perform(older.id,()=>{})).toBe(false)
})
