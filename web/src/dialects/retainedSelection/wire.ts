import {DIALECT_RETAINED_COLUMN_DRAFT, type DialectConversionContract} from '@judgment-pack/harness/dialects'
import {sha256Hex} from '../../research/verify/receipt'

// Inactive, pure transport. Admission, JSON semantics and source authenticity
// belong to B3. Input payloads never pass through JSON.parse/stringify here.
export const retainedSelectionMediaType = 'application/vnd.jpack-desk.retained-selection.v1'
export const retainedSelectionMaxInputBytes = 37748736
const magic = 'JPACK-RS/1\n'
const idsDomain = 'jpack-desk/retained-selection/1:ids\0'
const indexDomain = 'jpack-desk/retained-selection/1:index\0'
const encoder = new TextEncoder()
const zeroSHA = '0'.repeat(64)
const zeroView = 'rv_' + '0'.repeat(48)
const shaPattern = /^[a-f0-9]{64}$/
const viewPattern = /^rv_[a-f0-9]{48}$/
const fail = (): never => { throw new Error('Invalid or incomplete retained selection stream.') }

export interface RetainedSelectionSource {
 preparationId: string
 revision: string
 packDigest: string
 conversionContract: DialectConversionContract
 origin: 'asserted' | 'generated'
 recordIds: readonly string[]
 /** Actual byte count of the captured inputRequest body, not a wire limit. */
 requestBytes: number
}
export interface RetainedSelectionHeader {
 wireVersion: 1
 viewId: string
 preparationId: string
 revision: string
 packDigest: string
 conversionContract: DialectConversionContract
 origin: 'asserted' | 'generated'
 rows: number
 maxInputBytes: number
 selectionSha256: string
}
export interface RetainedSelectionRow {
 index: number
 id: string
 inputBytes: number
 inputSha256: string
}
export interface RetainedSelectionDescriptor extends RetainedSelectionRow { offset: number }
interface End {rows: number; inputBytesTotal: number; indexSha256: string}
export interface RetainedSelectionResult {
 header: RetainedSelectionHeader
 rows: RetainedSelectionDescriptor[]
 inputBytesTotal: number
 /** Verified end-frame digest; available only after complete validation and EOF. */
 indexSha256: string
 bytes: number
}

// Scalar text only in metadata/source IDs. Opaque payload JSON may retain any
// original JSON escape spelling, including a literal backslash-u sequence.
function scalar(s: unknown): s is string { return typeof s === 'string' && !/[\uD800-\uDFFF]/u.test(s) }
/** Go encoding/json default string escaping, independent of object key order. */
export function retainedSelectionQuote(s: string): string {
 if (!scalar(s)) return fail()
 return JSON.stringify(s).replace(/[<>&\u2028\u2029]/g, ch => '\\u' + ch.charCodeAt(0).toString(16).padStart(4, '0'))
}
const q = retainedSelectionQuote
function contractJSON(c: DialectConversionContract): string {
 return '{"contract":' + q(c.contract) + ',"spec":{"version":' + q(c.spec.version) + ',"revision":' + q(c.spec.revision) + ',"schemaSha256":' + q(c.spec.schemaSha256) + ',"corpusSha256":' + q(c.spec.corpusSha256) + '},"profile":' + q(c.profile) + '}'
}
function headerJSON(h: RetainedSelectionHeader): string {
 return '{"wireVersion":' + h.wireVersion + ',"viewId":' + q(h.viewId) + ',"preparationId":' + q(h.preparationId) + ',"revision":' + q(h.revision) + ',"packDigest":' + q(h.packDigest) + ',"conversionContract":' + contractJSON(h.conversionContract) + ',"origin":' + q(h.origin) + ',"rows":' + h.rows + ',"maxInputBytes":' + h.maxInputBytes + ',"selectionSha256":' + q(h.selectionSha256) + '}'
}
function rowJSON(r: RetainedSelectionRow): string { return '{"index":' + r.index + ',"id":' + q(r.id) + ',"inputBytes":' + r.inputBytes + ',"inputSha256":' + q(r.inputSha256) + '}' }
function endJSON(e: End): string { return '{"rows":' + e.rows + ',"inputBytesTotal":' + e.inputBytesTotal + ',"indexSha256":' + q(e.indexSha256) + '}' }
function checked(a: number, b: number): number {
 const n = a + b
 if (!Number.isSafeInteger(a) || !Number.isSafeInteger(b) || a < 0 || b < 0 || !Number.isSafeInteger(n)) return fail()
 return n
}
function bytes(s: string): Uint8Array { return encoder.encode(s) }
function equal(a: Uint8Array, b: Uint8Array): boolean { return a.length === b.length && a.every((v, i) => v === b[i]) }
function unhex(s: string): Uint8Array {
 if (!shaPattern.test(s)) return fail()
 return Uint8Array.from(s.match(/../g)!, part => parseInt(part, 16))
}
function u32(n: number): Uint8Array {
 if (!Number.isSafeInteger(n) || n < 0 || n > 0xffffffff) return fail()
 const out = new Uint8Array(4)
 new DataView(out.buffer).setUint32(0, n, false)
 return out
}
class Material {
 readonly raw: Uint8Array
 at = 0
 constructor(size: number) { this.raw = new Uint8Array(size) }
 put(part: Uint8Array): void {
  if (part.length > this.raw.length - this.at) return fail()
  this.raw.set(part, this.at); this.at += part.length
 }
 uint32(n: number): void { this.put(u32(n)) }
 uint64(n: number): void {
  if (!Number.isSafeInteger(n) || n < 0) return fail()
  const raw = new Uint8Array(8)
  new DataView(raw.buffer).setBigUint64(0, BigInt(n), false)
  this.put(raw)
 }
 finish(): Uint8Array { if (this.at !== this.raw.length) return fail(); return this.raw }
}
function sourceCopy(s: RetainedSelectionSource): RetainedSelectionSource {
 const installed = DIALECT_RETAINED_COLUMN_DRAFT
 // Explicit equality to the package helper; no copied release/source pins.
 if (!s || !/^prep_[a-f0-9]{32}$/.test(s.preparationId) || !shaPattern.test(s.revision) || !/^sha256:[a-f0-9]{64}$/.test(s.packDigest) || (s.origin !== 'asserted' && s.origin !== 'generated') || !Array.isArray(s.recordIds) || s.recordIds.length < 1 || s.recordIds.length > 1000 || !Number.isSafeInteger(s.requestBytes) || s.requestBytes < 1 || s.requestBytes > 2097152) return fail()
 const c = s.conversionContract
 if (!c || !c.spec || Object.keys(c).length !== 3 || Object.keys(c.spec).length !== 4 || c.contract !== installed.contract || c.profile !== installed.profile || c.spec.version !== installed.spec.version || c.spec.revision !== installed.spec.revision || c.spec.schemaSha256 !== installed.spec.schemaSha256 || c.spec.corpusSha256 !== installed.spec.corpusSha256) return fail()
 let idBytes = 0
 const seen = new Set<string>()
 for (const id of s.recordIds) {
  if (!scalar(id) || id === '' || seen.has(id)) return fail()
  seen.add(id); idBytes = checked(idBytes, bytes(id).length)
  if (idBytes > s.requestBytes) return fail()
 }
 return {...s, recordIds: [...s.recordIds], conversionContract: {contract: c.contract, spec: {...c.spec}, profile: c.profile}}
}
async function selectionSHA(s: RetainedSelectionSource): Promise<string> {
 let length = bytes(idsDomain).length + 4
 for (const id of s.recordIds) length = checked(length, 4 + bytes(id).length)
 const out = new Material(length)
 out.put(bytes(idsDomain)); out.uint32(s.recordIds.length)
 for (const id of s.recordIds) { const raw = bytes(id); out.uint32(raw.length); out.put(raw) }
 return sha256Hex(out.finish())
}
async function makeHeader(s: RetainedSelectionSource, viewId: string): Promise<RetainedSelectionHeader> {
 return {wireVersion: 1, viewId, preparationId: s.preparationId, revision: s.revision, packDigest: s.packDigest, conversionContract: s.conversionContract, origin: s.origin, rows: s.recordIds.length, maxInputBytes: retainedSelectionMaxInputBytes, selectionSha256: await selectionSHA(s)}
}
async function indexMaterial(s: RetainedSelectionSource, header: Uint8Array): Promise<Material> {
 let length = bytes(indexDomain).length + 32 + 4
 for (const id of s.recordIds) length = checked(length, 48 + bytes(id).length)
 const out = new Material(length)
 out.put(bytes(indexDomain)); out.put(unhex(await sha256Hex(header))); out.uint32(s.recordIds.length)
 return out
}
function indexRow(out: Material, row: RetainedSelectionRow): void {
 const id = bytes(row.id)
 out.uint32(row.index); out.uint32(id.length); out.put(id); out.uint64(row.inputBytes); out.put(unhex(row.inputSha256))
}
function bounds(s: RetainedSelectionSource, h: RetainedSelectionHeader): {body: number; header: number; end: number} {
 const header = bytes(headerJSON(h)).length
 u32(header)
 let body = 20 + header, inputs = 0
 for (let i = 0; i < s.recordIds.length; i++) {
  const metadata = bytes(rowJSON({index: i, id: s.recordIds[i]!, inputBytes: retainedSelectionMaxInputBytes, inputSha256: zeroSHA})).length
  u32(metadata)
  body = checked(body, 5 + metadata + retainedSelectionMaxInputBytes)
  inputs = checked(inputs, retainedSelectionMaxInputBytes)
 }
 const end = bytes(endJSON({rows: s.recordIds.length, inputBytesTotal: inputs, indexSha256: zeroSHA})).length
 return {body: checked(body, end), header, end}
}
function validUTF8(raw: Uint8Array): void {
 // Discard bounded decoded pieces; do not allocate a second whole-row string.
 const decoder = new TextDecoder('utf-8', {fatal: true, ignoreBOM: true})
 for (let at = 0; at < raw.length; at += 65536) decoder.decode(raw.subarray(at, at + 65536), {stream: true})
 decoder.decode()
}
function canonical<T>(raw: Uint8Array, encode: (value: T) => string): T {
 const text = new TextDecoder('utf-8', {fatal: true, ignoreBOM: true}).decode(raw)
 const value = JSON.parse(text) as T
 // Exact byte equality rejects duplicates (including escaped names), unknown
 // members, nulls, key order, whitespace, BOM and alternate numeric spellings.
 if (!equal(raw, bytes(encode(value)))) return fail()
 return value
}

class StreamReader {
 private readonly iterator: AsyncIterator<Uint8Array>
 private chunk: Uint8Array = new Uint8Array(0)
 private at = 0
 private ended = false
 count = 0
 constructor(chunks: AsyncIterable<Uint8Array>, private readonly length: number) { this.iterator = chunks[Symbol.asyncIterator]() }
 private async available(): Promise<boolean> {
  while (this.at === this.chunk.length && !this.ended) {
   const next = await this.iterator.next()
   this.ended = next.done === true
   this.chunk = this.ended ? new Uint8Array(0) : next.value
   this.at = 0
   if (!(this.chunk instanceof Uint8Array)) return fail()
  }
  return this.at < this.chunk.length
 }
 async read(n: number): Promise<Uint8Array> {
  if (!Number.isSafeInteger(n) || n < 0 || n > this.length - this.count) return fail()
  const out = new Uint8Array(n)
  let at = 0
  while (at < n) {
   if (!await this.available()) return fail()
   const size = Math.min(n - at, this.chunk.length - this.at)
   out.set(this.chunk.subarray(this.at, this.at + size), at)
   at += size; this.at += size
   if (this.at === this.chunk.length) { this.chunk = new Uint8Array(0); this.at = 0 }
  }
  this.count = checked(this.count, n)
  return out
 }
 async metadata(max: number, exact = false): Promise<Uint8Array> {
  const prefix = await this.read(4)
  const length = new DataView(prefix.buffer).getUint32(0, false)
  if (length < 1 || length > max || (exact && length !== max)) return fail()
  return this.read(length)
 }
 async eof(): Promise<void> { if (this.count !== this.length || await this.available()) return fail() }
 async close(): Promise<void> { this.chunk = new Uint8Array(0); await this.iterator.return?.() }
}

/** All callbacks are provisional until the returned promise passes actual EOF.
 * Keep at most one payload; readiness/mount/storage are separate owner work.
 * Iterator pulls and awaited callbacks provide backpressure; no chunk queue. */
export async function readRetainedSelectionWire(chunks: AsyncIterable<Uint8Array>, captured: RetainedSelectionSource, contentLength: number, onRow: (row: RetainedSelectionDescriptor, input: Uint8Array) => void | Promise<void>, onHeader?: (header: RetainedSelectionHeader) => void | Promise<void>): Promise<RetainedSelectionResult> {
 const source = sourceCopy(captured)
 const expected = await makeHeader(source, zeroView)
 const limit = bounds(source, expected)
 if (!Number.isSafeInteger(contentLength) || contentLength < 1 || contentLength > limit.body) return fail()
 const stream = new StreamReader(chunks, contentLength)
 try {
  if (!equal(await stream.read(bytes(magic).length), bytes(magic))) return fail()
  const headerBytes = await stream.metadata(limit.header, true)
  const header = canonical<RetainedSelectionHeader>(headerBytes, headerJSON)
  if (!viewPattern.test(header.viewId) || headerJSON(header) !== headerJSON({...expected, viewId: header.viewId})) return fail()
  const index = await indexMaterial(source, headerBytes)
  // Expose only a provisional reference for later best-effort close on failure.
  // Callback mutation cannot alter the frozen identity returned at EOF.
  await onHeader?.({...header, conversionContract: {...header.conversionContract, spec: {...header.conversionContract.spec}}})
  const rows: RetainedSelectionDescriptor[] = []
  let inputs = 0
  for (let i = 0; i < source.recordIds.length; i++) {
   if ((await stream.read(1))[0] !== 1) return fail()
   const id = source.recordIds[i]!
   const max = bytes(rowJSON({index: i, id, inputBytes: retainedSelectionMaxInputBytes, inputSha256: zeroSHA})).length
   const row = canonical<RetainedSelectionRow>(await stream.metadata(max), rowJSON)
   if (row.index !== i || row.id !== id || !Number.isSafeInteger(row.inputBytes) || row.inputBytes < 1 || row.inputBytes > retainedSelectionMaxInputBytes || !shaPattern.test(row.inputSha256)) return fail()
   const descriptor = {...row, offset: stream.count}
   const input = await stream.read(row.inputBytes)
   validUTF8(input)
   if (await sha256Hex(input) !== row.inputSha256) return fail()
   inputs = checked(inputs, row.inputBytes)
   indexRow(index, row)
   // Retain the internal descriptor independently of callback mutation.
   await onRow({...descriptor}, input)
   rows.push(descriptor)
  }
  if ((await stream.read(1))[0] !== 2) return fail()
  const end = canonical<End>(await stream.metadata(limit.end), endJSON)
  const expectedEnd: End = {rows: rows.length, inputBytesTotal: inputs, indexSha256: await sha256Hex(index.finish())}
  if (endJSON(end) !== endJSON(expectedEnd)) return fail()
  await stream.eof()
  return {header, rows, inputBytesTotal: inputs, indexSha256: end.indexSha256, bytes: stream.count}
 } finally { await stream.close() }
}

/** One row at a time; sink must consume each borrowed chunk before resolving.
 * A failure invalidates the entire sink. Never retain/replay partial output. */
export async function writeRetainedSelectionWire(sink: (chunk: Uint8Array) => void | Promise<void>, captured: RetainedSelectionSource, viewId: string, rows: AsyncIterable<{id: string; input: Uint8Array}>): Promise<number> {
 const source = sourceCopy(captured)
 if (!viewPattern.test(viewId)) return fail()
 const header = await makeHeader(source, viewId)
 const limit = bounds(source, header)
 const rawHeader = bytes(headerJSON(header))
 const index = await indexMaterial(source, rawHeader)
 let count = 0, inputs = 0, next = 0
 const write = async (raw: Uint8Array): Promise<void> => {
  count = checked(count, raw.length)
  if (count > limit.body) return fail()
  await sink(raw)
 }
 const frame = async (tag: number, raw: Uint8Array): Promise<void> => {
  if (tag) await write(Uint8Array.of(tag))
  await write(u32(raw.length)); await write(raw)
 }
 await write(bytes(magic)); await frame(0, rawHeader)
 for await (const value of rows) {
  if (next >= source.recordIds.length || value.id !== source.recordIds[next] || !(value.input instanceof Uint8Array) || value.input.length < 1 || value.input.length > retainedSelectionMaxInputBytes) return fail()
  validUTF8(value.input)
  const row: RetainedSelectionRow = {index: next, id: value.id, inputBytes: value.input.length, inputSha256: await sha256Hex(value.input)}
  await frame(1, bytes(rowJSON(row))); await write(value.input)
  indexRow(index, row); inputs = checked(inputs, row.inputBytes); next++
 }
 if (next !== source.recordIds.length) return fail()
 await frame(2, bytes(endJSON({rows: next, inputBytesTotal: inputs, indexSha256: await sha256Hex(index.finish())})))
 return count
}
