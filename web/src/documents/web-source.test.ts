import { expect, it } from 'vitest'
import { verifyDocument, documentContext } from './client'
import { signedDocument } from './__fixtures__/signedDocument'
import { readDocumentRecord, validWebURL } from './record'
it('verifies the selected URL and retained static text and includes source identity in context', async () => {
 const { object, pin, reference } = await signedDocument('web')
 const doc = await verifyDocument(object, pin, reference.digest)
 expect(doc.record.provenance.source.requestedUrl).toBe('https://example.com/policy')
 const context = documentContext(doc, reference)
 expect(context).toContain('https://example.com/policy'); expect(context).toContain('static-text-v1')
 expect(context).not.toContain(object.original.bytes)
})
it.each(['url','missing-request','cross-provider','source','snapshot','original','format'] as const)('rejects web %s substitution', async kind => {
 const { object, pin, reference } = await signedDocument('web')
 if (kind === 'url') object.proof!.web!.url = 'https://example.com/other'
 if (kind === 'missing-request') delete object.proof!.web
 if (kind === 'cross-provider') object.proof!.gmail = { messageId: 'abc1', grant: 'aa'.repeat(32) }
 if (kind === 'source') object.proof!.source = 'documents'
 if (kind === 'snapshot') object.proof!.response = object.proof!.response.replace('line one', 'line six')
 if (kind === 'original') object.original.bytes = btoa('Different text')
 if (kind === 'format') object.proof!.response = object.proof!.response.replace('static-text-v1', 'original-v1')
 await expect(verifyDocument(object, pin, reference.digest)).rejects.toThrow()
})
it('refuses incompatible web format, identity, digest, and OCR claims independently of the signature', async () => {
 const { object } = await signedDocument('web')
 const record = JSON.parse(object.proof!.response).result
 for (const [key, value] of [['format','original-v1'], ['mediaType','application/pdf'], ['url','http://example.com'], ['requestedUrl','https://user@example.com'], ['responseDigest','bad'], ['version','sha256:'+'0'.repeat(64)]]) {
  const changed = structuredClone(record); changed.provenance.source[key!] = value
  expect(() => readDocumentRecord(changed)).toThrow()
 }
 const changed = structuredClone(record); changed.document.version = null
 expect(() => readDocumentRecord(changed)).toThrow()
})
it.each(['https://@example.com','http://example.com','https://user:pass@example.com','https://example.com:8443','https://example.com/#section','https://example.com\\@localhost','https://example.com/\n'])('refuses unsupported URL spelling: %s', url => expect(validWebURL(url)).toBe(false))

it('accepts the gateway-produced HTML snapshot fixture', async () => {
 const { default: fixture } = await import('./__fixtures__/web-snapshot.json')
 const record=readDocumentRecord(fixture)
 expect(record.provenance.source.kind).toBe('web')
 expect(record.content.pages[0]?.text).toContain('First fact & second.')
})
