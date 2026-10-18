import {afterEach,expect,it,vi} from 'vitest'
import {renderHook,waitFor,cleanup} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {usePolicyChatContext} from './chatContext'
import {policyChatTools} from './chatTools'
import {readPolicy,type PolicyReply} from './client'
import {ChatStore} from '../chat/store'
import {INITIAL_STATE} from '../research/run'
import type {ResearchRunBinding} from '../research/useResearchRun'
import {memoryDraftPersistence} from '../testing/draftPersistence'
const fixture=vi.hoisted(()=>({store:undefined as ChatStore|undefined,initial:undefined as PolicyReply|undefined}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:fixture.store})}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),readPolicy:vi.fn(),policyQuery:(id:string)=>({queryKey:['policies',id],queryFn:async()=>fixture.initial})}))
afterEach(()=>{cleanup();fixture.store?.dispose();fixture.store=undefined;vi.resetAllMocks();sessionStorage.clear()})
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve}}
it('commits only the admitted Send snapshot when overlapping reads resolve different saved Policy revisions',async()=>{
 const id='policy_'+'a'.repeat(48),candidate={title:'Policy',narrative:'Revision A source',artifactVersion:'1.0.0'}
 const a={revision:'a'.repeat(64),document:{id,candidate}} as PolicyReply
 const b={revision:'b'.repeat(64),document:{id,candidate:{...candidate,narrative:'Revision B source'}}} as PolicyReply
 fixture.initial=a
 const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'absent',content:{version:1,chats:[]}}),write:async document=>({project:'/project',sha256:'saved',content:document})},memoryDraftPersistence('/project'));fixture.store=store
 await store.load()
 const chat=store.startChat(undefined,undefined,false,undefined,undefined,undefined,{id})
 let running=false
 const start=vi.fn((_prompt:string)=>{running=true})
 store.report(chat.id,{state:INITIAL_STATE,sources:[],blocked:'',model:'fixture',researchConfigured:false,ledger:null,run:{get running(){return running},start,stop:vi.fn()}} as unknown as ResearchRunBinding)
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const hook=renderHook(()=>usePolicyChatContext(id,chat.id),{wrapper:({children})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})
 await waitFor(()=>expect(hook.result.current?.busy).toBe(false))
 const firstRead=deferred<PolicyReply>(),secondRead=deferred<PolicyReply>()
 vi.mocked(readPolicy).mockReturnValueOnce(firstRead.promise).mockReturnValueOnce(secondRead.promise)
 const firstSend=hook.result.current!.prepare(),secondSend=hook.result.current!.prepare()
 firstRead.resolve(a)
 const first=await firstSend
 expect(store.getSnapshot().drafts[0]?.policyRequest).toBeUndefined()
 // The real shared admission callback is the sole snapshot commit point.
 expect(store.perform(chat.id,active=>{first.beforeSend();active.run?.start(first.text,[])})).toBe(true)
 secondRead.resolve(b)
 const second=await secondSend
 expect(store.perform(chat.id,active=>{second.beforeSend();active.run?.start(second.text,[])})).toBe(false)
 expect(start).toHaveBeenCalledOnce()
 expect(JSON.parse(start.mock.calls[0]![0] as string)).toMatchObject({revision:a.revision,candidate:a.document.candidate})
 const current=store.getSnapshot().chats.find(row=>row.id===chat.id)!
 expect(current.policyRequest).toEqual({revision:a.revision,candidate})
 // A later fresh read reports drift while preserving A as the active turn's
 // proposal basis, rather than silently substituting the abandoned Send B.
 vi.mocked(readPolicy).mockResolvedValueOnce(b)
 const tool=policyChatTools({id,request:()=>store.getSnapshot().chats.find(row=>row.id===chat.id)?.policyRequest,afterTurnId:()=>undefined,retain:vi.fn()})[0]!
 const result=await tool.execute({},new AbortController().signal)
 expect(JSON.parse(result.content![0]!.text!)).toMatchObject({revision:b.revision,requestRevision:a.revision})
 client.clear()
})
it('prepares current external ownership and reviewed status without treating it as local approval or adding a mutation tool',async()=>{
 const id='policy_'+'a'.repeat(48),candidate={title:'External policy',narrative:'Working draft text',artifactVersion:'1.0.0'}
 const old={revision:'a'.repeat(64),document:{id,candidate,managementMode:'desk-managed',reviewState:'approved'}} as PolicyReply
 const current={revision:'b'.repeat(64),document:{...old.document,managementMode:'externally-managed',reviewState:'unreviewed'},sourceAuthority:{revision:'authority-current',ownership:{mode:'externally-managed',inherited:false},externalVersionCount:1,lastApplied:{id:'external-one',status:'published',provenance:'admin-configured',configurationId:'configuration-one',observation:{id:'observation-one',sourceRevision:'provider-7'}}}} as PolicyReply
 fixture.initial=old;const store=new ChatStore('/project',{read:async()=>({project:'/project',sha256:'absent',content:{version:1,chats:[]}}),write:async content=>({project:'/project',sha256:'saved',content})},memoryDraftPersistence('/project'));fixture.store=store;await store.load()
 const chat=store.startChat(undefined,undefined,false,undefined,undefined,undefined,{id}),client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const hook=renderHook(()=>usePolicyChatContext(id,chat.id),{wrapper:({children})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})
 await waitFor(()=>expect(hook.result.current?.busy).toBe(false));vi.mocked(readPolicy).mockResolvedValueOnce(current)
 const prepared=await hook.result.current!.prepare(),context=JSON.parse(prepared.text)
 expect(context).toMatchObject({revision:current.revision,managementMode:'externally-managed',authorityRevision:'authority-current',reviewState:'unreviewed',sourceAuthority:{externalVersionCount:1,lastReviewed:{status:'published',provenance:'admin-configured',observationId:'observation-one',configurationId:'configuration-one'}}})
 expect(context.sourceStatusInstructions).toContain('manual reviewed actions');expect(context.sourceStatusInstructions).toContain('not external verified approval')
 expect(store.getSnapshot().drafts[0]?.policyRequest).toBeUndefined();prepared.beforeSend()
 expect([...store.getSnapshot().chats,...store.getSnapshot().drafts].find(row=>row.id===chat.id)?.policyRequest).toEqual({revision:current.revision,candidate})
 expect(policyChatTools({id,request:()=>undefined,afterTurnId:()=>undefined,retain:vi.fn()}).map(tool=>tool.name)).not.toContain('configure_policy_source_status')
 client.clear()
})
