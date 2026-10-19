import {beforeEach,expect,it,vi} from 'vitest'
import {checkDialect,DialectCheckError} from './authoring'
import {canonicalize,dialectDigest} from './canon'
import {deskFetch} from '../files/client'
vi.mock('../files/client',async original=>({...await original<object>(),deskFetch:vi.fn()}))
const doc={version:2,case:{facts:[{target:'/risk',source:'/value'}],evidence:[]}}
async function report(){return {status:'complete',document:canonicalize(doc),digest:await dialectDigest(doc),diagnostics:[],compatibility:{scope:'declared-inventory-coverage',status:'complete',pack:{sha256:'pack'},valueTypesChecked:false,executionChecked:false,notes:[]}}}
beforeEach(()=>vi.resetAllMocks())
it('sends exact snapshots using the shared authenticated transport without acquisition',async()=>{
 const checked=await report();vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify(checked)))
 const signal=new AbortController().signal
 expect(await checkDialect(doc,'pack bytes',signal)).toEqual(checked)
 const [url,init]=vi.mocked(deskFetch).mock.calls[0]!
 expect(url).toBe('/api/dialect-authoring/check');expect(init?.signal).toBe(signal)
 expect(JSON.parse(init?.body as string)).toEqual({document:canonicalize(doc),pack:'pack bytes'})
})
it('keeps Runtime diagnostics and refuses missing coverage and unsupported source kinds',async()=>{
 const checked=await report()
 for(const failure of [{...checked,status:'incomplete',compatibility:{...checked.compatibility,notes:[{code:'missing-producer',target:'/risk',severity:'error'}]}},{status:'refused',diagnostics:[{code:'JPS-DIALECT-SOURCE-UNSUPPORTED',instancePath:'/sources/0/kind',message:'Unsupported source.'}]}]){
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response(JSON.stringify(failure)))
  await expect(checkDialect(doc,'pack')).rejects.toBeInstanceOf(DialectCheckError)
 }
})
it('accepts acknowledged omissions only as review-required, without claiming execution',async()=>{
 const checked=await report();checked.status='review-required';checked.compatibility.status='review-required'
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify(checked)))
 expect((await checkDialect(doc,'pack')).compatibility?.executionChecked).toBe(false)
})
it('rejects mismatched canonical documents and discards cancelled results without retries',async()=>{
 const checked=await report();checked.document='{"version":3,"case":{"facts":[],"evidence":[]}}'
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify(checked)))
 await expect(checkDialect(doc,'pack')).rejects.toThrow('different dialect')
 const controller=new AbortController();controller.abort()
 await expect(checkDialect(doc,'pack',controller.signal)).rejects.toThrow()
 expect(deskFetch).toHaveBeenCalledTimes(1)
})
