import {afterEach,expect,it,vi} from 'vitest'
import {FileRequestError,readOptionalFile,writeFile} from '../../files/client'
import {assignCreatedPack,loadFolders,saveFolderAction} from './client'
import {defaultFolders,FOLDERS_FILE,HOME_FOLDER} from './model'
vi.mock('../../files/client',async original=>({...await original<typeof import('../../files/client')>(),readOptionalFile:vi.fn(),writeFile:vi.fn()}))
afterEach(()=>vi.resetAllMocks())
it('synthesizes home on precise absence without writing on a visit',async()=>{
 vi.mocked(readOptionalFile).mockResolvedValue(null)
 expect(await loadFolders()).toEqual({document:defaultFolders(),digest:''})
 expect(writeFile).not.toHaveBeenCalled()
})
it('does not replace unreadable or invalid metadata with a default',async()=>{
 for(const cause of [new FileRequestError(403,'Denied','chassis','forbidden'),new FileRequestError(404,'Unknown','chassis','other'),new Error('offline')]){
  vi.mocked(readOptionalFile).mockRejectedValueOnce(cause);await expect(loadFolders()).rejects.toBe(cause)
 }
 vi.mocked(readOptionalFile).mockResolvedValueOnce({path:FOLDERS_FILE,sha256:'original',bytes:3,content:'bad'})
 await expect(loadFolders()).rejects.toThrow(/not been changed/)
 expect(writeFile).not.toHaveBeenCalled()
})
it('writes organization only, using the observed digest, and refuses a stale write once',async()=>{
 vi.mocked(writeFile).mockRejectedValue(new FileRequestError(409,'Conflict','chassis','stale'))
 await expect(saveFolderAction({document:defaultFolders(),digest:'observed'},{type:'create',id:'dept',name:'Department',parentId:HOME_FOLDER})).rejects.toThrow(/another window/)
 expect(writeFile).toHaveBeenCalledOnce()
 expect(vi.mocked(writeFile).mock.calls[0]![0]).toMatchObject({path:FOLDERS_FILE,baseSha256:'observed'})
 expect(vi.mocked(writeFile).mock.calls[0]![0]).not.toHaveProperty('override')
})
it('re-reads the latest hierarchy when assigning a newly registered pack',async()=>{
 vi.mocked(readOptionalFile).mockResolvedValue({path:FOLDERS_FILE,sha256:'fresh',bytes:200,content:JSON.stringify(defaultFolders())})
 vi.mocked(writeFile).mockImplementation(async input=>({path:input.path,bytes:input.content.length,sha256:'new',content:input.content}))
 const saved=await assignCreatedPack('new-pack',HOME_FOLDER)
 expect(saved.document.assignments['new-pack']).toBe(HOME_FOLDER)
 expect(vi.mocked(writeFile).mock.calls[0]![0].baseSha256).toBe('fresh')
 expect(readOptionalFile).toHaveBeenCalledWith(FOLDERS_FILE,undefined)
})

it('creates a folder and moves packs and graphs in one write against the observed revision',async()=>{
 const before={document:defaultFolders(),digest:'observed'}
 vi.mocked(writeFile).mockImplementation(async input=>({path:input.path,bytes:input.content.length,sha256:'new',content:input.content}))
 const saved=await saveFolderAction(before,{type:'create-and-move',id:'briefs',name:'Client briefs',parentId:null,target:{kind:'items',ids:['alpha','graph:flow','graph-draft:pending']}})
 expect(writeFile).toHaveBeenCalledOnce()
 expect(vi.mocked(writeFile).mock.calls[0]![0]).toMatchObject({path:FOLDERS_FILE,baseSha256:'observed'})
 expect(saved.document.version).toBe(2)
 expect(saved.document.assignments).toEqual({'alpha':'briefs','graph:flow':'briefs','graph-draft:pending':'briefs'})
 expect(before.document).toEqual(defaultFolders())
})

it('does not create a destination when the accompanying folder move is invalid',async()=>{
 const before={document:defaultFolders(),digest:'observed'}
 await expect(saveFolderAction(before,{type:'create-and-move',id:'briefs',name:'Briefs',parentId:null,target:{kind:'folder',id:HOME_FOLDER}})).rejects.toThrow('The home folder cannot be renamed, moved, or deleted.')
 expect(writeFile).not.toHaveBeenCalled()
 expect(before.document).toEqual(defaultFolders())
})
it('writes and reopens scoped graph folder v3 against the exact project-file revision',async()=>{
 const original=defaultFolders(),before={document:original,digest:'observed'}
 vi.mocked(writeFile).mockImplementation(async input=>({path:input.path,bytes:input.content.length,sha256:'v3',content:input.content}))
 const saved=await saveFolderAction(before,{type:'assign-many',packIds:['graph-draft:origin-one:same-draft','graph-draft:origin-two:same-draft'],folderId:HOME_FOLDER})
 expect(saved.document.version).toBe(3)
 const input=vi.mocked(writeFile).mock.calls[0]![0]
 expect(input).toMatchObject({path:FOLDERS_FILE,baseSha256:'observed'})
 vi.mocked(readOptionalFile).mockResolvedValue({path:FOLDERS_FILE,bytes:input.content.length,sha256:'v3',content:input.content})
 expect(await loadFolders()).toEqual(saved);expect(writeFile).toHaveBeenCalledOnce()
 expect(original).toEqual(defaultFolders())
})
