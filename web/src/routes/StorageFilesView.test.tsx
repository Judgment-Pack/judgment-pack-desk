import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { UnsavedChangesProvider } from '../shell/UnsavedChanges'
import { StorageFiles } from './StorageFilesView'
import { ConnectionRequestError, authorizeDrive } from '../connections/client'
import { decodeText, encodeBytes, encodeEditedText } from '../connections/storage'
const mocks=vi.hoisted(()=>({call:vi.fn(),authorize:vi.fn()}))
vi.mock('../connections/client',async()=>({...await vi.importActual('../connections/client'),connectionCall:mocks.call,authorizeDrive:mocks.authorize}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({desk:{localGateway:{status:'ready'}}})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>({loading:false,entries:['obsidian','google-drive'].map(id=>({descriptor:{id,operations:['files-list']},status:{data:{state:'connected'}}}))})}))
const file={context:'scope-1',id:'notes/policy.txt',name:'policy.txt',kind:'file',sizeBytes:8,revision:'r1',mediaType:'text/plain',editable:true,deletable:true}
const plan={id:'a'.repeat(64),action:'delete',target:file.id,name:file.name,revision:'r1',sizeBytes:0,state:'prepared',expires:'2099-01-01T00:00:00Z',confirmation:'policy.txt',effect:'trash'}
const open=()=>render(<MemoryRouter><UnsavedChangesProvider><StorageFiles provider="obsidian"/></UnsavedChangesProvider></MemoryRouter>)
beforeEach(()=>{document.title="Desk";sessionStorage.clear();mocks.call.mockReset();mocks.call.mockImplementation(async(method)=>{
 if(method==='files-list')return{context:'scope-1',items:[file],scope:'vault',searchMode:'names',truncated:false}
 if(method==='files-read')return{file,contentBase64:btoa('original')}
 if(method==='files-prepare')return plan
 if(method==='files-commit')return{...plan,state:'completed'}
 throw new Error('Unexpected method '+method)
})})
afterEach(cleanup)
it('browses metadata without reading contents and requires exact typed deletion consent',async()=>{
 open();fireEvent.click(await screen.findByRole('button',{name:/policy.txt/}))
 await screen.findByRole('textbox',{name:'File contents'})
 fireEvent.click(screen.getByRole('button',{name:'Delete file'}))
 const dialog=await screen.findByRole('dialog',{name:'Delete file'})
 const confirm=within(dialog).getByRole('button',{name:'Delete file'})
 expect(confirm.hasAttribute('disabled')).toBe(true)
 expect(mocks.call.mock.calls.filter(c=>c[0]==='files-commit')).toHaveLength(0)
 fireEvent.change(within(dialog).getByRole('textbox'),{target:{value:'Yes'}})
 expect(confirm.hasAttribute('disabled')).toBe(true)
 fireEvent.change(within(dialog).getByRole('textbox'),{target:{value:'policy.txt'}})
 fireEvent.click(confirm)
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledWith('files-commit',{id:plan.id,confirmation:'policy.txt'},undefined,'obsidian'))
 await screen.findByText('Change completed.')
 expect(mocks.call.mock.calls.filter(c=>c[0]==='files-read')).toHaveLength(1)
})
it('marks edits dirty, sends the loaded revision, and checks uncertain writes without repeating them',async()=>{
 mocks.call.mockImplementation(async(method)=>{
  if(method==='files-list')return{context:'scope-1',items:[file],truncated:false}
  if(method==='files-read')return{file,contentBase64:btoa('original')}
  if(method==='files-prepare')return{...plan,action:'update',confirmation:'',effect:'write'}
  if(method==='files-commit')throw new Error('Disconnected')
  if(method==='files-status')return{...plan,action:'update',state:'needs-attention'}
 })
 open();fireEvent.click(await screen.findByRole('button',{name:/policy.txt/}))
 fireEvent.change(await screen.findByRole('textbox',{name:'File contents'}),{target:{value:'updated résumé'}})
 await waitFor(()=>expect(document.title.startsWith('* ')).toBe(true))
 fireEvent.click(screen.getByRole('button',{name:'Review change'}))
 const dialog=await screen.findByRole('dialog',{name:'Review change'})
 expect(mocks.call).toHaveBeenCalledWith('files-prepare',expect.objectContaining({id:file.id,revision:'r1',context:'scope-1',contentBase64:encodeBytes(new TextEncoder().encode('updated résumé'))}),undefined,'obsidian')
 fireEvent.click(within(dialog).getByRole('button',{name:'Save'}))
 fireEvent.click(await within(dialog).findByRole('button',{name:'Check status'}))
 await waitFor(()=>expect(mocks.call.mock.calls.filter(c=>c[0]==='files-status')).toHaveLength(1))
 expect(mocks.call.mock.calls.filter(c=>c[0]==='files-commit')).toHaveLength(1)
})
it('loads one page at a time and never downloads matching contents while searching',async()=>{
 mocks.call.mockImplementation(async()=>({context:'scope-1',items:[],nextPageToken:'cursor',truncated:false}))
 open();await screen.findByText('No files on this page.')
 expect(mocks.call).toHaveBeenCalledTimes(1)
 fireEvent.change(screen.getByRole('textbox',{name:'Search files'}),{target:{value:'policy'}})
 fireEvent.click(screen.getByRole('button',{name:'Search'}))
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledTimes(2))
 fireEvent.click(screen.getByRole('button',{name:'Next page'}))
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledWith('files-list',{folder:'',query:'policy',pageToken:'cursor'},undefined,'obsidian'))
 expect(mocks.call.mock.calls.every(c=>c[0]==='files-list')).toBe(true)
})
it('roundtrips Unicode and refuses to coerce binary content into text',()=>{
 expect(decodeText(encodeBytes(new TextEncoder().encode('明細 résumé')))).toBe('明細 résumé')
 expect(decodeText(btoa('\0binary'))).toBeUndefined()
 expect(decodeText(btoa('\xff\xfe'))).toBeUndefined()
})
it('keeps a new file bound to its original destination when browsing another folder',async()=>{
 open();await screen.findByRole('button',{name:/policy.txt/})
 fireEvent.change(screen.getByRole('textbox',{name:'Folder'}),{target:{value:'first'}})
 fireEvent.click(screen.getByRole('button',{name:'Search'}))
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledWith('files-list',{folder:'first',query:'',pageToken:''},undefined,'obsidian'))
 fireEvent.click(screen.getByRole('button',{name:'New file'}))
 fireEvent.change(await screen.findByRole('textbox',{name:'File name'}),{target:{value:'new.txt'}})
 fireEvent.change(screen.getByRole('textbox',{name:'File contents'}),{target:{value:'new content'}})
 fireEvent.change(screen.getByRole('textbox',{name:'Folder'}),{target:{value:'second'}})
 fireEvent.click(screen.getByRole('button',{name:'Search'}))
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledWith('files-list',{folder:'second',query:'',pageToken:''},undefined,'obsidian'))
 fireEvent.click(screen.getByRole('button',{name:'Review change'}))
 await screen.findByRole('dialog')
 expect(mocks.call).toHaveBeenCalledWith('files-prepare',expect.objectContaining({action:'create',folder:'first',name:'new.txt',context:'scope-1'}),undefined,'obsidian')
 expect(mocks.call.mock.calls.some(c=>c[0]==='files-commit')).toBe(false)
})
it('does not silently rebase an open file after the connection changes',async()=>{
 open();fireEvent.click(await screen.findByRole('button',{name:/policy.txt/}))
 await screen.findByRole('textbox',{name:'File contents'})
 mocks.call.mockImplementation(async method=>{if(method==='files-list')return{context:'scope-2',items:[{...file,context:'scope-2'}],truncated:false};throw new Error('Unexpected new read')})
 fireEvent.click(screen.getByRole('button',{name:'Reload'}))
 await screen.findByRole('alert')
 expect((screen.getByRole('textbox',{name:'File contents'}) as HTMLTextAreaElement).value).toBe('original')
 expect(mocks.call.mock.calls.filter(c=>c[0]==='files-read')).toHaveLength(1)
})
it('retains an uncertain plan ID across a failed recovery status call',async()=>{
 sessionStorage.setItem('jpack.storage-plan.v1:default:obsidian',plan.id)
 mocks.call.mockImplementation(async method=>{
  if(method==='files-list')return{context:'scope-1',items:[],truncated:false}
  if(method==='files-status')throw new Error('Temporarily unavailable')
  throw new Error('Unexpected mutation')
 })
 open();const dialog=await screen.findByRole('dialog')
 await waitFor(()=>expect(within(dialog).getByRole('button',{name:'Check status'}).hasAttribute('disabled')).toBe(false))
 expect(sessionStorage.getItem('jpack.storage-plan.v1:default:obsidian')).toBe(plan.id)
 expect(mocks.call.mock.calls.some(c=>c[0]==='files-commit')).toBe(false)
})

it('opens a Drive file through a safe navigation link without using it as an API endpoint',async()=>{
 render(<MemoryRouter><UnsavedChangesProvider><StorageFiles provider="google-drive"/></UnsavedChangesProvider></MemoryRouter>)
 fireEvent.click(await screen.findByRole('button',{name:/policy.txt/}))
 const link=await screen.findByRole('link',{name:'Open original source'})
 expect(link.getAttribute('href')).toBe('https://drive.google.com/open?id=notes%2Fpolicy.txt')
 expect(link.getAttribute('target')).toBe('_blank')
 expect(link.getAttribute('rel')).toBe('noopener noreferrer')
 expect(mocks.call.mock.calls.every(c=>['files-list','files-read'].includes(c[0]))).toBe(true)
})

it('unlocks a completed plan recovered after a lost response and reload',async()=>{
 sessionStorage.setItem('jpack.storage-plan.v1:default:obsidian',plan.id)
 mocks.call.mockImplementation(async method=>{
  if(method==='files-list')return{context:'scope-1',items:[file],truncated:false}
  if(method==='files-status')return{...plan,state:'completed'}
  throw new Error('Unexpected method '+method)
 })
 open();await screen.findByText('Change completed.')
 await waitFor(()=>expect(screen.getByRole('button',{name:'New file'}).hasAttribute('disabled')).toBe(false))
 expect(screen.getByRole('button',{name:'New file'}).hasAttribute('disabled')).toBe(false)
 expect(sessionStorage.getItem('jpack.storage-plan.v1:default:obsidian')).toBeNull()
})

it('preserves exact UTF-8 BOM bytes for an upload without editor changes',async()=>{
 const bytes=new Uint8Array([239,187,191,104,101,108,108,111])
 const exact=encodeBytes(bytes)
 open();await screen.findByRole('button',{name:/policy.txt/})
 fireEvent.click(screen.getByRole('button',{name:'New file'}))
 await screen.findByRole('textbox',{name:'File name'})
 const upload=new File([bytes],'bom.txt',{type:'text/plain'})
 Object.defineProperty(upload,'arrayBuffer',{value:async()=>bytes.buffer})
 fireEvent.change(document.querySelector('input[type="file"]')!,{target:{files:[upload]}})
 await waitFor(()=>expect((screen.getByRole('textbox',{name:'File name'}) as HTMLInputElement).value).toBe('bom.txt'))
 fireEvent.click(screen.getByRole('button',{name:'Review change'}))
 await screen.findByRole('dialog')
 expect(mocks.call).toHaveBeenCalledWith('files-prepare',expect.objectContaining({contentBase64:exact}),undefined,'obsidian')
})

it('holds an unresolved retained plan when the first list request fails',async()=>{
 sessionStorage.setItem('jpack.storage-plan.v1:default:obsidian',plan.id)
 let lists=0
 mocks.call.mockImplementation(async method=>{
  if(method==='files-list'){if(++lists===1)throw new Error('Temporary listing failure');return{context:'scope-1',items:[file],truncated:false}}
  if(method==='files-status')return{...plan,state:'needs-attention'}
  if(method==='files-prepare')return{...plan,action:'create',confirmation:'',effect:'write'}
  throw new Error('Unexpected method '+method)
 })
 open();const dialog=await screen.findByRole('dialog')
 await within(dialog).findByRole('alert')
 fireEvent.click(within(dialog).getByRole('button',{name:'Close'}))
 fireEvent.click(screen.getByRole('button',{name:'Search'}))
 await screen.findByRole('button',{name:/policy.txt/})
 expect(screen.getByRole('button',{name:'New file'}).hasAttribute('disabled')).toBe(true)
})

it('preserves source CRLF and BOM bytes while editing text',()=>{
 const original=encodeBytes(new TextEncoder().encode('\ufeffhello\r\nworld\r\n'))
 expect(encodeEditedText('\ufeffhello\nworld\n',original)).toBe(original)
 expect(decodeText(encodeEditedText('\ufeffupdated\nworld\n',original))).toBe('\ufeffupdated\r\nworld\r\n')
})

it("does not consume another desk's recovery hint",async()=>{
 const other='jpack.storage-plan.v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:obsidian'
 sessionStorage.setItem(other,plan.id)
 open();await screen.findByRole('button',{name:/policy.txt/})
 expect(sessionStorage.getItem(other)).toBe(plan.id)
 expect(mocks.call.mock.calls.some(c=>c[0]==='files-status')).toBe(false)
})

it('offers explicit Drive reconnect after scope migration while preserving an edited buffer',async()=>{
 render(<MemoryRouter><UnsavedChangesProvider><StorageFiles provider="google-drive"/></UnsavedChangesProvider></MemoryRouter>)
 fireEvent.click(await screen.findByRole('button',{name:/policy.txt/}))
 fireEvent.change(await screen.findByRole('textbox',{name:'File contents'}),{target:{value:'unsaved text'}})
 mocks.call.mockRejectedValueOnce(new ConnectionRequestError('reconnect-required','google-drive'))
 fireEvent.click(screen.getByRole('button',{name:'Search'}))
 const reconnect=await screen.findByRole('button',{name:'Reconnect'})
 expect(authorizeDrive).not.toHaveBeenCalled()
 fireEvent.click(reconnect)
 await waitFor(()=>expect(authorizeDrive).toHaveBeenCalledWith('connect',expect.any(AbortSignal),'google-drive',undefined))
 expect((screen.getByRole('textbox',{name:'File contents'}) as HTMLTextAreaElement).value).toBe('unsaved text')
 expect(mocks.call.mock.calls.some(c=>c[0]==='disconnect'||c[0]==='files-commit')).toBe(false)
})
