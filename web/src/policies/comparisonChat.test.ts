import {afterEach,expect,it,vi} from 'vitest'
import {ChatStore,type Chat,type ChatPersistence} from '../chat/store'
import {memoryDraftPersistence} from '../testing/draftPersistence'
import {findPolicyChat,ensurePolicyChat,preparePolicyComparisonChat} from './comparisonChat'
import type {PolicyComparisonRequest} from './comparison'
const id='policy_'+'a'.repeat(48),other='policy_'+'b'.repeat(48)
const request:PolicyComparisonRequest={before:{kind:'publication',publicationId:'publication_'+'c'.repeat(48)},after:{kind:'revision',revisionId:'revision_'+'d'.repeat(48)}}
const stores:ChatStore[]=[]
afterEach(()=>{stores.splice(0).forEach(s=>s.dispose());sessionStorage.clear()})
async function setup(initial:Chat[]=[]){let content={version:1 as const,chats:initial};const io:ChatPersistence={read:async()=>({project:'/test',sha256:'digest',content:structuredClone(content)}),write:vi.fn(async next=>{content=structuredClone(next);return {project:'/test',sha256:'digest',content:structuredClone(content)}})};const drafts=memoryDraftPersistence('/test'),store=new ChatStore('/test',io,drafts);stores.push(store);await store.load();return {store,io,drafts}}
it('reuses live linked chat and appends an unsent exact request while preserving composer, Plan and allowance',async()=>{
 const chat:Chat={id:'linked',title:'Existing',policy:{id},pinned:false,archived:false,updatedAt:'2026-10-18T00:00:00Z',composer:'Keep my composed request',model:'fixture',mode:'draft',view:'chat',interaction:'plan'}
 const {store,io}=await setup([chat]),before=structuredClone(store.getSnapshot().chats[0]),opened=preparePolicyComparisonChat(store,id,request)
 expect(opened.id).toBe(chat.id);expect(opened.composer).toMatch(/^Keep my composed request\n\n/);expect(opened.composer).toContain(JSON.stringify(request));expect(opened.interaction).toBe('plan');expect(opened.checkpoint).toEqual(before?.checkpoint);expect(opened.model).toBe(before?.model)
 expect(store.getSnapshot().drafts).toEqual([]);await store.checkpoint(new AbortController().signal)
 const again=new ChatStore('/test',io,memoryDraftPersistence('/test'));stores.push(again);await again.load();expect(findPolicyChat(again,id)?.composer).toBe(opened.composer)
})
it('uses the fresh store snapshot to avoid duplicate Assistant/comparison drafts and never replaces an unrelated composer',async()=>{
 const {store}=await setup(),unrelated=ensurePolicyChat(store,other);store.update(unrelated.id,{composer:'Unrelated'})
 const opened=preparePolicyComparisonChat(store,id,request),assistant=ensurePolicyChat(store,id)
 expect(assistant.id).toBe(opened.id);expect(store.getSnapshot().chats.filter(c=>c.policy?.id===id)).toHaveLength(1);expect(store.getSnapshot().drafts.filter(c=>c.policy?.id===id)).toHaveLength(0);expect(findPolicyChat(store,other)?.composer).toBe('Unrelated');expect(opened.checkpoint).toBeUndefined()
})
it('ignores preferred chats from a different Policy and refuses malformed selectors before changing the store',async()=>{
 const {store}=await setup(),unrelated=ensurePolicyChat(store,other),opened=preparePolicyComparisonChat(store,id,request,unrelated.id)
 expect(opened.id).not.toBe(unrelated.id);const snapshot=store.getSnapshot()
 expect(()=>preparePolicyComparisonChat(store,id,{...request,before:{kind:'revision',revisionId:'latest'}})).toThrow();expect(store.getSnapshot()).toBe(snapshot)
})
it('retains a newly opened linked comparison chat and exact unsent request across refresh',async()=>{
 const {store,io}=await setup(),opened=preparePolicyComparisonChat(store,id,request)
 expect(store.getSnapshot().chats.map(chat=>chat.id)).toEqual([opened.id]);expect(store.getSnapshot().drafts).toEqual([])
 expect(opened.composer).toContain(JSON.stringify(request));expect(opened.checkpoint).toBeUndefined()
 await store.checkpoint(new AbortController().signal)
 const reloaded=new ChatStore('/test',io,memoryDraftPersistence('/test'));stores.push(reloaded);await reloaded.load()
 expect(findPolicyChat(reloaded,id)).toMatchObject({id:opened.id,composer:opened.composer,policy:{id}})
 expect(findPolicyChat(reloaded,id)?.checkpoint).toBeUndefined()
})
it('retains an existing unsent draft with its composer and Plan unchanged except for appended selectors across refresh',async()=>{
 const {store,io}=await setup(),draft=ensurePolicyChat(store,id)
 store.update(draft.id,{composer:'My prior unsent question',interaction:'plan',model:'fixture'})
 const opened=preparePolicyComparisonChat(store,id,request,draft.id)
 expect(opened.id).toBe(draft.id);expect(opened.composer).toMatch(/^My prior unsent question\n\n/);expect(opened.interaction).toBe('plan');expect(opened.model).toBe('fixture');expect(opened.checkpoint).toBeUndefined()
 await store.checkpoint(new AbortController().signal)
 const reloaded=new ChatStore('/test',io,memoryDraftPersistence('/test'));stores.push(reloaded);await reloaded.load()
 expect(findPolicyChat(reloaded,id)).toMatchObject({id:draft.id,composer:opened.composer,interaction:'plan',model:'fixture'})
 expect(reloaded.getSnapshot().chats).toHaveLength(1);expect(reloaded.getSnapshot().drafts).toEqual([])
})
it('refuses a full history before changing an existing unsent draft composer or claiming persistence',async()=>{
 const {store,io}=await setup(),draft=ensurePolicyChat(store,id);store.update(draft.id,{composer:'Preserve my question',interaction:'plan'})
 for(let index=0;index<256;index++)store.create()
 const before=findPolicyChat(store,id)!
 expect(store.canCreate).toBe(false);expect(()=>preparePolicyComparisonChat(store,id,request,draft.id)).toThrow('Chat history is full')
 expect(findPolicyChat(store,id)).toBe(before);expect(findPolicyChat(store,id)?.composer).toBe('Preserve my question');expect(store.getSnapshot().drafts.map(chat=>chat.id)).toContain(draft.id)
 expect(io.write).not.toHaveBeenCalled()
})
it('reports persistence failure through checkpoint, preserves the pending request and refuses further edits until recovery',async()=>{
 const {store,io}=await setup(),draft=ensurePolicyChat(store,id);store.update(draft.id,{composer:'Keep my question',interaction:'plan'})
 vi.mocked(io.write).mockRejectedValue(new Error('Disk unavailable'))
 const opened=preparePolicyComparisonChat(store,id,request,draft.id)
 await expect(store.checkpoint(new AbortController().signal)).rejects.toThrow('Chat changes are not saved')
 expect(store.getSnapshot()).toMatchObject({dirty:true});expect(store.getSnapshot().error).toContain('Disk unavailable');expect(findPolicyChat(store,id)?.composer).toBe(opened.composer)
 expect(()=>preparePolicyComparisonChat(store,id,request,draft.id)).toThrow('Save or recover');expect(findPolicyChat(store,id)?.composer).toBe(opened.composer)
 const reloaded=new ChatStore('/test',io,memoryDraftPersistence('/test'));stores.push(reloaded);await reloaded.load();expect(findPolicyChat(reloaded,id)).toBeUndefined()
})
it('refuses preparation before history is ready without creating an unsent draft',()=>{
 const store=new ChatStore('/test');stores.push(store)
 expect(()=>preparePolicyComparisonChat(store,id,request)).toThrow('Save or recover');expect(store.getSnapshot().drafts).toEqual([]);expect(store.getSnapshot().chats).toEqual([])
})
