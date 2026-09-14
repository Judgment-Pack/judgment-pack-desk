import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fakeGateway, TEST_PUBLIC_KEY } from '../__fixtures__/fakeGateway'
import { bytesToHex, hexToBytes, parseJsonText, stringMember } from './canon'
import { isSmallOrderPoint, verifyEd25519Pure } from './ed25519'
import { receiptSigningInput, resetEd25519ProbeForTesting, sealSigningInput, verifyEd25519 } from './receipt'

const FIXTURES = join(import.meta.dirname, 'fixtures')
const vectors = (
  JSON.parse(readFileSync(join(FIXTURES, 'ed25519-vectors.json'), 'utf8')) as {
    vectors: { publicKey: string; message: string; signature: string }[]
  }
).vectors

afterEach(() => {
  vi.restoreAllMocks()
  resetEd25519ProbeForTesting()
})

describe('the pure Ed25519 verifier', () => {
  it('verifies every corpus vector and refuses a flipped bit, a wrong key, and a non-canonical S', async () => {
    for (const vector of vectors) {
      const message = hexToBytes(vector.message)
      expect(await verifyEd25519Pure(hexToBytes(vector.publicKey), message, hexToBytes(vector.signature))).toBe(true)
      const flipped = hexToBytes(vector.signature)
      flipped[3] = flipped[3]! ^ 0x10
      expect(await verifyEd25519Pure(hexToBytes(vector.publicKey), message, flipped)).toBe(false)
      const other = hexToBytes(vectors[(vectors.indexOf(vector) + 1) % vectors.length]!.publicKey)
      expect(await verifyEd25519Pure(other, message, hexToBytes(vector.signature))).toBe(false)
      // S past the group order: the same signature with L added to S, which
      // is the same scalar mod L and must still be refused as non-canonical.
      const big = hexToBytes(vector.signature)
      let s = 0n
      for (let i = 63; i >= 32; i -= 1) s = (s << 8n) | BigInt(big[i]!)
      s += (1n << 252n) + 27742317777372353535851937790883648493n
      for (let i = 32; i < 64; i += 1) {
        big[i] = Number(s & 0xffn)
        s >>= 8n
      }
      expect(await verifyEd25519Pure(hexToBytes(vector.publicKey), message, big)).toBe(false)
    }
    expect(await verifyEd25519Pure(new Uint8Array(31), new Uint8Array(), new Uint8Array(64))).toBe(false)
    // The identity as the key and as R, with S = 0: both sides of the
    // equation are the identity for every message, and it is refused.
    const identity = new Uint8Array(32)
    identity[0] = 1
    const forged = new Uint8Array(64)
    forged.set(identity, 0)
    expect(await verifyEd25519Pure(identity, new TextEncoder().encode('anything'), forged)).toBe(false)
    expect(isSmallOrderPoint(identity)).toBe(true)
    expect(isSmallOrderPoint(hexToBytes(vectors[0]!.publicKey))).toBe(false)
    expect(await verifyEd25519(bytesToHex(identity), new TextEncoder().encode('anything'), bytesToHex(forged))).toBe(false)
    expect(await verifyEd25519Pure(new Uint8Array(32).fill(0xff), new Uint8Array(), new Uint8Array(64))).toBe(false)
  })

  it('verifies a receipt and a seal the fake gateway signed under the corpus seed', async () => {
    const gateway = fakeGateway()
    const acquired = await gateway.acquire('s1', 'read', { data: { title: 'T' } })
    await gateway.seal('s1')
    const receipt = acquired.receipt
    const signature = stringMember(receipt, 'signature')!
    expect(await verifyEd25519Pure(hexToBytes(TEST_PUBLIC_KEY), receiptSigningInput(receipt, '3'), hexToBytes(signature))).toBe(true)
    const seal = parseJsonText((await gateway.registry()).trim())
    expect(await verifyEd25519Pure(hexToBytes(TEST_PUBLIC_KEY), sealSigningInput(seal), hexToBytes(stringMember(seal, 'signature')!))).toBe(true)
  })

  it('is what verifyEd25519 uses where WebCrypto has no Ed25519, or a broken one, and agrees with it where it works', async () => {
    const original = crypto.subtle.importKey.bind(crypto.subtle)
    const importKey = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(async (format, key, algorithm, extractable, usages) => {
      if ((algorithm as { name?: string })?.name === 'Ed25519') throw new DOMException('Unrecognized name.', 'NotSupportedError')
      return original(format as never, key as never, algorithm as never, extractable, usages)
    })
    const vector = vectors[0]!
    expect(await verifyEd25519(vector.publicKey, hexToBytes(vector.message), vector.signature)).toBe(true)
    const wrong = vector.signature.slice(0, -2) + (vector.signature.endsWith('00') ? '01' : '00')
    expect(await verifyEd25519(vector.publicKey, hexToBytes(vector.message), wrong)).toBe(false)
    // The probe ran once and was remembered; the algorithm was asked for
    // exactly once even across two verifications.
    expect(importKey.mock.calls.filter(([, , algorithm]) => (algorithm as { name?: string })?.name === 'Ed25519')).toHaveLength(1)
    importKey.mockRestore()
    resetEd25519ProbeForTesting()
    // An implementation that imports the key and then cannot verify: the
    // probe verifies a known signature, so this one is found out too.
    const verify = vi.spyOn(crypto.subtle, 'verify').mockImplementation(async () => {
      throw new DOMException('not here', 'NotSupportedError')
    })
    expect(await verifyEd25519(vector.publicKey, hexToBytes(vector.message), vector.signature)).toBe(true)
    expect(await verifyEd25519(vector.publicKey, hexToBytes(vector.message), wrong)).toBe(false)
    verify.mockRestore()
    resetEd25519ProbeForTesting()
    // And an implementation that answers false to a known-good signature.
    const lying = vi.spyOn(crypto.subtle, 'verify').mockImplementation(async () => false)
    expect(await verifyEd25519(vector.publicKey, hexToBytes(vector.message), vector.signature)).toBe(true)
    lying.mockRestore()
    resetEd25519ProbeForTesting()
    expect(await verifyEd25519(vector.publicKey, hexToBytes(vector.message), vector.signature)).toBe(true)
  })
})
