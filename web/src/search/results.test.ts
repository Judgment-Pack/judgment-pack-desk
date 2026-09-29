import { describe, expect, it } from 'vitest'
import { fakeGateway, TEST_PUBLIC_KEY } from '../research/__fixtures__/fakeGateway'
import { canonicalText } from '../research/verify/canon'
import { sha256Hex } from '../research/verify/receipt'
import { base64, type DocumentObject } from '../documents/client'
import { validateSearch, verifySearch, type SearchResult } from './results'

export async function signedSearch(){
 const gw=fakeGateway()
 const request={connection:'demo',revision:'ab'.repeat(32),query:'find public policy',maxResults:5}
 const result:SearchResult={version:1,...request,provider:'tavily',kind:'search-results',retrievedAt:'2026-09-29T12:00:00Z',hits:[{title:'Policy',url:'https://example.org/policy',snippet:'A search snippet'}]}
 const raw=canonicalText(JSON.stringify(result)),digest=`sha256:${await sha256Hex(raw)}`
 const acquired=await gw.acquire('search-test','web-search',result),receipt=JSON.parse(acquired.text).receipt
 receipt.acquisition.shape='http';receipt.resultDigest=digest
 const args=canonicalText(JSON.stringify(request)),committed=new Uint8Array(37+args.length)
 committed.set(new Uint8Array(32).fill(7));committed.set(new TextEncoder().encode('args:'),32);committed.set(args,37)
 receipt.argumentsCommitment=`sha256:${await sha256Hex(committed)}`
 const signed=JSON.parse(await gw.resign(receipt));gw.receipts.set('search-test',[JSON.stringify(signed)]);await gw.seal('search-test')
 const object:DocumentObject={version:1,original:{name:'web-search.json',mediaType:'application/json',bytes:base64(raw),sha256:digest},proof:{source:'web-search',session:'search-test',authority:gw.authority,publicKey:TEST_PUBLIC_KEY,response:JSON.stringify({receipt:signed,result,salts:{args:'07'.repeat(32)}}),registry:await gw.registry()}}
 const pin={url:'http://localhost:9876',authority:gw.authority,signer:{algorithm:'ed25519' as const,public:TEST_PUBLIC_KEY}}
 return {object,pin,reference:{id:'12345678-1234-1234-1234-123456789abc',digest,request},result}
}
describe('retained search results',()=>{
 it('verifies the normalized leads and exact connection revision and query',async()=>{
  const {object,pin,reference,result}=await signedSearch()
  expect((await verifySearch(object,reference,pin)).result).toEqual(result)
 })
 it.each(['query','revision','connection','maxResults','pin','digest','original','result','registry','salt','source','shape'] as const)('rejects a substituted %s',async field=>{
  const {object,pin,reference}=await signedSearch()
  if(field==='query')reference.request.query='other'
  if(field==='revision')reference.request.revision='ff'.repeat(32)
  if(field==='connection')reference.request.connection='other'
  if(field==='maxResults')reference.request.maxResults=10
  if(field==='pin')pin.signer.public='ff'.repeat(32)
  if(field==='digest')reference.digest='sha256:'+'f'.repeat(64)
  if(field==='original')object.original.bytes=base64(new TextEncoder().encode('{}'))
  if(field==='registry')object.proof!.registry=''
  if(field==='source')object.proof!.source='web'
  if(['result','salt','shape'].includes(field)){
   const response=JSON.parse(object.proof!.response)
   if(field==='result')response.result.hits[0].url='https://evil.example/'
   if(field==='salt')response.salts.args='ff'.repeat(32)
   if(field==='shape')response.receipt.acquisition.shape='command'
   object.proof!.response=JSON.stringify(response)
  }
  await expect(verifySearch(object,reference,pin)).rejects.toThrow()
 })
 it('rejects unsafe URLs, provider text masquerading as search results, and over-budget results',async()=>{
  const {reference,result}=await signedSearch()
  for(const change of [()=>result.hits[0]!.url='javascript:alert(1)',()=>result.generatedAnswer='not a page',()=>result.hits=Array(6).fill(result.hits[0])]){
   const copy=structuredClone(result);change();expect(()=>validateSearch(result,reference.request)).toThrow();Object.assign(result,copy);delete result.generatedAnswer
  }
 })
})
