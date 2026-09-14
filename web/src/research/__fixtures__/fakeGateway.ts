/**
 * A gateway for tests: mints version 3 acquisition receipts and seals under
 * the corpus test seed (`verify/fixtures/TEST-SEED`, published by the gateway
 * for exactly this — constructing vectors — and signing nothing real), in the
 * shape the real gateway writes, over the canonical form the verifier checks.
 * It lets a run be driven end to end against fixture provider answers with
 * receipts that verify under `TEST-PUBLIC-KEY`, and it lets a test tamper
 * with one thing and watch the verifier catch it.
 */
import { createPrivateKey, sign } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import type { Acquired } from '../gatewayClient'
import { bytesToHex, canonicalText, parseJsonText, type JsonNode } from '../verify/canon'
import { RECEIPT_PREFIX_3, SEAL_PREFIX } from '../verify/receipt'

const FIXTURES = join(import.meta.dirname, '..', 'verify', 'fixtures')
const SEED = readFileSync(join(FIXTURES, 'TEST-SEED'), 'utf8').trim()
export const TEST_PUBLIC_KEY = readFileSync(join(FIXTURES, 'TEST-PUBLIC-KEY'), 'utf8').trim()
const PKCS8_ED25519_PREFIX = '302e020100300506032b657004220420'
const privateKey = createPrivateKey({ key: Buffer.from(PKCS8_ED25519_PREFIX + SEED, 'hex'), format: 'der', type: 'pkcs8' })

function signHex(message: Uint8Array): string {
  return bytesToHex(new Uint8Array(sign(null, message, privateKey)))
}

async function sha256(bytes: Uint8Array): Promise<string> {
  return bytesToHex(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes as BufferSource)))
}

/** The key id the corpus public key yields. */
export const TEST_KEY_ID = 'ddb406e95cad582adc111a7d6fbff25d'

export interface FakeGateway {
  authority: string
  /** One acquire: the provider's answer as the adapter would envelope it. */
  acquire(session: string, source: string, providerAnswer: unknown, statement?: unknown): Promise<Acquired>
  seal(session: string): Promise<void>
  registry(): Promise<string>
  /** Every receipt minted, by session, for a test that tampers. */
  receipts: Map<string, string[]>
  sealsText: string[]
}

export function fakeGateway(authority = 'gateway:test'): FakeGateway {
  const receipts = new Map<string, string[]>()
  const seals: string[] = []
  return {
    authority,
    receipts,
    sealsText: seals,
    async acquire(session, source, providerAnswer, statement) {
      const chain = receipts.get(session) ?? []
      const resultText = JSON.stringify({ status: 200, headers: { 'content-type': 'application/json' }, bodyEncoding: 'json', body: providerAnswer })
      const resultCanon = canonicalText(resultText)
      const resultDigest = 'sha256:' + (await sha256(resultCanon))
      const stamp = '2026-09-14T12:00:00Z'
      const unsigned = {
        acquisition: {
          adapter: { digest: 'sha256:' + 'ab'.repeat(32), name: 'adapter-http', version: '0' },
          endpoint: `https://provider.example/${source}`,
          observedAt: stamp,
          peerIdentity: 'tls:sha256:' + 'cd'.repeat(32),
          schema: null,
          shape: 'http',
          snapshot: null,
          statement: 'sha256:' + (await sha256(new TextEncoder().encode(JSON.stringify(statement ?? { source })))),
          upstreamToken: null
        },
        argumentsCommitment: 'sha256:' + 'ef'.repeat(32),
        authority,
        callIndex: chain.length,
        caller: null,
        keyId: TEST_KEY_ID,
        kind: 'acquisition',
        prevSignature: chain.length === 0 ? null : JSON.parse(chain[chain.length - 1]!).signature,
        receiptVersion: '3',
        resultDigest,
        servedAt: stamp,
        sessionId: session,
        source
      }
      const covered = canonicalText(JSON.stringify(unsigned))
      const input = new Uint8Array(RECEIPT_PREFIX_3.length + covered.length)
      input.set(new TextEncoder().encode(RECEIPT_PREFIX_3), 0)
      input.set(covered, RECEIPT_PREFIX_3.length)
      const signature = signHex(input)
      const receiptText = new TextDecoder().decode(canonicalText(JSON.stringify({ ...unsigned, signature })))
      chain.push(receiptText)
      receipts.set(session, chain)
      const text = `{"result":${new TextDecoder().decode(resultCanon)},"receipt":${receiptText},"salts":{"args":"00","statement":"11"}}`
      const parsed = parseJsonText(text)
      const member = (name: string): JsonNode => (parsed.kind === 'object' ? parsed.members.find((m) => m.name === name)!.value : parsed)
      return { text, result: member('result'), receipt: member('receipt'), salts: { args: '00', statement: '11' } }
    },
    async seal(session) {
      const count = (receipts.get(session) ?? []).length
      const unsigned = { finalCount: count, keyId: TEST_KEY_ID, sealedAt: '2026-09-14T12:00:01Z', sessionId: session }
      const covered = canonicalText(JSON.stringify(unsigned))
      const input = new Uint8Array(SEAL_PREFIX.length + covered.length)
      input.set(new TextEncoder().encode(SEAL_PREFIX), 0)
      input.set(covered, SEAL_PREFIX.length)
      const signature = signHex(input)
      seals.push(new TextDecoder().decode(canonicalText(JSON.stringify({ ...unsigned, signature }))))
    },
    async registry() {
      return seals.map((line) => line + '\n').join('')
    }
  }
}
