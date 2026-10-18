import { afterEach, expect, it } from 'vitest'
import { ChatStore, type ChatPersistence } from '../chat/store'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { validGraphDraft,validGraphSnapshot, type GraphDraft } from './drafts'
const stores:ChatStore[]=[]
afterEach(()=>stores.forEach(store=>store.dispose()))
const draft:GraphDraft={draftId:'draft-1',id:'flow',path:'graphs/flow.graph.json',content:'{}',createdAt:'2026-10-09T12:00:00Z'}
it('retains typed graph drafts across chat reload without making pack candidates',async()=>{
 let content:unknown={version:1,chats:[]}
 const io:ChatPersistence={read:async()=>({project:'/project',sha256:'absent',content}),write:async doc=>{content=doc;return {project:'/project',sha256:'saved',content:doc}}}
 const store=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(store);await store.load()
 const chat=store.create();store.update(chat.id,{graphDrafts:[draft]});expect(await store.flush()).toBe(true)
 const reopened=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(reopened);await reopened.load()
 expect(reopened.getSnapshot().chats[0].graphDrafts).toEqual([draft])
 expect(reopened.getSnapshot().packDrafts).toEqual([])
})
it('validates persisted graph metadata and bounds draft bytes',()=>{
 expect(validGraphDraft(draft)).toBe(true)
 for(const patch of [{id:'../bad'}, {draftId:{}}, {content:'é'.repeat(524289)}, {baseSha256:'wrong'}, {createdAt:'yesterday'}]) expect(validGraphDraft({...draft,...patch})).toBe(false)
})
it('keeps graph workspace chats separate from home and pack drafts and restores their identity',async()=>{
 let content:unknown={version:1,chats:[]}
 const io:ChatPersistence={read:async()=>({project:'/project',sha256:'absent',content}),write:async doc=>{content=doc;return {project:'/project',sha256:'saved',content:doc}}}
 const store=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(store);await store.load()
 const home=store.startChat();store.update(home.id,{composer:'Unsent home question'})
 const graph={id:'flow',path:'flow.graph.json',workspace:'flow'}
 const chat=store.startChat(undefined,'draft',false,undefined,graph)
 expect(chat.id).not.toBe(home.id);expect(store.startChat().id).toBe(home.id)
 expect(store.startChat(undefined,'draft',false,undefined,graph).id).toBe(chat.id)
 store.update(chat.id,{composer:'Review this graph',graphDrafts:[draft]});store.retainChat(chat.id);expect(await store.flush()).toBe(true)
 const restored=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(restored);await restored.load()
 expect(restored.getSnapshot().chats[0]).toMatchObject({graph,composer:'Review this graph',graphDrafts:[draft]})
 expect(restored.getSnapshot().chats[0].pack).toBeUndefined();expect(restored.getSnapshot().packDrafts).toEqual([])
})

it('refuses nested private provenance in historical conversation snapshots',()=>{
 expect(validGraphSnapshot(draft)).toBe(true)
 for(const patch of [{policySources:[]},{PolicySources:[]},{originChatId:'other'},{OriginChatId:'other'}])expect(validGraphSnapshot({...draft,...patch})).toBe(false)
})

it('keeps nested private graph provenance out of restored chat data',async()=>{
 const io:ChatPersistence={read:async()=>({project:'/project',sha256:'legacy',content:{version:1,chats:[{id:'chat-one',title:'Unsafe legacy',pinned:false,archived:false,updatedAt:'2026-10-18T12:00:00Z',composer:'',model:'',mode:'draft',view:'chat',graphDrafts:[{...draft,policySources:[]}]}]}}),write:async doc=>({project:'/project',sha256:'saved',content:doc})}
 const store=new ChatStore('/project',io,memoryDraftPersistence('/project'));stores.push(store);await store.load()
 expect(store.getSnapshot().chats).toEqual([]);expect(store.getSnapshot().error).toContain('Invalid saved graph drafts')
})
