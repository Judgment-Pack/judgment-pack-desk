/**
 * What a receipt's and a seal's signatures cover (gateway SPEC.md §1.2, §1.2a
 * and §3), the key id a public key yields, and Ed25519 verification through
 * WebCrypto — the browser's own implementation, under the key the desk-level
 * file pinned and never one a gateway handed the page.
 */
import { CanonError, bytesToHex, canonicalize, hexToBytes, memberOf, type JsonNode } from './canon'
import { isSmallOrderPoint, verifyEd25519Pure } from './ed25519'

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

/**
 * Whether this WebCrypto speaks Ed25519, decided once: Chrome before 137,
 * among others, does not, and the pure implementation is what verifies then.
 * A refusal is remembered, so a browser without the algorithm does not ask
 * again on every receipt; a browser with it is used for every one.
 */
let webCryptoEd25519: Promise<boolean> | null = null

/**
 * A known-good vector (RFC 8032 §7.1, test 1: the empty message under the
 * first seed), so the probe establishes that verification *works* here and
 * not merely that a key imports.
 */
const PROBE_PUBLIC = 'd75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a'
const PROBE_SIGNATURE =
  'e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b'

function webCryptoSpeaksEd25519(): Promise<boolean> {
  webCryptoEd25519 ??= (async () => {
    try {
      const key = await crypto.subtle.importKey('raw', hexToBytes(PROBE_PUBLIC) as BufferSource, { name: 'Ed25519' }, false, ['verify'])
      return (await crypto.subtle.verify({ name: 'Ed25519' }, key, hexToBytes(PROBE_SIGNATURE) as BufferSource, new Uint8Array() as BufferSource)) === true
    } catch {
      // No algorithm, a partial implementation, or anything else thrown at
      // the name: the verifier in this repository is what verifies then.
      return false
    }
  })()
  return webCryptoEd25519
}

/** For tests: forget what was decided about WebCrypto. */
export function resetEd25519ProbeForTesting(): void {
  webCryptoEd25519 = null
}

/** Ed25519 over `message`, under the raw 32-byte public key given in hex. */
export async function verifyEd25519(
  publicKeyHex: string,
  message: Uint8Array,
  signatureHex: string
): Promise<boolean> {
  let publicKey: Uint8Array
  let signature: Uint8Array
  try {
    publicKey = hexToBytes(publicKeyHex)
    signature = hexToBytes(signatureHex)
  } catch {
    return false
  }
  if (publicKey.length !== 32 || signature.length !== 64) return false
  // A small-order key or R is refused before either backend is asked: the
  // identity as a key would verify every message under S = 0, and neither
  // WebCrypto nor Go's verifier refuses it on its own.
  if (isSmallOrderPoint(publicKey) || isSmallOrderPoint(signature.subarray(0, 32))) return false
  if (!(await webCryptoSpeaksEd25519())) return verifyEd25519Pure(publicKey, message, signature)
  let key: CryptoKey
  try {
    key = await crypto.subtle.importKey('raw', publicKey as BufferSource, { name: 'Ed25519' }, false, ['verify'])
  } catch {
    return false
  }
  try {
    return await crypto.subtle.verify({ name: 'Ed25519' }, key, signature as BufferSource, message as BufferSource)
  } catch {
    return false
  }
}
