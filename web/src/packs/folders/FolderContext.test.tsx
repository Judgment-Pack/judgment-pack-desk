import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {RouterProvider,createMemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {deskFetch} from '../../files/client'
import {testQueryClient} from '../../testing/harness'
import {PackFoldersProvider,usePackFolders} from './FolderContext'
import {applyFolderAction as apply,defaultFolders,HOME_FOLDER} from './model'
import {loadFolders,saveFolderAction} from './client'
const state=vi.hoisted(()=>({records:[] as import('../../graphs/drafts').GraphDraft[]}))
vi.mock('../../chat/ChatProvider',()=>({useChats:()=>({packDrafts:[],deletedDrafts:[],chats:[],drafts:[],ready:true})}))
vi.mock('../../files/queries',()=>({useFileListing:()=>({data:{root:'/project'}})}))
vi.mock('../../files/client',async original=>({...await original(),deskFetch:vi.fn()}))
vi.mock('./client',async original=>({...await original(),loadFolders:vi.fn(),saveFolderAction:vi.fn()}))
vi.mock('../../shell/useDirtyGuard',()=>({useDirtyGuard:()=>vi.fn()}))
afterEach(()=>{cleanup();vi.clearAllMocks();localStorage.clear()})
function Probe(){const state=usePackFolders()!;return <><output aria-label="Active folder">{state.activeFolder}</output><output aria-label="Assignments">{JSON.stringify(state.document.assignments)}</output><button onClick={()=>state.mutation.mutate({action:{type:'assign',packId:'graph-draft:origin-one:same-draft',folderId:'two'}})}>Move first draft</button></>}
it('resolves draft-owner folder routes independently of the selected helper chat and writes scoped placement',async()=>{
 const initial={draftId:'same-draft',id:'flow',path:'flow.graph.json',content:'{}',createdAt:'2026-10-18T12:00:00Z'}
 state.records=[{...initial,originChatId:'origin-one'},{...initial,originChatId:'origin-two'}]
 let document=apply(apply(defaultFolders(),{type:'create',id:'one',name:'One',parentId:null}),{type:'create',id:'two',name:'Two',parentId:null})
 document=apply(apply(document,{type:'assign',packId:'graph-draft:origin-one:same-draft',folderId:'one'}),{type:'assign',packId:'graph-draft:origin-two:same-draft',folderId:'two'})
 vi.mocked(loadFolders).mockImplementation(async()=>({document,digest:'observed'}))
 vi.mocked(saveFolderAction).mockImplementation(async(before,action)=>({document:document=apply(before.document,action),digest:'saved'}))
 vi.mocked(deskFetch).mockResolvedValue(Response.json({project:'/project',sha256:'graphs',content:{version:1,drafts:state.records,deleted:[]}}))
 const router=createMemoryRouter([{path:'/graphs',element:<PackFoldersProvider><Probe/></PackFoldersProvider>}],{initialEntries:['/graphs?chat=origin-one&draft=same-draft']})
 render(<QueryClientProvider client={testQueryClient()}><RouterProvider router={router}/></QueryClientProvider>)
 await waitFor(()=>expect(screen.getByLabelText('Active folder').textContent).toBe('one'))
 await act(()=>router.navigate('/graphs?chat=helper&draftChat=origin-two&draft=same-draft'))
 expect(screen.getByLabelText('Active folder').textContent).toBe('two')
 fireEvent.click(screen.getByText('Move first draft'));await waitFor(()=>expect(saveFolderAction).toHaveBeenCalledOnce())
 expect(document.version).toBe(3);expect(document.assignments).toEqual({'graph-draft:origin-one:same-draft':'two','graph-draft:origin-two:same-draft':'two'})
 await act(()=>router.navigate('/graphs?chat=unknown&draft=same-draft'));expect(screen.getByLabelText('Active folder').textContent).toBe(HOME_FOLDER)
})
