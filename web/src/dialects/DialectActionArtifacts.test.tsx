import {DeskNotices,DeskNoticesProvider} from '../shell/DeskNotices'
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {DialectActionNotice,DialectChatProgress} from './DialectActionArtifacts'
import {usePublishDialectEditing} from './recipeEditing'
import {dialectAPI,type PreparationReply} from './client'
import type {DialectChatAction} from './chatActions'
const fake=vi.hoisted(()=>({update:vi.fn(),chats:[] as any[]}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({chats:fake.chats,store:{getSnapshot:()=>({chats:fake.chats}),update:fake.update}})}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('./ResearchRecipe',()=>({ResearchRecipe:()=>null}))
vi.mock('../shell/UnsavedChanges',async original=>({...await original<typeof import('../shell/UnsavedChanges')>(),useConfirmDiscard:()=>()=>Promise.resolve(true)}))
const inputId='prep_'+'a'.repeat(32),revision='a'.repeat(64),runId='research_'+'b'.repeat(32)
const research={version:1 as const,target:'companies' as const,fields:[{name:'sector',type:'string' as const,description:'Sector'}],criteria:[],limits:{records:3,searches:1,pages:3,modelCalls:10},ai:{connection:'chosen',revision:'connection-revision',model:'model'}}
const saved:PreparationReply={sha256:revision,document:{version:1,id:inputId,name:'Companies',recipe:{intent:'Original',research},recipeVersion:1,createdAt:'2026-01-01',updatedAt:'2026-01-01',records:[],imports:[],activity:[]}}
const action:DialectChatAction={version:1,id:'action-1',inputId,revision,recipeVersion:1,name:'Companies',createdAt:'2026-01-01',kind:'update-recipe',patch:{intent:'Proposed'}}
let cache:QueryClient
function show(proposal=action,editing=false){
 fake.chats=[{id:'chat',inputActions:[proposal]}]
 function Lock(){usePublishDialectEditing(inputId,editing);return null}
 return <QueryClientProvider client={cache}><Tooltip.Provider><MemoryRouter><Lock/><DialectActionNotice chatId="chat" actions={[proposal]}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>
}
beforeEach(()=>{vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false}}});vi.mocked(dialectAPI).mockResolvedValue(saved)})
afterEach(()=>{cleanup();cache.clear()})
it('reviews an update and only saves the edited recipe after explicit confirmation',async()=>{
 render(show());fireEvent.click(screen.getByRole('button',{name:'Review changes'}))
 const instructions=await screen.findByRole('textbox',{name:'What records do you need?'})
 expect((instructions as HTMLTextAreaElement).value).toBe('Proposed')
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method)).toBe(true)
 fireEvent.change(instructions,{target:{value:'Reviewed instructions'}})
 fireEvent.click(screen.getByRole('button',{name:'Save recipe'}))
 await waitFor(()=>expect(fake.update).toHaveBeenCalledOnce())
 expect(dialectAPI).toHaveBeenCalledWith('/'+inputId,'PUT',{name:'Companies',recipe:{...saved.document.recipe,intent:'Reviewed instructions'}},revision)
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>path?.includes('/research'))).toBe(false)
})
it('retains edited fields when a background refresh changes the revision, and disables saving',async()=>{
 render(show());fireEvent.click(screen.getByRole('button',{name:'Review changes'}))
 const instructions=await screen.findByRole('textbox',{name:'What records do you need?'})
 fireEvent.change(instructions,{target:{value:'Do not lose this'}})
 act(()=>cache.setQueryData(['input-preparations',inputId],{...saved,sha256:'c'.repeat(64)}))
 expect((instructions as HTMLTextAreaElement).value).toBe('Do not lose this')
 await waitFor(()=>expect((screen.getByRole('button',{name:'Save recipe'}) as HTMLButtonElement).disabled).toBe(true))
 expect(screen.getByText(/Your proposed changes remain here/)).toBeTruthy()
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method)).toBe(true)
})
it('blocks a stale unopened proposal and a live unsaved Recipe editor',async()=>{
 const view=render(show(action,true))
 expect((screen.getByRole('button',{name:'Review changes'}) as HTMLButtonElement).disabled).toBe(true)
 view.rerender(show(action,false))
 vi.mocked(dialectAPI).mockResolvedValue({...saved,sha256:'c'.repeat(64)})
 fireEvent.click(screen.getByRole('button',{name:'Review changes'}))
 await screen.findByText('Dialects changed. Request a new proposal against the current recipe.')
 expect(screen.queryByRole('button',{name:'Save recipe'})).toBeNull()
})
it('leaves a failed save editable instead of marking the action completed',async()=>{
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(method==='PUT')throw Error('Save failed');return saved as any})
 render(show());fireEvent.click(screen.getByRole('button',{name:'Review changes'}))
 fireEvent.click(await screen.findByRole('button',{name:'Save recipe'}))
 await screen.findByText('Save failed');expect(fake.update).not.toHaveBeenCalled()
 expect((screen.getByRole('textbox',{name:'What records do you need?'}) as HTMLTextAreaElement).value).toBe('Proposed')
})
it('confirms removing setup with a recipe-only update, retaining records and history',async()=>{
 render(show({...action,kind:'remove-recipe',patch:undefined,section:'all'}))
 fireEvent.click(screen.getByRole('button',{name:'Review action'}))
 await screen.findByText('Existing records and earlier recipe versions will remain available. No run will be started or stopped.')
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method)).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Remove setup'}))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/'+inputId,'PUT',{name:'Companies',recipe:{intent:''}},revision))
})
it('uses the existing research review and never starts before Start research',async()=>{
 const run={id:runId,recipe:research,intent:'Original',state:'review',records:[]}
 vi.mocked(dialectAPI).mockImplementation(async(path,method)=>method==='POST'?{...run,state:path?.endsWith('/confirm')?'queued':'review'} as any:saved as any)
 render(show({...action,kind:'research',patch:undefined}))
 fireEvent.click(screen.getByRole('button',{name:'Review research'}))
 const start=await screen.findByRole('button',{name:'Start research'})
 await waitFor(()=>expect((start as HTMLButtonElement).disabled).toBe(false))
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toEqual([['/'+inputId+'/research','POST',{},revision]])
 fireEvent.click(start)
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledWith('/'+inputId+'/research/'+runId+'/confirm','POST',{}))
 expect(fake.update).toHaveBeenCalledOnce()
})

function progress(id=inputId){
 return <QueryClientProvider client={cache}><Tooltip.Provider><MemoryRouter><DeskNoticesProvider><DialectChatProgress inputId={id} emptyFallback={<p>Welcome to Dialects chat</p>}/><footer><DeskNotices/></footer></DeskNoticesProvider></MemoryRouter></Tooltip.Provider></QueryClientProvider>
}
it('replaces the empty-chat welcome with externally started research and retains the result',async()=>{
 let resolve!:(value:any)=>void
 vi.mocked(dialectAPI).mockImplementation(()=>new Promise(done=>{resolve=done}))
 render(progress())
 // Do not flash the welcome before an existing run has loaded.
 expect(screen.queryByText('Welcome to Dialects chat')).toBeNull()
 await act(async()=>resolve({items:[]}))
 expect(await screen.findByText('Welcome to Dialects chat')).toBeTruthy()
 const run={id:runId,state:'queued',recordCount:0,stopRequested:false,updatedAt:new Date().toISOString()}
 for(const state of ['queued','running','completed','interrupted']){
  act(()=>cache.setQueryData(['input-research',inputId],{items:[{...run,state}]}))
  await waitFor(()=>expect(screen.queryByText('Welcome to Dialects chat')).toBeNull())
  expect(await screen.findByRole('button',{name:'View steps'})).toBeTruthy()
  await waitFor(()=>expect(Boolean(screen.queryByRole('button',{name:'Stop'}))).toBe(['queued','running'].includes(state)))
 }
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method)).toBe(true)
})
it('shows status recovery instead of a welcome when research status is unavailable',async()=>{
 vi.mocked(dialectAPI).mockRejectedValue(new Error('Unavailable'))
 render(progress())
 fireEvent.click(await screen.findByRole('button',{name:'Research status unavailable'}))
 expect(await screen.findByText(/Research status could not be refreshed/)).toBeTruthy()
 expect(screen.queryByText('Welcome to Dialects chat')).toBeNull()
 expect(screen.getByRole('button',{name:'Retry'})).toBeTruthy()
})
it('keeps the general Dialects welcome without requesting a record-specific status',()=>{
 render(progress('list'))
 expect(screen.getByText('Welcome to Dialects chat')).toBeTruthy()
 expect(dialectAPI).not.toHaveBeenCalled()
})

it('hides resolved actions in every placement while retaining pending review actions',()=>{
 const completed={...action,applied:{at:'2026-01-02'}}
 const view=render(<MemoryRouter><DialectActionNotice chatId="chat" actions={[action,completed]}/></MemoryRouter>)
 expect(screen.queryByRole('link',{name:'Open dialect'})).toBeNull()
 expect(screen.queryByRole('button',{name:'Review changes'})).toBeNull()
 view.rerender(<MemoryRouter><DialectActionNotice chatId="chat" actions={[action]}/></MemoryRouter>)
 expect(screen.getByRole('button',{name:'Review changes'})).toBeTruthy()
})
it('leaves status and approvals to the visible Dialects page without flashing a welcome',async()=>{
 vi.mocked(dialectAPI).mockResolvedValue({items:[{id:runId,state:'paused',recordCount:3,canIncrease:true,problem:'Detailed research explanation'}]} as any)
 render(<QueryClientProvider client={cache}><DialectChatProgress inputId={inputId} presentation="hidden" emptyFallback={<p>Welcome</p>}/></QueryClientProvider>)
 await waitFor(()=>expect(cache.getQueryData(['input-research',inputId])).toBeTruthy())
 expect(screen.queryByText('Welcome')).toBeNull();expect(screen.queryByText('Detailed research explanation')).toBeNull()
 expect(screen.queryByRole('button')).toBeNull()
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method)).toBe(true)
})
it('keeps compact research status and leaves navigation to Related work',async()=>{
 vi.mocked(dialectAPI).mockResolvedValue({items:[{id:runId,state:'completed-with-gaps',recordCount:3,problem:'Long source explanation',sourceCount:9,canContinue:true}]} as any)
 render(<QueryClientProvider client={cache}><MemoryRouter><DialectChatProgress inputId={inputId} presentation="compact"/></MemoryRouter></QueryClientProvider>)
 expect(await screen.findByText('Research finished with gaps · 3 records saved')).toBeTruthy()
 expect(screen.queryByRole('link',{name:'Open dialect'})).toBeNull()
 expect(screen.queryByText('Long source explanation')).toBeNull()
 expect(screen.queryByRole('button',{name:'Continue from saved sources'})).toBeNull()
})
