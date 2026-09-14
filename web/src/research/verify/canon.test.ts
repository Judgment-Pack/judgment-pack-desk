import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { CanonError, bytesToHex, canonicalText, compareCodePoints, parseJsonText } from './canon'
import { keyIdOf, receiptSigningInput, sealSigningInput, verifyEd25519 } from './receipt'

const FIXTURES = join(import.meta.dirname, 'fixtures')

interface CanonVector {
  note: string
  inputJson: string
  expectedHex?: string
  reject?: boolean
}

describe('the canonical form answers to the gateway corpus', () => {
  const vectors = (JSON.parse(readFileSync(join(FIXTURES, 'canon.json'), 'utf8')) as { vectors: CanonVector[] })
    .vectors
  it('reads every vector the corpus carries', () => {
    expect(vectors.length).toBe(30)
  })
  for (const vector of vectors) {
    it(vector.note, () => {
      if (vector.reject) {
        expect(() => canonicalText(vector.inputJson)).toThrow(CanonError)
        return
      }
      expect(bytesToHex(canonicalText(vector.inputJson))).toBe(vector.expectedHex)
    })
  }
})

describe('the parser is strict where a browser is lenient', () => {
  it('keeps a number literal and refuses a duplicate member', () => {
    const node = parseJsonText('{"a": 1, "b": 1.0}')
    expect(node).toMatchObject({
      kind: 'object',
      members: [
        { name: 'a', value: { kind: 'number', literal: '1' } },
        { name: 'b', value: { kind: 'number', literal: '1.0' } }
      ]
    })
    expect(() => parseJsonText('{"a":1,"a":2}')).toThrow(/duplicate/)
    expect(() => parseJsonText('{"a":1} x')).toThrow(/trailing/)
    expect(() => parseJsonText('"\\ud800"')).toThrow(/surrogate/)
    expect(() => parseJsonText('"\ud800"')).toThrow(/surrogate/)
    expect(() => parseJsonText('"a\nb"')).toThrow(/control/)
  })
  it('orders by code point, not by UTF-16 code unit', () => {
    // U+FF5E (one code unit) sorts after U+1F600 (a surrogate pair) by code
    // point, and before it by code unit.
    expect(compareCodePoints('\u{1F600}', '\uff5e')).toBeGreaterThan(0)
    expect('\u{1F600}' < '\uff5e').toBe(true)
  })
})

describe('signatures and key ids answer to the corpus', () => {
  const vectors = (
    JSON.parse(readFileSync(join(FIXTURES, 'ed25519-vectors.json'), 'utf8')) as {
      vectors: { publicKey: string; message: string; signature: string }[]
    }
  ).vectors
  it('verifies every third-party vector and refuses a flipped bit', async () => {
    expect(vectors.length).toBeGreaterThan(3)
    for (const vector of vectors) {
      const message = Uint8Array.from(Buffer.from(vector.message, 'hex'))
      expect(await verifyEd25519(vector.publicKey, message, vector.signature)).toBe(true)
      const flipped = (parseInt(vector.signature.slice(0, 2), 16) ^ 1).toString(16).padStart(2, '0') + vector.signature.slice(2)
      expect(await verifyEd25519(vector.publicKey, message, flipped)).toBe(false)
    }
    expect(await verifyEd25519('zz', new Uint8Array(), vectors[0]!.signature)).toBe(false)
    expect(await verifyEd25519(vectors[0]!.publicKey, new Uint8Array(), 'ab')).toBe(false)
  })
  it('derives the corpus key id from the corpus public key', async () => {
    const publicKey = readFileSync(join(FIXTURES, 'TEST-PUBLIC-KEY'), 'utf8').trim()
    expect(await keyIdOf(publicKey)).toBe('ddb406e95cad582adc111a7d6fbff25d')
  })
  it('signs a corpus receipt and seal under the stated prefixes', async () => {
    const publicKey = readFileSync(join(FIXTURES, 'TEST-PUBLIC-KEY'), 'utf8').trim()
    const store = JSON.parse(readFileSync(join(FIXTURES, 'stores', 'v3-valid-sealed.json'), 'utf8')) as {
      files: Record<string, string>
      registry: string
    }
    const receipt = parseJsonText(store.files['receipts/s1/1.json']!)
    const signature = (receipt.kind === 'object' && receipt.members.find((m) => m.name === 'signature')?.value) || null
    expect(signature?.kind).toBe('string')
    expect(
      await verifyEd25519(publicKey, receiptSigningInput(receipt, '3'), (signature as { value: string }).value)
    ).toBe(true)
    // The wrong prefix is the wrong signature.
    expect(
      await verifyEd25519(publicKey, receiptSigningInput(receipt, '2'), (signature as { value: string }).value)
    ).toBe(false)
    const seal = parseJsonText(store.registry.trim())
    const sealSignature = seal.kind === 'object' ? seal.members.find((m) => m.name === 'signature')?.value : undefined
    expect(
      await verifyEd25519(publicKey, sealSigningInput(seal), (sealSignature as { value: string }).value)
    ).toBe(true)
  })
})
