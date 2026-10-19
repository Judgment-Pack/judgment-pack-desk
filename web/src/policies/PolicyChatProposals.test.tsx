import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {PolicyChatProposals} from './PolicyChatProposals'
import {usePublishPolicyEditing} from './chatEditing'
import {INITIAL_STATE} from '../research/run'
import {deskFetch} from '../files/client'
import {actionApprovalAPI} from '../assistant/approvals/client'
import {readPolicy,recoverPolicyOperation} from './client'
import type {PolicyProposal} from './chat'
const shared=vi.hoisted(()=>({snapshot:{} as any,listeners:new Set<()=>void>(),store:{} as any,approval:{} as any}))
vi.mock('../chat/ChatProvider',async()=>{const {useSyncExternalStore}=await import('react');return {useChats:()=>({store:shared.store,...useSyncExternalStore(listener=>{shared.listeners.add(listener);return()=>{shared.listeners.delete(listener)}},()=>shared.snapshot)})}})
vi.mock('../assistant/approvals/client',async original=>({...await original<typeof import('../assistant/approvals/client')>(),useActionApproval:()=>shared.approval,actionApprovalAPI:vi.fn()}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),readPolicy:vi.fn(),recoverPolicyOperation:vi.fn()}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),deskFetch:vi.fn()}))
const id='policy_'+'a'.repeat(48),revision='b'.repeat(64),candidate={title:'Original',narrative:'Current saved text',artifactVersion:'1.0.0'}
const proposal:PolicyProposal={version:1,id:'proposal',policyId:id,revision,before:candidate,candidate:{...candidate,title:'Updated'},createdAt:'2026-10-18T00:00:00Z',operationId:'operation-policy-one',check:{scope:'draft-shape-and-revision',status:'passed'}}
const result={delivery:'committed',receipt:{},policy:{revision:'c'.repeat(64),document:{id,candidate:{...proposal.candidate,digest:'d'.repeat(64)}}}}
beforeEach(()=>{
 shared.snapshot={chats:[{id:'chat-one',interaction:'work',policy:{id},policyProposals:[structuredClone(proposal)]}],drafts:[],bindings:new Map([['chat-one',{state:{...INITIAL_STATE,status:'complete'}}]])}
 shared.approval={isError:false,data:{mode:'manual'}}
 shared.store={getSnapshot:()=>shared.snapshot,checkpoint:vi.fn(async()=>{}),update:vi.fn((chatId:string,patch:unknown)=>{shared.snapshot={...shared.snapshot,chats:shared.snapshot.chats.map((chat:any)=>chat.id===chatId?{...chat,...patch as object}:chat)};for(const listener of shared.listeners)listener()})}
 vi.mocked(readPolicy).mockResolvedValue({revision,document:{id,candidate}} as any)
 vi.mocked(deskFetch).mockImplementation(async()=>{expect(shared.snapshot.chats[0].policyProposals[0].save.state).toBe('pending');return Response.json(result)})
 vi.mocked(recoverPolicyOperation).mockResolvedValue(result as any)
 vi.mocked(actionApprovalAPI).mockImplementation(async(_chat,operation)=>{if(operation==='grant'){const grant={id:'grant-one',action:'policy-update',target:proposal.id,path:'policy:'+id,revision,state:'ready',expiresAt:new Date(Date.now()+900000).toISOString()};shared.approval={isError:false,data:{mode:'auto',grant}};return shared.approval.data}return {mode:'manual'}})
})
afterEach(()=>{cleanup();shared.listeners.clear();vi.resetAllMocks()})
function View({dirty=false}:{dirty?:boolean}){usePublishPolicyEditing(id,dirty);return <PolicyChatProposals chatId="chat-one"/>}
function show(dirty=false){return render(<QueryClientProvider client={new QueryClient()}><MemoryRouter><View dirty={dirty}/></MemoryRouter></QueryClientProvider>)}
it('Ask me presents the exact diff and saves only after explicit confirmation, with a retained pending operation before dispatch',async()=>{
 show();fireEvent.click(screen.getByRole('button',{name:'Review and save draft'}));expect(deskFetch).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Save draft update'}))
 await screen.findByText(/Policy draft saved/)
 expect(deskFetch).toHaveBeenCalledOnce()
 const [path,request]=vi.mocked(deskFetch).mock.calls[0]!
 expect(path).toBe('/api/assistant-approvals/chat-one/apply')
 expect(JSON.parse(request!.body as string)).toMatchObject({action:'policy-update',target:proposal.id,reviewed:true,revision,content:JSON.stringify({operationId:proposal.operationId,baseRevision:revision,...proposal.candidate})})
})
it('Auto grants one exact retained draft update through the shared dispatcher',async()=>{
 show();fireEvent.click(screen.getByRole('button',{name:'Auto within limits…'}));expect(actionApprovalAPI).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Allow one draft update'}))
 await screen.findByText(/Policy draft saved/)
 expect(actionApprovalAPI).toHaveBeenCalledWith('chat-one','grant',{action:'policy-update',target:proposal.id,revision})
 expect(vi.mocked(deskFetch).mock.calls[0]![0]).toBe('/api/assistant-approvals/chat-one/commit')
})
it('blocks dirty editors and refuses a changed saved revision without discarding the proposal',async()=>{
 const view=show(true);expect((screen.getByRole('button',{name:'Review and save draft'}) as HTMLButtonElement).disabled).toBe(true);expect(deskFetch).not.toHaveBeenCalled();view.unmount()
 vi.mocked(readPolicy).mockResolvedValue({revision:'e'.repeat(64),document:{id,candidate}} as any)
 show();fireEvent.click(screen.getByRole('button',{name:'Review and save draft'}));fireEvent.click(screen.getByRole('button',{name:'Save draft update'}))
 await screen.findAllByText(/The saved Policy changed/)
 expect(deskFetch).not.toHaveBeenCalled();expect(shared.snapshot.chats[0].policyProposals[0].candidate).toEqual(proposal.candidate)
})
it('restores an uncertain proposal and recovers its exact receipt without replaying a mutation',async()=>{
 shared.snapshot.chats[0].policyProposals[0].save={state:'uncertain'}
 show();fireEvent.click(screen.getByRole('button',{name:'Recover exact save result'}))
 await waitFor(()=>expect(recoverPolicyOperation).toHaveBeenCalledWith(proposal.operationId))
 await screen.findByText(/Policy draft saved/)
 expect(deskFetch).not.toHaveBeenCalled();expect(actionApprovalAPI).not.toHaveBeenCalled()
})
it('admits one Auto save when pane and Related work render the same proposal concurrently',async()=>{
 shared.approval={isError:false,data:{mode:'auto',grant:{id:'grant-one',action:'policy-update',target:proposal.id,path:'policy:'+id,revision,state:'ready',expiresAt:new Date(Date.now()+900000).toISOString()}}}
 show();show()
 await screen.findAllByText(/Policy draft saved/)
 expect(deskFetch).toHaveBeenCalledOnce()
 expect(shared.snapshot.chats[0].policyProposals[0].save.state).toBe('complete')
})
