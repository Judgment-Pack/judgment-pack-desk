import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {createMemoryRouter,RouterProvider} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {UnsavedChangesProvider} from '../shell/UnsavedChanges'
import {PolicyEditor} from './PolicyPage'
import {ChatStore,type Chat} from '../chat/store'
import {type PolicyReply} from './client'
const context=vi.hoisted(()=>({store:null as ChatStore|null,reveal:vi.fn()}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:context.store,ready:!!context.store})}))
vi.mock('../shell/InspectorSlot',()=>({useInspectorControls:()=>({reveal:context.reveal})}))
vi.mock('./PolicyAssistant',()=>({PolicyAssistant:()=>null}))
vi.mock('./PolicySourceStatus',()=>({PolicySourceStatus:()=>null}))
vi.mock('./PolicyConnectedSource',()=>({PolicyConnectedSource:({newerPolicyState}:{newerPolicyState:boolean})=><span data-testid="source-currentness" data-newer={newerPolicyState}/>}))
vi.mock('./PolicyHistory',()=>({PolicyHistory:({newerPolicyState}:{newerPolicyState:boolean})=><span data-testid="history-currentness" data-newer={newerPolicyState}/>}))
vi.mock('./PolicyLifecycle',()=>({PolicyLifecycle:()=>null}))
vi.mock('./PolicyInterpretation',()=>({PolicyInterpretationAction:()=>null}))
vi.mock('./PolicyComparison',()=>({PolicyComparison:({request,onDiscuss}:{request:unknown;onDiscuss:(request:unknown)=>void})=><button onClick={()=>onDiscuss(request)}>Discuss changes</button>}))
afterEach(()=>{cleanup();vi.unstubAllGlobals();context.store=null;context.reveal.mockReset()})
const id='policy_'+'1'.repeat(48),original='revision_'+'2'.repeat(48),later='revision_'+'3'.repeat(48)
const at='2026-10-17T12:00:00Z',actor={kind:'local',subject:'admin'}
function policy(revision:string):PolicyReply{
 const candidate={title:'Policy',artifactVersion:'1.0.0',narrative:'Saved '+revision,revisionId:revision,digest:'digest',content:{sha256:'sha',bytes:100}},workflow={independentReviewRequired:false}
 return {revision,document:{id,candidate,workflow,workflowDigest:'workflow',createdAt:at,updatedAt:at,managementMode:'desk-managed',reviewState:'unreviewed',revisions:[{id:revision,sequence:1,title:'Policy',artifactVersion:'1.0.0',digest:'digest',content:candidate.content,workflow,workflowDigest:'workflow',at,actor}],reviews:[],publications:[]}}
}
function mount(composer:string){
 const chat={id:'existing-chat',policy:{id},composer,updatedAt:at,archived:false,interaction:'plan',checkpoint:{state:{turns:[{id:'prior-turn',role:'user',text:'Prior question'}]}}} as unknown as Chat
 const update=vi.fn((_id:string,patch:Partial<Chat>)=>Object.assign(chat,patch)),startChat=vi.fn(),checkpoint=vi.fn().mockResolvedValue(undefined),activate=vi.fn()
 const store={getSnapshot:()=>({chats:[chat],drafts:[],ready:true}),canCreate:true,update,startChat,checkpoint,activate} as unknown as ChatStore
 context.store=store
 vi.stubGlobal('fetch',vi.fn(async()=>Response.json({revision:'source-none',supported:false,pending:false})))
 const router=createMemoryRouter([{path:'/policies/:policyId',element:<UnsavedChangesProvider><PolicyEditor initial={policy(original)} observed={policy(later)}/></UnsavedChangesProvider>}],{initialEntries:['/policies/'+id+'?chat=existing-chat']})
 render(<QueryClientProvider client={testQueryClient()}><RouterProvider router={router}/></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'Compare saved revisions'}))
 return {chat,router,update,startChat,checkpoint,activate}
}
it('confirms appending to an existing composer, opens the same linked pane and leaves the request unsent',async()=>{
 const app=mount('My existing composed message')
 fireEvent.click(screen.getByRole('button',{name:'Discuss changes'}))
 expect(await screen.findByRole('dialog',{name:'Append comparison request?'})).toBeTruthy()
 expect(app.chat.composer).toBe('My existing composed message');expect(app.update).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Append request'}))
 await waitFor(()=>expect(app.checkpoint).toHaveBeenCalledTimes(1))
 expect(screen.getByTestId('source-currentness').getAttribute('data-newer')).toBe('true');expect(screen.getByTestId('history-currentness').getAttribute('data-newer')).toBe('true')
 expect(app.chat.composer.startsWith('My existing composed message\n\n')).toBe(true)
 expect(app.chat.composer).toContain(original);expect(app.chat.composer).toContain(later)
 expect(app.chat.checkpoint?.state.turns).toEqual([{id:'prior-turn',role:'user',text:'Prior question'}]);expect(app.chat.interaction).toBe('plan')
 expect(app.startChat).not.toHaveBeenCalled();expect(app.activate).toHaveBeenCalledWith('existing-chat');expect(context.reveal).toHaveBeenCalledTimes(1)
 expect(app.router.state.location.search).toBe('?chat=existing-chat')
 expect(screen.getByRole('textbox',{name:'Policy text'})).toHaveProperty('value','Saved '+original)
})
it('can cancel append without changing the composer or opening the pane',async()=>{
 const app=mount('My existing composed message')
 fireEvent.click(screen.getByRole('button',{name:'Discuss changes'}));await screen.findByRole('dialog',{name:'Append comparison request?'})
 fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 expect(app.chat.composer).toBe('My existing composed message');expect(app.update).not.toHaveBeenCalled();expect(context.reveal).not.toHaveBeenCalled()
})
it('prefills an empty existing composer without sending, creating a chat, or changing Plan',async()=>{
 const app=mount('')
 fireEvent.click(screen.getByRole('button',{name:'Discuss changes'}))
 await waitFor(()=>expect(app.checkpoint).toHaveBeenCalledTimes(1))
 expect(screen.queryByRole('dialog',{name:'Append comparison request?'})).toBeNull()
 expect(app.chat.composer).toContain('read_policy_comparison');expect(app.chat.interaction).toBe('plan')
 expect(app.chat.checkpoint?.state.turns).toHaveLength(1);expect(app.startChat).not.toHaveBeenCalled()
})
