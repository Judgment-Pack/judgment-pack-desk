import { afterEach, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { sha256 } from '@judgment-pack/harness/records'
import { INITIAL_STATE } from '../research/initialState'
import { savedChatRun } from '../assistant/serverRuns/controller'
import { giveThisPageASessionForTesting } from '../mcp/session'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { memberOf, parseJsonText } from '../research/verify/canon'
import { ChatStore, type Chat } from './store'
import { encodeWireChat, wireJSON, conversationWire } from './wire'
const at='2026-10-20T00:00:00Z'
const one:Chat={id:'chat-one',title:'Conversation',pinned:false,archived:false,updatedAt:at,composer:'',model:'model-one',mode:'draft',view:'chat',checkpoint:{state:{...INITIAL_STATE,turns:[{id:'message-one',role:'user',kind:'message',text:'Original message',at}]},sources:[]},serverRequest:{requestId:'message-one',admissionId:'admission-one'}}
const other:Chat={...one,id:'chat-two',checkpoint:undefined,serverRequest:undefined,title:'Other'}
const stores:ChatStore[]=[]
afterEach(()=>{stores.forEach(s=>s.dispose());stores.length=0;vi.unstubAllGlobals();vi.restoreAllMocks()})
function fixture(raw=JSON.stringify({version:1,chats:[one,other]})) {
 let document=raw,digest=sha256(raw)
 const writes:string[]=[]
 const reply=()=>new Response(`{"project":"/project","sha256":${JSON.stringify(digest)},"content":${document},"chatRevisions":{"chat-one":"${'a'.repeat(64)}"}}`,{headers:{'Content-Type':'application/json'}})
 const fetch=vi.fn(async(_path:unknown,init?:RequestInit)=>{
  if(init?.method==='PUT') {
   if((init.headers as Record<string,string>)['If-Match']!==digest)return new Response('{}',{status:409})
   document=String(init.body);digest=sha256(document);writes.push(document)
  }
  return reply()
 });vi.stubGlobal('fetch',fetch)
 const store=new ChatStore('/project',undefined,memoryDraftPersistence('/project'));stores.push(store)
 const remote=(value:string)=>{document=value;digest=sha256(document)}
 return {store,fetch,writes,remote,get raw(){return document}}
}
function bind(store:ChatStore,id='chat-one'){
 const chat=store.getSnapshot().chats.find(c=>c.id===id)!,run=savedChatRun(store,chat)
 store.report(id,{run,state:run.getSnapshot(),ledger:null,sources:[],blocked:'',model:chat.model,researchConfigured:false})
 return run
}
it('canonical observations do not autosave a pinned checkpoint and dispose only disconnects',async()=>{
 const h=fixture();await h.store.load();const run=bind(h.store),stop=vi.spyOn(run,'stop'),disconnect=vi.spyOn(run,'disconnect')
 const before=h.store.getSnapshot().chats[0].checkpoint
 expect(()=>run.appendConversation([{id:'display-only',role:'assistant',kind:'message',text:'Observation',at}])).toThrow()
 h.store.report(one.id,{run,state:run.getSnapshot(),ledger:null,sources:[],blocked:'',model:'model',researchConfigured:false})
 expect(h.store.getSnapshot().chats[0].checkpoint).toBe(before);expect(h.store.getSnapshot().dirty).toBe(false)
 h.store.dispose();expect(disconnect).toHaveBeenCalledOnce();expect(stop).not.toHaveBeenCalled();expect(h.writes).toEqual([])
})
it('merges terminal server output with an unrelated unsent local edit under the latest document CAS',async()=>{
 const h=fixture();await h.store.load();bind(h.store);h.store.pinServerChat(one.id)
 h.store.update(other.id,{composer:'Unsent local draft'})
 const terminal={...one,checkpoint:{state:{...one.checkpoint!.state,turns:[...one.checkpoint!.state.turns,{id:'answer',role:'assistant' as const,kind:'message' as const,text:'Server response',at}]},sources:[]}}
 h.remote(JSON.stringify({version:1,chats:[terminal,other]}))
 expect(await h.store.flush()).toBe(true)
 const saved=JSON.parse(h.raw).chats as Chat[]
 expect(saved[0].checkpoint!.state.turns.at(-1)?.text).toBe('Server response');expect(saved[1].composer).toBe('Unsent local draft')
 expect(h.store.getSnapshot().chats[0].checkpoint!.state.turns.at(-1)?.text).toBe('Server response')
 expect(h.writes).toHaveLength(1)
})
it('same-field CAS conflict retains dirty local drafts and current server output without a write',async()=>{
 const h=fixture();await h.store.load();bind(h.store)
 h.store.update(other.id,{composer:'Local draft'})
 h.remote(JSON.stringify({version:1,chats:[one,{...other,composer:'Remote draft'}]}))
 expect(await h.store.flush()).toBe(false);expect(h.writes).toEqual([])
 expect(h.store.getSnapshot().chats.find(c=>c.id===other.id)?.composer).toBe('Local draft');expect(h.store.getSnapshot().dirty).toBe(true)
 expect(h.store.getSnapshot().error).toContain('same chat field');expect(JSON.parse(h.raw).chats[1].composer).toBe('Remote draft')
})
it.each(['1.0','1e0','-0','9007199254740993'])('preserves untouched numeric wire literal %s during an unrelated active-chat edit',async literal=>{
 const raw=JSON.stringify({version:1,chats:[one,other]}).replace('"state":{',`"state":{"retainedNumeric":${literal},`)
 const h=fixture(raw);await h.store.load();bind(h.store);h.store.pinServerChat(one.id)
 h.store.update(other.id,{composer:'Local draft'});expect(await h.store.flush()).toBe(true)
 expect(h.raw).toContain(`"retainedNumeric":${literal}`)
})
it.each([['1.0','1e0'],['-0','0'],['9007199254740993','9007199254740992']])('refuses a same-field lexical server change %s to %s rather than normalizing it',async(before,after)=>{
 const raw=JSON.stringify({version:1,chats:[one,other]}).replace('"state":{',`"state":{"retainedNumeric":${before},`)
 const h=fixture(raw);await h.store.load();bind(h.store)
 h.store.update(one.id,{checkpoint:{...one.checkpoint!,state:{...one.checkpoint!.state,detail:'Local checkpoint edit'}}})
 h.remote(raw.replace(`"retainedNumeric":${before}`,`"retainedNumeric":${after}`))
 expect(await h.store.flush()).toBe(false);expect(h.writes).toEqual([]);expect(h.store.getSnapshot().dirty).toBe(true)
})
it('rejects pinned-chat edits and test-design append while permitting unrelated local work',async()=>{
 const h=fixture();await h.store.load();bind(h.store);h.store.pinServerChat(one.id)
 h.store.update(one.id,{interaction:'plan'});expect(h.store.getSnapshot().chats[0].interaction).toBeUndefined()
 expect(()=>h.store.appendConversation(one.id,[{id:'new',role:'user',kind:'message',text:'Unadmitted',at}])).toThrow()
 expect(h.writes).toEqual([])
})
it('wire reuse requires explicit untouched provenance, keeping changed numbers separate',()=>{
 const raw='{"id":"one","n":9007199254740993,"zero":-0,"decimal":1e0}'
 expect(encodeWireChat({id:'one',n:9007199254740992,zero:0,decimal:1} as {id:string},raw)).toContain('9007199254740993')
 const changed=encodeWireChat({id:'one',n:9007199254740992,zero:0,decimal:1} as {id:string},raw,new Set(['n','zero','decimal']))
 expect(changed).toContain('9007199254740992');expect(changed).toContain('"zero":0');expect(changed).toContain('"decimal":1')
 expect(()=>conversationWire('{"content":{"chats":[]},"content":{}}')).toThrow()
})
const produced=process.env.DESK_RUN_CLIENT_OUTPUTS
it.skipIf(!produced)('reconciles actual Go acknowledged terminal projection through the controller and ChatStore with a local draft',async()=>{
 giveThisPageASessionForTesting('store-session')
 const sourceRaw=readFileSync(`${produced}/consumer-source.json`,'utf8'),terminal=JSON.parse(readFileSync(`${produced}/consumer-terminal.json`,'utf8')),savedRaw=readFileSync(`${produced}/consumer-conversations.json`,'utf8')
 const source=wireJSON(memberOf(parseJsonText(sourceRaw),'content')!),saved=wireJSON(memberOf(parseJsonText(savedRaw),'content')!)
 const addOther=(raw:string)=>{const node=parseJsonText(raw),chats=memberOf(node,'chats');if(chats?.kind==='array')chats.items.push(parseJsonText(JSON.stringify(other)));return wireJSON(node)}
 const h=fixture(addOther(source));await h.store.load();const run=bind(h.store)
 h.store.update(other.id,{composer:'Local draft while server finished'})
 h.remote(addOther(saved))
 const fetch=h.fetch;vi.stubGlobal('fetch',vi.fn(async(path:unknown,init?:RequestInit)=>String(path).startsWith('/api/assistant-runs/')?new Response(JSON.stringify({runs:[terminal]}),{headers:{'Content-Type':'application/json'}}):fetch(path,init)))
 await run.reconnect()
 expect(run.getSnapshot().turns.at(-1)?.text).toBe('Service-owned terminal response')
 expect(h.store.getSnapshot().chats.find(c=>c.id===other.id)?.composer).toBe('Local draft while server finished')
 expect(await h.store.flush()).toBe(true);expect(h.raw).toContain('Service-owned terminal response');expect(h.raw).toContain('Local draft while server finished')
})
it('a proven pre-POST stop clears only its matching marker and retains the stopped original message through CAS',async()=>{
 const h=fixture();await h.store.load();bind(h.store);h.store.pinServerChat(one.id)
 h.store.releaseUnsentServerRequest(one.id,'different-request',one.checkpoint!.state)
 expect(h.store.getSnapshot().chats[0].serverRequest).toBeDefined()
 h.store.releaseUnsentServerRequest(one.id,'message-one',one.checkpoint!.state)
 expect(await h.store.flush()).toBe(true)
 const saved=JSON.parse(h.raw).chats[0]
 expect(saved.serverRequest).toBeUndefined();expect(saved.checkpoint.state.status).toBe('stopped');expect(saved.checkpoint.state.turns[0].text).toBe('Original message')
})

it('retains the original controller and store admission while terminal reconciliation is pending',async()=>{
 giveThisPageASessionForTesting('store-reconcile-session')
 const {default:pending}=await import('../assistant/serverRuns/fixtures/pending.json')
 const saved={...one,serverRequest:{requestId:pending.requestId,admissionId:pending.admissionId}}
 const h=fixture(JSON.stringify({version:1,chats:[saved,other]}));await h.store.load();const run=bind(h.store)
 const terminal={...pending,state:'complete',chatProjection:{responseId:`response-${sha256(pending.id).slice(0,32)}`,digest:'b'.repeat(64),acknowledged:true}}
 let release!:()=>void;const held=new Promise<void>(r=>{release=r})
 const reconcile=vi.spyOn(h.store,'reconcileServer').mockImplementationOnce(async(...args)=>{await held;return original(...args)})
 const original=ChatStore.prototype.reconcileServer.bind(h.store)
 const prepare=vi.spyOn(h.store,'prepareServerRequest'),baseFetch=h.fetch
 const fetch=vi.fn(async(path:unknown,init?:RequestInit)=>String(path).startsWith('/api/assistant-runs/')?new Response(JSON.stringify({runs:[terminal]}),{headers:{'Content-Type':'application/json'}}):baseFetch(path,init));vi.stubGlobal('fetch',fetch)
 const reconnect=run.reconnect();await vi.waitFor(()=>expect(reconcile).toHaveBeenCalledOnce())
 const before=run.getSnapshot();expect(run.canRetryResponse).toBe(false)
 run.send('Too early');run.retryResponse();expect(()=>run.appendConversation([])).toThrow()
 expect(run.getSnapshot()).toBe(before);expect(prepare).not.toHaveBeenCalled();expect(h.writes).toEqual([])
 expect(h.store.getSnapshot().chats[0].serverRequest).toEqual(saved.serverRequest)
 release();await reconnect;await run.reconnect()
 expect(fetch.mock.calls.filter(([path])=>String(path).endsWith(`/requests/${pending.requestId}`))).toHaveLength(2)
 expect(prepare).not.toHaveBeenCalled();expect(h.writes).toEqual([]);expect(run.canRetryResponse).toBe(true)
})
