/**
 * The buffer of record, and the three things a viewer can do to it that are
 * not edits: undo one action, discard everything, and start again from a
 * revision.
 *
 * **Dirty is a byte comparison.** Not a parse comparison: two documents that
 * parse the same are still two files, and a save writes bytes. Re-indenting a
 * rule, adding the trailing newline a formatter wants, replacing a tab — each
 * of those is a change to the file on disk, and an editor that called them
 * clean would offer to throw them away without asking.
 *
 * **One snapshot per committed action, typing coalesced per field.** A stack
 * of every keystroke makes Undo a character eraser and puts a hundred copies
 * of the document in memory for one sentence; a stack of one entry per *edit*
 * makes it the action it looks like. Coalescing is by key — the field being
 * typed into — so moving to another field starts a new entry, and an action
 * that is not typing (a move, a kind change, an add) always does.
 *
 * The cap is a real cap: past it the **oldest** entry is dropped and
 * `canUndo` goes false when the stack is empty, rather than the stack lying
 * about how far back it can go.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { FileContent } from '../../files/client'

/** How many committed actions can be taken back. */
export const UNDO_DEPTH = 50

export interface DocumentBuffer {
  /** The revision this edit is against. Moves only where the viewer acts. */
  base: FileContent | undefined
  /** The bytes on screen. */
  text: string | undefined
  /** A byte comparison against the base. */
  dirty: boolean
  /** Write the buffer, pushing one undo entry. */
  commit: (next: string, options?: { coalesceKey?: string }) => void
  undo: () => void
  canUndo: boolean
  /** Put the buffer back to the base, and forget the stack. */
  discard: () => void
  /** Take a fresh revision as the base: load, explicit reload, saved. */
  rebase: (fresh: FileContent) => void
}

/** One entry on the stack: the bytes, and what was being typed into. */
interface Snapshot {
  text: string
  coalesceKey?: string
}

export function useDocumentBuffer(
  loaded: FileContent | undefined,
  /**
   * What else a discard puts back.
   *
   * The last save attempt's verdict lives with the save, not with the buffer,
   * and leaving "Saved, and verified" — or a stale-write offer to overwrite —
   * standing beside a buffer that no longer differs states something about
   * bytes nobody is proposing. `AuthorView`'s Discard already clears both for
   * this reason; this is the same rule with the two halves in two modules.
   */
  onDiscard?: () => void
): DocumentBuffer {
  const [base, setBase] = useState<FileContent | undefined>(undefined)
  const [text, setText] = useState<string | undefined>(undefined)
  const [stack, setStack] = useState<Snapshot[]>([])
  // The stack as of this render, readable from a callback without making that
  // callback depend on it.
  const stackNow = useRef<Snapshot[]>(stack)
  stackNow.current = stack

  // The first successful load seeds both, once. A later answer about the same
  // file is a watcher refetch and must not move the base — that is the whole
  // of the stale-write story, and it is stated here as well as in
  // `useFileEditing` because this hook can be driven on its own.
  useEffect(() => {
    if (loaded !== undefined && base === undefined) {
      setBase(loaded)
      setText(loaded.content)
    }
  }, [loaded, base])

  // The bytes a commit is pushing *away from*, readable without asking React
  // for them inside another updater — an updater that calls a setter is not
  // the pure function React is entitled to call twice.
  const current = useRef<string | undefined>(undefined)
  current.current = text

  const commit = useCallback((next: string, options?: { coalesceKey?: string }) => {
    const previous = current.current
    if (previous === undefined) {
      setText(next)
      return
    }
    setStack((entries) => {
      const key = options?.coalesceKey
      const top = entries[entries.length - 1]
      // Typing into the field the last entry is already about replaces
      // nothing: one word typed is one action, not nine.
      if (key !== undefined && top !== undefined && top.coalesceKey === key) return entries
      const grown = [...entries, { text: previous, coalesceKey: key }]
      // The oldest goes, so the depth is a depth rather than a suggestion.
      return grown.length > UNDO_DEPTH ? grown.slice(grown.length - UNDO_DEPTH) : grown
    })
    current.current = next
    setText(next)
  }, [])

  const undo = useCallback(() => {
    // **The updater is pure.** React is entitled to call an updater twice —
    // StrictMode does — and one that called another setter would fire that
    // setter twice for one action. So the top is read from the ref that
    // already tracks the stack, and both setters are called from here.
    const entries = stackNow.current
    const top = entries[entries.length - 1]
    if (top === undefined) return
    current.current = top.text
    setText(top.text)
    setStack(entries.slice(0, -1))
  }, [])

  const discard = useCallback(() => {
    if (base !== undefined) {
      current.current = base.content
      setText(base.content)
    }
    setStack([])
    onDiscard?.()
  }, [base, onDiscard])

  const rebase = useCallback((fresh: FileContent) => {
    setBase(fresh)
    current.current = fresh.content
    setText(fresh.content)
    setStack([])
  }, [])

  const dirty = text !== undefined && base !== undefined && text !== base.content

  return useMemo(
    () => ({
      base,
      text,
      dirty,
      commit,
      undo,
      canUndo: stack.length > 0,
      discard,
      rebase
    }),
    [base, text, dirty, commit, undo, stack.length, discard, rebase]
  )
}
