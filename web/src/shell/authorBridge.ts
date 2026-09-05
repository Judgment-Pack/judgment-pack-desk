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

/**
 * Which paths have unsaved bytes — **a map, not a flag**.
 *
 * There is a second editor now. A single `let dirty` is a claim about "the
 * editor", and with two of them the last one to publish decided for both: the
 * pack editor mounting clean withdrew the dot the authoring view had raised,
 * and the authoring view unmounting withdrew the dot the pack editor was
 * holding. Each publisher owns its own key and withdraws only its own.
 *
 * What the rail's dot means is unchanged, and is what `useAuthorDirty`
 * answers: **something** on this desk has unsaved bytes.
 */
const dirtyPaths = new Map<string, boolean>()
const dirtyListeners = new Set<() => void>()
let anyDirty = false

function announceDirty(): void {
  const next = [...dirtyPaths.values()].some(Boolean)
  if (next === anyDirty) return
  anyDirty = next
  for (const listener of dirtyListeners) listener()
}

/** An editor publishes for its own path; the rail reads the union. */
export function publishDirty(path: string, next: boolean): void {
  if (next) dirtyPaths.set(path, true)
  else dirtyPaths.delete(path)
  announceDirty()
}

function subscribeDirty(listener: () => void): () => void {
  dirtyListeners.add(listener)
  return () => dirtyListeners.delete(listener)
}

function readDirty(): boolean {
  return anyDirty
}

export function useAuthorDirty(): boolean {
  return useSyncExternalStore(subscribeDirty, readDirty, readDirty)
}

/** Whether one particular path has unsaved bytes. */
export function isPathDirty(path: string): boolean {
  return dirtyPaths.get(path) === true
}

/**
 * Publish one view's unsaved state for the rail to read, and withdraw **its
 * own key** on unmount — a dot left standing after the editor is gone is a
 * claim about a buffer that no longer exists, and a withdrawal that cleared
 * the whole map would be a claim about somebody else's.
 */
export function usePublishedDirty(path: string, dirty: boolean): void {
  useEffect(() => {
    publishDirty(path, dirty)
    return () => publishDirty(path, false)
  }, [path, dirty])
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
 *
 * **There is no producer at the moment, and that is deliberate.** The Create-
 * pack dialog was the one caller; it now opens the new pack's own page instead,
 * which is what the redesign asks for. The mechanism and its consumer are kept
 * rather than deleted because the next thing to open a file in the editor —
 * "edit this pack", a file the console names — wants exactly this shape, and
 * because what it holds is a bug that a `[]`-dependency effect reintroduces on
 * sight. `AuthorView.test.tsx` drives it directly, so what is held here is
 * held by a test and not by a caller; deleting both is a one-line change
 * whenever the maintainer decides it should go.
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
  dirtyPaths.clear()
  requestedOpen = undefined
  anyDirty = false
  for (const listener of dirtyListeners) listener()
  announceOpen()
}
