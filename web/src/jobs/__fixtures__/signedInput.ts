import { fakeGateway, TEST_PUBLIC_KEY } from '../../research/__fixtures__/fakeGateway'
import { canonicalText } from '../../research/verify/canon'
import { sha256Hex } from '../../research/verify/receipt'
import { base64, type DocumentObject } from '../../documents/client'
import fixture from '../../documents/__fixtures__/complete-verbatim-text.json'
export async function signedInput(text = '{"request":{"type":"data-access"}}') {
 const raw = new TextEncoder().encode(text), gw = fakeGateway(), session = 'job-input-test', source = 'drive'
 const original = { name: 'input.json', mediaType: 'application/json', bytes: base64(raw), sha256: `sha256:${await sha256Hex(raw)}` }
 const record = structuredClone(fixture)
 Object.assign(record.document, { ...original, bytes: undefined, sha256: undefined, id: original.sha256, size: raw.length, version: '7' })
 Object.assign(record.original, { retention: 'inline', encoding: 'base64', bytes: original.bytes })
 Object.assign(record.provenance.source, { kind: 'google-drive', fileId: 'file-A', version: '7', mediaType: 'application/json' })
 Object.assign(record.content, { chars: text.length, pages: [{ chars: text.length, extraction: 'verbatim', number: 1, status: 'ok', text, unmapped: 0 }] })
 const drive = { fileId: 'file-A', grant: 'aa'.repeat(32) }
 const response = await gw.acquire(session, source, record), receipt = JSON.parse(response.text).receipt
 receipt.acquisition.shape = 'http'; receipt.resultDigest = `sha256:${await sha256Hex(canonicalText(JSON.stringify(record)))}`
 const args = canonicalText(JSON.stringify(drive)), committed = new Uint8Array(37 + args.length)
 committed.set(new Uint8Array(32).fill(7)); committed.set(new TextEncoder().encode('args:'), 32); committed.set(args, 37)
 receipt.argumentsCommitment = `sha256:${await sha256Hex(committed)}`
 const signed = JSON.parse(await gw.resign(receipt)); gw.receipts.set(session, [JSON.stringify(signed)]); await gw.seal(session)
 const object: DocumentObject = { version: 1, original, proof: { session, source, authority: gw.authority, publicKey: TEST_PUBLIC_KEY, drive, response: JSON.stringify({ result: record, receipt: signed, salts: { args: '07'.repeat(32) } }), registry: await gw.registry() } }
 return { object, pin: { url: 'http://localhost:9876', authority: gw.authority, signer: { algorithm: 'ed25519' as const, public: TEST_PUBLIC_KEY } } }
}
