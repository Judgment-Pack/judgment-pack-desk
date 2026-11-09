import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { giveThisPageASessionForTesting } from '../mcp/session'
import { readFile, writeFile } from '../files/client'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { SavedChatRun, savedChatRun } from '../assistant/serverRuns/controller'
import { publishDirty } from '../shell/authorBridge'
import { INITIAL_STATE } from '../research/initialState'
import { sha256 } from '@judgment-pack/harness/records'
import { serverRunClient } from '../assistant/serverRuns/client'
import pending from '../assistant/serverRuns/fixtures/pending.json'
import type { Candidate } from '../research/run'
import type { Chat } from './store'
import { ChatStore } from './store'
import { usePackProposalSave } from './usePackProposalSave'
const m=vi.hoisted(()=>({store:null as ChatStore|null,call:vi.fn()}))
vi.mock('./ChatProvider',()=>({useChatStore:()=>m.store}))
vi.mock('../files/client',async original=>({...await original(),readFile:vi.fn(),writeFile:vi.fn()}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/project'}})}))
vi.mock('../mcp/McpProvider',()=>({useMcp:()=>({client:{callTool:m.call}})}))
afterEach(()=>{cleanup();publishDirty('one.json',false);m.store?.dispose();m.store=null;vi.unstubAllGlobals();vi.clearAllMocks()})
const produced=process.env.DESK_RUN_CLIENT_OUTPUTS
async function journey(index=-1){
 giveThisPageASessionForTesting('proposal-session')
 const document=JSON.parse(readFileSync(`${produced}/saved-pack-chat.json`,'utf8')),snapshot=JSON.parse(readFileSync(`${produced}/saved-pack-snapshot.json`,'utf8')),suite=JSON.parse(readFileSync(`${produced}/saved-pack-suite.json`,'utf8'))
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'saved',content:document}),write:vi.fn()},memoryDraftPersistence('/project'));m.store=store;await store.load()
 const chat=store.getSnapshot().chats[0],run=savedChatRun(store,{...chat,serverRequest:{requestId:snapshot.requestId,admissionId:snapshot.admissionId}})
 let result:unknown,changedSuite=false
 const fetch=vi.fn(async(path:unknown)=>{
  if(String(path).includes('/owners/'))return Response.json({runs:[snapshot]})
  if(path==='/api/pack-tests')return Response.json({project:'/project',sha256:'suite',content:changedSuite?{...suite,suites:{...suite.suites,one:{...suite.suites.one,cases:[]}}}:suite})
  if(String(path).endsWith('/pack-checks'))return Response.json(result)
  throw Error('Unexpected browser model or host dispatch')
 });vi.stubGlobal('fetch',fetch);await run.reconnect()
 store.report(chat.id,{run,state:run.getSnapshot(),ledger:null,sources:[],blocked:'',model:chat.model,researchConfigured:false})
 const candidate=run.getSnapshot().candidates.at(index)!,basis=candidate.packReview!.basis
 result=candidate.packReview!.check
 let file={path:basis.path,content:basis.text,sha256:'saved-file',bytes:basis.text.length}
 vi.mocked(readFile).mockImplementation(async()=>file)
 vi.mocked(writeFile).mockImplementation(async input=>{file={...file,content:input.content,sha256:'written'};return file})
 const query=new QueryClient({defaultOptions:{queries:{retry:false}}}),wrapper=({children}:{children:React.ReactNode})=><QueryClientProvider client={query}>{children}</QueryClientProvider>
 const hook=renderHook(()=>usePackProposalSave({candidate,basis,busy:false}),{wrapper})
 return {hook,candidate,run,fetch,setFile:(text:string)=>{file={...file,content:text}},setCheck:(value:unknown)=>{result=value},setChangedSuite:()=>{changedSuite=true}}
}
it.skipIf(!produced)('routes fresh checks for an actual Go proposal through the canonical endpoint before explicit Save and exact readback',async()=>{
 const h=await journey();await act(()=>h.hook.result.current.save())
 expect(h.hook.result.current.error).toBe('');expect(writeFile).toHaveBeenCalledOnce();expect(readFile).toHaveBeenCalledTimes(2)
 expect(h.fetch.mock.calls.some(([p])=>String(p).endsWith('/pack-checks'))).toBe(true);expect(m.call).not.toHaveBeenCalled()
})
it.skipIf(!produced)('dirty editor, stale source, changed suite and actual Go mismatch findings veto Save without replay',async()=>{
 const dirty=await journey();act(()=>publishDirty('one.json',true));await act(()=>dirty.hook.result.current.save());expect(writeFile).not.toHaveBeenCalled();expect(dirty.fetch.mock.calls.some(([p])=>String(p).endsWith('/pack-checks'))).toBe(false)
 cleanup();publishDirty('one.json',false);m.store?.dispose()
 const stale=await journey();stale.setFile('{"changed":true}');await act(()=>stale.hook.result.current.save());expect(writeFile).not.toHaveBeenCalled();expect(stale.hook.result.current.error).toContain('pack changed')
 cleanup();m.store?.dispose()
 const suite=await journey();suite.setChangedSuite();await act(()=>suite.hook.result.current.save());expect(writeFile).not.toHaveBeenCalled();expect(suite.fetch.mock.calls.filter(([p])=>String(p).endsWith('/pack-checks'))).toHaveLength(1)
 cleanup();m.store?.dispose()
 const mismatch=await journey(0);mismatch.setCheck(JSON.parse(readFileSync(`${produced}/saved-pack-recheck.json`,'utf8')));await act(()=>mismatch.hook.result.current.save());expect(writeFile).not.toHaveBeenCalled();expect(mismatch.hook.result.current.error).toContain('mismatches');expect(m.call).not.toHaveBeenCalled()
})

// Handwritten restored proposal and discovery controls, independent of generated Go output.
async function readinessJourney(){
 giveThisPageASessionForTesting('proposal-readiness-session')
 const text='{"id":"one","version":"0.1.1"}',before='{"id":"one","version":"0.1.0"}',basis={packId:'one',path:'one.json',text:before}
 const check={documentDigest:sha256(text),suiteKey:'[]',at:'2026-10-25T00:00:00Z',valid:true,diagnostics:[],totalCases:0}
 const candidate:Candidate={revision:1,producedBy:'conversation',text,document:JSON.parse(text),digest:sha256(text),packReview:{basis,check}}
 const chat:Chat={id:'chat-one',title:'Proposal',pinned:false,archived:false,updatedAt:check.at,composer:'',model:'model-one',mode:'draft',view:'chat',pack:{id:'one',path:'one.json',digest:sha256(before)},serverRequest:{requestId:pending.requestId,admissionId:pending.admissionId},checkpoint:{state:{...INITIAL_STATE,status:'complete',turns:[{id:pending.requestId,role:'user',kind:'message',text:'Update version only.',at:check.at}],candidates:[candidate]},sources:[]}}
 const store=new ChatStore('/project',undefined,memoryDraftPersistence('/project'));m.store=store
 const run=new SavedChatRun(chat.id,chat,{project:'/project',client:serverRunClient,prepare:vi.fn(),reconcile:async()=>chat,pin:vi.fn(),problem:vi.fn()})
 const report=()=>store.report(chat.id,{run,state:run.getSnapshot(),ledger:null,sources:[],blocked:'',model:chat.model,researchConfigured:false});report();run.subscribe(report)
 let resolve!:(value:Response)=>void
 const eventsBody=new ReadableStream<Uint8Array>()
 const fetch=vi.fn(async(path:unknown)=>{
  if(String(path).includes('/owners/'))return await new Promise<Response>(r=>{resolve=r})
  if(path==='/api/pack-tests')return Response.json({project:'/project',sha256:'suite',content:{version:1,suites:{}}})
  if(String(path).endsWith('/pack-checks'))return Response.json(check)
  if(String(path).includes('/events'))return new Response(eventsBody,{headers:{'Content-Type':'text/event-stream'}})
  throw Error('Unexpected model or host dispatch')
 });vi.stubGlobal('fetch',fetch)
 let file={path:basis.path,content:before,sha256:'saved-file',bytes:before.length}
 vi.mocked(readFile).mockImplementation(async()=>file);vi.mocked(writeFile).mockImplementation(async input=>{file={...file,content:input.content,sha256:'written'};return file})
 const query=new QueryClient({defaultOptions:{queries:{retry:false}}}),wrapper=({children}:{children:React.ReactNode})=><QueryClientProvider client={query}>{children}</QueryClientProvider>
 const hook=renderHook(()=>usePackProposalSave({candidate,basis,busy:false}),{wrapper})
 const response=(patch:Record<string,unknown>={})=>({...pending,state:'complete',chatProjection:{responseId:`response-${sha256(pending.id).slice(0,32)}`,digest:'b'.repeat(64),acknowledged:true},...patch})
 return {hook,run,fetch,candidate,response,resolve:(value:Response)=>resolve(value)}
}
it('blocks pending initial discovery then reacts to eligible adoption of the same candidate before canonical check and Save',async()=>{
 const h=await readinessJourney(),original=h.candidate
 expect(h.run.canCheckProposal(original)).toBe(false);expect(h.hook.result.current.reason).toContain('Reconnect')
 await act(()=>h.hook.result.current.save());expect(readFile).not.toHaveBeenCalled();expect(writeFile).not.toHaveBeenCalled();expect(m.call).not.toHaveBeenCalled()
 let connecting!:Promise<void>;act(()=>{connecting=h.run.reconnect()});await waitFor(()=>expect(h.fetch).toHaveBeenCalledOnce())
 expect(h.hook.result.current.reason).toContain('Reconnect')
 await act(async()=>{h.resolve(Response.json({runs:[h.response()]}));await connecting})
 expect(h.candidate).toBe(original);expect(h.run.canCheckProposal(original)).toBe(true);expect(h.hook.result.current.reason).toBe('')
 await act(()=>h.hook.result.current.save());expect(h.hook.result.current.error).toBe('');expect(writeFile).toHaveBeenCalledOnce();expect(readFile).toHaveBeenCalledTimes(2);expect(h.fetch.mock.calls.filter(([p])=>String(p).endsWith('/pack-checks'))).toHaveLength(1);expect(m.call).not.toHaveBeenCalled()
})
it.each(['refused','ambiguous','historical'] as const)('keeps restored proposal Save unavailable after %s discovery without local-check fallback',async kind=>{
 const h=await readinessJourney();let connecting!:Promise<void>;act(()=>{connecting=h.run.reconnect()});await waitFor(()=>expect(h.fetch).toHaveBeenCalledOnce())
 const response=kind==='refused'?new Response('{}',{status:403}):Response.json({runs:kind==='ambiguous'?[h.response(),h.response({id:'arun_'+'2'.repeat(32)})]:[kind==='historical'?h.response({historical:true}):h.response({state:'running',chatProjection:undefined})]})
 await act(async()=>{h.resolve(response);await connecting})
 expect(h.run.canCheckProposal(h.candidate)).toBe(false);expect(h.hook.result.current.reason).toContain('Reconnect');await act(()=>h.hook.result.current.save());expect(readFile).not.toHaveBeenCalled();expect(writeFile).not.toHaveBeenCalled();expect(m.call).not.toHaveBeenCalled();expect(h.fetch.mock.calls.every(([p])=>!String(p).endsWith('/pack-checks'))).toBe(true)
})
it('keeps restored proposal Save unavailable during a live running discovery without local-check fallback',async()=>{
 const h=await readinessJourney();let connecting!:Promise<void>;act(()=>{connecting=h.run.reconnect()});await waitFor(()=>expect(h.fetch).toHaveBeenCalledOnce())
 await act(async()=>{h.resolve(Response.json({runs:[h.response({state:'running',chatProjection:undefined})]}))})
 await waitFor(()=>expect(h.fetch.mock.calls.some(([path])=>String(path).includes('/events'))).toBe(true))
 expect(h.run.getSnapshot().status).toBe('running');expect(h.run.running).toBe(true)
 expect(h.run.canCheckProposal(h.candidate)).toBe(false);expect(h.hook.result.current.reason).toContain('Reconnect');await act(()=>h.hook.result.current.save());expect(readFile).not.toHaveBeenCalled();expect(writeFile).not.toHaveBeenCalled();expect(m.call).not.toHaveBeenCalled();expect(h.fetch.mock.calls.every(([p])=>!String(p).endsWith('/pack-checks'))).toBe(true)
 h.run.disconnect();await connecting
})
it('rechecks live server readiness after file read and never falls back when the eligible owner disappears',async()=>{
 const h=await readinessJourney();let connecting!:Promise<void>;act(()=>{connecting=h.run.reconnect()});await waitFor(()=>expect(h.fetch).toHaveBeenCalledOnce());await act(async()=>{h.resolve(Response.json({runs:[h.response()]}));await connecting})
 expect(h.hook.result.current.reason).toBe('');let resolve!:(value:Awaited<ReturnType<typeof readFile>>)=>void
 vi.mocked(readFile).mockImplementationOnce(()=>new Promise(r=>{resolve=r}))
 let saving!:Promise<void>;act(()=>{saving=h.hook.result.current.save()});act(()=>h.run.endSession());await act(async()=>{resolve({path:'one.json',content:h.candidate.packReview!.basis.text,sha256:'saved-file',bytes:34});await saving})
 expect(h.hook.result.current.error).toContain('Reconnect');expect(writeFile).not.toHaveBeenCalled();expect(m.call).not.toHaveBeenCalled();expect(h.fetch.mock.calls.every(([p])=>!String(p).endsWith('/pack-checks'))).toBe(true)
})
