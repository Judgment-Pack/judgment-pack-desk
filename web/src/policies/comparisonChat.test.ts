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
 expect(assistant.id).toBe(opened.id);expect(store.getSnapshot().drafts.filter(c=>c.policy?.id===id)).toHaveLength(1);expect(findPolicyChat(store,other)?.composer).toBe('Unrelated');expect(opened.checkpoint).toBeUndefined()
})
it('ignores preferred chats from a different Policy and refuses malformed selectors before changing the store',async()=>{
 const {store}=await setup(),unrelated=ensurePolicyChat(store,other),opened=preparePolicyComparisonChat(store,id,request,unrelated.id)
 expect(opened.id).not.toBe(unrelated.id);const snapshot=store.getSnapshot()
 expect(()=>preparePolicyComparisonChat(store,id,{...request,before:{kind:'revision',revisionId:'latest'}})).toThrow();expect(store.getSnapshot()).toBe(snapshot)
})
