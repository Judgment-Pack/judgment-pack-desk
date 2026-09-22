import { readFileSync, readdirSync } from 'node:fs'
import { strict as assert } from 'node:assert'
import { readDocumentRecord } from '../desk/web/src/documents/record.ts'
import { verifyDocument } from '../desk/web/src/documents/client.ts'
import { signedResource } from '../desk/web/src/documents/__fixtures__/signedResource.ts'

// Producer records are freshly generated from the exact reviewed gateway tree.
// The existing fake signer uses only the repository's public corpus TEST-SEED.
const directory = new URL('./records/', import.meta.url)
for (const name of readdirSync(directory)) {
 const record = JSON.parse(readFileSync(new URL(name, directory), 'utf8'))
 assert.equal(readDocumentRecord(record).provenance.source.kind, 'connection-resource')
 const { object, pin } = await signedResource(undefined, record)
 const verified = await verifyDocument(object, pin)
 assert.equal(verified.record.document.id, record.document.id)
 assert.equal(verified.record.provenance.ocr, null)
 assert.equal(verified.record.provenance.source.resourceId, record.provenance.source.resourceId)
 await assert.rejects(verifyDocument(object, { ...pin, signer: { ...pin.signer, public: '11'.repeat(32) } }))
 await assert.rejects(verifyDocument(object, { ...pin, authority: 'gateway:other' }))
 const changed = structuredClone(object)
 changed.proof!.resource!.resourceId = 'other/resource'
 await assert.rejects(verifyDocument(changed, pin))
 const unsealed = structuredClone(object)
 unsealed.proof!.registry = ''
 await assert.rejects(verifyDocument(unsealed, pin))
 console.log('PASS signed current-pin, seal, resource and bytes verification:', name)
}
