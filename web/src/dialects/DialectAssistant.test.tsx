import {cleanup,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {DialectAssistant} from './DialectAssistant'
import {dialectAPI} from './client'
import {useDialectRecipeEditing} from './recipeEditing'
const fake=vi.hoisted(()=>({snapshot:{} as any,context:{} as any,locked:false}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>fake.snapshot}))
vi.mock('../chat/ChatPanel',()=>({ChatPanel:({chat,context,inputActionsLocked}:any)=>{fake.context=context;fake.locked=inputActionsLocked;return <div>{chat.id}</div>}}))
vi.mock('../shell/InspectorSlot',()=>({useInspectorControls:()=>({open:true}),useInspectorPortal:(node:any)=>node}))
vi.mock('../shell/InspectorPresentation',()=>({useInspectorPresentation:()=>{}}))
vi.mock('../builders/BuilderSplit',()=>({useBuilderPreference:()=>[true,()=>{}]}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('uses the explicitly chosen input conversation, passes selection and locks edits without sharing private settings',async()=>{
 const id='prep_'+'a'.repeat(32),activate=vi.fn()
 fake.snapshot={ready:true,store:{activate},chats:[{composer:'',title:'Old conversation',id:'old',input:{id},updatedAt:'2026-01-01'},{composer:'',title:'New conversation',id:'new',input:{id},updatedAt:'2026-10-01'},{composer:'',title:'General conversation',id:'general',input:{id:'list'},updatedAt:'2026-10-12'}],drafts:[]}
 vi.mocked(dialectAPI).mockResolvedValue({sha256:'a'.repeat(64),document:{name:'Company research',recipeVersion:3,records:[{id:'row',values:{private:'Not automatic context'}}],recipe:{intent:'Find companies',research:{target:'companies',fields:[],criteria:[],limits:{records:3},ai:{connection:'private-connection'}}}}})
 function Lock(){const editing=useDialectRecipeEditing(id);return <output aria-label="Editor lock">{String(editing)}</output>}
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemoryRouter initialEntries={['/dialects/'+id+'?tab=recipe&chat=old']}><DialectAssistant id={id} selection={['row']} tab="recipe" editing/><Lock/></MemoryRouter></QueryClientProvider>)
 expect(await screen.findByText('old')).toBeTruthy();expect(activate).toHaveBeenCalledWith('old')
 expect(screen.getByLabelText('Editor lock').textContent).toBe('true');expect(fake.locked).toBe(true)
 await vi.waitFor(()=>expect(fake.context.title).toBe('Company research'))
 const context=JSON.parse(fake.context.text)
 expect(context).toMatchObject({id,recipeVersion:3,selectedRecordIds:['row'],unsavedRecipeEdits:true,tab:'recipe'})
 expect(fake.context.text).not.toMatch(/private-connection|Not automatic context/)
})

const inputId='prep_'+'b'.repeat(32)
const sourceChat={id:'source-chat',title:'Design my recipe',composer:'',updatedAt:'2026-10-01',inputDrafts:[{savedInputId:inputId}]}
function mountAssistant(path='/dialects/'+inputId+'?tab=recipe',id=inputId){
 return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemoryRouter initialEntries={[path]}><DialectAssistant id={id} tab="recipe"/></MemoryRouter></QueryClientProvider>)
}
it('restores the main-chat recipe conversation ahead of an empty pane draft without reassigning it',async()=>{
 const activate=vi.fn(),startChat=vi.fn(),update=vi.fn()
 fake.snapshot={ready:true,store:{activate,startChat,update,canCreate:true},chats:[sourceChat],drafts:[{id:'empty',title:'New chat',composer:'',input:{id:inputId},updatedAt:'2026-10-12'}]}
 vi.mocked(dialectAPI).mockResolvedValue({document:{name:'Recipe',recipeVersion:1,records:[],recipe:{intent:''},chatOrigin:{chatId:'source-chat',draftId:'draft'}}} as any)
 mountAssistant()
 expect(await screen.findByText('source-chat')).toBeTruthy()
 expect(activate).toHaveBeenCalledWith('source-chat');expect(startChat).not.toHaveBeenCalled();expect(update).not.toHaveBeenCalled()
 expect(sourceChat).not.toHaveProperty('input')
})
it('waits for the saved recipe origin before making a new chat when the reciprocal proposal link is missing',async()=>{
 let resolve!:(value:any)=>void
 const activate=vi.fn(),startChat=vi.fn()
 fake.snapshot={ready:true,store:{activate,startChat,canCreate:true},chats:[{...sourceChat,inputDrafts:[]}],drafts:[]}
 vi.mocked(dialectAPI).mockImplementation(()=>new Promise(done=>{resolve=done}))
 mountAssistant();expect(startChat).not.toHaveBeenCalled()
 resolve({document:{name:'Recipe',recipeVersion:1,records:[],recipe:{intent:''},chatOrigin:{chatId:'source-chat',draftId:'draft'}}})
 expect(await screen.findByText('source-chat')).toBeTruthy();expect(startChat).not.toHaveBeenCalled()
})
it('includes recipe-producing main conversations in the Dialects list pane',async()=>{
 fake.snapshot={ready:true,store:{activate:vi.fn()},chats:[sourceChat],drafts:[]}
 mountAssistant('/dialects','list')
 expect(await screen.findByText('source-chat')).toBeTruthy()
 expect(dialectAPI).not.toHaveBeenCalled()
})
it('honors an explicitly selected empty New chat instead of reopening the source chat',async()=>{
 fake.snapshot={ready:true,store:{activate:vi.fn()},chats:[sourceChat],drafts:[{id:'empty',title:'New chat',composer:'',input:{id:inputId},updatedAt:'2026-10-12'}]}
 vi.mocked(dialectAPI).mockResolvedValue({document:{name:'Recipe',recipeVersion:1,records:[],recipe:{intent:''}}} as any)
 mountAssistant('/dialects/'+inputId+'?tab=recipe&chat=empty')
 expect(await screen.findByText('empty')).toBeTruthy()
})
it('does not pick an unrelated conversation or automatically reopen an archived source',async()=>{
 const startChat=vi.fn(()=>({id:'new-input',input:{id:inputId}}))
 fake.snapshot={ready:true,store:{activate:vi.fn(),startChat,canCreate:true},chats:[{...sourceChat,archived:true},{...sourceChat,id:'unrelated',inputDrafts:[]}],drafts:[]}
 vi.mocked(dialectAPI).mockResolvedValue({document:{name:'Recipe',recipeVersion:1,records:[],recipe:{intent:''},chatOrigin:{chatId:'source-chat',draftId:'draft'}}} as any)
 mountAssistant()
 await vi.waitFor(()=>expect(startChat).toHaveBeenCalledTimes(1))
 expect(screen.queryByText('source-chat')).toBeNull();expect(screen.queryByText('unrelated')).toBeNull()
})
