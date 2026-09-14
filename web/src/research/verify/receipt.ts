/**
 * What a receipt's and a seal's signatures cover (gateway SPEC.md §1.2, §1.2a
 * and §3), the key id a public key yields, and Ed25519 verification through
 * WebCrypto — the browser's own implementation, under the key the desk-level
 * file pinned and never one a gateway handed the page.
 */
import { CanonError, bytesToHex, canonicalize, hexToBytes, memberOf, type JsonNode } from './canon'

export const RECEIPT_PREFIX_2 = 'judgment-pack-gateway/receipt/2:'
export const RECEIPT_PREFIX_3 = 'judgment-pack-gateway/receipt/3:'
export const SEAL_PREFIX = 'judgment-pack-gateway/seal/2:'

function withPrefix(prefix: string, canonical: Uint8Array): Uint8Array {
  const head = new TextEncoder().encode(prefix)
  const out = new Uint8Array(head.length + canonical.length)
  out.set(head, 0)
  out.set(canonical, head.length)
  return out
}

/**
 * `prefix + canon(receipt without its top-level signature)`: every other
 * member retained, nested ones included, so appending anything anywhere
 * invalidates the signature (§1.2a).
 */
export function receiptSigningInput(receipt: JsonNode, version: '2' | '3'): Uint8Array {
  if (receipt.kind !== 'object') throw new CanonError('a receipt is an object')
  const covered: JsonNode = {
    kind: 'object',
    members: receipt.members.filter((member) => member.name !== 'signature')
  }
  return withPrefix(version === '3' ? RECEIPT_PREFIX_3 : RECEIPT_PREFIX_2, canonicalize(covered))
}

/** `"judgment-pack-gateway/seal/2:" + canon({sessionId, finalCount, sealedAt, keyId})`. */
export function sealSigningInput(seal: JsonNode): Uint8Array {
  if (seal.kind !== 'object') throw new CanonError('a seal is an object')
  const members: JsonNode & { kind: 'object' } = { kind: 'object', members: [] }
  for (const name of ['sessionId', 'finalCount', 'sealedAt', 'keyId']) {
    const value = memberOf(seal, name)
    if (value === undefined) throw new CanonError(`a seal carries ${name}`)
    members.members.push({ name, value })
  }
  return withPrefix(SEAL_PREFIX, canonicalize(members))
}

export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', bytes as BufferSource)
  return bytesToHex(new Uint8Array(digest))
}

/** `sha256(public key, 32 raw bytes)` in hex, the first 32 characters (§1.2). */
export async function keyIdOf(publicKeyHex: string): Promise<string> {
  return (await sha256Hex(hexToBytes(publicKeyHex))).slice(0, 32)
}

/** Ed25519 over `message`, under the raw 32-byte public key given in hex. */
export async function verifyEd25519(
  publicKeyHex: string,
  message: Uint8Array,
  signatureHex: string
): Promise<boolean> {
  let key: CryptoKey
  try {
    key = await crypto.subtle.importKey('raw', hexToBytes(publicKeyHex) as BufferSource, { name: 'Ed25519' }, false, [
      'verify'
    ])
  } catch {
    return false
  }
  let signature: Uint8Array
  try {
    signature = hexToBytes(signatureHex)
  } catch {
    return false
  }
  if (signature.length !== 64) return false
  try {
    return await crypto.subtle.verify({ name: 'Ed25519' }, key, signature as BufferSource, message as BufferSource)
  } catch {
    return false
  }
}
