import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { parseJsonText, stringMember, type JsonNode } from './canon'
import { verifySession, type Finding, type HeldReceipt } from './session'
import { fakeGateway } from '../__fixtures__/fakeGateway'

const FIXTURES = join(import.meta.dirname, 'fixtures')
const PUBLIC_KEY = readFileSync(join(FIXTURES, 'TEST-PUBLIC-KEY'), 'utf8').trim()

interface StoreVector {
  name: string
  authority: string
  files: Record<string, string>
  registry: string
  decisionRecords?: unknown
  expected: { ok: boolean; findings: Record<string, unknown>[] }
}

function vector(name: string): StoreVector {
  return JSON.parse(readFileSync(join(FIXTURES, 'stores', `${name}.json`), 'utf8')) as StoreVector
}

/** The receipts of one session as the page would hold them, with their artifacts. */
function held(store: StoreVector, sessionId: string): HeldReceipt[] {
  const entries = Object.entries(store.files)
    .filter(([path]) => path.startsWith(`receipts/${sessionId}/`))
    .map(([path, text]) => ({ index: Number(path.split('/')[2]!.replace(/\.json$/, '')), text }))
    .sort((a, b) => a.index - b.index)
  return entries.map(({ text }) => {
    let receipt: JsonNode
    try {
      receipt = parseJsonText(text)
    } catch {
      // A receipt that does not parse is held as a string node, which the
      // verifier reports as malformed exactly as a file that would not parse.
      return { receipt: { kind: 'string', value: text }, result: null }
    }
    const digest = stringMember(receipt, 'resultDigest') ?? ''
    const artifact = store.files[`artifacts/${digest.replace(/^sha256:/, '')}`]
    return { receipt, result: artifact === undefined ? null : parseJsonText(artifact) }
  })
}

function normalize(findings: Record<string, unknown>[] | Finding[]): string[] {
  return findings
    .map((finding) => JSON.stringify([finding.sessionId, finding.callIndex ?? null, finding.status, (finding as { file?: string }).file ?? null]))
    .sort()
}

describe('the session verifier answers to the corpus store vectors it can see', () => {
  const names = readdirSync(join(FIXTURES, 'stores'))
    .filter((name) => name.endsWith('.json'))
    .map((name) => name.replace(/\.json$/, ''))
  // A vector is in scope where its expectations are about acquisition receipts
  // and the registry: no decision records, no action receipts, no citation
  // findings. Those are the §4 steps a live consumer holding one session cannot
  // perform, and the verifier says so rather than pretending.
  const inScope = names.filter((name) => {
    const store = vector(name)
    if (store.decisionRecords !== undefined) return false
    const statuses = new Set(store.expected.findings.map((f) => f.status))
    for (const status of ['citation-unresolved', 'decision-record-mismatch', 'record-citation-unresolved', 'record-citation-malformed']) {
      if (statuses.has(status)) return false
    }
    return true
  })
  it('keeps a majority of the corpus in scope', () => {
    expect(inScope.length).toBeGreaterThanOrEqual(9)
  })
  for (const name of inScope) {
    it(name, async () => {
      const store = vector(name)
      const sessions = [...new Set(Object.keys(store.files).filter((p) => p.startsWith('receipts/')).map((p) => p.split('/')[1]!))]
      for (const sessionId of sessions) {
        const verdict = await verifySession({
          sessionId,
          authority: store.authority,
          publicKeyHex: PUBLIC_KEY,
          receipts: held(store, sessionId),
          registryText: store.registry
        })
        const expected = store.expected.findings.filter((f) => f.sessionId === sessionId)
        expect(normalize(verdict.findings), `${name}/${sessionId}`).toEqual(normalize(expected))
        if (sessions.length === 1) expect(verdict.ok, `${name}: ok`).toBe(store.expected.ok)
      }
    })
  }
})

describe('the verifier catches what a held session can be tampered with', () => {
  const store = vector('v3-valid-sealed')
  const base = () => ({
    sessionId: 's1',
    authority: store.authority,
    publicKeyHex: PUBLIC_KEY,
    receipts: held(store, 's1'),
    registryText: store.registry
  })
  const statusesOf = (findings: Finding[]) => findings.map((f) => f.status)

  it('passes the untouched session, session-scoped', async () => {
    const verdict = await verifySession(base())
    expect(verdict.ok).toBe(true)
    expect(verdict.sealed).toBe(true)
    expect(verdict.keyId).toBe('ddb406e95cad582adc111a7d6fbff25d')
    expect(statusesOf(verdict.findings)).toEqual(['ok', 'ok', 'ok'])
  })
  it('refuses a receipt under another key, and a seal under another key', async () => {
    const other = '03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8'
    const verdict = await verifySession({ ...base(), publicKeyHex: other })
    expect(verdict.ok).toBe(false)
    expect(statusesOf(verdict.findings)).toEqual(['key-mismatch', 'key-mismatch', 'key-mismatch', 'unregistered-session'])
  })
  it('refuses a receipt whose signature does not verify', async () => {
    const input = base()
    const text = store.files['receipts/s1/1.json']!.replace('"source":"corpus"', '"source":"corpus-edited"')
    input.receipts[1] = { ...input.receipts[1]!, receipt: parseJsonText(text) }
    const verdict = await verifySession(input)
    expect(statusesOf(verdict.findings)).toContain('signature-mismatch')
    expect(statusesOf(verdict.findings)).toContain('sequence-broken')
    expect(verdict.ok).toBe(false)
  })
  it('refuses an artifact that re-digests to something else, and a missing one', async () => {
    const input = base()
    input.receipts[0] = { ...input.receipts[0]!, result: parseJsonText('{"n":0,"session":"s1","extra":true}') }
    input.receipts[2] = { ...input.receipts[2]!, result: null }
    const verdict = await verifySession(input)
    expect(statusesOf(verdict.findings)).toEqual(['artifact-mismatch', 'ok', 'artifact-missing', 'sequence-broken'])
  })
  it('refuses a session whose tail was dropped, and one grown past its seal', async () => {
    const short = base()
    short.receipts = short.receipts.slice(0, 2)
    expect(statusesOf((await verifySession(short)).findings)).toEqual(['ok', 'ok', 'tail-rollback'])
    const long = base()
    long.receipts = [...long.receipts, long.receipts[2]!]
    const verdict = await verifySession(long)
    expect(statusesOf(verdict.findings)).toEqual(['ok', 'ok', 'ok', 'misfiled', 'count-exceeds-seal'])
  })
  it('refuses a seal that was altered, and a session the registry does not carry', async () => {
    const altered = base()
    altered.registryText = store.registry.replace('"finalCount":3', '"finalCount":2')
    expect(statusesOf((await verifySession(altered)).findings)).toEqual(['ok', 'ok', 'ok', 'unregistered-session'])
    const none = base()
    none.registryText = ''
    const verdict = await verifySession(none)
    expect(verdict.sealed).toBe(false)
    expect(verdict.ok).toBe(false)
  })
  it('refuses a receipt held out of order, and one for another authority', async () => {
    const swapped = base()
    ;[swapped.receipts[0], swapped.receipts[1]] = [swapped.receipts[1]!, swapped.receipts[0]!]
    expect(statusesOf((await verifySession(swapped)).findings)).toEqual(['misfiled', 'misfiled', 'ok', 'sequence-broken'])
    const elsewhere = base()
    elsewhere.authority = 'gateway:other'
    expect(statusesOf((await verifySession(elsewhere)).findings)).toEqual(['authority-mismatch', 'authority-mismatch', 'authority-mismatch'])
  })
  it('withholds an action receipt rather than half-checking it', async () => {
    const action = vector('v3-action-valid')
    const verdict = await verifySession({
      sessionId: 's2',
      authority: action.authority,
      publicKeyHex: PUBLIC_KEY,
      receipts: held(action, 's2'),
      registryText: action.registry
    })
    expect(verdict.ok).toBe(false)
    expect(statusesOf(verdict.findings)).toContain('action-unchecked')
  })
  it('drops a seal naming a foreign key id even where the signature is the real key’s, and takes the first loadable seal', async () => {
    const gateway = fakeGateway(store.authority)
    const first = await gateway.acquire('t1', 'read', { n: 1 })
    await gateway.seal('t1')
    const genuine = gateway.sealsText[0]!
    // The same seal, its keyId renamed and re-signed under the real key: the
    // signature verifies, and the key id rule alone has to drop it.
    const foreign = await gateway.resign({ ...JSON.parse(genuine), keyId: 'ab'.repeat(16) })
    const held = [{ receipt: first.receipt, result: first.result }]
    const foreignOnly = await verifySession({ sessionId: 't1', authority: store.authority, publicKeyHex: PUBLIC_KEY, receipts: held, registryText: foreign + '\n' })
    expect(statusesOf(foreignOnly.findings)).toEqual(['ok', 'unregistered-session'])
    // Two loadable seals: the first decides, whatever the second counts.
    const second = await gateway.resign({ ...JSON.parse(genuine), finalCount: 7 })
    const firstWins = await verifySession({ sessionId: 't1', authority: store.authority, publicKeyHex: PUBLIC_KEY, receipts: held, registryText: genuine + '\n' + second + '\n' })
    expect(firstWins.ok).toBe(true)
    const secondFirst = await verifySession({ sessionId: 't1', authority: store.authority, publicKeyHex: PUBLIC_KEY, receipts: held, registryText: second + '\n' + genuine + '\n' })
    expect(statusesOf(secondFirst.findings)).toEqual(['ok', 'tail-rollback'])
  })
  it('breaks the chain on a genuine receipt whose prevSignature names another, same version', async () => {
    const gateway = fakeGateway(store.authority)
    const a = await gateway.acquire('c1', 'read', { n: 1 })
    const b = await gateway.acquire('c1', 'read', { n: 2 })
    // Receipt 1 re-signed with a prevSignature that is not receipt 0's: every
    // receipt verifies on its own, the version is one, and only the link
    // between them is wrong.
    const relinked = await gateway.resign({ ...JSON.parse(store.files['receipts/s1/0.json']!.trim().length ? gateway.receipts.get('c1')![1]! : '{}'), prevSignature: 'ab'.repeat(64) })
    await gateway.seal('c1')
    const verdict = await verifySession({
      sessionId: 'c1',
      authority: store.authority,
      publicKeyHex: PUBLIC_KEY,
      receipts: [{ receipt: a.receipt, result: a.result }, { receipt: parseJsonText(relinked), result: b.result }],
      registryText: await gateway.registry()
    })
    expect(statusesOf(verdict.findings)).toEqual(['ok', 'ok', 'chain-broken'])
  })
  it('reports a receipt outside the canonical domain as malformed before its key is looked at', async () => {
    const input = base()
    const text = store.files['receipts/s1/1.json']!.replace('"keyId":"ddb406e95cad582adc111a7d6fbff25d"', '"extra":1.5,"keyId":"ab"')
    input.receipts[1] = { ...input.receipts[1]!, receipt: parseJsonText(text) }
    const verdict = await verifySession(input)
    expect(verdict.findings[1]).toMatchObject({ status: 'malformed', file: '1.json' })
  })
  it('holds nothing, verifies nothing', async () => {
    const verdict = await verifySession({ ...base(), receipts: [] })
    expect(verdict.ok).toBe(false)
  })
})
