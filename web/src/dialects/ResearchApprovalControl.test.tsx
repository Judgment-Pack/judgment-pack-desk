import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {ResearchApprovalControl} from './ResearchApprovalControl'
import {dialectAPI,type PreparationReply} from './client'
import {deskFetch} from '../files/client'
import {usePublishDialectEditing} from './recipeEditing'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),deskFetch:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const id='prep_'+'a'.repeat(32)
const saved:PreparationReply={sha256:'a'.repeat(64),document:{version:1,id,name:'Companies',recipeVersion:2,createdAt:'2026-01-01',updatedAt:'2026-01-01',recipe:{intent:'Find companies',research:{version:1,target:'companies',fields:[{name:'country',type:'string',description:'Country'}],criteria:[],ai:{connection:'ai-1',revision:'rev',model:'model'},limits:{records:10,searches:10,pages:20,modelCalls:40}}},records:[],imports:[],activity:[]}}
const run={version:1,id:'research_'+'a'.repeat(32),preparationId:id,recipeVersion:2,recipe:saved.document.recipe.research,intent:saved.document.recipe.intent,records:[],sources:[],attempts:[],companies:[],state:'review'}
function show(children:React.ReactNode){const cache=new QueryClient({defaultOptions:{queries:{retry:false}}});return render(<QueryClientProvider client={cache}><Tooltip.Provider>{children}</Tooltip.Provider></QueryClientProvider>)}
function serve(initial:any={mode:'manual'}){
 let status=initial
 vi.mocked(deskFetch).mockImplementation(async(url)=>new Response(JSON.stringify(String(url).includes('/api/input-research-approvals/')?status:{}),{headers:{'Content-Type':'application/json'}}))
 vi.mocked(dialectAPI).mockImplementation(async(path='',method)=>{
  if(path.endsWith('/authorize')){status={mode:'auto',grant:{name:'Companies',runId:run.id,preparationId:id,recipeVersion:2,limits:run.recipe!.limits,state:'ready',approvedAt:'2026-10-13T00:00:00Z',expiresAt:'2026-10-13T01:00:00Z'}};return {mode:'auto'} as any}
  if(path.endsWith('/research')&&method==='POST')return run as any
  return saved as any
 })
}
async function openAuto(){const trigger=await screen.findByRole('button',{name:'Approvals'});await waitFor(()=>expect((trigger as HTMLButtonElement).disabled).toBe(false));fireEvent.pointerDown(trigger,{button:0,ctrlKey:false});fireEvent.click(await screen.findByRole('menuitem',{name:/Auto within limits/}))}
it('keeps Ask me until explicit approval and cancellation makes no grant or start',async()=>{
 serve();show(<ResearchApprovalControl chatId="chat-1" inputId={id}/>);await openAuto()
 const dialog=await screen.findByRole('dialog',{name:'Allow automatic research'})
 await within(dialog).findByText('Up to 10 records, 10 searches, 20 page reads and 40 model requests.')
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>path?.endsWith('/authorize')||path?.endsWith('/confirm'))).toBe(false)
 fireEvent.click(within(dialog).getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(screen.getByRole('button',{name:'Approvals'}).textContent).toBe('Ask me')
})
it('authorizes only the reviewed run and duration without confirming or executing it',async()=>{
 serve();show(<ResearchApprovalControl chatId="chat-1" inputId={id}/>);await openAuto()
 const allow=await screen.findByRole('button',{name:'Allow one run'});await waitFor(()=>expect((allow as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(allow);fireEvent.click(allow)
 await waitFor(()=>expect(screen.getByRole('button',{name:'Approvals'}).textContent).toBe('Auto'))
 expect(vi.mocked(dialectAPI).mock.calls.filter(([path])=>path?.endsWith('/authorize'))).toEqual([['/'+id+'/research/'+run.id+'/authorize','POST',{chatId:'chat-1',minutes:60},saved.sha256]])
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>path?.endsWith('/confirm'))).toBe(false)
})
it('refuses a stale review and retains Ask me',async()=>{
 serve();const implementation=vi.mocked(dialectAPI).getMockImplementation()!
 vi.mocked(dialectAPI).mockImplementation(async(...args)=>{if(args[0]?.endsWith('/authorize'))throw Error('Dialects changed after this review.');return implementation(...args)})
 show(<ResearchApprovalControl chatId="chat-1" inputId={id}/>);await openAuto()
 const allow=await screen.findByRole('button',{name:'Allow one run'});await waitFor(()=>expect((allow as HTMLButtonElement).disabled).toBe(false));fireEvent.click(allow)
 await screen.findByText('Dialects changed after this review.')
 expect((allow as HTMLButtonElement).disabled).toBe(true)
})
it('does not open or grant Auto over unsaved recipe edits',async()=>{
 serve();function Dirty(){usePublishDialectEditing(id,true);return <ResearchApprovalControl chatId="chat-1" inputId={id}/>}
 show(<Dirty/>);const trigger=await screen.findByRole('button',{name:'Approvals'});await waitFor(()=>expect((trigger as HTMLButtonElement).disabled).toBe(false));fireEvent.pointerDown(trigger,{button:0,ctrlKey:false})
 expect((await screen.findByRole('menuitem',{name:/Auto within limits/})).getAttribute('data-disabled')).not.toBeNull()
 expect(dialectAPI).not.toHaveBeenCalled()
})
