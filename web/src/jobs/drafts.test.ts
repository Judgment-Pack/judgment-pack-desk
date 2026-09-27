import { beforeEach, describe, expect, it, vi } from 'vitest'
import { decodeJobDraft, saveJobDraft, loadJobDraft, type JobDraftValues } from './drafts'
import { readFile, writeFile } from '../files/client'
vi.mock('../files/client',()=>({readFile:vi.fn(),writeFile:vi.fn(),listFiles:vi.fn()}))
const values:JobDraftValues={name:'Daily review',packId:'policy',inputMode:'mapped',facts:'{',supplied:false,evidence:'{}',mapped:{text:'{"version":2}',caseText:'{}'}}
beforeEach(()=>vi.resetAllMocks())
describe('local job drafts',()=>{
 it('saves incomplete configuration with optimistic concurrency and checks read-back',async()=>{
  vi.mocked(writeFile).mockImplementation(async input=>({path:input.path,content:input.content,sha256:'next',bytes:input.content.length}))
  const saved=await saveJobDraft('example',Object.assign({},values,{source:{grant:'not-for-drafts'},preview:{releaseId:'not-approved'}}),'previous')
  expect(writeFile).toHaveBeenCalledWith(expect.objectContaining({path:'.desk/job-drafts/example.json',baseSha256:'previous',createParents:true}))
  expect(saved.draft.values).toEqual(values);expect(saved.draft.status).toBe('draft')
  expect(Object.keys(saved.draft.values)).not.toContain('source');expect(Object.keys(saved.draft.values)).not.toContain('preview')
  vi.mocked(readFile).mockResolvedValue(saved.file);expect((await loadJobDraft('example')).draft.values).toEqual(values)
 })
 it('refuses stale or unverifiable writes without pretending a draft was saved',async()=>{
  vi.mocked(writeFile).mockRejectedValueOnce(Error('stale'));await expect(saveJobDraft('example',values,'base')).rejects.toThrow('stale')
  vi.mocked(writeFile).mockResolvedValueOnce({path:'x',content:'different',sha256:'x',bytes:9});await expect(saveJobDraft('example',values)).rejects.toThrow('verified')
 })
 it('rejects path escapes and malformed draft envelopes',async()=>{
  await expect(saveJobDraft('../outside',values)).rejects.toThrow('Invalid');expect(writeFile).not.toHaveBeenCalled()
  expect(()=>decodeJobDraft('{"version":1}')).toThrow('Invalid')
  expect(()=>decodeJobDraft(JSON.stringify({version:1,id:'example',updatedAt:'now',status:'draft',values:{...values,trigger:{name:'bad',kind:'schedule'}}}))).toThrow('Invalid')
 })
})
