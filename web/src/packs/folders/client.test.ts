import {afterEach,expect,it,vi} from 'vitest'
import {FileRequestError,readFile,writeFile} from '../../files/client'
import {assignCreatedPack,loadFolders,saveFolderAction} from './client'
import {defaultFolders,FOLDERS_FILE,HOME_FOLDER} from './model'
vi.mock('../../files/client',async original=>({...await original<typeof import('../../files/client')>(),readFile:vi.fn(),writeFile:vi.fn()}))
afterEach(()=>vi.resetAllMocks())
const missing=()=>new FileRequestError(404,'No file','chassis','not-found')
it('synthesizes home on precise absence without writing on a visit',async()=>{
 vi.mocked(readFile).mockRejectedValue(missing())
 expect(await loadFolders()).toEqual({document:defaultFolders(),digest:''})
 expect(writeFile).not.toHaveBeenCalled()
})
it('does not replace unreadable or invalid metadata with a default',async()=>{
 for(const cause of [new FileRequestError(403,'Denied','chassis','forbidden'),new FileRequestError(404,'Unknown','chassis','other'),new Error('offline')]){
  vi.mocked(readFile).mockRejectedValueOnce(cause);await expect(loadFolders()).rejects.toBe(cause)
 }
 vi.mocked(readFile).mockResolvedValueOnce({path:FOLDERS_FILE,sha256:'original',bytes:3,content:'bad'})
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
 vi.mocked(readFile).mockResolvedValue({path:FOLDERS_FILE,sha256:'fresh',bytes:200,content:JSON.stringify(defaultFolders())})
 vi.mocked(writeFile).mockImplementation(async input=>({path:input.path,bytes:input.content.length,sha256:'new',content:input.content}))
 const saved=await assignCreatedPack('new-pack',HOME_FOLDER)
 expect(saved.document.assignments['new-pack']).toBe(HOME_FOLDER)
 expect(vi.mocked(writeFile).mock.calls[0]![0].baseSha256).toBe('fresh')
 expect(readFile).toHaveBeenCalledWith(FOLDERS_FILE,undefined)
})
