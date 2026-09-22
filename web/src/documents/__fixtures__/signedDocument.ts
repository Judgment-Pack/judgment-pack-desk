import { fakeGateway, TEST_PUBLIC_KEY } from '../../research/__fixtures__/fakeGateway'
import { canonicalText } from '../../research/verify/canon'
import { sha256Hex } from '../../research/verify/receipt'
import { base64, documentArguments, type DocumentObject } from '../client'
import fixture from './complete-verbatim-text.json'

export async function signedDocument(drive: boolean | 'gmail' | 'web' = false) {
  const gw = fakeGateway(), raw = new TextEncoder().encode('line one\nline two')
  const original = { name: 'notes.txt', mediaType: 'text/plain', bytes: base64(raw), sha256: `sha256:${await sha256Hex(raw)}` }
  const record = structuredClone(fixture); record.document.id = original.sha256; record.document.size = raw.length
  const mailRequest = {grant: 'aa'.repeat(32), messageId: 'abc1'}
  const driveRequest = {grant: 'aa'.repeat(32), fileId: 'file-A'}
  if (drive) {
    Object.assign(record.document, {version: '7'})
    Object.assign(record.original, {retention: 'inline', encoding: 'base64', bytes: original.bytes})
    Object.assign(record.provenance.source, drive === 'gmail' ? {kind: 'gmail', messageId: 'abc1', threadId: 'abc2', version: '7', format: 'text-export-v1'} : {kind: 'google-drive', fileId: 'file-A', version: '7', mediaType: 'text/plain'})
  }
  const webRequest = { url: 'https://example.com/policy' }
  if (drive === 'web') {
   Object.assign(record.document, { version: original.sha256 })
   record.provenance.source = { kind: 'web', requestedUrl: webRequest.url, url: webRequest.url, version: original.sha256, responseDigest: 'sha256:'+'a'.repeat(64), mediaType: 'text/html', format: 'static-text-v1' } as typeof record.provenance.source
  }
  const resultDigest = `sha256:${await sha256Hex(canonicalText(JSON.stringify(record)))}`
  const response = await gw.acquire('doc-test',drive === 'web' ? 'web' : drive === 'gmail' ? 'gmail' : 'documents',record)
  const receipt = JSON.parse(response.text).receipt
  receipt.acquisition.shape = drive ? 'http' : 'command'; receipt.resultDigest = resultDigest
  const salt = new Uint8Array(32).fill(7), args = canonicalText(JSON.stringify(drive === 'web' ? webRequest : drive === 'gmail' ? mailRequest : drive ? driveRequest : documentArguments(original)))
  const commitment = new Uint8Array(37 + args.length); commitment.set(salt); commitment.set(new TextEncoder().encode('args:'),32); commitment.set(args,37)
  receipt.argumentsCommitment = `sha256:${await sha256Hex(commitment)}`
  const signed = JSON.parse(await gw.resign(receipt)); gw.receipts.set('doc-test',[JSON.stringify(signed)]); await gw.seal('doc-test')
  const object: DocumentObject = { version:1, original, proof: { session:'doc-test', source:drive === 'web' ? 'web' : drive === 'gmail' ? 'gmail' : 'documents', authority:gw.authority, publicKey:TEST_PUBLIC_KEY, response:JSON.stringify({ result:record, receipt:signed, salts:{args:'07'.repeat(32)} }), registry:await gw.registry(), ...(drive === 'web' ? { web: webRequest } : drive === 'gmail' ? {gmail: mailRequest} : drive ? {drive: driveRequest} : {}) } }
  const pin = { url:'http://localhost:9876', authority:gw.authority, signer:{algorithm:'ed25519' as const, public:TEST_PUBLIC_KEY} }
  const reference = { id:'12345678-1234-1234-1234-123456789abc', digest:resultDigest, pages:[1], allowPartial:false }
  return {object,pin,reference,gw}
}
