import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { UnsavedChangesProvider } from '../shell/UnsavedChanges'
import { StorageFiles } from './StorageFilesView'
import { decodeText, encodeBytes } from '../connections/storage'
const mocks=vi.hoisted(()=>({call:vi.fn()}))
vi.mock('../connections/client',async()=>({...await vi.importActual('../connections/client'),connectionCall:mocks.call}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({desk:{localGateway:{status:'ready'}}})}))
vi.mock('../connections/catalog',()=>({useConnections:()=>({loading:false,entries:[{descriptor:{id:'obsidian',operations:['files-list']},status:{data:{state:'connected'}}}]})}))
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
 sessionStorage.setItem('jpack.storage-plan.v1:obsidian',plan.id)
 mocks.call.mockImplementation(async method=>{
  if(method==='files-list')return{context:'scope-1',items:[],truncated:false}
  if(method==='files-status')throw new Error('Temporarily unavailable')
  throw new Error('Unexpected mutation')
 })
 open();const dialog=await screen.findByRole('dialog')
 await waitFor(()=>expect(within(dialog).getByRole('button',{name:'Check status'}).hasAttribute('disabled')).toBe(false))
 expect(sessionStorage.getItem('jpack.storage-plan.v1:obsidian')).toBe(plan.id)
 expect(mocks.call.mock.calls.some(c=>c[0]==='files-commit')).toBe(false)
})
