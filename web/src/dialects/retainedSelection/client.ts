import {DIALECT_RETAINED_COLUMN_DRAFT} from '@judgment-pack/harness/dialects'
import {activeDeskId, readDeskSelection} from '../../desks/scope'
import {deskFetch} from '../../files/client'
import {discardBody, sessionBearer, whenSessionEnds} from '../../mcp/session'
import {sha256Hex} from '../../research/verify/receipt'
import {readRetainedSelectionWire, retainedSelectionMaxInputBytes, retainedSelectionMediaType, type RetainedSelectionDescriptor, type RetainedSelectionHeader, type RetainedSelectionSource} from './wire'

export type ReviewFailure = 'invalid-request' | 'invalid-response' | 'unavailable' | 'resource' | 'expired' | 'review-not-ready' | 'session-ended' | 'cancelled'
export class RetainedReviewError extends Error {
 constructor(readonly kind: ReviewFailure, readonly status?: number) { super(`Retained review ${kind}.`); this.name = 'RetainedReviewError' }
}
export interface RetainedReviewView {
 readonly header: Readonly<RetainedSelectionHeader>
 readonly rows: readonly Readonly<RetainedSelectionDescriptor>[]
 readonly indexSha256: string
 readonly inputBytesTotal: number
}
export interface ExactRetainedInput {
 readonly text: string
 readonly sourceRevision: string
 readonly rowId: string
 readonly index: number
 readonly inputSha256: string
}
const encoder = new TextEncoder()
const invalid = (): never => { throw new RetainedReviewError('invalid-response') }
const desk = (): string => readDeskSelection() ?? activeDeskId
// One payload processor for this document, including a remounted/new client.
let processing = false

/** No credentials or URL come from the source. Request size is measured here. */
function capture(source: RetainedSelectionSource): {source: RetainedSelectionSource; body: string} {
 try {
  const c = source.conversionContract, installed = DIALECT_RETAINED_COLUMN_DRAFT
  if (!/^prep_[a-f0-9]{32}$/.test(source.preparationId) || !/^[a-f0-9]{64}$/.test(source.revision) || !/^sha256:[a-f0-9]{64}$/.test(source.packDigest) || !['asserted','generated'].includes(source.origin) || !Array.isArray(source.recordIds) || source.recordIds.length < 1 || source.recordIds.length > 1000 || !c || !c.spec || Object.keys(c).length !== 3 || Object.keys(c.spec).length !== 4 || c.contract !== installed.contract || c.profile !== installed.profile || c.spec.version !== installed.spec.version || c.spec.revision !== installed.spec.revision || c.spec.schemaSha256 !== installed.spec.schemaSha256 || c.spec.corpusSha256 !== installed.spec.corpusSha256) throw Error()
  const ids = [...source.recordIds]
  if (ids.some(id => typeof id !== 'string' || !id || /[\uD800-\uDFFF]/u.test(id)) || new Set(ids).size !== ids.length) throw Error()
  const body = JSON.stringify({recordIds: ids}), requestBytes = encoder.encode(body).length
  if (requestBytes > 2097152) throw Error()
  return {body, source: {preparationId: source.preparationId, revision: source.revision, packDigest: source.packDigest, origin: source.origin, conversionContract: {contract: c.contract, profile: c.profile, spec: {...c.spec}}, recordIds: ids, requestBytes}}
 } catch (cause) { throw new RetainedReviewError(cause instanceof RangeError ? 'resource' : 'invalid-request') }
}

function length(response: Response, media: string): number {
 const raw = response.headers.get('Content-Length'), encoding = response.headers.get('Content-Encoding')
 if (response.status !== 200 || response.headers.get('Content-Type') !== media || encoding !== null && encoding !== 'identity' || raw === null || !/^[1-9][0-9]*$/.test(raw)) return invalid()
 const n = Number(raw)
 if (!Number.isSafeInteger(n) || !response.body) return invalid()
 return n
}

// The native typed-array tag getter reads the internal brand across realms;
// unlike Object#toString it cannot be forged with an own Symbol.toStringTag.
const typedArrayTag = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(Uint8Array.prototype), Symbol.toStringTag)!.get!
function responseBytes(value: unknown): Uint8Array {
 if (!ArrayBuffer.isView(value) || typedArrayTag.call(value) !== 'Uint8Array') return invalid()
 // Fetch in this browser already uses our realm. Node Response in jsdom (or a
 // genuine foreign window) may not. Borrow the exact span without copying a
 // coalesced response buffer; the wire codec receives its own realm's type.
 if (value instanceof Uint8Array) return value
 const bytes = value as Uint8Array
 return new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength)
}

/** One outstanding pull, no queue or concatenation. Abort also interrupts a pending read.
 * A coalesced fetch buffer is borrowed, never copied wholesale or rejected by size. */
export async function* retainedResponseChunks(response: Response, signal: AbortSignal): AsyncGenerator<Uint8Array> {
 if (!response.body) return invalid()
 const reader = response.body.getReader()
 let cancelled = false
 const cancel = () => { if (!cancelled) { cancelled = true; void reader.cancel().catch(() => {}) } }
 signal.addEventListener('abort', cancel, {once: true})
 try {
  for (;;) {
   if (signal.aborted) throw new RetainedReviewError('cancelled')
   const next = await reader.read()
   if (signal.aborted) throw new RetainedReviewError('cancelled')
   if (next.done) return
   yield responseBytes(next.value)
  }
 } finally {
  signal.removeEventListener('abort', cancel)
  // Do not wait for a hostile/failed source's cancel promise to settle.
  cancel()
  reader.releaseLock()
 }
}

/** Error prose is never surfaced. A bounded refusal envelope only supplies stable codes. */
async function refusal(response: Response, signal: AbortSignal): Promise<never> {
 let code: unknown
 try {
  const parts: Uint8Array[] = []
  let bytes = 0
  for await (const part of retainedResponseChunks(response, signal)) {
   bytes += part.length
   if (bytes > 16384) break
   parts.push(part)
  }
  if (bytes <= 16384) {
   const raw = new Uint8Array(bytes)
   let at = 0
   for (const part of parts) { raw.set(part, at); at += part.length }
   code = (JSON.parse(new TextDecoder('utf-8', {fatal: true}).decode(raw)) as {code?: unknown})?.code
  }
 } catch { /* An unreadable refusal is unavailable, never successful content. */ }
 if (response.status === 409 && code === 'review-not-ready') throw new RetainedReviewError('review-not-ready', 409)
 if (response.status === 410 && code === 'review-unavailable') throw new RetainedReviewError('expired', 410)
 throw new RetainedReviewError(response.status === 503 || response.status === 507 ? 'resource' : 'unavailable', response.status)
}

/** Factory captures the legitimate page session. No constructor accepts a bearer. */
export async function retainedSelectionClient(): Promise<RetainedSelectionClient> {
 return RetainedSelectionClient.capture()
}
export class RetainedSelectionClient {
 private readonly owned = new WeakSet<RetainedReviewView>()
 /** A cancelled WebCrypto digest still owns its raw payload until it settles.
  * Refuse overlap rather than retain an unbounded queue of superseded actions. */
 private claim(): () => void {
  if (processing) throw new RetainedReviewError('resource')
  processing = true
  return () => { processing = false }
 }
 private constructor(private readonly session: string, private readonly deskId: string) {}
 static async capture(): Promise<RetainedSelectionClient> {
  const session = await sessionBearer(), deskId = desk()
  if (deskId !== activeDeskId) throw new RetainedReviewError('session-ended')
  return new RetainedSelectionClient(session, deskId)
 }
 async current(signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) throw new RetainedReviewError('cancelled')
  try {
   if (desk() !== this.deskId || this.deskId !== activeDeskId || await sessionBearer() !== this.session || signal?.aborted) throw Error()
  } catch { throw new RetainedReviewError(signal?.aborted ? 'cancelled' : 'session-ended') }
 }
 private async scope(signal?: AbortSignal) {
  const controller = new AbortController(), abort = () => controller.abort()
  const stop = whenSessionEnds(abort)
  signal?.addEventListener('abort', abort, {once: true})
  if (signal?.aborted) abort()
  try {
   await this.current(controller.signal)
   return {signal: controller.signal, abort, close: () => { stop(); signal?.removeEventListener('abort', abort) }}
  } catch (cause) { stop(); signal?.removeEventListener('abort', abort); throw cause }
 }
 private path(preparationId: string, suffix: string): string { return `/api/input-preparations/${preparationId}/${suffix}` }
 async prepare(source: RetainedSelectionSource, signal?: AbortSignal): Promise<RetainedReviewView> {
  const frozen = capture(source), release = this.claim()
  let scope: Awaited<ReturnType<RetainedSelectionClient['scope']>> | undefined
  let response: Response | undefined, sealed: RetainedReviewView | undefined
  // This provisional reference is never published or used to mount. The close
  // protocol needs the verified end digest; before then cancellation owns cleanup.
  let provisional: RetainedSelectionHeader | undefined
  try {
   scope = await this.scope(signal)
   const active = scope
   response = await deskFetch(this.path(frozen.source.preparationId, 'prepare'), {method: 'POST', redirect: 'error', signal: scope.signal, headers: {'Content-Type': 'application/json', Accept: retainedSelectionMediaType, 'If-Match': frozen.source.revision}, body: frozen.body})
   await this.current(scope.signal)
   if (!response.ok) return await refusal(response, scope.signal)
   const result = await readRetainedSelectionWire(retainedResponseChunks(response, scope.signal), frozen.source, length(response, retainedSelectionMediaType), async () => { await this.current(active.signal) }, header => { provisional = header })
   // Result is obtained only after every payload, index digest and actual EOF.
   const header = Object.freeze({...result.header, conversionContract: Object.freeze({...result.header.conversionContract, spec: Object.freeze({...result.header.conversionContract.spec})})})
   sealed = Object.freeze({header, rows: Object.freeze(result.rows.map(row => Object.freeze(row))), indexSha256: result.indexSha256, inputBytesTotal: result.inputBytesTotal})
   this.owned.add(sealed)
   await this.current(scope.signal)
   return sealed
  } catch (cause) {
   scope?.abort()
   if (sealed) void this.close(sealed).catch(() => {})
   throw cause instanceof RetainedReviewError ? cause : new RetainedReviewError(cause instanceof RangeError ? 'resource' : 'unavailable')
  } finally {
   // Drop the provisional reference even when cancellation cannot use auth HTTP.
   if (provisional) provisional = undefined
   if (response) void discardBody(response)
   scope?.close()
   release()
  }
 }
 async read(view: RetainedReviewView, index: number, signal?: AbortSignal): Promise<ExactRetainedInput> {
  if (!this.owned.has(view) || !Number.isInteger(index) || index < 0 || index >= view.rows.length) throw new RetainedReviewError('invalid-request')
  const row = view.rows[index]!
  if (row.index !== index || row.id === '' || row.inputBytes < 1 || row.inputBytes > retainedSelectionMaxInputBytes) throw new RetainedReviewError('invalid-request')
  const release = this.claim()
  let scope: Awaited<ReturnType<RetainedSelectionClient['scope']>> | undefined
  let response: Response | undefined
  try {
   scope = await this.scope(signal)
   response = await deskFetch(this.path(view.header.preparationId, 'prepared-row'), {method: 'POST', redirect: 'error', signal: scope.signal, headers: {'Content-Type': 'application/json', 'If-Match': view.indexSha256}, body: JSON.stringify({viewId: view.header.viewId, indexSha256: view.indexSha256, index, id: row.id})})
   await this.current(scope.signal)
   if (!response.ok) return await refusal(response, scope.signal)
   if (length(response, 'application/json') !== row.inputBytes) return invalid()
   const raw = new Uint8Array(row.inputBytes)
   let at = 0
   for await (const part of retainedResponseChunks(response, scope.signal)) {
    if (part.length > raw.length - at) return invalid()
    raw.set(part, at); at += part.length
   }
   if (at !== raw.length || await sha256Hex(raw) !== row.inputSha256) return invalid()
   // Digest cannot be aborted. Recheck ownership before allocating old text.
   await this.current(scope.signal)
   const text = new TextDecoder('utf-8', {fatal: true, ignoreBOM: true}).decode(raw)
   await this.current(scope.signal)
   return Object.freeze({text, sourceRevision: view.header.revision, rowId: row.id, index, inputSha256: row.inputSha256})
  } catch (cause) {
   scope?.abort()
   throw cause instanceof RetainedReviewError ? cause : new RetainedReviewError(cause instanceof RangeError ? 'resource' : 'unavailable')
  } finally { if (response) void discardBody(response); scope?.close(); release() }
 }
 /** A normal close uses only this still-current session; revoke cleanup is local/server-owned. */
 async close(view: RetainedReviewView): Promise<void> {
  if (!this.owned.has(view)) return
  this.owned.delete(view)
  await this.current()
  const scope = await this.scope(AbortSignal.timeout(10000))
  let response: Response | undefined
  try {
   response = await deskFetch(this.path(view.header.preparationId, 'prepared-view-close'), {method: 'POST', redirect: 'error', signal: scope.signal, headers: {'Content-Type': 'application/json'}, body: JSON.stringify({viewId: view.header.viewId, indexSha256: view.indexSha256})})
   await this.current(scope.signal)
   if (!response.ok) await refusal(response, scope.signal)
  } finally { if (response) void discardBody(response); scope.close() }
 }
}
