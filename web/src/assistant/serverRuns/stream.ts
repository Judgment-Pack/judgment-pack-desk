import { cursorParts, decodeEvent, decodeProgress, decodeSnapshot, invalid, MAX_PROGRESS_BYTES, MAX_RUN_BYTES, parseRunJSON } from './decode'
import type { RunObservation, RunProgress, RunReference, RunSnapshot } from './types'

export type SSEFrame = { readonly kind: 'event' | 'snapshot'; readonly cursor: string; readonly data: unknown } | { readonly kind: 'progress'; readonly cursor?: never; readonly data: unknown }
/** One fetch stream. No reconnect, retry, continuation or mutation is scheduled here. */
export async function* readFrames(body: ReadableStream<Uint8Array>, signal?: AbortSignal): AsyncGenerator<SSEFrame> {
  const reader = body.getReader(), decoder = new TextDecoder('utf-8', {fatal:true})
  let buffer = '', kind = '', cursor = '', data: string[] = [], bytes = 0, ended = false, idSeen = false
  const abort = () => { void reader.cancel().catch(() => {}) }
  signal?.addEventListener('abort', abort, {once:true})
  function line(value: string): SSEFrame | undefined {
    bytes += new TextEncoder().encode(value).length + 1
    if (bytes > (kind === 'progress' ? 6 * MAX_PROGRESS_BYTES + 2048 : MAX_RUN_BYTES)) return invalid()
    if (value === '') {
      if (!kind && !cursor && data.length === 0) { bytes = 0; return }
      if (!['event','snapshot','progress'].includes(kind) || data.length === 0 || (kind === 'progress' ? idSeen : !cursor)) return invalid()
      if (kind !== 'progress') cursorParts(cursor)
      const frame = {kind, ...(kind === 'progress' ? {} : {cursor}), data:parseRunJSON(data.join('\n'))}
      kind = ''; cursor = ''; data = []; bytes = 0; idSeen = false
      return frame as SSEFrame
    }
    if (value.startsWith(':')) return
    const colon = value.indexOf(':'), key = colon === -1 ? value : value.slice(0,colon)
    const raw = colon === -1 ? '' : value.slice(colon+1), said = raw.startsWith(' ') ? raw.slice(1) : raw
    if (key === 'event') { if (kind) return invalid(); kind = said }
    else if (key === 'id') { if (idSeen) return invalid(); idSeen = true; cursor = said }
    else if (key === 'data') data.push(said)
    else return invalid()
  }
  try {
    if (signal?.aborted) return
    while (true) {
      const part = await reader.read()
      if (signal?.aborted) return
      buffer += part.done ? decoder.decode() : decoder.decode(part.value, {stream:true})
      // Leave a trailing CR until the next chunk, so a split CRLF is one newline.
      let at: number
      while ((at = buffer.search(/[\r\n]/)) !== -1) {
        if (!part.done && buffer[at] === '\r' && at === buffer.length-1) break
        const value = buffer.slice(0,at), width = buffer[at] === '\r' && buffer[at+1] === '\n' ? 2 : 1
        buffer = buffer.slice(at+width)
        const frame = line(value)
        if (frame) yield frame
      }
      if (bytes + new TextEncoder().encode(buffer).length > (kind === 'progress' ? 6 * MAX_PROGRESS_BYTES + 2048 : MAX_RUN_BYTES)) return invalid()
      if (part.done) { ended = true; break }
    }
    if (buffer || kind || cursor || data.length) return invalid()
  } catch { if (!signal?.aborted) return invalid() }
  finally {
    signal?.removeEventListener('abort', abort)
    if (!ended) { try { await reader.cancel() } catch { /* Already released by fetch abort. */ } }
    reader.releaseLock()
  }
}

/** Bounded observations of one exact run; callers retain this object across explicit reconnects. */
export class RunObservations {
  private readonly identities = new Map<string,string>()
  private readonly positions = new Map<number,string>()
  private held?: RunSnapshot
  private last?: string
  private epoch?: string
  private sequence = 0
  private reconciled = false
  private readonly prefixes = new Map<string,RunProgress>()
  readonly reference: RunReference
  constructor(reference: RunReference, initial?: RunSnapshot) {
    this.reference = Object.freeze({id:reference.id, owner:Object.freeze({...reference.owner}), requestId:reference.requestId})
    if (initial) this.accept({kind:'snapshot',cursor:initial.cursor,data:initial})
  }
  /** Called for each explicit stream, before its required durable reconciliation. */
  beginStream(): void { this.prefixes.clear(); this.epoch = undefined; this.sequence = 0; this.reconciled = false }
  endStream(): void { this.prefixes.clear(); this.reconciled = false }
  get answer(): string { return this.prefixes.get('')?.text ?? '' }
  get children(): ReadonlyMap<string,RunProgress> { return new Map([...this.prefixes].filter(([key])=>key !== '')) }
  get snapshot(): RunSnapshot | undefined { return this.held }
  get cursor(): string | undefined { return this.last }
  accept(frame: SSEFrame): RunObservation | undefined {
    if (frame.kind === 'progress') {
      const progress = decodeProgress(frame.data,this.reference)
      if (!this.reconciled || !this.held || this.held.historical || !['admitted','running'].includes(this.held.state) || this.held.checkpoint && this.held.checkpoint.status !== 'running' || progress.cursor !== this.last) return invalid()
      if (this.epoch !== undefined && progress.epoch !== this.epoch) return invalid()
      if (progress.sequence <= this.sequence) return undefined
      const key = progress.taskId ?? ''
      if (!this.prefixes.has(key) && this.prefixes.size >= 17) return invalid()
      const rows = [...this.prefixes].filter(([k])=>k !== key).map(([,p])=>p).concat(progress)
      if (rows.reduce((n,p)=>n + new TextEncoder().encode(JSON.stringify(p)).length + 128,0) > 1024*1024) return invalid()
      this.epoch = progress.epoch; this.sequence = progress.sequence
      this.prefixes.set(key,progress)
      return {kind:'progress',progress}
    }
    const [revision, count] = cursorParts(frame.cursor)
    if (frame.kind === 'snapshot') {
      const snapshot = decodeSnapshot(frame.data, this.reference)
      if (snapshot.cursor !== frame.cursor || this.last && revision < cursorParts(this.last)[0] || this.held && (snapshot.revision < this.held.revision || snapshot.events.length < this.held.events.length)) return invalid()
      for (const [position,id] of this.positions) if (snapshot.events[position-1]?.id !== id) return invalid()
      for (const event of snapshot.events) if (this.identities.has(event.id) && this.identities.get(event.id) !== JSON.stringify(event.event)) return invalid()
      if (this.held?.checkpoint && !snapshot.checkpoint) return invalid()
      if (this.held?.checkpoint && snapshot.checkpoint && (this.held.checkpoint.requestKey !== snapshot.checkpoint.requestKey || this.held.checkpoint.phase !== snapshot.checkpoint.phase || this.held.checkpoint.unit !== snapshot.checkpoint.unit)) return invalid()
      for (const old of this.held?.operations ?? []) {
        const next = snapshot.operations.find(op=>op.id === old.id)
        if (!next || next.callId !== old.callId || next.kind !== old.kind || next.name !== old.name || next.intent !== old.intent || old.result !== null && (next.result !== old.result || next.state !== old.state)) return invalid()
      }
      for (const [index,event] of snapshot.events.entries()) { this.position(index+1,event.id); this.remember(event.id,event.event) }
      if (!this.held || snapshot.events.length !== this.held.events.length || !['admitted','running'].includes(snapshot.state) || snapshot.checkpoint && snapshot.checkpoint.status !== 'running') this.prefixes.clear()
      this.held = snapshot; this.last = frame.cursor; this.reconciled = true
      return {kind:'snapshot',snapshot,cursor:frame.cursor}
    }
    const event = decodeEvent(frame.data)
    if (count < 1 || this.last && revision < cursorParts(this.last)[0]) return invalid()
    const previous = this.identities.get(event.id)
    if (previous !== undefined && previous !== JSON.stringify(event.event)) return invalid()
    this.prefixes.clear(); this.reconciled = false
    this.position(count,event.id)
    const duplicate = this.identities.has(event.id)
    this.remember(event.id,event.event)
    if (!this.last || revision > cursorParts(this.last)[0] || count >= cursorParts(this.last)[1]) this.last = frame.cursor
    return duplicate ? undefined : {kind:'event',event,cursor:frame.cursor}
  }
  private position(count: number, id: string): void {
    if (this.positions.has(count) && this.positions.get(count) !== id) return invalid()
    this.positions.set(count,id)
  }
  private remember(id: string, event: unknown): void {
    const raw = JSON.stringify(event), previous = this.identities.get(id)
    if (previous !== undefined && previous !== raw || previous === undefined && this.identities.size >= 1024) return invalid()
    this.identities.set(id,raw)
  }
}
