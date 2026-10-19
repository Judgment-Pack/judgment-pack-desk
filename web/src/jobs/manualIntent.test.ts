import {afterEach,expect,it,vi} from 'vitest'
import {webcrypto} from 'node:crypto'
import {manualIntentJournal} from './manualIntent'
import {canonicalJobsRequest,jobsAPI} from './client'
import {parseInput} from './inputModel'
const mocks=vi.hoisted(()=>({fetch:vi.fn()}))
vi.mock('../mcp/session',async original=>({...await original<typeof import('../mcp/session')>(),sessionBearer:async()=> 'current-private-session'}))
vi.mock('../desks/scope',()=>({activeDeskId:'desk-synthetic'}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),deskFetch:mocks.fetch}))
afterEach(()=>{sessionStorage.clear();vi.unstubAllGlobals();vi.resetAllMocks()})
it('retains only original key and exact transmitted-body digest across refresh and never retries',async()=>{
 vi.stubGlobal('crypto',webcrypto)
 const input=parseInput('{"large":9007199254740993.00,"flag":false,"zero":0,"nothing":null}',false,'')
 const bytes=canonicalJobsRequest(input)
 const journal=await manualIntentJournal('job_synthetic'),pending=await journal.begin(bytes)
 const again=await manualIntentJournal('job_synthetic')
 expect(again.read()).toEqual(pending)
 const raw=Object.values(sessionStorage).join('')
 expect(raw).not.toContain('current-private-session');expect(raw).not.toContain('9007199254740993');expect(raw).not.toContain('facts')
 await expect(again.begin(bytes)).rejects.toThrow('unresolved')
 expect(mocks.fetch).not.toHaveBeenCalled()
 mocks.fetch.mockResolvedValue(new Response('{"id":"run_synthetic"}',{status:202}))
 await jobsAPI('jobs/job_synthetic/runs',input,pending.key)
 expect(mocks.fetch).toHaveBeenCalledOnce()
 const sent=mocks.fetch.mock.calls[0][1]
 expect(sent.body).toBe(bytes)
 expect(sent.body).toContain('9007199254740993.00')
 expect(sent.headers['Idempotency-Key']).toBe(pending.key)
 again.clear();expect(again.read()).toBeUndefined()
 const next=await again.begin(bytes);expect(next.key).not.toBe(pending.key)
})
it('holds when the reminder cannot be durably retained',async()=>{
 vi.stubGlobal('crypto',webcrypto)
 const journal=await manualIntentJournal('job_synthetic')
 const denied=vi.spyOn(Storage.prototype,'setItem').mockImplementation(()=>{throw Error('storage unavailable')})
 await expect(journal.begin('{"facts":false}')).rejects.toThrow('storage unavailable')
 expect(mocks.fetch).not.toHaveBeenCalled();denied.mockRestore()
})
it('allocates one original key under concurrent submit gestures',async()=>{
 vi.stubGlobal('crypto',webcrypto)
 const journal=await manualIntentJournal('job_synthetic')
 const results=await Promise.allSettled([journal.begin('{"facts":false}'),journal.begin('{"facts":false}')])
 expect(results.filter(r=>r.status==='fulfilled')).toHaveLength(1)
 expect(results.filter(r=>r.status==='rejected')).toHaveLength(1)
 expect(mocks.fetch).not.toHaveBeenCalled()
})
