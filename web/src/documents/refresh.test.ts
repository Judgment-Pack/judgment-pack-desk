import { expect,it } from 'vitest'
import { sourceChanges,sameSource,sourceImpacts,decodeReviews } from './refresh'
import { signedDocument } from './__fixtures__/signedDocument'
import { verifyDocument } from './client'
import { emptySuite, newCase } from '../packs/test-workspace/model'
it('ignores new receipt identities when source content is unchanged and detects changed pages independently of bytes',async()=>{
 const f=await signedDocument('web'),before=await verifyDocument(f.object,f.pin),after=structuredClone(before)
 after.digest='sha256:'+'b'.repeat(64);after.record.provenance.observedAt='2026-09-24T12:00:00Z'
 expect(sourceChanges(before,after)).toEqual({bytesChanged:false,changedPages:[],extractionChanged:false})
 after.record.content.pages[0]!.text='changed policy'
 expect(sourceChanges(before,after).changedPages).toEqual([1])
 expect(sameSource(before,after)).toBe(true)
 after.record.provenance.source.requestedUrl='https://example.com/other';expect(sameSource(before,after)).toBe(false)
})
it('matches exact evidence identities, never inventing impacts for another snapshot',async()=>{
 const f=await signedDocument(),c=newCase();c.sources=[{id:f.reference.id,name:'source',text:'',document:f.reference}]
 const store={version:1 as const,suites:{policy:{...emptySuite(),cases:[c]}}}
 expect(sourceImpacts(f.reference,store,[])[0]?.href).toBe('/packs/policy/evaluate')
 expect(sourceImpacts({...f.reference,digest:'sha256:'+'0'.repeat(64)},store,[])).toEqual([])
})
it('refuses malformed refresh history or replacement of the same attachment identity',async()=>{
 const f=await signedDocument(),r={id:crypto.randomUUID(),name:'Policy',before:f.reference,after:{...f.reference,id:crypto.randomUUID()},checkedAt:'2026-09-24T12:00:00Z'}
 expect(decodeReviews({version:1,reviews:[r]}).reviews).toHaveLength(1)
 expect(()=>decodeReviews({version:1,reviews:[{...r,after:r.before}]})).toThrow()
 expect(()=>decodeReviews({version:1,reviews:[r,r]})).toThrow()
})
