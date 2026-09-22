import { fakeGateway, TEST_PUBLIC_KEY } from '../../research/__fixtures__/fakeGateway'
import { canonicalText } from '../../research/verify/canon'
import { sha256Hex } from '../../research/verify/receipt'
import type { DocumentObject } from '../client'
import snapshot from './resource-snapshot.json'

/** Actual gateway producer output, signed with the public corpus test seed.
 * Semantic mutations are signed too: these tests exercise consumer bindings. */
export async function signedResource(change?: (record: typeof snapshot) => void, fixture = snapshot) {
 const record=structuredClone(fixture)
 const original={name:record.document.name,mediaType:record.document.mediaType,bytes:record.original.bytes,sha256:record.document.id}
 const resource={resourceId:record.provenance.source.resourceId,grant:'aa'.repeat(32)}
 change?.(record)
 const gw=fakeGateway(),session='resource-test',source='fixture-files'
 const response=await gw.acquire(session,source,record),receipt=JSON.parse(response.text).receipt
 receipt.acquisition.shape='command'
 receipt.resultDigest=`sha256:${await sha256Hex(canonicalText(JSON.stringify(record)))}`
 const args=canonicalText(JSON.stringify(resource)),committed=new Uint8Array(37+args.length)
 committed.set(new Uint8Array(32).fill(7));committed.set(new TextEncoder().encode('args:'),32);committed.set(args,37)
 receipt.argumentsCommitment=`sha256:${await sha256Hex(committed)}`
 const signed=JSON.parse(await gw.resign(receipt));gw.receipts.set(session,[JSON.stringify(signed)]);await gw.seal(session)
 const object:DocumentObject={version:1,original,proof:{session,source,authority:gw.authority,publicKey:TEST_PUBLIC_KEY,resource,response:JSON.stringify({result:record,receipt:signed,salts:{args:'07'.repeat(32)}}),registry:await gw.registry()}}
 return {object,pin:{url:'http://127.0.0.1:1',authority:gw.authority,signer:{algorithm:'ed25519' as const,public:TEST_PUBLIC_KEY}}}
}
