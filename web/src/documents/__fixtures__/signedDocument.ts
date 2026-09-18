import { fakeGateway, TEST_PUBLIC_KEY } from '../../research/__fixtures__/fakeGateway'
import { canonicalText } from '../../research/verify/canon'
import { sha256Hex } from '../../research/verify/receipt'
import { base64, documentArguments, type DocumentObject } from '../client'
import fixture from './complete-verbatim-text.json'

export async function signedDocument() {
  const gw = fakeGateway(), raw = new TextEncoder().encode('line one\nline two')
  const original = { name: 'notes.txt', mediaType: 'text/plain', bytes: base64(raw), sha256: `sha256:${await sha256Hex(raw)}` }
  const record = structuredClone(fixture); record.document.id = original.sha256; record.document.size = raw.length
  const resultDigest = `sha256:${await sha256Hex(canonicalText(JSON.stringify(record)))}`
  const response = await gw.acquire('doc-test','documents',record)
  const receipt = JSON.parse(response.text).receipt
  receipt.acquisition.shape = 'command'; receipt.resultDigest = resultDigest
  const salt = new Uint8Array(32).fill(7), args = canonicalText(JSON.stringify(documentArguments(original)))
  const commitment = new Uint8Array(37 + args.length); commitment.set(salt); commitment.set(new TextEncoder().encode('args:'),32); commitment.set(args,37)
  receipt.argumentsCommitment = `sha256:${await sha256Hex(commitment)}`
  const signed = JSON.parse(await gw.resign(receipt)); gw.receipts.set('doc-test',[JSON.stringify(signed)]); await gw.seal('doc-test')
  const object: DocumentObject = { version:1, original, proof: { session:'doc-test', source:'documents', authority:gw.authority, publicKey:TEST_PUBLIC_KEY, response:JSON.stringify({ result:record, receipt:signed, salts:{args:'07'.repeat(32)} }), registry:await gw.registry() } }
  const pin = { url:'http://localhost:9876', authority:gw.authority, signer:{algorithm:'ed25519' as const, public:TEST_PUBLIC_KEY} }
  const reference = { id:'12345678-1234-1234-1234-123456789abc', digest:resultDigest, pages:[1], allowPartial:false }
  return {object,pin,reference,gw}
}
