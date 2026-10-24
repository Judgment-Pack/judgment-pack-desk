import { act, cleanup, renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { giveThisPageASessionForTesting } from '../mcp/session'
import { readFile, writeFile } from '../files/client'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { savedChatRun } from '../assistant/serverRuns/controller'
import { publishDirty } from '../shell/authorBridge'
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
