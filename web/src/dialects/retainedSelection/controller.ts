import {whenSessionEnds} from '../../mcp/session'
import type {RetainedSelectionSource} from './wire'
import {RetainedReviewError, type ExactRetainedInput, type RetainedReviewView, type RetainedSelectionClient, type ReviewFailure} from './client'

export type RetainedMountState =
 | {readonly kind: 'empty'}
 | {readonly kind: 'loading'; readonly index: number}
 | {readonly kind: 'mounted'; readonly input: ExactRetainedInput}
 | {readonly kind: 'unavailable'; readonly index: number; readonly reason: ReviewFailure}
export type RetainedReviewState =
 | {readonly kind: 'idle'}
 | {readonly kind: 'preparing'}
 | {readonly kind: 'ready'; readonly view: RetainedReviewView; readonly mount: RetainedMountState}
 | {readonly kind: 'expired'}
 | {readonly kind: 'unavailable'; readonly reason: ReviewFailure}
 | {readonly kind: 'session-ended'}
 | {readonly kind: 'disposed'}

type Client = Pick<RetainedSelectionClient, 'current' | 'prepare' | 'read' | 'close'>
const reason = (cause: unknown): ReviewFailure => cause instanceof RetainedReviewError ? cause.kind : 'unavailable'

/** Inactive page-memory owner. Construct/subscribe do no I/O. Call prepare only
 * from an explicit action, never a mount effect/query/refetch/reconnect hook.
 * Rendering must escape input.text; this owner retains no parsed Input/AST. */
export class RetainedReviewController {
 private value: RetainedReviewState = Object.freeze({kind: 'idle'})
 private readonly listeners = new Set<() => void>()
 private readonly stopSession: () => void
 private readonly pagehide = () => this.dispose()
 private generation = 0
 private mountGeneration = 0
 private preparing?: Promise<void>
 private work?: AbortController
 private mountWork?: AbortController
 private view?: RetainedReviewView
 private terminal = false
 constructor(private readonly client: Client) {
  this.stopSession = whenSessionEnds(() => this.endSession())
  window.addEventListener('pagehide', this.pagehide)
 }
 get state(): RetainedReviewState { return this.value }
 subscribe(listener: () => void): () => void { this.listeners.add(listener); return () => { this.listeners.delete(listener) } }
 private publish(value: RetainedReviewState): void {
  this.value = Object.freeze(value)
  for (const listener of this.listeners) { try { listener() } catch { /* UI observer cannot retain ownership of a failed operation. */ } }
 }
 private alive(generation: number): boolean { return !this.terminal && generation === this.generation }
 private clear(): RetainedReviewView | undefined {
  this.generation++; this.mountGeneration++
  this.work?.abort(); this.mountWork?.abort()
  this.work = undefined; this.mountWork = undefined
  this.preparing = undefined
  const view = this.view
  this.view = undefined
  return view
 }
 private close(view?: RetainedReviewView): void { if (view) void this.client.close(view).catch(() => {}) }
 /** Repeated dispatch while in flight shares one action, including StrictMode callers. */
 prepare(source: RetainedSelectionSource): Promise<void> {
  if (this.terminal) return Promise.resolve()
  if (this.preparing) return this.preparing
  let frozen: RetainedSelectionSource
  try { frozen = {...source, recordIds: [...source.recordIds], conversionContract: {...source.conversionContract, spec: {...source.conversionContract.spec}}} }
  catch (cause) { this.close(this.clear()); this.publish({kind: 'unavailable', reason: cause instanceof RangeError ? 'resource' : 'invalid-request'}); return Promise.resolve() }
  const old = this.clear()
  this.close(old)
  const generation = this.generation, work = new AbortController()
  this.work = work
  // The microtask starts after the promise is installed, even for reentrant observers.
  const task = Promise.resolve().then(async () => {
   try {
    if (!this.alive(generation)) return
    const view = await this.client.prepare(frozen, work.signal)
    if (!this.alive(generation)) { this.close(view); return }
    this.view = view
    await this.client.current(work.signal)
    if (!this.alive(generation)) { this.close(view); return }
    this.publish({kind: 'ready', view, mount: Object.freeze({kind: 'empty'})})
   } catch (cause) {
    if (!this.alive(generation)) return
    const why = reason(cause)
    this.close(this.view); this.view = undefined
    if (why === 'session-ended') { this.endSession(); return }
    this.publish(why === 'expired' ? {kind: 'expired'} : {kind: 'unavailable', reason: why})
   } finally {
    if (this.alive(generation)) { this.work = undefined; this.preparing = undefined }
   }
  })
  this.preparing = task
  this.publish({kind: 'preparing'})
  return task
 }
 /** Pure read only. A retry is another explicit mount(index), never prepare.
  * A superseded digest may still own the client slot; its replacement then
  * reports resource until another explicit action after settlement. */
 async mount(index: number): Promise<void> {
  if (this.terminal || this.value.kind !== 'ready' || !this.view || !Number.isInteger(index) || index < 0 || index >= this.view.rows.length) return
  const view = this.view, generation = this.generation, mountGeneration = ++this.mountGeneration
  this.mountWork?.abort()
  const work = new AbortController()
  this.mountWork = work
  // Drop old text before any new read, including reads which later fail.
  this.publish({kind: 'ready', view, mount: Object.freeze({kind: 'loading', index})})
  const current = () => this.alive(generation) && this.mountGeneration === mountGeneration && this.view === view
  try {
   if (!current()) return
   const input = await this.client.read(view, index, work.signal)
   if (!current()) return
   await this.client.current(work.signal)
   if (!current()) return
   const expected = view.rows[index]!
   if (input.index !== index || input.rowId !== expected.id || input.sourceRevision !== view.header.revision || input.inputSha256 !== expected.inputSha256) throw new RetainedReviewError('invalid-response')
   this.publish({kind: 'ready', view, mount: Object.freeze({kind: 'mounted', input})})
  } catch (cause) {
   if (!current()) return
   const why = reason(cause)
   if (why === 'session-ended') { this.endSession(); return }
   if (why === 'expired') {
    this.close(this.clear())
    this.publish({kind: 'expired'})
   } else {
    this.publish({kind: 'ready', view, mount: Object.freeze({kind: 'unavailable', index, reason: why})})
   }
  } finally { if (current()) this.mountWork = undefined }
 }
 private endSession(): void {
  if (this.terminal) return
  this.terminal = true
  this.clear()
  this.stopSession()
  window.removeEventListener('pagehide', this.pagehide)
  // Server session cancellation owns spool cleanup. No authenticated close is required.
  this.publish({kind: 'session-ended'})
  this.listeners.clear()
 }
 dispose(): void {
  if (this.terminal) return
  this.terminal = true
  const view = this.clear()
  this.stopSession()
  window.removeEventListener('pagehide', this.pagehide)
  this.publish({kind: 'disposed'})
  this.listeners.clear()
  this.close(view)
 }
}
