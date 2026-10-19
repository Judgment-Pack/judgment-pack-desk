import {createHash,webcrypto} from 'node:crypto'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {deskFetch,FileRequestError,StaleWrite} from '../files/client'
import {exchangeAPI} from './client'
vi.mock('../files/client',async original=>({...await original<object>(),deskFetch:vi.fn()}))
const request={reviewId:'a'.repeat(64),entryId:'example.non-gtm',base:'b'.repeat(64),reviewed:true}
const id=createHash('sha256').update(request.reviewId+'\n'+request.entryId).digest('hex')
beforeEach(()=>{vi.resetAllMocks();vi.stubGlobal('crypto',webcrypto)})
afterEach(()=>vi.unstubAllGlobals())
it('accepts only the immutable receipt for the exact reviewed import',async()=>{
 const receipt={id,entryId:request.entryId};vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify(receipt),{status:201}))
 expect(await exchangeAPI('imports',request)).toEqual(receipt)
 expect(JSON.parse(vi.mocked(deskFetch).mock.calls[0][1]!.body as string)).toEqual(request)
})
it.each([null,{}, {id,entryId:'different'}, {id:'c'.repeat(64),entryId:request.entryId}])('preserves uncertainty for a mismatched successful receipt %j',async receipt=>{
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify(receipt),{status:201}))
 await expect(exchangeAPI('imports',request)).rejects.toMatchObject({name:'FileRequestError',status:201,source:'desk'})
 expect(vi.mocked(deskFetch)).toHaveBeenCalledTimes(1)
})
it('preserves shared stale-write classification and does not retry a refused import',async()=>{
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify({error:'changed',code:'bad-request'}),{status:409}))
 await expect(exchangeAPI('imports',request)).rejects.toBeInstanceOf(StaleWrite)
 expect(vi.mocked(deskFetch)).toHaveBeenCalledTimes(1)
})
it('does not turn an unreadable successful response into an imported template',async()=>{
 vi.mocked(deskFetch).mockResolvedValue(new Response('incomplete',{status:201}))
 await expect(exchangeAPI('imports',request)).rejects.toBeInstanceOf(FileRequestError)
 expect(vi.mocked(deskFetch)).toHaveBeenCalledTimes(1)
})
