/** Synthetic v3 receipts signed under the published corpus test seed.
 * Alterations happen BEFORE signing, to test bindings rather than just signatures. */
import { fakeGateway, TEST_PUBLIC_KEY } from '../../research/__fixtures__/fakeGateway'
import { canonicalText } from '../../research/verify/canon'
import { sha256Hex } from '../../research/verify/receipt'
import { type DocumentObject } from '../client'
import notion from './notion-snapshot.json'
import obsidian from './obsidian-snapshot.json'

export async function signedConnected(provider: 'notion' | 'obsidian', options: {
 record?: (record: typeof notion | typeof obsidian) => void
 source?: string
 resourceId?: string
 shape?: string
} = {}) {
 const record = structuredClone(provider === 'notion' ? notion : obsidian)
 const original = {name: record.document.name, mediaType: record.document.mediaType, bytes: record.original.bytes, sha256: record.document.id}
 const connected = {resourceId: options.resourceId ?? record.provenance.source.resourceId, grant: 'aa'.repeat(32)}
 options.record?.(record)
 const source = options.source ?? provider, gw = fakeGateway(), session = 'connected-test'
 const response = await gw.acquire(session,source,record)
 const receipt = JSON.parse(response.text).receipt
 receipt.acquisition.shape = options.shape ?? (provider === 'notion' ? 'mcp' : 'command')
 receipt.resultDigest = `sha256:${await sha256Hex(canonicalText(JSON.stringify(record)))}`
 const args = canonicalText(JSON.stringify(connected)), commitment = new Uint8Array(37+args.length)
 commitment.set(new Uint8Array(32).fill(7));commitment.set(new TextEncoder().encode('args:'),32);commitment.set(args,37)
 receipt.argumentsCommitment = `sha256:${await sha256Hex(commitment)}`
 const signed = JSON.parse(await gw.resign(receipt));gw.receipts.set(session,[JSON.stringify(signed)]);await gw.seal(session)
 const object: DocumentObject = {version:1, original, proof:{session,source,authority:gw.authority,publicKey:TEST_PUBLIC_KEY,connected,response:JSON.stringify({result:record,receipt:signed,salts:{args:'07'.repeat(32)}}),registry:await gw.registry()}}
 return {object,pin:{url:'http://127.0.0.1:1',authority:gw.authority,signer:{algorithm:'ed25519' as const,public:TEST_PUBLIC_KEY}}}
}
