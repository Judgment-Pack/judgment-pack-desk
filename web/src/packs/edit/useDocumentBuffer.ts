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
  /**
   * Take a fresh revision as the base: an initial load, or an explicit reload.
   *
   * `expect` is the identity the caller believed it was reloading — the path it
   * asked for and the generation the buffer was on. A read that resolves after
   * the page has moved on is answering about a document nobody is looking at,
   * and installing it replaced another pack's dirty buffer with it.
   */
  rebase: (fresh: FileContent, expect?: BufferIdentity) => void
  /** What this buffer is about now, for a caller that must outlive an await. */
  identity: BufferIdentity | undefined
  /** Bumped whenever this buffer is put down; part of `identity`. */
  generation: number
  /**
   * Move the base onto what a save landed, **keeping work the save did not
   * carry**.
   *
   * The live text is replaced only where it is still the bytes that were
   * submitted *and* the chassis read those bytes back. Anything else — text
   * typed while the PUT was in flight, a read-back that is not what was sent —
   * leaves the buffer where it is, dirty against the new base, because the
   * alternative is an editor that throws away an author's last sentence
   * whenever a save is slower than they are.
   */
  landed: (fresh: FileContent, submitted: string) => void
  /**
   * A revision for **another path**, held because this buffer has unsaved
   * work.
   *
   * The path a page is about can move under a route that has not navigated:
   * the listing re-answers, `get_pack` names a different file. Seeding on that
   * silently replaced an author's edits with another document's bytes, so it
   * is offered instead of taken.
   */
  waiting: FileContent | undefined
  /** Take the waiting revision, discarding what is in the buffer. */
  takeWaiting: () => void
  /** Forget this document entirely, so the next file seeds cleanly. */
  forget: () => void
}

/** Which document a buffer is holding, and which incarnation of it. */
export interface BufferIdentity {
  path: string
  generation: number
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
  onDiscard?: () => void,
  options?: {
    /**
     * Work this hook cannot see, which still has to be lost before a different
     * file may be taken.
     *
     * Operand text an author has typed but not written lives beside the bytes,
     * so a buffer that asked only "are the bytes dirty" adopted another
     * document over it and the text went with nothing having asked.
     */
    otherWork?: boolean
    /**
     * Called when a new file actually becomes the buffer.
     *
     * Whatever the caller holds *about* a document — the unwritten drafts —
     * goes then, and not a moment before: the route used to clear them the
     * instant the path moved, which threw them away while the buffer itself was
     * still asking whether the new file could be taken at all.
     */
    onAdopt?: () => void
  }
): DocumentBuffer {
  const [base, setBase] = useState<FileContent | undefined>(undefined)
  const [text, setText] = useState<string | undefined>(undefined)
  const [stack, setStack] = useState<Snapshot[]>([])
  // The stack as of this render, readable from a callback without making that
  // callback depend on it.
  const stackNow = useRef<Snapshot[]>(stack)
  stackNow.current = stack

  // **The buffer follows the file, and it is seeded once per file.**
  //
  // A later answer about the *same* file is a watcher refetch and must not move
  // the base — that is the whole of the stale-write story, and it is stated
  // here as well as in `useFileEditing` because this hook can be driven on its
  // own. A later answer about a *different* file is not a refetch at all: it is
  // another document, and a buffer that kept the first one would draw pack A
  // under pack B's address and send A's bytes to B's path on the next save.
  // `AuthorView` holds the same rule by remounting its editor per file
  // (`key={selected}`); this hook holds it without a remount, so the mode
  // toggle can keep the mount, the scroll and the buffer.
  //
  // The path is what is compared, never the content: two revisions of one file
  // have the same path, which is exactly the case that must not rebase.
  // The bytes a commit is pushing *away from*, readable without asking React
  // for them inside another updater — an updater that calls a setter is not
  // the pure function React is entitled to call twice.
  const current = useRef<string | undefined>(undefined)
  current.current = text

  const seeded = useRef<string | undefined>(undefined)
  const [waiting, setWaiting] = useState<FileContent | undefined>(undefined)
  /**
   * Bumped by `forget`, so the seeding effect runs again for a file it has
   * already seen.
   *
   * A route that leaves a pack and comes back gets the same `FileContent`
   * object out of the query cache, and an effect keyed on that object alone
   * never fires again — the buffer stayed forgotten and the editor had nothing
   * to edit.
   */
  const [generation, setGeneration] = useState(0)
  // Whether there is work to lose, readable from the effect below without
  // making it depend on the text: it must run when the *file* changes and not
  // on every keystroke.
  const dirtyNow = useRef(false)

  // Read from effects and callbacks that must not re-run on every keystroke.
  const otherWork = useRef(false)
  otherWork.current = options?.otherWork === true
  const onAdopt = useRef<(() => void) | undefined>(undefined)
  onAdopt.current = options?.onAdopt

  const adopt = useCallback((fresh: FileContent) => {
    seeded.current = fresh.path
    setBase(fresh)
    current.current = fresh.content
    setText(fresh.content)
    // The stack is about the document it was built over. An entry from the
    // previous pack would put that pack's bytes into this one.
    setStack([])
    setWaiting(undefined)
    onAdopt.current?.()
  }, [])

  useEffect(() => {
    void generation
    if (loaded === undefined) return
    if (seeded.current === loaded.path) {
      // **Back on the file this buffer is about.** A→B→A left B waiting behind
      // the page, and its offer — "open it and lose these changes" — was still
      // on screen: pressing it discarded a dirty A and adopted a document the
      // address is not about.
      setWaiting((held) => (held === undefined ? held : undefined))
      return
    }
    // **A different file arriving over unsaved work is a question, not an
    // answer.** A route that navigated has already asked it — the dirty guard
    // is on the pathname — but a path that moves *under* one address has asked
    // nobody, and taking the new bytes there discards an edit with nothing on
    // screen having offered to keep it.
    if (seeded.current !== undefined && (dirtyNow.current || otherWork.current)) {
      setWaiting(loaded)
      return
    }
    adopt(loaded)
  }, [loaded, generation, adopt])

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

  // This file is now seeded, whichever way it got here — so a watcher answer
  // arriving after a save or a reload is still a refetch and still does not
  // re-seed.
  //
  // **An identity may be stated, and a stale one is refused.** A reload is a
  // read that takes as long as it takes; between the ask and the answer the
  // page can be about another pack, with its own unsaved work. Installing the
  // answer there replaced that work with a document nobody asked for.
  const generationNow = useRef(generation)
  generationNow.current = generation
  const rebase = useCallback(
    (fresh: FileContent, expect?: BufferIdentity) => {
      if (expect !== undefined) {
        if (expect.generation !== generationNow.current) return
        if (seeded.current !== undefined && seeded.current !== expect.path) return
        if (fresh.path !== expect.path) return
      }
      adopt(fresh)
    },
    [adopt]
  )

  const landed = useCallback((fresh: FileContent, submitted: string) => {
    seeded.current = fresh.path
    setBase(fresh)
    setWaiting(undefined)
    // **Only a save that carried everything replaces the buffer.** The author
    // is free to keep typing while the PUT is in flight, and every one of those
    // keystrokes is work this save did not send; so is a read-back that is not
    // what was sent. Either way the text stays exactly where it is and `dirty`
    // says what is true of it against the revision that landed.
    if (current.current !== submitted || fresh.content !== submitted) return
    current.current = fresh.content
    setText(fresh.content)
    setStack([])
  }, [])

  const takeWaiting = useCallback(() => {
    // Only the file the address is about now. The offer can outlive the address
    // that produced it — A→B→A — and taking it then would discard the work in
    // front of the viewer for a document they are not looking at.
    if (waiting === undefined || loaded?.path !== waiting.path) return
    adopt(waiting)
  }, [waiting, loaded, adopt])

  const forget = useCallback(() => {
    seeded.current = undefined
    setGeneration((count) => count + 1)
    setBase(undefined)
    current.current = undefined
    setText(undefined)
    setStack([])
    setWaiting(undefined)
  }, [])

  const dirty = text !== undefined && base !== undefined && text !== base.content
  dirtyNow.current = dirty

  return useMemo(
    () => ({
      base,
      text,
      dirty,
      commit,
      undo,
      canUndo: stack.length > 0,
      discard,
      rebase,
      identity: seeded.current === undefined ? undefined : { path: seeded.current, generation },
      generation,
      landed,
      waiting,
      takeWaiting,
      forget
    }),
    [
      base,
      text,
      dirty,
      commit,
      undo,
      stack.length,
      discard,
      rebase,
      generation,
      landed,
      waiting,
      takeWaiting,
      forget
    ]
  )
}
