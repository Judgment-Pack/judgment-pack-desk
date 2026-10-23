/**
 * Text an author has typed into an operand that is not JSON yet — and the one
 * rule about when holding it counts as an edit.
 *
 * The text lives beside the bytes rather than in them, so nothing about it
 * moves the buffer. That is why it needs saying out loud: it is still the
 * author's work, and a reload that lands over it would take it away with the
 * document it adopted. So holding it moves the buffer's edit revision, and a
 * reload ticket issued before it goes stale exactly as a commit makes one.
 *
 * **Only where something actually changes.** Releasing text is not an edit —
 * the write that follows it is, and the operand controls release and then write
 * in one gesture, so counting both would spend two revisions on one action and
 * make the counter something other than a count of edits. Holding the same text
 * twice is not an edit either: a control that re-reports what it already holds
 * has told the desk nothing new.
 */
import { useCallback, useMemo, useRef, useState } from 'react'
import type { PendingText } from './editingContext'

/**
 * Whether holding this draft is a change to the author's work.
 *
 * A release is never one. A first hold always is. Otherwise it is a change
 * exactly where the draft differs — the text, the bytes it started from, or
 * the card it sits in, because the last two are what retire it.
 */
export function heldChanged(before: PendingText | undefined, draft: PendingText | null): boolean {
  if (draft === null) return false
  if (before === undefined) return true
  return before.text !== draft.text || before.from !== draft.from || before.owner !== draft.owner
}

export interface HeldText {
  /** What is held now, by pointer. */
  drafts: ReadonlyMap<string, PendingText>
  /** Includes a hold made before React renders its state update. */
  hasPending: () => boolean
  /** Hold what was typed at one pointer, or let it go once it is written. */
  hold: (pointer: string, draft: PendingText | null) => void
  /** Let all of it go, which adopting another document does. */
  forget: () => void
  /**
   * Replace the map wholesale — the retirement pass, which drops every draft
   * whose bytes have moved. It is not an edit: it is the *consequence* of one,
   * and the edit that caused it has moved the revision already.
   */
  update: (
    next: (held: ReadonlyMap<string, PendingText>) => ReadonlyMap<string, PendingText>
  ) => void
}

export function useHeldText(touch: () => void): HeldText {
  const [drafts, setDrafts] = useState<ReadonlyMap<string, PendingText>>(new Map())
  // Updated synchronously by every mutation so a captured writer can veto a
  // hold before its React state update renders.
  const now = useRef<ReadonlyMap<string, PendingText>>(drafts)
  now.current = drafts
  const touchNow = useRef(touch)
  touchNow.current = touch

  const hold = useCallback((pointer: string, draft: PendingText | null) => {
    // **Outside the updater.** React is entitled to call an updater twice, and
    // one that reported an edit would report two.
    if (heldChanged(now.current.get(pointer), draft)) touchNow.current()
    if (draft === null && !now.current.has(pointer)) return
    const next = new Map(now.current)
    if (draft === null) next.delete(pointer)
    else next.set(pointer, draft)
    now.current = next
    setDrafts(next)
  }, [])

  const forget = useCallback(() => { now.current = new Map(); setDrafts(now.current) }, [])
  const update = useCallback(
    (next: (held: ReadonlyMap<string, PendingText>) => ReadonlyMap<string, PendingText>) =>
      { now.current = next(now.current); setDrafts(now.current) },
    []
  )

  const hasPending = useCallback(() => now.current.size > 0, [])
  return useMemo(() => ({ drafts, hold, forget, update, hasPending }), [drafts, hold, forget, update, hasPending])
}
