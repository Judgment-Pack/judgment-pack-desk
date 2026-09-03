/**
 * The two facts the shell and the authoring view have to share.
 *
 * **This is a real deviation from "the routes move verbatim", and it is
 * argued rather than slipped in.** The rail's dirty dot and the Create-pack
 * dialog's open-the-new-file cannot work without it: `dirty` is component-
 * local state in `AuthorView`, fed by `FileEditor`'s `onDirty`, and the file
 * selection has no URL parameter to carry it. The alternative is to drop both
 * features in phase A and say so; that is a choice for the maintainer, not
 * something to decide silently by writing the easier code.
 *
 * **What it costs `AuthorView.tsx` is three lines** — one import and two hook
 * calls — and the effects those hooks wrap live here, with the state they
 * publish to. That is not tidying: the cost of this deviation is the number
 * quoted in the argument for it, and the argument said eight while the file
 * carried ten lines of effects, their dependency rulings and their eslint
 * suppressions. Moving them here makes the sentence above true by measurement
 * rather than by rounding.
 *
 * The module shape is `refetchLedger.ts`'s: module state, a subscription, and
 * an explicit `forget…()` for tests.
 */
import { useEffect, useSyncExternalStore } from 'react'

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
 * Publish one view's unsaved state for the rail to read, and withdraw it on
 * unmount — a dot left standing after the editor is gone is a claim about a
 * buffer that no longer exists.
 */
export function usePublishedDirty(dirty: boolean): void {
  useEffect(() => {
    publishDirty(dirty)
    return () => publishDirty(false)
  }, [dirty])
}

/**
 * A path for the authoring view to open.
 *
 * **Subscribable, not a mount-only take**, and that is the whole of the fix it
 * carries. `navigate('/author')` from `/author` matches the same route element,
 * so `AuthorView` does not remount and a `[]`-dependency effect never runs
 * again: creating a pack while the editor was already open changed nothing on
 * screen and left the request sitting in this module, to be picked up later by
 * an unrelated mount. So the value is published like `dirty` is, and the view
 * consumes it through its own guarded `choose()` — which asks about an unsaved
 * buffer before it switches, exactly as clicking a file in the list does.
 *
 * Still consumed once, by `takeRequestedOpen`. AuthorView takes it in an
 * **effect** and not in a lazy `useState` initializer: StrictMode invokes an
 * initializer twice, and a consume-once take would be swallowed by the run
 * whose state React throws away. An effect's mount → cleanup → mount cycle
 * preserves state, so the second run simply finds nothing to do.
 */
let requestedOpen: string | undefined
const openListeners = new Set<() => void>()

function announceOpen(): void {
  for (const listener of openListeners) listener()
}

export function requestOpen(path: string): void {
  if (path === requestedOpen) return
  requestedOpen = path
  announceOpen()
}

function subscribeOpen(listener: () => void): () => void {
  openListeners.add(listener)
  return () => openListeners.delete(listener)
}

function readOpen(): string | undefined {
  return requestedOpen
}

/** The pending request, as a value a component re-renders on. */
export function useRequestedOpen(): string | undefined {
  return useSyncExternalStore(subscribeOpen, readOpen, readOpen)
}

/**
 * Hand each pending open request to one consumer, once.
 *
 * Driven by the published value rather than by mount, because creating a pack
 * while the editor is already open navigates to the route it is already on:
 * the element does not remount, and a `[]`-dependency effect would never run
 * again. The request then sat in module state until an unrelated mount
 * consumed it.
 *
 * Still an effect and not a lazy `useState` initializer: StrictMode invokes an
 * initializer twice and would swallow a consume-once take in the run whose
 * state React discards. An effect's mount → cleanup → mount preserves state,
 * so the second run simply finds nothing to do.
 */
export function useOpenRequests(open: (path: string) => void): void {
  const requestedOpen = useRequestedOpen()
  useEffect(() => {
    const requested = takeRequestedOpen()
    if (requested) open(requested)
    // `open` is re-created on every render of the view and is deliberately not
    // a dependency: this fires on a new request, never on a re-render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestedOpen])
}

export function takeRequestedOpen(): string | undefined {
  const path = requestedOpen
  if (path === undefined) return undefined
  requestedOpen = undefined
  // Announced, so no subscriber is left rendering a request that is gone.
  // It terminates: the next read is `undefined`, which publishes nothing.
  announceOpen()
  return path
}

/** Start from a shell that has published nothing. Tests only. */
export function forgetAuthorBridge(): void {
  dirty = false
  requestedOpen = undefined
  for (const listener of dirtyListeners) listener()
  announceOpen()
}
