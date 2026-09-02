/**
 * The two facts the shell and the authoring view have to share.
 *
 * **This is a real deviation from "the routes move verbatim", and it is
 * argued rather than slipped in.** The rail's dirty dot and the Create-pack
 * dialog's open-the-new-file cannot work without it: `dirty` is component-
 * local state in `AuthorView`, fed by `FileEditor`'s `onDirty`, and the file
 * selection has no URL parameter to carry it. The alternative is to drop both
 * features in phase A and say so; that is a choice for the maintainer, not
 * something to decide silently by writing the easier code. What it costs is
 * eight lines in `AuthorView.tsx` — one import and two effects — and nothing
 * else in that file changes.
 *
 * The module shape is `refetchLedger.ts`'s: module state, a subscription, and
 * an explicit `forget…()` for tests.
 */
import { useSyncExternalStore } from 'react'

let dirty = false
const dirtyListeners = new Set<() => void>()

/** AuthorView publishes; the rail reads. Nothing here decides anything. */
export function publishDirty(next: boolean): void {
  if (next === dirty) return
  dirty = next
  for (const listener of dirtyListeners) listener()
}

function subscribeDirty(listener: () => void): () => void {
  dirtyListeners.add(listener)
  return () => dirtyListeners.delete(listener)
}

function readDirty(): boolean {
  return dirty
}

export function useAuthorDirty(): boolean {
  return useSyncExternalStore(subscribeDirty, readDirty, readDirty)
}

/**
 * A path for the authoring view to open when it next mounts.
 *
 * Consumed once, by `takeRequestedOpen`. AuthorView reads it in a mount
 * **effect** and not in a lazy `useState` initializer: StrictMode invokes an
 * initializer twice, and a consume-once take would be swallowed by the run
 * whose state React throws away. An effect's mount → cleanup → mount cycle
 * preserves state, so the second run simply finds nothing to do.
 */
let requestedOpen: string | undefined

export function requestOpen(path: string): void {
  requestedOpen = path
}

export function takeRequestedOpen(): string | undefined {
  const path = requestedOpen
  requestedOpen = undefined
  return path
}

/** Start from a shell that has published nothing. Tests only. */
export function forgetAuthorBridge(): void {
  dirty = false
  requestedOpen = undefined
  for (const listener of dirtyListeners) listener()
}
