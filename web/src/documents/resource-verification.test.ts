import { expect,it } from 'vitest'
import { verifyDocument } from './client'
import pdfSnapshot from './__fixtures__/resource-pdf.json'
import { signedResource } from './__fixtures__/signedResource'

it('verifies a retained source from a provider with no Desk-specific handler',async()=>{
 const {object,pin}=await signedResource()
 expect((await verifyDocument(object,pin)).record.provenance.source.provider).toBe('fixture-files')
})
it.each(['provider','resourceId','version','format','url'] as const)('rejects a validly signed resource with inconsistent %s',async key=>{
 const {object,pin}=await signedResource(record=>{record.provenance.source[key]=key==='url'?'https://example.com/file?token=secret':'different'})
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
it.each(['grant','selection','proof-kind','multiple-proof','bytes','signature'] as const)('refuses %s tampering',async kind=>{
 const {object,pin}=await signedResource()
 if(kind==='grant') object.proof!.resource!.grant='bb'.repeat(32)
 if(kind==='selection') object.proof!.resource!.resourceId='other-file'
 if(kind==='proof-kind'){object.proof!.connected=object.proof!.resource;delete object.proof!.resource}
 if(kind==='multiple-proof') object.proof!.connected=object.proof!.resource
 if(kind==='bytes') object.original.bytes=btoa('changed')
 if(kind==='signature') object.proof!.response=object.proof!.response.replace('Fixture policy.','Different policy')
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})

it('verifies PDF bytes and extracted text through the same provider-independent resource contract',async()=>{
 const {object,pin}=await signedResource(undefined,pdfSnapshot)
 const verified=await verifyDocument(object,pin)
 expect(verified.record.document.mediaType).toBe('application/pdf')
 expect(verified.record.content.pages[0]?.text).toContain('Fixture PDF policy.')
 expect(verified.record.provenance.ocr).toBeNull()
})
