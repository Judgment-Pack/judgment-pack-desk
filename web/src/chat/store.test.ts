import { memoryDraftPersistence } from '../testing/draftPersistence'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ChatStore, type ChatPersistence } from './store'
import { INITIAL_STATE } from '../research/run'
import type { ResearchRunBinding } from '../research/useResearchRun'

const stores: ChatStore[] = []
afterEach(() => { stores.forEach(store => store.dispose()); stores.length = 0; sessionStorage.clear(); vi.useRealTimers() })
function setup(io?: Partial<ChatPersistence>) {
  const read = vi.fn(async () => ({ project: '/project', sha256: 'absent', content: { version: 1, chats: [] } }))
  const write = vi.fn(async (document: unknown) => ({ project: '/project', sha256: 'saved', content: document }))
  const store = new ChatStore('/project', { read, write, ...io }, memoryDraftPersistence('/project')); stores.push(store)
  return { store, read, write }
}
describe('project conversation history', () => {
  it('persists a production Policy identity, request and checked proposal independently of pack drafts',async()=>{
    const {store,write}=setup();await store.load()
    const policyId='policy_'+'a'.repeat(48),revision='b'.repeat(64),candidate={title:'Policy',narrative:'Saved exact source',artifactVersion:'1.0.0'}
    const chat=store.startChat(undefined,undefined,false,undefined,undefined,undefined,{id:policyId})
    expect(store.startChat(undefined,undefined,false,undefined,undefined,undefined,{id:policyId}).id).toBe(chat.id)
    expect(store.startChat().id).not.toBe(chat.id)
    store.update(chat.id,{policyRequest:{revision,candidate},policyProposals:[{version:1,id:'proposal',policyId,revision,before:candidate,candidate:{...candidate,title:'Updated title'},operationId:crypto.randomUUID(),createdAt:'2026-10-18T00:00:00Z',check:{scope:'draft-shape-and-revision',status:'passed'},save:{state:'uncertain'}}]})
    store.retainChat(chat.id);await store.flush()
    const document=write.mock.calls.at(-1)![0] as {version:1;chats:import('./store').Chat[]}
    const loaded=setup({read:async()=>({project:'/project',sha256:'saved',content:document})});await loaded.store.load()
    expect(loaded.store.getSnapshot().chats[0]).toMatchObject({id:chat.id,policy:{id:policyId},policyRequest:{revision,candidate},policyProposals:[{save:{state:'uncertain'}}]})
    expect(loaded.store.getSnapshot().packDrafts).toEqual([])
    loaded.store.remove(chat.id);await loaded.store.flush()
    expect(loaded.store.getSnapshot().chats).toEqual([])
  })

  it('keeps home visits, typing and New chat out of history until the first accepted Send', async () => {
    const { store, write } = setup(); await store.load()
    const draft = store.startChat()
    store.update(draft.id, { composer: 'A decision brief', model: 'model-a' })
    expect(store.startChat().id).toBe(draft.id)
    await store.flush()
    expect(store.getSnapshot().chats).toHaveLength(0)
    expect(store.getSnapshot().dirty).toBe(false)
    expect(write).not.toHaveBeenCalled()

    const restored = setup(); await restored.store.load()
    expect(restored.store.startChat()).toMatchObject({ id: draft.id, composer: 'A decision brief', model: 'model-a' })
    expect(restored.store.getSnapshot().chats).toHaveLength(0)

    const run = { running: false, stop: vi.fn() }
    const binding = { state: INITIAL_STATE, sources: [], blocked: 'Configure Assistant', model: 'model-a', researchConfigured: false, run, ledger: null } as unknown as ResearchRunBinding
    store.report(draft.id, binding)
    const send = vi.fn()
    expect(store.perform(draft.id, send)).toBe(false)
    expect(store.getSnapshot().chats).toHaveLength(0)
    expect(send).not.toHaveBeenCalled()
    store.report(draft.id, { ...binding, blocked: '' })
    expect(store.perform(draft.id, send)).toBe(true)
    expect(store.perform(draft.id, send)).toBe(true)
    expect(store.getSnapshot().chats.map(chat => chat.id)).toEqual([draft.id])
    expect(store.getSnapshot().drafts).toHaveLength(0)
    await store.flush()
    expect(write).toHaveBeenCalledOnce()
    expect((write.mock.calls[0]![0] as { chats: unknown[] }).chats).toHaveLength(1)

    const fresh = store.startChat(undefined, undefined, true)
    expect(fresh.id).not.toBe(draft.id)
    await store.flush()
    expect(store.getSnapshot().chats).toHaveLength(1)
    expect(write).toHaveBeenCalledOnce()
  })

  it('does not confuse a different project’s unsent home draft with this project', async () => {
    const { store } = setup(); await store.load()
    const draft = store.startChat(); store.update(draft.id, { composer: 'Private to this project' })
    const other = new ChatStore('/other', { read: async () => ({ project: '/other', sha256: 'absent', content: { version: 1, chats: [] } }), write: vi.fn() }, memoryDraftPersistence('/other'))
    stores.push(other); await other.load()
    expect(other.startChat().composer).toBe('')
    expect(other.getSnapshot().chats).toHaveLength(0)
  })
  it('preserves composer and model separately for each chat and links creation without replacing history', async () => {
    const { store, write } = setup(); await store.load()
    const first = store.create(); store.update(first.id,{ composer: 'My unfinished prompt', model: 'model-a' })
    const second = store.create(); store.update(second.id,{ composer: 'A different prompt' })
    store.activate(first.id)
    expect(store.getSnapshot().chats.find(chat => chat.id === first.id)).toMatchObject({ composer: 'My unfinished prompt', model: 'model-a' })
    store.update(first.id,{ pack: { id: 'my-pack', path: 'packs/my-pack.pack.json', digest: 'abc' } })
    await store.flush()
    const saved = write.mock.calls[0]![0] as { chats: { id: string; composer: string }[] }
    expect(saved.chats).toHaveLength(2)
    expect(saved.chats.find(chat => chat.id === first.id)?.composer).toBe('My unfinished prompt')
    expect(store.getSnapshot().dirty).toBe(false)
  })
  it('switching chats does not stop the running task; a second task cannot start', async () => {
    const { store } = setup(); await store.load()
    const a = store.create(), b = store.create()
    const stop = vi.fn()
    let running = false
    const binding = { state: INITIAL_STATE, sources: [], blocked: '', model: 'model', researchConfigured: false,
      run: { get running() { return running }, stop }, ledger: null } as unknown as ResearchRunBinding
    store.report(a.id,binding); store.report(b.id,{ ...binding, run: { running: false, stop: vi.fn() } as unknown as ResearchRunBinding['run'] })
    expect(store.perform(a.id, () => { running = true })).toBe(true)
    store.activate(b.id)
    const second = vi.fn(); expect(store.perform(b.id,second)).toBe(false)
    expect(second).not.toHaveBeenCalled(); expect(stop).not.toHaveBeenCalled()
    expect(() => store.remove(a.id)).toThrow('Stop')
    running = false
    expect(store.perform(b.id,second)).toBe(true)
  })
  it('does not overwrite unreadable history or claim saving succeeded after a conflict', async () => {
    const bad = setup({ read: async () => ({ project: '/other-project', sha256: 'x', content: { version: 1, chats: [] } }) })
    await bad.store.load(); expect(bad.store.getSnapshot().ready).toBe(false)
    expect(() => bad.store.create()).toThrow(); expect(await bad.store.flush()).toBe(false); expect(bad.write).not.toHaveBeenCalled()
    const { store } = setup({ write: async () => { throw new Error('history changed in another window') } })
    await store.load(); store.create(); expect(await store.flush()).toBe(false)
    expect(store.getSnapshot()).toMatchObject({ dirty: true, saving: false })
    expect(store.getSnapshot().error).toContain('not saved')
  })
  it('writes newer edits after the in-flight checkpoint, without advancing its saved revision early', async () => {
    vi.useFakeTimers()
    let release!: (reply: Awaited<ReturnType<ChatPersistence['write']>>) => void
    const write = vi.fn<ChatPersistence['write']>().mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
      .mockImplementation(async document => ({ project: '/project', sha256: 'second', content: document }))
    const { store } = setup({ write }); await store.load()
    const chat = store.create(); const saving = store.flush()
    store.update(chat.id,{ composer: 'Typed while saving' })
    release({ project: '/project', sha256: 'first', content: { version: 1, chats: [] } }); await saving
    expect(store.getSnapshot().dirty).toBe(true)
    await vi.advanceTimersByTimeAsync(601)
    expect(write.mock.calls[1]![1]).toBe('first')
    expect(write.mock.calls[1]![0].chats[0]?.composer).toBe('Typed while saving')
    expect(store.getSnapshot().dirty).toBe(false)
  })
  it('rejects duplicate conversation identifiers and keeps the file untouched', async () => {
    const base = setup(); await base.store.load(); const chat = base.store.create()
    const { store, write } = setup({ read: async () => ({ project: '/project', sha256: 'x', content: { version: 1, chats: [chat, chat] } }) })
    await store.load(); expect(store.getSnapshot().ready).toBe(false); expect(write).not.toHaveBeenCalled()
  })
})
it('keeps recency stable for opening, restoring, typing, pinning and switching models', async () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-01T12:00:00Z'))
  const { store } = setup(); await store.load()
  const chat = store.create()
  vi.setSystemTime(new Date('2026-09-16T12:00:00Z'))
  store.activate(chat.id)
  store.update(chat.id, { pinned: true, composer: 'Unsent', model: 'different' })
  expect(store.getSnapshot().chats[0]!.updatedAt).toBe(chat.updatedAt)
  const state = { ...INITIAL_STATE, phase: 'conversation' as const, status: 'complete' as const, turns: [{ role: 'assistant' as const, kind: 'message' as const, text: 'Saved response', at: '2026-09-01T12:00:01Z' }] }
  const binding = { state, sources: [], blocked: '', model: 'model', researchConfigured: false, run: null, ledger: null }
  store.report(chat.id, binding)
  expect(store.getSnapshot().chats[0]!.updatedAt).toBe('2026-09-01T12:00:01Z')
  store.report(chat.id, { ...binding, state: { ...state } })
  expect(store.getSnapshot().chats[0]!.updatedAt).toBe('2026-09-01T12:00:01Z')
})
it('keeps attachments separate from message text and restores an unsent attachment', async () => {
  const { store } = setup(); await store.load()
  const chat = store.startChat()
  store.update(chat.id, { composer: 'Read this', attachments: [{ id: 'file-one', name: 'requirements.md', text: '# Policy\nExample' }] })
  const restored = setup(); await restored.store.load()
  expect(restored.store.startChat()).toMatchObject({ composer: 'Read this', attachments: [{ id: 'file-one', name: 'requirements.md', text: '# Policy\nExample' }] })
  store.update(chat.id, { attachments: [] })
  expect(store.getSnapshot().drafts[0]!.composer).toBe('Read this')
})
it('allows upgrading a draft to research but never downgrading a research candidate to bypass its checks', async () => {
  const { store } = setup(); await store.load()
  const chat = store.create()
  store.report(chat.id, { state: { ...INITIAL_STATE, candidates: [{} as any] }, sources: [], blocked: '', model: 'model', researchConfigured: true, run: null, ledger: null })
  store.update(chat.id, { mode: 'research' })
  expect(store.getSnapshot().chats[0]!.mode).toBe('research')
  store.update(chat.id, { mode: 'draft' })
  expect(store.getSnapshot().chats[0]!.mode).toBe('research')
})

it('retains the selected folder across an unsent draft reload and saved history',async()=>{
 const {store,write}=setup();await store.load()
 const draft=store.startChat();store.update(draft.id,{targetFolderId:'finance'})
 const restored=setup();await restored.store.load()
 expect(restored.store.startChat().targetFolderId).toBe('finance')
 const saved=store.create();store.update(saved.id,{targetFolderId:'legal'});await store.flush()
 const history=setup({read:async()=>({project:'/project',sha256:'saved',content:write.mock.calls.at(-1)![0]})})
 await history.store.load()
 expect(history.store.getSnapshot().chats.find(chat=>chat.id===saved.id)?.targetFolderId).toBe('legal')
})

it('keeps the link a read document came from and refuses a malformed one', async () => {
  const base = setup(); await base.store.load(); const chat = base.store.create()
  const reference = { id: '12345678-1234-1234-1234-123456789abc', digest: `sha256:${'a'.repeat(64)}`, pages: [1], allowPartial: false }
  const document = { id: reference.id, name: 'policy-evidence.txt', text: '', document: reference, link: { url: 'https://example.com/policy', anchor: 'brief-human-review' } }
  const good = { ...chat, documents: [document] }
  const restored = setup({ read: async () => ({ project: '/project', sha256: 'x', content: { version: 1, chats: [good] } }) })
  await restored.store.load()
  expect(restored.store.getSnapshot().chats[0]?.documents?.[0]?.link).toEqual({ url: 'https://example.com/policy', anchor: 'brief-human-review' })
  const malformed = [
    { ...document, link: { url: 'http://example.com/policy' } },
    { ...document, link: { url: 'https://example.com/policy#section' } },
    { ...document, link: { url: 'https://example.com/policy', anchor: 'a\u0000b' } },
    { ...document, link: 'https://example.com/policy' },
    { id: 'text-one', name: 'notes.txt', text: 'notes', link: { url: 'https://example.com/policy' } }
  ]
  for (const file of malformed) {
    const bad = setup({ read: async () => ({ project: '/project', sha256: 'x', content: { version: 1, chats: [{ ...chat, documents: [file] }] } }) })
    await bad.store.load()
    expect(bad.store.getSnapshot().ready, JSON.stringify(file.link)).toBe(false)
    expect(bad.write).not.toHaveBeenCalled()
  }
})

it('restores bounded website references without embedding the crawl results in chat',async()=>{
 const base=setup();await base.store.load();const chat=base.store.create()
 const ref={id:'12345678-1234-1234-1234-123456789abc',digest:'sha256:'+'a'.repeat(64),seed:'https://example.com/'}
 const restore=async(websites:unknown)=>{const value=setup({read:async()=>({project:'/project',sha256:'x',content:{version:1,chats:[{...chat,websites}]}})});await value.store.load();return value}
 expect((await restore([ref])).store.getSnapshot().chats[0]!.websites).toEqual([ref])
 for(const bad of [[{...ref,seed:'http://example.com/'}],[{...ref,digest:'bad'}],[ref,ref],Array(17).fill(ref)])expect((await restore(bad)).store.getSnapshot().ready).toBe(false)
})


it('saves response work without advancing the independent pack artifact generation', async () => {
  const {store}=setup();await store.load();const chat=store.create()
  const state={...INITIAL_STATE,phase:'review' as const,status:'ready' as const,candidates:[{revision:1,text:'{"title":"Policy"}',document:{title:'Policy'},digest:'a'.repeat(64),producedBy:'conversation' as const}]}
  const binding={state,sources:[],blocked:'',model:'model',researchConfigured:false,run:null,ledger:null}
  store.report(chat.id,binding)
  const artifact=store.getSnapshot().packDrafts[0]!
  const responses=[{id:'response',messageId:'message',documents:[],websites:[],sourceIds:[],work:{items:[{id:'call',name:'read_link',status:'complete' as const}],notices:[]}}]
  store.report(chat.id,{...binding,state:{...state,responses}})
  expect(store.getSnapshot().packDrafts[0]?.generation).toBe(artifact.generation)
  expect(store.getSnapshot().chats[0]?.checkpoint?.state.responses).toEqual(responses)
})
it('retains a search connection, policy and response-owned search reference through save and reload',async()=>{
 const {store,write}=setup();await store.load();const chat=store.create()
 const search={id:'12345678-1234-1234-1234-123456789abc',digest:'sha256:'+'a'.repeat(64),request:{connection:'demo',revision:'ab'.repeat(32),query:'policy',maxResults:5}}
 store.update(chat.id,{searchConnection:'demo',researchMode:'provided',searches:[search],checkpoint:{sources:[],state:{...INITIAL_STATE,responses:[{id:'response-1',searches:[search],documents:[],websites:[],sourceIds:[],work:{items:[],notices:[]}}]}}})
 await store.flush()
 const saved=write.mock.calls.at(-1)![0]
 const restored=setup({read:async()=>({project:'/project',sha256:'saved',content:saved})});await restored.store.load()
 expect(restored.store.getSnapshot().error).toBe('')
 expect(restored.store.getSnapshot().chats[0]).toMatchObject({searchConnection:'demo',researchMode:'provided',searches:[search],checkpoint:{state:{responses:[{id:'response-1',searches:[search]}]}}})
})

it('persists web Research and upgrades only an unsubmitted legacy selection', async () => {
  const { store, write } = setup(); await store.load()
  const draft = store.startChat(undefined, 'research')
  store.update(draft.id, { composer: 'Please research this subject' })
  const reopened = setup(); await reopened.store.load()
  expect(reopened.store.startChat()).toMatchObject({ mode: 'web-research', composer: 'Please research this subject' })
  const chat = store.create(undefined, 'web-research')
  await store.flush()
  const saved = write.mock.calls.at(-1)![0]
  const restored = setup({ read: async () => ({ project: '/project', sha256: 'saved', content: saved }) })
  await restored.store.load()
  expect(restored.store.getSnapshot().chats.find(c => c.id === chat.id)?.mode).toBe('web-research')
})

it('keeps saved research checkpoints on their source-led lifecycle', async () => {
  const { store, write } = setup(); await store.load()
  const chat = store.create(undefined, 'research')
  store.update(chat.id, { checkpoint: { state: INITIAL_STATE, sources: [] } })
  await store.flush()
  const restored = setup({ read: async () => ({ project: '/project', sha256: 'saved', content: write.mock.calls.at(-1)![0] }) })
  await restored.store.load()
  expect(restored.store.getSnapshot().chats.find(c => c.id === chat.id)?.mode).toBe('research')
})

it('round-trips model-scoped chat reasoning and rejects invalid saved effort without rewriting history', async () => {
 const {store,write}=setup();await store.load();const chat=store.create()
 store.update(chat.id,{reasoning:{model:'chosen',effort:'low'}});await store.flush()
 const saved=write.mock.calls.at(-1)![0] as {version:1;chats:import('./store').Chat[]}
 const restored=setup({read:async()=>({project:'/project',sha256:'saved',content:saved})});await restored.store.load()
 expect(restored.store.getSnapshot().chats[0]?.reasoning).toEqual({model:'chosen',effort:'low'})
 restored.store.update(chat.id,{reasoning:{model:'chosen',effort:null}});await restored.store.flush()
 expect((restored.write.mock.calls.at(-1)![0] as typeof saved).chats[0]?.reasoning?.effort).toBeNull()
 const invalid=setup({read:async()=>({project:'/project',sha256:'saved',content:{...saved,chats:[{...saved.chats[0],reasoning:{model:'chosen',effort:'invented'}}]}})})
 await invalid.store.load();expect(invalid.store.getSnapshot().ready).toBe(false);expect(invalid.write).not.toHaveBeenCalled()
})

it('refuses a saved chat whose AI connection is not a connection id, without rewriting history', async () => {
 const {store,write}=setup();await store.load();const chat=store.create()
 store.update(chat.id,{aiConnection:'ai-111111111111111111111111',aiConnectionName:'Work'});await store.flush()
 const saved=write.mock.calls.at(-1)![0] as {version:1;chats:import('./store').Chat[]}
 expect(saved.chats[0]?.aiConnection).toBe('ai-111111111111111111111111')
 for(const aiConnection of ['../secrets/assistant','ai-XYZ','legacy-other']){
  const invalid=setup({read:async()=>({project:'/project',sha256:'saved',content:{...saved,chats:[{...saved.chats[0],aiConnection}]}})})
  await invalid.store.load();expect(invalid.store.getSnapshot().ready,aiConnection).toBe(false);expect(invalid.write).not.toHaveBeenCalled()
 }
})

it('saves model-free checks from an unsent draft workspace without creating a conversation',async()=>{
  const io=memoryDraftPersistence('/project'),base=setup();await base.store.load()
  const original=base.store.create(),state={...INITIAL_STATE,phase:'review' as const,status:'needs-input' as const,candidates:[{revision:1,text:'{"title":"Policy"}',document:{title:'Policy'},digest:'a'.repeat(64),producedBy:'conversation' as const}]}
  base.store.report(original.id,{state,sources:[],blocked:'',model:'',researchConfigured:false,run:null,ledger:null})
  const artifact=base.store.getSnapshot().packDrafts[0]!
  await io.write({version:1,drafts:[artifact],deleted:[]},'absent')
  const history={read:async()=>({project:'/project',sha256:'absent',content:{version:1,chats:[]}}),write:vi.fn(async(content:unknown)=>({project:'/project',sha256:'saved',content}))}
  const store=new ChatStore('/project',history,io);stores.push(store);await store.load()
  const chat=store.startChat(undefined,undefined,false,artifact.id)
  const binding={state:{...state,restored:true},sources:[],blocked:'No model connected',model:'',researchConfigured:false,run:{running:false,stop:vi.fn()},ledger:null} as unknown as ResearchRunBinding
  store.report(chat.id,binding)
  const action=vi.fn();expect(store.perform(chat.id,action,false)).toBe(true);expect(action).toHaveBeenCalledOnce()
  store.report(chat.id,{...binding,state:{...state,restored:true,status:'running',checkRun:'running'}})
  expect(await store.flush()).toBe(true)
  const interrupted=new ChatStore('/project',history,io);stores.push(interrupted);await interrupted.load()
  expect(interrupted.getSnapshot().packDrafts[0]?.checkpoint.state.checkRun).toBe('running')
  const check={documentDigest:'a'.repeat(64),valid:true,diagnostics:[],cases:[]}
  store.report(chat.id,{...binding,state:{...state,restored:false,checkRun:'complete',candidates:[{...state.candidates[0]!,check}]}})
  expect(await store.flush()).toBe(true)
  expect(store.getSnapshot().chats).toEqual([])
  const next=new ChatStore('/project',history,io);stores.push(next);await next.load()
  expect(next.getSnapshot().chats).toEqual([])
  expect(next.getSnapshot().packDrafts[0]?.checkpoint.state.candidates[0]?.previousCheck).toMatchObject(check)
  expect(next.getSnapshot().packDrafts[0]?.checkpoint.state.checkRun).toBe('complete')
  // A second workspace still holding the previous generation cannot check over it.
  const stale=store.startChat(undefined,undefined,true,artifact.id)
  store.report(stale.id,binding)
  store.update(stale.id,{draftGeneration:artifact.generation})
  const before=action.mock.calls.length
  expect(store.perform(stale.id,action,false)).toBe(false);expect(action).toHaveBeenCalledTimes(before)
})

it('retains portable input proposals and their saved link across chat reloads',async()=>{
 const {store,write}=setup();await store.load();const chat=store.create()
 const proposal={version:1 as const,draftId:'proposal-1',createdAt:'2026-10-12T12:00:00Z',name:'Find owners',intent:'Research decision owners',target:'people' as const,fields:[{name:'role',description:'Current role',type:'string' as const}],criteria:[],limits:{records:25,searches:10,pages:30,modelCalls:30},afterTurnId:'turn-1',savedInputId:'prep_'+'a'.repeat(32)}
 store.update(chat.id,{inputDrafts:[proposal]});await store.flush()
 const saved=write.mock.calls.at(-1)![0]
 const restored=setup({read:async()=>({project:'/project',sha256:'loaded',content:saved})});await restored.store.load()
 expect(restored.store.getSnapshot().chats[0].inputDrafts).toEqual([proposal])
 const poisoned=structuredClone(saved) as any;poisoned.chats[0].inputDrafts[0].ai={connection:'injected'}
 const invalid=setup({read:async()=>({project:'/project',sha256:'loaded',content:poisoned})});await invalid.store.load()
 expect(invalid.store.getSnapshot().ready).toBe(false);expect(invalid.write).not.toHaveBeenCalled()
})

it('keeps Dialects drafts separate from home, packs, and other inputs and restores their action history',async()=>{
 const {store,write}=setup();await store.load()
 const id='prep_'+'a'.repeat(32),other='prep_'+'b'.repeat(32)
 const home=store.startChat(),first=store.startChat(undefined,undefined,false,undefined,undefined,{id}),second=store.startChat(undefined,undefined,false,undefined,undefined,{id:other})
 expect(store.startChat().id).toBe(home.id)
 expect(store.startChat(undefined,undefined,false,undefined,undefined,{id}).id).toBe(first.id)
 expect(new Set([home.id,first.id,second.id]).size).toBe(3)
 const action={version:1 as const,id:'action',createdAt:'2026-01-01',inputId:id,name:'Company research',revision:'a'.repeat(64),recipeVersion:2,kind:'research' as const}
 store.update(first.id,{inputActions:[action]});store.retainChat(first.id);await store.flush()
 const content=write.mock.calls.at(-1)![0]
 const restored=setup({read:async()=>({project:'/project',sha256:'saved',content})});await restored.store.load()
 expect(restored.store.getSnapshot().chats[0]).toMatchObject({id:first.id,input:{id},inputActions:[action]})
 expect(restored.store.startChat().input).toBeUndefined()
})
it('rejects persisted input actions with injected mutation authority',async()=>{
 const inputId='prep_'+'a'.repeat(32)
 const chat={id:'bad-input',title:'Dialects',mode:'draft',view:'chat',pinned:false,archived:false,updatedAt:'2026-01-01',composer:'',model:'',input:{id:inputId},inputActions:[{version:1,id:'action',createdAt:'2026-01-01',inputId,name:'Input',revision:'a'.repeat(64),recipeVersion:1,kind:'update-recipe',patch:{research:{ai:{connection:'injected'}}}}]}
 const {store}=setup({read:async()=>({project:'/project',sha256:'saved',content:{version:1,chats:[chat]}})});await store.load()
 expect(store.getSnapshot().chats).toHaveLength(0)
 expect(store.getSnapshot().error).toContain('Invalid saved dialect actions')
})

it('waits for an in-flight save and checkpoints the newer task state before dispatch',async()=>{
 let release!:()=>void
 let calls=0
 const saved:unknown[]=[]
 const {store}=setup({write:async document=>{saved.push(document);if(++calls===1)await new Promise<void>(resolve=>{release=resolve});return {project:'/project',sha256:'revision-'+calls,content:document}}})
 await store.load();const chat=store.create();const first=store.flush()
 await vi.waitFor(()=>expect(calls).toBe(1))
 store.update(chat.id,{composer:'Checkpoint requested after the first save started'})
 let finished=false
 const checkpoint=store.checkpoint(new AbortController().signal).then(()=>{finished=true})
 await Promise.resolve();expect(finished).toBe(false)
 release();await first;await checkpoint
 expect(calls).toBe(2)
 expect((saved[1] as {chats:{composer:string}[]}).chats[0]?.composer).toBe('Checkpoint requested after the first save started')
})

it('refuses further delegated work when its checkpoint cannot be saved',async()=>{
 const {store}=setup({write:async()=>{throw Error('disk unavailable')}})
 await store.load();store.create()
 await expect(store.checkpoint(new AbortController().signal)).rejects.toThrow('disk unavailable')
 expect(store.getSnapshot().dirty).toBe(true)
})

it('cancels a checkpoint wait without cancelling or replaying the existing save',async()=>{
 let release!:()=>void
 const {store}=setup({write:async document=>{await new Promise<void>(resolve=>{release=resolve});return {project:'/project',sha256:'saved',content:document}}})
 await store.load();store.create();const saved=store.flush()
 await vi.waitFor(()=>expect(release).toBeTypeOf('function'))
 const stop=new AbortController(),waiting=store.checkpoint(stop.signal)
 stop.abort();await expect(waiting).rejects.toMatchObject({name:'AbortError'})
 release();await saved
 expect(store.getSnapshot().error).toBe('')
})

it('stores test-design exchanges in the existing session, deduplicates checkpoints, and reloads them', async () => {
 const {AuthoringRun}=await import('../research/run')
 const {store,write}=setup();await store.load()
 const chat=store.startChat({id:'pack',path:'pack.json',digest:'digest'})
 const run=new AuthoringRun({} as import('../research/run').RunPorts)
 store.report(chat.id,{run,state:run.getSnapshot(),sources:[],model:'fixture'} as unknown as ResearchRunBinding)
 const turns=[{id:'test-user',role:'user' as const,kind:'message' as const,text:'Design boundary cases',at:'2026-10-13T12:00:00Z',testDesign:{owner:'pack',proposalId:'proposal'}},{id:'test-assistant',role:'assistant' as const,kind:'message' as const,text:'Proposed cases for review',at:'2026-10-13T12:01:00Z',testDesign:{owner:'pack',proposalId:'proposal'}}]
 store.appendConversation(chat.id,turns);store.appendConversation(chat.id,turns)
 store.report(chat.id,{run,state:INITIAL_STATE,sources:[],model:'fixture'} as unknown as ResearchRunBinding)
 await store.checkpoint(new AbortController().signal)
 expect(store.getSnapshot().chats).toHaveLength(1)
 expect(run.getSnapshot().turns).toHaveLength(2)
 expect(write).toHaveBeenCalledOnce()
 const content=write.mock.calls[0]![0]
 const restored=setup({read:async()=>({project:'/project',sha256:'saved',content})});await restored.store.load()
 expect(restored.store.getSnapshot().chats[0]?.checkpoint?.state.turns).toHaveLength(2)
 expect(restored.store.getSnapshot().chats[0]?.checkpoint?.state.turns).toMatchObject(turns)
 expect(()=>store.appendConversation(chat.id,[{...turns[0]!,text:'Changed answer'}])).toThrow('conflicts')
})

it('retries failed initial hydration without replaying writes or reloading a live transcript',async()=>{
 const io=vi.fn().mockRejectedValueOnce(Error('storage busy')).mockResolvedValue({project:'/project',sha256:'saved',content:{version:1,chats:[]}})
 const {store,write}=setup({read:io})
 await store.load()
 expect(store.getSnapshot().ready).toBe(false)
 await store.retryInitialLoad()
 expect(store.getSnapshot().ready).toBe(true)
 expect(store.getSnapshot().error).toBe('')
 const chat=store.startChat();store.update(chat.id,{composer:'Keep this unfinished message'})
 await store.retryInitialLoad()
 expect(io).toHaveBeenCalledTimes(2)
 expect(store.getSnapshot().drafts[0].composer).toBe('Keep this unfinished message')
 expect(write).not.toHaveBeenCalled()
})

it('restores refused Dialect candidates and exact findings without saved-action authority',async()=>{
 const {store,write}=setup();await store.load();const chat=store.create()
 const action={version:1 as const,id:'mapping-attempt',createdAt:'2026-10-16',inputId:'prep_'+'a'.repeat(32),name:'Triage',revision:'a'.repeat(64),recipeVersion:1,kind:'update-dialect' as const,
  dialect:{packKey:'triage',packDigest:'sha256:'+'b'.repeat(64),facts:[],evidence:[],summary:'Unknown fields remain unknown.'},
  dialectCheck:{document:'{"version":3,"case":{"facts":[],"evidence":[]}}',report:{status:'refused',diagnostics:[{code:'JPS-DIALECT-FORM',instancePath:'/case',message:'Review this declaration.'}]}}}
 store.update(chat.id,{inputActions:[action]});await store.flush()
 const saved=write.mock.calls.at(-1)![0]
 const restored=setup({read:async()=>({project:'/project',sha256:'loaded',content:saved})});await restored.store.load()
 expect(restored.store.getSnapshot().chats[0].inputActions).toEqual([action])
 expect(restored.store.getSnapshot().chats[0].inputActions?.[0].applied).toBeUndefined()
 const bad=structuredClone(saved) as any;bad.chats[0].inputActions[0].dialectCheck.report.compatibility={notes:'not findings'}
 const invalid=setup({read:async()=>({project:'/project',sha256:'loaded',content:bad})});await invalid.store.load()
 expect(invalid.store.getSnapshot().ready).toBe(false);expect(invalid.write).not.toHaveBeenCalled()
})
