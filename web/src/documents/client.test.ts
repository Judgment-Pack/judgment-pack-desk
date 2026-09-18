import { expect, it } from 'vitest'
import { base64, verifyDocument, documentContext, matchesPageQuote } from './client'
import { signedDocument } from './__fixtures__/signedDocument'
it('verifies original, v3 commitment, seal and page identity without sending original bytes', async () => {
  const {object,pin,reference} = await signedDocument()
  const document = await verifyDocument(object,pin,reference.digest)
  const prompt = documentContext(document,reference)
  expect(prompt).toContain('line one'); expect(prompt).toContain(reference.digest)
  expect(prompt).not.toContain(object.original.bytes); expect(prompt).not.toContain('signature')
  expect(matchesPageQuote(document,1,'line one line two')).toBe(true)
  expect(matchesPageQuote(document,1,'invented words')).toBe(false)
  expect(matchesPageQuote(document,2,'line one')).toBe(false)
  expect(() => documentContext(document,{...reference,pages:[2]})).toThrow()
})
it.each(['original','name','salt','response','seal','pin','source','digest'] as const)('withholds a tampered %s', async kind => {
  const {object,pin,reference} = await signedDocument()
  if (kind === 'original') object.original.bytes = base64(new TextEncoder().encode('different bytes'))
  if (kind === 'name') object.original.name = 'other.txt'
  if (kind === 'salt') object.proof!.response = object.proof!.response.replace('07'.repeat(32),'08'.repeat(32))
  if (kind === 'response') object.proof!.response = object.proof!.response.replace('line one','Line one')
  if (kind === 'seal') object.proof!.registry = ''
  if (kind === 'pin') pin.signer.public = '11'.repeat(32)
  if (kind === 'source') object.proof!.source = 'another'
  if (kind === 'digest') reference.digest = 'sha256:'+'11'.repeat(32)
  await expect(verifyDocument(object,pin,reference.digest)).rejects.toThrow()
})
it('verifies a Drive acquisition with its retained original and selected file', async () => {
  const {object,pin,reference} = await signedDocument(true)
  const doc = await verifyDocument(object,pin,reference.digest)
  expect(doc.record.provenance.source.fileId).toBe('file-A')
  expect(documentContext(doc,reference)).not.toContain(object.proof!.drive!.grant)
})
it.each(['file','grant','missing-request','source-swap'] as const)('rejects Drive %s substitution', async kind => {
  const {object,pin,reference} = await signedDocument(true)
  if (kind === 'file') object.proof!.drive!.fileId = 'file-B'
  if (kind === 'grant') object.proof!.drive!.grant = 'bb'.repeat(32)
  if (kind === 'missing-request') delete object.proof!.drive
  if (kind === 'source-swap') object.proof!.response = object.proof!.response.replace('google-drive','inline')
  await expect(verifyDocument(object,pin,reference.digest)).rejects.toThrow()
})
