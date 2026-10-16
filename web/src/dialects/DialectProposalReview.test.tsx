import {checkDialect,DialectCheckError} from '../dialect/authoring'
vi.mock('../dialect/authoring',async original=>({...await original<object>(),checkDialect:vi.fn()}))
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {beforeEach,afterEach,expect,it,vi} from 'vitest'
import {DialectActionNotice} from './DialectActionArtifacts'
import {dialectAPI,type PreparationReply} from './client'
import {readFile} from '../files/client'
import {packBinding} from './qualification'
import type {DialectChatAction} from './chatActions'
const fake=vi.hoisted(()=>({update:vi.fn(),chats:[] as any[]}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({chats:fake.chats,store:{getSnapshot:()=>({chats:fake.chats}),update:fake.update}})}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),readFile:vi.fn()}))
vi.mock('../shell/UnsavedChanges',async original=>({...await original<typeof import('../shell/UnsavedChanges')>(),useConfirmDiscard:()=>()=>Promise.resolve(true)}))
const id='prep_'+'a'.repeat(32),revision='a'.repeat(64),pack=JSON.stringify({id:'fit',version:'1.0.0',title:'Company fit',decision:{},rules:[{when:{op:'fact',path:'/size',operator:'greater-than',value:'50'}}]})
const saved:PreparationReply={sha256:revision,document:{version:1,id,name:'Companies',recipe:{intent:'Keep this instruction'},recipeVersion:1,createdAt:'2026-10-14',updatedAt:'2026-10-14',records:[{id:'row_'+'b'.repeat(64),name:'Example',sourceId:'source',row:1,class:'asserted',status:'provided',values:{employees:'60'}}],imports:[],activity:[]}}
let cache:QueryClient,action:DialectChatAction,livePack:string
beforeEach(async()=>{
 vi.resetAllMocks();vi.mocked(checkDialect).mockResolvedValue({status:'complete',diagnostics:[]});cache=new QueryClient({defaultOptions:{queries:{retry:false}}});livePack=pack
 action={version:1,id:'action-1',inputId:id,name:'Companies',revision,recipeVersion:1,createdAt:'2026-10-14',kind:'update-dialect',dialect:{packKey:'fit',packDigest:(await packBinding('fit',pack)).digest,facts:[{target:'/size',column:'employees',conversion:{kind:'decimal'}}],evidence:[],summary:'Read the employee count as a decimal.'}}
 fake.chats=[{id:'chat',inputActions:[action]}]
 vi.mocked(readFile).mockImplementation(async path=>({content:path==='jpack.json'?JSON.stringify({packs:{fit:{path:'fit.pack.json'}}}):livePack}) as any)
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>method==='POST'?{rows:[{id:saved.document.records[0].id,prepared:{'/size':'60'},issues:[]}]} as any:method==='PUT'?{...saved,sha256:'c'.repeat(64)} as any:saved as any)
})
afterEach(()=>{cleanup();cache.clear()})
function show(){return render(<QueryClientProvider client={cache}><Tooltip.Provider><MemoryRouter><DialectActionNotice chatId="chat" actions={[action]}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)}
it('requires a preview and explicit Save dialect, retaining the original recipe and marking completion',async()=>{
 show();fireEvent.click(screen.getByRole('button',{name:'Review dialect'}))
 const save=await screen.findByRole('button',{name:'Save dialect'})
 expect((save as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByRole('combobox',{name:'Use size as'}).textContent).toContain('Decimal')
 expect(vi.mocked(dialectAPI).mock.calls.every(([,m])=>!m)).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Preview conversions'}))
 await waitFor(()=>expect((save as HTMLButtonElement).disabled).toBe(false))
 expect(screen.getByText('"60"')).toBeTruthy()
 fireEvent.click(save);await waitFor(()=>expect(fake.update).toHaveBeenCalledOnce())
 const put=vi.mocked(dialectAPI).mock.calls.find(([,m])=>m==='PUT')!
 expect(put[3]).toBe(revision);expect((put[2] as any).recipe.intent).toBe(saved.document.recipe.intent)
 expect((put[2] as any).recipe.qualification.conversions['/size']).toEqual({kind:'decimal'})
 expect(fake.update.mock.calls[0][1].inputActions[0].applied.revision).toBe('c'.repeat(64))
 expect(vi.mocked(dialectAPI).mock.calls.some(([p])=>/research|evaluate|jobs/.test(p??''))).toBe(false)
})
it('rechecks the live pack before saving and preserves the proposal on failure',async()=>{
 show();fireEvent.click(screen.getByRole('button',{name:'Review dialect'}))
 fireEvent.click(await screen.findByRole('button',{name:'Preview conversions'}))
 const save=screen.getByRole('button',{name:'Save dialect'});await waitFor(()=>expect((save as HTMLButtonElement).disabled).toBe(false))
 livePack=pack.replace('1.0.0','1.0.1');fireEvent.click(save)
 expect(await screen.findByText('The pack changed. Request a new dialect proposal before saving.')).toBeTruthy()
 expect(vi.mocked(dialectAPI).mock.calls.some(([,m])=>m==='PUT')).toBe(false);expect(fake.update).not.toHaveBeenCalled()
})
it('keeps a failed save open and does not mark the chat proposal saved',async()=>{
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(method==='PUT')throw Error('Could not save');return method==='POST'?{rows:[]} as any:saved as any})
 show();fireEvent.click(screen.getByRole('button',{name:'Review dialect'}))
 fireEvent.click(await screen.findByRole('button',{name:'Preview conversions'}))
 const save=screen.getByRole('button',{name:'Save dialect'});await waitFor(()=>expect((save as HTMLButtonElement).disabled).toBe(false));fireEvent.click(save)
 expect(await screen.findByText('Could not save')).toBeTruthy();expect(fake.update).not.toHaveBeenCalled()
 expect(screen.getByRole('combobox',{name:'Source for size'}).textContent).toContain('employees')
})

it('keeps saving disabled when the shared Runtime check refuses the candidate',async()=>{
 vi.mocked(checkDialect).mockRejectedValue(new DialectCheckError({status:'refused',diagnostics:[{code:'JPS-DIALECT-FORM',message:'Invalid mapping target.'}]}))
 show();fireEvent.click(screen.getByRole('button',{name:'Review dialect'}))
 fireEvent.click(await screen.findByRole('button',{name:'Preview conversions'}))
 await screen.findByText('JPS-DIALECT-FORM · Invalid mapping target.')
 expect((screen.getByRole('button',{name:'Save dialect'}) as HTMLButtonElement).disabled).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='PUT')).toHaveLength(0)
 expect(fake.update).not.toHaveBeenCalled()
})
