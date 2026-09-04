/**
 * Which bytes the check is about, and when they are sent.
 *
 * `useValidate` is keyed on the exact bytes, so a call per keystroke is a call
 * per keystroke. What is sent is a **snapshot** the buffer settles on: the
 * first bytes go at once, because the check on load is not a response to
 * typing, and every later change waits for a pause.
 *
 * The gap between the snapshot and the buffer is the whole of the stale story
 * and is deliberately visible: while they differ, the report on screen is
 * about bytes that have moved, `isStale` says so, and **no** diagnostic is
 * anchored — because nothing in a pointer says which of them would still be
 * right after an edit. `checkNow` closes the gap on demand, which is what the
 * toolbar's Check does and what the save path does before it submits.
 */
import { useCallback, useEffect, useRef, useState } from 'react'

/** How long the buffer has to sit still before it is checked. */
export const IDLE_MS = 600

export interface IdleCheck {
  /** The bytes the check is running over, which may be behind the buffer. */
  checkedText: string | undefined
  /** True while the buffer has moved past the snapshot. */
  behind: boolean
  /** Send the buffer now: the toolbar's Check, and the save path. */
  checkNow: () => void
}

export function useIdleCheck(text: string | undefined, idleMs = IDLE_MS): IdleCheck {
  const [snapshot, setSnapshot] = useState<string | undefined>(undefined)
  // The buffer as of this render, readable from the timer without making the
  // timer depend on it — a timer rebuilt on every keystroke never fires.
  const latest = useRef<string | undefined>(text)
  latest.current = text

  useEffect(() => {
    if (text === undefined) return
    // The first bytes are not a response to typing: the check on load runs at
    // once, exactly as it did before there was an editor.
    if (snapshot === undefined) {
      setSnapshot(text)
      return
    }
    if (text === snapshot) return
    const timer = setTimeout(() => setSnapshot(latest.current), idleMs)
    return () => clearTimeout(timer)
  }, [text, snapshot, idleMs])

  const checkNow = useCallback(() => setSnapshot(latest.current), [])

  return {
    checkedText: snapshot,
    behind: snapshot !== undefined && text !== undefined && snapshot !== text,
    checkNow
  }
}
