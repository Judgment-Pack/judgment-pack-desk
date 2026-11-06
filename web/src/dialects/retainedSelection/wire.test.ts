import {readFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {describe, expect, it} from 'vitest'
import {readRetainedSelectionWire, retainedSelectionQuote, writeRetainedSelectionWire, type RetainedSelectionSource} from './wire'

interface FixtureRow {
 index: number; id: string; inputHex: string; inputBytes: number; inputSha256: string
 metadataJSON: string; frameOffset: number; payloadOffset: number
}
interface Fixture {
 name: string; file: string; source: RetainedSelectionSource; viewId: string
 headerJSON: string; headerBytes: number; selectionSha256: string; indexSha256: string
 endJSON: string; endOffset: number; rows: FixtureRow[]; bytes: number; sha256: string
}
interface Mutation {
 name: string; base: string; edits: {offset: number; remove: number; hex: string}[]
 contentLength?: number; recordIds?: string[]
}
const root = resolve(import.meta.dirname, '../../../../internal/desk/testdata/retained-selection-wire')
const manifest = JSON.parse(readFileSync(resolve(root, 'manifest.json'), 'utf8')) as {positives: Fixture[]; invalidFixtures: Fixture[]; mutations: Mutation[]}
const fromHex = (s: string): Uint8Array => Uint8Array.from(Buffer.from(s.trim(), 'hex'))
const hex = (b: Uint8Array): string => Buffer.from(b).toString('hex')
const fixtureRaw = (f: Fixture): Uint8Array => fromHex(readFileSync(resolve(root, f.file), 'utf8'))
async function sha(raw: Uint8Array): Promise<string> { return hex(new Uint8Array(await crypto.subtle.digest('SHA-256', raw as BufferSource))) }
function join(parts: Uint8Array[]): Uint8Array {
 const out = new Uint8Array(parts.reduce((n, part) => n + part.length, 0))
 let at = 0
 for (const part of parts) { out.set(part, at); at += part.length }
 return out
}
async function* chunks(raw: Uint8Array, split?: number): AsyncGenerator<Uint8Array> {
 if (split === undefined) { yield raw; return }
 yield raw.subarray(0, split)
 yield raw.subarray(split)
}
async function* byteChunks(raw: Uint8Array): AsyncGenerator<Uint8Array> { for (const b of raw) yield Uint8Array.of(b) }
async function* rows(f: Fixture): AsyncGenerator<{id: string; input: Uint8Array}> { for (const row of f.rows) yield {id: row.id, input: fromHex(row.inputHex)} }
async function positive(f: Fixture, input: AsyncIterable<Uint8Array>): Promise<void> {
 let calls = 0
 const result = await readRetainedSelectionWire(input, f.source, f.bytes, (row, raw) => {
  const want = f.rows[calls++]!
  expect(row).toEqual({index: want.index, id: want.id, inputBytes: want.inputBytes, inputSha256: want.inputSha256, offset: want.payloadOffset})
  expect(hex(raw)).toBe(want.inputHex)
 })
 expect(calls).toBe(f.rows.length)
 expect(result.rows).toHaveLength(f.rows.length)
 expect(result.header.selectionSha256).toBe(f.selectionSha256)
 expect(result.indexSha256).toBe(f.indexSha256)
 expect(result.header).toEqual(JSON.parse(f.headerJSON))
 expect(result.bytes).toBe(f.bytes)
 expect(result.inputBytesTotal).toBe(f.rows.reduce((n, r) => n + r.inputBytes, 0))
}

describe('inactive retained selection wire codecs', () => {
 it.each(manifest.positives)('reads/writes independent shared $name bytes and literal accounting', async f => {
  const raw = fixtureRaw(f)
  expect(raw.length).toBe(f.bytes)
  expect(await sha(raw)).toBe(f.sha256)
  await positive(f, chunks(raw))
  await positive(f, byteChunks(raw))
  const written: Uint8Array[] = [] // Only tiny test fixtures accumulate output.
  expect(await writeRetainedSelectionWire(part => { written.push(part.slice()) }, f.source, f.viewId, rows(f))).toBe(f.bytes)
  expect(hex(join(written))).toBe(hex(raw))
  for (const row of f.rows) {
   expect(new TextEncoder().encode(row.metadataJSON).length).toBe(111 + String(row.index).length + String(row.inputBytes).length + new TextEncoder().encode(retainedSelectionQuote(row.id)).length)
  }
  const tupleBytes = new TextEncoder().encode(JSON.stringify(f.source.conversionContract)).length
  const quoted = (s: string): number => new TextEncoder().encode(retainedSelectionQuote(s)).length
  expect(f.headerBytes).toBe(412 + quoted(f.source.preparationId) + tupleBytes + quoted(f.source.origin) + String(f.rows.length).length)
  expect(f.endJSON.length).toBe(109 + String(f.rows.length).length + String(f.rows.reduce((n, r) => n + r.inputBytes, 0)).length)
 })

 it('accepts every two-chunk split of compact framing, plus one-byte chunks', async () => {
  const f = manifest.positives[0]!, raw = fixtureRaw(f)
  for (let split = 0; split <= raw.length; split++) await positive(f, chunks(raw, split))
  await positive(f, byteChunks(raw))
 }, 60000)

 it('rejects truncation at every compact byte, without returning a ready result', async () => {
  const f = manifest.positives[0]!, raw = fixtureRaw(f)
  for (let cut = 0; cut < raw.length; cut++) await expect(readRetainedSelectionWire(chunks(raw.subarray(0, cut)), f.source, f.bytes, () => {})).rejects.toThrow()
 }, 60000)

 it.each(manifest.mutations)('rejects shared mutation $name', async mutation => {
  const f = manifest.positives.find(v => v.name === mutation.base)!
  let raw = fixtureRaw(f)
  for (const edit of mutation.edits) raw = join([raw.subarray(0, edit.offset), fromHex(edit.hex), raw.subarray(edit.offset + edit.remove)])
  const source = mutation.recordIds === undefined ? f.source : {...f.source, recordIds: mutation.recordIds}
  await expect(readRetainedSelectionWire(byteChunks(raw), source, mutation.contentLength ?? raw.length, () => {})).rejects.toThrow()
 })

 it.each(manifest.invalidFixtures)('rejects self-consistent malformed bytes $name in both directions', async f => {
  const raw = fixtureRaw(f)
  expect(await sha(raw)).toBe(f.sha256)
  await expect(readRetainedSelectionWire(chunks(raw), f.source, f.bytes, () => {})).rejects.toThrow()
  await expect(writeRetainedSelectionWire(() => {}, f.source, f.viewId, rows(f))).rejects.toThrow()
 })

 it('uses exact Go string escaping and refuses unpaired source scalars', () => {
  expect(retainedSelectionQuote('<>&\u2028\u2029\x00\b\f\n\r\t"\\🙂')).toBe('"\\u003c\\u003e\\u0026\\u2028\\u2029\\u0000\\b\\f\\n\\r\\t\\"\\\\🙂"')
  expect(() => retainedSelectionQuote('\ud800')).toThrow()
  expect(() => retainedSelectionQuote('\udc00')).toThrow()
 })

 it('keeps callbacks provisional and closes the iterator on refusal or consumer failure', async () => {
  const f = manifest.positives[1]!, raw = fixtureRaw(f)
  let closed = 0, calls = 0, provisionalView = ''
  async function* owned(): AsyncGenerator<Uint8Array> { try { yield raw } finally { closed++ } }
  await expect(readRetainedSelectionWire(owned(), f.source, f.bytes, () => { calls++; throw Error('consumer stopped') }, h => { provisionalView = h.viewId })).rejects.toThrow('consumer stopped')
  expect(calls).toBe(1); expect(closed).toBe(1); expect(provisionalView).toBe(f.viewId)
  // The first verified row is not readiness: a corrupt final end must refuse.
  const corrupt = raw.slice(); corrupt[corrupt.length - 2] = 0
  await expect(readRetainedSelectionWire(ownedCorrupt(), f.source, corrupt.length, () => { calls++ })).rejects.toThrow()
  expect(calls).toBe(3)
  async function* ownedCorrupt(): AsyncGenerator<Uint8Array> { yield corrupt }
 })

 it('awaits a row consumer before pulling the next source chunk', async () => {
  const f = manifest.positives[1]!, raw = fixtureRaw(f)
  const boundary = f.rows[0]!.payloadOffset + f.rows[0]!.inputBytes
  let pulls = 0, release!: () => void, first!: () => void
  const blocked = new Promise<void>(resolve => { release = resolve })
  const entered = new Promise<void>(resolve => { first = resolve })
  async function* controlled(): AsyncGenerator<Uint8Array> {
   pulls++; yield raw.subarray(0, boundary)
   pulls++; yield raw.subarray(boundary)
  }
  let calls = 0
  const pending = readRetainedSelectionWire(controlled(), f.source, raw.length, async () => { if (++calls === 1) { first(); await blocked } })
  await entered
  expect(pulls).toBe(1)
  release()
  await pending
  expect(pulls).toBe(2)
 })

 it('copies captured IDs before awaits and isolates callback descriptor edits', async () => {
  const f = manifest.positives[0]!, raw = fixtureRaw(f)
  const source = {...f.source, recordIds: [...f.source.recordIds]}
  const pending = readRetainedSelectionWire(chunks(raw), source, raw.length, row => { row.id = 'changed'; row.inputBytes = 1 })
  source.recordIds[0] = 'changed after capture'
  expect((await pending).rows[0]!.id).toBe('r')
 })

 it('rejects writer count/order/empty rows, sink failure and actual request-owner violations', async () => {
  const f = manifest.positives[0]!
  async function* supplied(values: {id: string; input: Uint8Array}[]): AsyncGenerator<{id: string; input: Uint8Array}> { yield* values }
  // Match the test realm's Uint8Array, as fromHex does for the shared fixtures.
  const r = {id: 'r', input: Uint8Array.from(new TextEncoder().encode('{}'))}
  for (const values of [[], [r, r], [{...r, id: 'wrong'}], [{...r, input: new Uint8Array()}]]) await expect(writeRetainedSelectionWire(() => {}, f.source, f.viewId, supplied(values))).rejects.toThrow()
  await expect(writeRetainedSelectionWire(() => { throw Error('sink stopped') }, f.source, f.viewId, rows(f))).rejects.toThrow('sink stopped')
  const longID = '<&'.repeat(10000)
  await expect(writeRetainedSelectionWire(() => {}, {...f.source, recordIds: [longID]}, f.viewId, supplied([{...r, id: longID}]))).resolves.toBeGreaterThan(100000)
  await expect(writeRetainedSelectionWire(() => {}, {...f.source, recordIds: [longID], requestBytes: new TextEncoder().encode(longID).length - 1}, f.viewId, supplied([{...r, id: longID}]))).rejects.toThrow()
  for (const length of [0, -1, Number.MAX_SAFE_INTEGER, Infinity, NaN, 1.5]) await expect(readRetainedSelectionWire(chunks(fixtureRaw(f)), f.source, length, () => {})).rejects.toThrow()
 })
})
