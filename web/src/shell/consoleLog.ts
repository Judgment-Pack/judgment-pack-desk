/**
 * The console's buffer: what this session has seen, in memory.
 *
 * **This is session traffic, not an audit trail**, and the difference is the
 * whole boundary. It is capped, it is gone on reload, the desk never writes it
 * to disk, and it carries no tool arguments — `PackEvaluate`'s facts and
 * evidence are whatever the user pasted, and a console that quietly retained
 * them would be a second copy of the thing the desk was careful not to keep.
 * ADR-0018's runtime-written audit records are the audit trail; the desk's job
 * there is to render files the runtime wrote.
 *
 * The module shape is `refetchLedger.ts`'s: module state plus an explicit
 * `forget…()` so a test starts from a session that has seen nothing, rather
 * than from whatever the test before it left behind.
 */

export type ConsoleChannel = 'connection' | 'files'

export interface ConsoleEntry {
  seq: number
  at: number
  channel: ConsoleChannel
  text: string
}

/** Bounded, because a long session is not a reason to hold a long buffer. */
const CAPACITY = 500

let entries: ConsoleEntry[] = []
let sequence = 0
const listeners = new Set<() => void>()

/**
 * The last connection line recorded.
 *
 * StrictMode runs an effect mount → cleanup → mount, so the effect that
 * watches the connection fires twice for one transition. Dropping an identical
 * consecutive line is what stops the buffer double-reporting a state the
 * connection entered once.
 */
let lastConnectionText: string | undefined

function publish(): void {
  for (const listener of listeners) listener()
}

function append(channel: ConsoleChannel, text: string): void {
  sequence += 1
  const next = entries.concat({ seq: sequence, at: Date.now(), channel, text })
  entries = next.length > CAPACITY ? next.slice(next.length - CAPACITY) : next
  publish()
}

/** One connection transition, recorded once however many times it is offered. */
export function recordConnection(text: string): void {
  if (text === lastConnectionText) return
  lastConnectionText = text
  append('connection', text)
}

/** One file the chassis' watcher reported. The path, and nothing else. */
export function recordFileChange(path: string): void {
  append('files', path === '' ? '(a file changed; the notification carried no path)' : path)
}

export function subscribeConsole(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** The current buffer. Stable between publishes, as the store contract needs. */
export function consoleSnapshot(): ConsoleEntry[] {
  return entries
}

/** Start from nothing. The page never needs it; a test always does. */
export function forgetConsole(): void {
  entries = []
  sequence = 0
  lastConnectionText = undefined
  publish()
}
