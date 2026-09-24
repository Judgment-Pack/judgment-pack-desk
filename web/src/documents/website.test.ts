import { describe, expect, it } from 'vitest'
import { fakeGateway, TEST_PUBLIC_KEY } from '../research/__fixtures__/fakeGateway'
import { canonicalText } from '../research/verify/canon'
import { sha256Hex } from '../research/verify/receipt'
import { base64, type DocumentObject } from './client'
import { validateDiscovery, verifyWebsite, type WebsiteDiscovery } from './website'

const seed='https://example.com/'
export function discoveryFixture():WebsiteDiscovery{return {version:1,seed,origin:'https://example.com',pages:[{url:seed,from:'',depth:0,title:'Home',status:'discovered',reason:''},{url:seed+'policy',from:seed,depth:1,title:'Policy',status:'discovered',reason:''}],limits:{pages:10,depth:2,links:100,bytes:8<<20,seconds:45},stopReason:'finished',bytes:512,requests:3,externalLinks:0}}
async function signedWebsite(){
 const gw=fakeGateway(),result=discoveryFixture(),raw=canonicalText(JSON.stringify(result)),digest=`sha256:${await sha256Hex(raw)}`
 const acquired=await gw.acquire('website-test','web-discovery',result)
 const receipt=JSON.parse(acquired.text).receipt
 receipt.acquisition.shape='http';receipt.resultDigest=digest
 const args=canonicalText(JSON.stringify({url:seed})),committed=new Uint8Array(37+args.length)
 committed.set(new Uint8Array(32).fill(7));committed.set(new TextEncoder().encode('args:'),32);committed.set(args,37)
 receipt.argumentsCommitment=`sha256:${await sha256Hex(committed)}`
 const signed=JSON.parse(await gw.resign(receipt));gw.receipts.set('website-test',[JSON.stringify(signed)]);await gw.seal('website-test')
 const object:DocumentObject={version:1,original:{name:'website-discovery.json',mediaType:'application/json',bytes:base64(raw),sha256:digest},proof:{source:'web-discovery',session:'website-test',authority:gw.authority,publicKey:TEST_PUBLIC_KEY,response:JSON.stringify({receipt:signed,result,salts:{args:'07'.repeat(32)}}),registry:await gw.registry()}}
 const pin={url:'http://localhost:9876',authority:gw.authority,signer:{algorithm:'ed25519' as const,public:TEST_PUBLIC_KEY}}
 const reference={id:'12345678-1234-1234-1234-123456789abc',digest,seed}
 return {object,pin,reference}
}
describe('website discovery verification',()=>{
 it('accepts a sealed, signed, retained manifest under the current pin',async()=>{
  const {object,pin,reference}=await signedWebsite()
  expect((await verifyWebsite(object,reference,pin)).discovery).toEqual(discoveryFixture())
 })
 it.each(['seed','digest','pin','result','original','registry','source','salt','shape'] as const)('rejects a substituted %s',async field=>{
  const {object,pin,reference}=await signedWebsite()
  if(field==='seed')reference.seed+='other'
  if(field==='digest')reference.digest='sha256:'+'f'.repeat(64)
  if(field==='pin')pin.signer.public='ff'.repeat(32)
  if(field==='original')object.original.bytes=base64(new TextEncoder().encode('{}'))
  if(field==='registry')object.proof!.registry=''
  if(field==='source')object.proof!.source='web'
  if(['result','salt','shape'].includes(field)){
   const response=JSON.parse(object.proof!.response)
   if(field==='result')response.result.pages[1].url='https://evil.example/'
   if(field==='salt')response.salts.args='ff'.repeat(32)
   if(field==='shape')delete response.receipt.acquisition
   object.proof!.response=JSON.stringify(response)
  }
  await expect(verifyWebsite(object,reference,pin)).rejects.toThrow()
 })
 it.each(['external','parent','duplicate','depth','budget','status','origin'] as const)('rejects a malformed %s manifest',field=>{
  const d=discoveryFixture()
  if(field==='external')d.pages[1]!.url='https://evil.example/'
  if(field==='parent')d.pages[1]!.from='https://example.com/missing'
  if(field==='duplicate')d.pages[1]!.url=seed
  if(field==='depth')d.pages[1]!.depth=3
  if(field==='budget')d.bytes=(8<<20)+1
  if(field==='status')d.pages[1]!.status='pending' as never
  if(field==='origin')d.origin='https://elsewhere.example'
  expect(()=>validateDiscovery(d,seed)).toThrow()
 })
})
