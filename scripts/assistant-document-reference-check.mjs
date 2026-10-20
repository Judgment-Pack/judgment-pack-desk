// Bundle this focused offline probe with the repository's esbuild, then run it
// against inputs exported by TestAssistantDocumentSupportedSources. Those inputs
// are authored by the independent fixture writer, never a verifier's answers.
import assert from 'node:assert/strict'
import { readFile, readdir } from 'node:fs/promises'
import { join } from 'node:path'
import { verifyDocument, selectedPages } from '../web/src/documents/client.ts'

const directory = process.argv[2]
assert(directory, 'fixture directory is required')
const files = (await readdir(directory)).filter(name => name.endsWith('.json'))
assert.equal(files.length, 10, 'all supported acquisition shapes must be present')
for (const file of files) {
  const input = JSON.parse(await readFile(join(directory, file), 'utf8'))
  const pin = { authority: input.pin.authority, signer: { algorithm: 'ed25519', public: input.pin.public } }
  const document = await verifyDocument(input.object, pin, input.reference.digest)
  assert.deepEqual(selectedPages(document, input.reference), [{
    page: 1,
    citation: `attachment:12345678-1234-1234-1234-123456789abc/${input.reference.digest}/page/1`,
    text: 'Selected first page.'
  }], file)
  const changed = structuredClone(input.object)
  changed.proof.response = changed.proof.response.replace('Selected first page.', 'Injected first page.')
  await assert.rejects(verifyDocument(changed, pin, input.reference.digest), undefined, file)
}
console.log(`Browser reference verification: ${files.length} supported source/shape inputs; exact selected page/citation and tampered-result refusals passed.`)
