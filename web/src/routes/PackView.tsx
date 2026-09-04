/**
 * One pack: the document, the check under it, and the Inspector beside it —
 * read or edited, over one buffer.
 *
 * 1. Reads the **served** document — `get_pack` gives the parsed pack, its raw
 *    text, and the metadata beside it: the declared path, the byte count and
 *    the digest.
 * 2. Reads the **file** at that path through the chassis, which is the bytes
 *    an editor holds and the base digest a save states. One read serves both
 *    modes.
 * 3. **Binds the two.** They are two answers about one file and they only
 *    describe one revision when the digests are equal. Where they are not,
 *    this says so plainly rather than picking one.
 * 4. Runs `validate` over the buffer, on idle and on demand, and labels which
 *    bytes it was about.
 * 5. Renders the document with the check strip under its outline.
 * 6. Publishes the Inspector — or the what-if pane — through the shell's slot.
 *
 * `?at` is the selection and `#pointer` is the deep link, and both are the
 * pointer address space. Selection writes are `replace: true`: choosing what
 * to inspect is not a navigation and must not fill the Back stack.
 *
 * `?edit` is the mode, on this same route, so the toggle keeps the mount, the
 * scroll, the selection and the buffer — and so the dirty blocker, whose
 * predicate is the pathname alone, never asks about it.
 *
 * **The document on screen is the buffer, and there is no fallback behind it.**
 * Both modes draw `indexDocument(buffer).value`, so a keystroke in the JSON
 * view and an edit in a form move the reading document the same way, and the
 * form is never over one revision while the page is over another. The served
 * document is drawn only **before a file has been loaded at all** — the file
 * read has not answered, or there is no path to read. Once the editor holds
 * bytes they are what the page is about, whatever they say: bytes that do not
 * scan are the JSON view, bytes that scan into something that is not a document
 * are the JSON view too, and a member of the wrong shape states itself where it
 * sits. Falling back to the runtime's answer there would put a document nobody
 * has on disk on screen, over a file that no longer holds it.
 *
 * **Save is never gated on the check.** The chassis writes bytes and the
 * runtime judges them, in that order. Outstanding diagnostics stay on screen
 * through a save, because a desk that decided what may exist on disk would be
 * the authority this whole surface exists not to be.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom'
import { ErrorBox, Loading } from '../components/primitives'
import { AlertPanel } from '../ui/AlertPanel'
import { Button } from '../ui/Button'
import { StaleWrite } from '../files/client'
import { useFileContent, useFileListing } from '../files/queries'
import { useFileEditing } from '../files/useFileEditing'
import { useMcp } from '../mcp/McpProvider'
import { usePack, usePacks, useValidate } from '../mcp/queries'
import type { PackDocument } from '../mcp/types'
import { agreesWithParse } from '../packs/documentText'
import { CHECK_BEHIND_BUFFER, anchor, isStale, truncationNote } from '../packs/checks'
import type { AnchoredDiagnostic } from '../packs/checks'
import { CheckStrip } from '../packs/CheckStrip'
import { PackDocumentView } from '../packs/document/PackDocumentView'
import { isRecord } from '../packs/document/MisshapenMember'
import { SelectionContext } from '../packs/document/Block'
import { EditToolbar } from '../packs/edit/EditToolbar'
import {
  EditingContext,
  declaredIds,
  ownerOf,
  type EditingSession,
  type PendingText
} from '../packs/edit/editingContext'
import { editShape, isEditing, withEditing, withShape } from '../packs/edit/editMode'
import { LockLine } from '../packs/edit/LockLine'
import { RawJsonEditor, positionOf } from '../packs/edit/RawJsonEditor'
import { StaleWriteAlert } from '../packs/edit/StaleWriteAlert'
import { TryItPane } from '../packs/edit/TryItPane'
import { useDocumentBuffer } from '../packs/edit/useDocumentBuffer'
import { useIdleCheck } from '../packs/edit/useIdleCheck'
import { buffered, bytesAt, type Buffered } from '../packs/edit/writes'
import { PackInspector } from '../packs/inspector/PackInspector'
import { outlineRepresentatives, readingOrder } from '../packs/document/members'
import { pointerFromHash } from '../packs/pointers'
import { useDocumentSpy } from '../packs/useDocumentSpy'
import { useInspectorPortal, useInspectorSlot } from '../shell/InspectorSlot'
import { usePublishedDirty } from '../shell/authorBridge'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { useMeasuredBox } from '../shell/measured'
import styles from './PackView.module.css'

/**
 * What the what-if pane needs, and what the editor must keep beside it.
 *
 * **One number, and the stylesheet reads it from here.** The predicate said 392
 * and `.pane` was `24rem`, which is 384: the arithmetic asked for eight pixels
 * that were never taken, so at the shell's own maximum — a 60rem box less its
 * padding is exactly 912 — the pane fitted and the page said it did not. The
 * width is set as a custom property on the workspace and `.pane` is `width:
 * var(--tryit-pane-width)`, so the two cannot drift again.
 */
const PANE_WIDTH = 384
const EDITOR_FLOOR = 512
/** The `1rem` between the two, from `.workspace`'s own gap. */
const PANE_GAP = 16

const LEAVING = 'This pack has unsaved changes. Leave without saving?'

export function PackView() {
  const { packId } = useParams<{ packId: string }>()
  const { hash, key: locationKey } = useLocation()
  const [params, setParams] = useSearchParams()
  const { known, status, validateSupported, rehearsalSupported } = useMcp()
  const queryClient = useQueryClient()
  const pack = usePack(packId)
  const listing = usePacks()
  const files = useFileListing()
  const summary = (listing.data?.packs ?? []).find((entry) => entry.id === packId)
  const meta = pack.data?.meta
  /**
   * The file this page is about.
   *
   * `get_pack` names it, and the **listing** names it too — which is what
   * keeps the editor reachable for a pack the runtime will not serve. Bytes
   * this desk wrote that do not parse are exactly that case, and a page that
   * learned the path only from `get_pack` had no way back to them.
   */
  const path = meta?.path ?? summary?.path
  const file = useFileContent(path)
  const slot = useInspectorSlot()

  const editing = isEditing(params)
  const askedShape = editShape(params)

  // The revision, the write, and the verdict the last write left behind. The
  // buffer's discard clears that verdict, which is why the two are wired
  // together here rather than each holding half of it.
  const editor = useFileEditing()
  /**
   * Text an author has typed into an operand that is not JSON yet.
   *
   * It is the route's rather than the field's, because both ways out of a form
   * — the JSON view, and a save — unmount the field. See `EditingSession`.
   */
  const [drafts, setDrafts] = useState<ReadonlyMap<string, PendingText>>(new Map())
  const hold = useCallback((pointer: string, draft: PendingText | null) => {
    setDrafts((held) => {
      const next = new Map(held)
      if (draft === null) next.delete(pointer)
      else next.set(pointer, draft)
      return next
    })
  }, [])
  const forgetDrafts = useCallback(() => setDrafts(new Map()), [])
  /**
   * **The drafts are work, and the buffer has to know.** They live beside the
   * bytes, so a buffer asking only "are the bytes dirty" adopted another
   * document over an unfinished operand without anyone being asked — and they
   * go when a new file is actually taken, rather than the moment a path moves.
   */
  const buffer = useDocumentBuffer(file.data, editor.reset, {
    otherWork: drafts.size > 0,
    onAdopt: forgetDrafts
  })
  /**
   * **The path is part of the buffer's identity.**
   *
   * `path` moves the moment `get_pack(B)` answers, while the buffer still
   * holds A until B's file read lands and the seeding effect runs. In that gap
   * the page drew A's document under B's address and Save combined B's path
   * with A's bytes and A's digest — which a 409 usually catches and equal
   * digests, or the Overwrite the 409 itself offers, does not. Nothing is
   * drawn from the buffer, and nothing may be written from it, until the two
   * agree.
   */
  const onPath = path !== undefined && buffer.base?.path === path
  const bufferText = onPath ? buffer.text : undefined

  /**
   * What one file's page must forget when the address moves to another.
   *
   * The last write's verdict, which is about a file nobody is looking at any
   * more — a stale-write alert carried across would name a conflict on another
   * pack — and the single-flight latch, which belongs to the save that was in
   * flight for the file this page has left.
   *
   * **Not the drafts.** They are work, and the buffer is what decides whether
   * work may be replaced; clearing them here threw them away while the buffer
   * was still asking. They go when a file is actually adopted (`onAdopt`).
   */
  const reset = editor.reset
  const opened = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (path === undefined || opened.current === path) return
    const first = opened.current === undefined
    opened.current = path
    if (first) return
    reset()
    saving.current = undefined
  }, [path, reset])

  /**
   * A different **pack** is a different document, and this one is put down.
   *
   * The dirty guard is on the pathname, so arriving at another pack's address
   * is a navigation the viewer has already been asked about. What is left is to
   * forget the buffer rather than hold it as work to be rescued — that offer
   * belongs to a path that moved under *one* address, which nobody was asked
   * about (`buffer.waiting`).
   */
  const forget = buffer.forget
  const openedPack = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (packId === undefined || openedPack.current === packId) return
    const first = openedPack.current === undefined
    openedPack.current = packId
    if (first) return
    forget()
    // A different pack is a different document and the guard has already asked
    // about it. Every pointer these drafts name is in a document nobody is
    // looking at.
    forgetDrafts()
  }, [packId, forget, forgetDrafts])

  // This desk's reading of the bytes on screen, computed once per revision of
  // them. Every form field reads its value out of this and every write splices
  // into it.
  const read: Buffered | undefined = useMemo(
    () => (bufferText === undefined ? undefined : buffered(bufferText)),
    [bufferText]
  )

  const at = params.get('at')
  const select = (pointer: string) => {
    const next = new URLSearchParams(params)
    next.set('at', pointer)
    setParams(next, { replace: true })
  }

  /**
   * Arriving with a selection is arriving at a link someone sent, so the pane
   * it addresses is opened — once per arrival, and an arrival is a
   * `location.key`.
   */
  const visited = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (visited.current === locationKey) return
    visited.current = locationKey
    if (at === null) return
    slot.reveal()
  }, [at, locationKey, slot])

  // The bytes the page is about: the buffer where the file loaded, the served
  // document where it did not.
  const servedText = pack.data?.raw
  const shownText = bufferText ?? servedText
  /**
   * Whether the revision this editor loaded is still the one on disk.
   *
   * A watcher refetch moves the file query and deliberately does **not** move
   * the base — that is the whole of the stale-write story — so a clean buffer
   * can be a *previous* revision of the file while the page calls it "the bytes
   * of packs/x.pack.json". It is the bytes that were loaded, and where the disk
   * has moved on this says so and offers the read.
   */
  const behindDisk =
    onPath &&
    buffer.base !== undefined &&
    file.data !== undefined &&
    file.data.sha256 !== buffer.base.sha256
  const whichBytes =
    bufferText !== undefined
      ? buffer.dirty
        ? 'the bytes in the editor'
        : behindDisk
          ? `the bytes you loaded from ${path ?? 'the file on disk'}`
          : `the bytes of ${path ?? 'the file on disk'}`
      : 'the document the runtime served'

  // What is sent, and when. The first bytes go at once; every later change
  // waits for a pause, and `checkNow` closes the gap for the toolbar's Check
  // and for the save path.
  const idle = useIdleCheck(shownText)
  const check = useValidate(idle.checkedText)

  /**
   * The desk's own reading of the bytes, against `JSON.parse`'s.
   *
   * A disagreement — a duplicated member, bytes that do not scan — withholds
   * form mode: a form that wrote through a reading the runtime does not share
   * would edit a document nobody has. The raw bytes stay available, which is
   * the only view that is honest about a document neither reading can take.
   */
  const disagreement = useMemo(
    () => (bufferText === undefined || read === undefined ? [] : agreesWithParse(bufferText, read.index)),
    [bufferText, read]
  )
  /**
   * **A document is an object.** `null`, `[]`, `"a string"` and `7` are each
   * valid JSON that scans, agrees with `JSON.parse`, and is not a pack — and
   * every reader below starts with `Object.keys` or a member lookup. Bytes that
   * are not a record reach the JSON view and the strip and nothing else.
   */
  const formAvailable = disagreement.length === 0 && isRecord(read?.index.value)
  const shape = formAvailable ? askedShape : 'json'
  const withheld = formAvailable ? undefined : formWithheld(read, bufferText, disagreement)

  /**
   * The document drawn on the page — **the buffer's**, in both modes.
   *
   * A form over the served document and a page over the buffer is exactly the
   * failure the digest binding exists to prevent, one component further in: a
   * field would write at a pointer computed from bytes the reader is not
   * looking at. Where the buffer does not scan there is nothing to draw from
   * it, and the served document is the fallback — with the JSON view holding
   * the bytes that do not scan and saying where they stop.
   */
  const served = pack.data?.document
  const drawn: PackDocument | undefined = onPath
    ? isRecord(read?.index.value)
      ? (read.index.value as unknown as PackDocument)
      : undefined
    // Before a file has been loaded at all, the runtime's answer is what there
    // is — and it is held to the same question: `get_pack` can serve `null`,
    // and `Object.keys(null)` is where the route ended.
    : isRecord(served)
      ? (served as PackDocument)
      : undefined

  // Which blocks are actually on screen. Read off the DOM rather than derived
  // from the document, because "nearest **rendered** ancestor" is a claim about
  // what is on the page.
  const [rendered, setRendered] = useState<ReadonlySet<string>>(new Set(['']))
  const rawMode = editing && shape === 'json'
  /**
   * What makes this page *this* page, for the two effects that walk the DOM.
   *
   * The **checked** snapshot and not the live buffer. Both of them tear down
   * and rebuild — the rendered set re-walks every `[data-pointer]`, the scroll
   * spy disconnects and re-observes twelve elements and drops its answer back
   * to the `?at` selection — and keyed on the buffer that happened on every
   * keystroke, which is a reader's outline flickering while they type. The
   * snapshot moves on a pause, which is also when the diagnostics these two
   * feed arrive: the rendered set and the report it anchors against are then
   * about the same bytes by construction.
   */
  const documentKey = `${idle.checkedText ?? shownText ?? ''}|${editing ? shape : 'read'}`
  useEffect(() => {
    if (rawMode) {
      // **The JSON view puts every member on the page**, as the bytes it is.
      // There are no blocks to read off the DOM, and the honest answer is not
      // "nothing is rendered" — that would send every diagnostic to the strip
      // and leave the Checks panel saying no diagnostic names a member the
      // runtime had just refused. The document's own pointers are what is on
      // screen, because the whole document is.
      const found = new Set<string>([''])
      for (const pointer of read?.index.spans.keys() ?? []) found.add(pointer)
      setRendered((previous) => (sameSet(previous, found) ? previous : found))
      return
    }
    const article = document.querySelector('[data-pointer=""]')
    if (article === null) return
    const found = new Set<string>([''])
    for (const element of article.querySelectorAll('[data-pointer]')) {
      const pointer = element.getAttribute('data-pointer')
      if (pointer !== null) found.add(pointer)
    }
    setRendered((previous) => (sameSet(previous, found) ? previous : found))
  }, [documentKey, at, rawMode, read])

  /**
   * Whether this report is about the bytes on screen.
   *
   * The report carries the bytes it checked, and they are compared with the
   * bytes being rendered. **No diagnostic is re-anchored across an edit**: a
   * reorder moves every `/rules/N` pointer, so an anchor computed from the old
   * text would print a real diagnostic on the wrong rule — which is worse than
   * no diagnostic at all, because it looks like an answer.
   */
  const stale = isStale(check.data?.checkedBytes, shownText)

  const unavailable = checkUnavailable(known, validateSupported, check.error, idle.checkedText)
  const fetching = check.fetchStatus === 'fetching'
  const provenance =
    unavailable !== undefined
      ? undefined
      : fetching
        ? `checking against ${whichBytes}`
        : check.data !== undefined
          ? `checked against ${whichBytes}`
          : undefined

  const report = stale ? undefined : check.data?.report
  const anchored = useMemo(() => anchor(report, rendered), [report, rendered])
  const byPointer = useMemo(() => {
    const found = new Map<string, AnchoredDiagnostic[]>()
    for (const entry of anchored) {
      const held = found.get(entry.anchor)
      if (held === undefined) found.set(entry.anchor, [entry])
      else held.push(entry)
    }
    return found
  }, [anchored])

  // The deep link. `getElementById`, never `querySelector`: a pointer contains
  // `/` and `~`, which are legal in an id and are not a selector.
  useEffect(() => {
    const pointer = pointerFromHash(hash)
    if (pointer === undefined) return
    const element = document.getElementById(pointer)
    if (element === null) return
    element.scrollIntoView()
    // In the reading view the address is a `Block` and takes focus itself. In
    // edit mode it is a field *group*, and the thing worth being focused is
    // the control inside it — so the control is preferred and the group is the
    // fallback, rather than a link that quietly stops moving focus.
    const control = element.querySelector('input, textarea, [role="combobox"]')
    ;(control instanceof HTMLElement ? control : element).focus()
    const next = new URLSearchParams(params)
    next.set('at', pointer)
    setParams(next, { replace: true })
    slot.reveal()
    // The fragment is the trigger and the only one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hash, documentKey])

  const order = useMemo(() => (drawn === undefined ? [] : readingOrder(drawn)), [drawn])
  const outlinePointers = useMemo(() => order.map((unit) => unit.pointer), [order])
  const representative = useMemo(
    () => (drawn === undefined ? new Map<string, string>() : outlineRepresentatives(drawn, order)),
    [drawn, order]
  )
  const seen = useDocumentSpy(outlinePointers, at, documentKey)
  const active = seen === null ? null : (representative.get(seen) ?? seen)

  /* The editing session ---------------------------------------------------- */

  const commit = buffer.commit
  const session: EditingSession = useMemo(
    () => ({
      editing: editing && shape === 'form' && read !== undefined,
      buffer: read ?? { text: '', index: { spans: new Map(), duplicates: [] } },
      write: (edit, options) => {
        if (read === undefined) return
        commit(edit(read).text, options)
      },
      diagnosticsAt: (pointer) => byPointer.get(pointer) ?? [],
      ids: declaredIds(drawn ?? {}, summary?.consultedFactPaths ?? []),
      pending: drafts,
      hold
    }),
    [editing, shape, read, commit, byPointer, drawn, summary?.consultedFactPaths, drafts, hold]
  )

  /**
   * How many operands are holding text that is not written.
   *
   * Only the ones still about the bytes they started from: undo, the JSON view
   * and a kind change all retire a draft, and counting a retired one would say
   * a field is unwritten when nothing on screen says so.
   */
  /**
   * A draft is retired **permanently** when the bytes it started from move.
   *
   * It was an equality mask over a map that kept everything: a raw edit away
   * from the operand and an Undo back to it made `from` match again, and text
   * an author had abandoned two actions ago reappeared in a field. Retirement
   * is a deletion, so what is gone is gone.
   */
  useEffect(() => {
    if (read === undefined) return
    setDrafts((held) => {
      let changed = false
      const next = new Map(held)
      for (const [pointer, draft] of held) {
        // **The bytes at the pointer and the card they sit in.** A pointer is a
        // position: moving rule 1 above rule 0 leaves `/rules/0/when/value`
        // naming another rule's operand, and where the two read the same the
        // byte comparison alone saw nothing move — the draft stayed, and
        // finishing it wrote the rule the author was not editing.
        if (
          (bytesAt(read, pointer) ?? '') === draft.from &&
          ownerOf(read, pointer) === draft.owner
        ) {
          continue
        }
        next.delete(pointer)
        changed = true
      }
      return changed ? next : held
    })
  }, [read])

  const unwritten = drafts.size

  /* Saving ----------------------------------------------------------------- */

  const dirty = buffer.dirty
  const staleWrite = editor.write.error instanceof StaleWrite ? editor.write.error : undefined
  // A refusal that is not a conflict — the path is not writable, the chassis is
  // not there, the body did not parse. It was rendered nowhere at all: the
  // button stopped saying "Saving…" and the page said nothing about why.
  const saveFailure = editor.write.error !== null && staleWrite === undefined ? editor.write.error : undefined
  /**
   * Reload, with the identity of what is being reloaded travelling with it.
   *
   * A read takes as long as it takes. Holding Reload on pack A, moving to B and
   * editing it, and *then* letting A's read land replaced B's dirty buffer with
   * A's bytes — no confirmation, no undo, and the alert about it named B. The
   * ticket says which pack, which path and which incarnation of the buffer the
   * read was for, and the buffer refuses one that no longer holds.
   */
  const rebase = buffer.rebase
  const reloadNow = useCallback(() => {
    if (path === undefined || buffer.identity === undefined) return
    // **The identity travels with the read**, and the buffer is the one place
    // that decides whether it still holds. Leaving a pack bumps the generation
    // and re-seeds on the new path, so a ticket taken before that is refused —
    // by `rebase`, once, rather than by two readers that could disagree.
    const ticket = buffer.identity
    editor.reload(path, (fresh) => rebase(fresh, ticket))
  }, [path, editor, buffer.identity, rebase])

  /**
   * The path a save is in flight for, or nothing.
   *
   * **Scoped to the file, and released on settlement.** It was a boolean
   * released by the mutation's own `onSettled`, which react-query delivers
   * through the observer — and `write.reset()` on a path change detaches that
   * observer, so a save in flight across the change never settled and the latch
   * was held for ever: every later Save returned silently, on every pack.
   */
  const saving = useRef<string | undefined>(undefined)
  const save = useCallback(
    (override?: boolean) => {
      if (path === undefined || bufferText === undefined || buffer.base === undefined) return
      // **One save at a time, decided synchronously.** `write.isPending` is
      // state and arrives a render later, so two chords inside one frame both
      // read "not saving" and issue two PUTs against one base — the second of
      // which the chassis answers with a 409 this page then explains as
      // somebody else's edit.
      if (saving.current !== undefined) return
      // The bytes are the ones sent, and they are checked against the path they
      // are being sent to: `path` moves with `get_pack` and the buffer moves
      // with the file read, so the two are not always about one file.
      if (buffer.base.path !== path) return
      const submitted = bufferText
      saving.current = path
      // The check runs before the save and does **not** gate it. Sending the
      // buffer now means the diagnostics on screen afterwards are about the
      // bytes that were written rather than about whatever was last idle.
      idle.checkNow()
      editor.save({
        path,
        content: submitted,
        baseSha256: buffer.base.sha256,
        override,
        onSettled: () => {
          // Only the save this latch was taken for releases it: a settlement
          // arriving after the page moved on is about another file.
          if (saving.current === path) saving.current = undefined
        },
        onSaved: (landed) => {
          buffer.landed(landed, submitted)
          // The runtime is now serving a file it has already read. These three
          // are what would otherwise keep answering about the old revision.
          void queryClient.invalidateQueries({ queryKey: ['list_packs'] })
          void queryClient.invalidateQueries({ queryKey: ['get_pack', packId] })
          void queryClient.invalidateQueries({ queryKey: ['validate'] })
        }
      })
    },
    [path, bufferText, buffer, editor, idle, packId, queryClient]
  )

  /**
   * Mod+S, while this pack is being edited.
   *
   * **It is the mode's chord and not a subtree's.** It has to fire inside a
   * text field, which is the one place the shell's own chords are suppressed —
   * `isTypingTarget` exists so typing an `s` never opens a pane — so it is not
   * registered through `installShortcuts`. But it was registered on the
   * editor's column, and `document.body` is a reachable resting place for
   * focus: it is where focus sits the moment edit mode opens, because the Edit
   * button unmounts itself. From there the chord neither saved nor called
   * `preventDefault`, so the browser's own "Save page as…" opened over unsaved
   * work. Installed here it is claimed wherever focus is, for exactly as long
   * as the mode it belongs to is on screen.
   *
   * `preventDefault` runs whether or not there is anything to save: the chord
   * is this page's in this mode, and handing a clean buffer's Mod+S back to the
   * browser would make the behaviour depend on something the viewer cannot see.
   */
  const saveNow = useRef(save)
  saveNow.current = save
  useEffect(() => {
    if (!editing) return
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 's') return
      if (event.altKey || event.shiftKey) return
      // **Held down is one save.** A key repeat is the operating system saying
      // the key is still down, not the author asking again; and an event
      // another handler has already answered is not this page's to answer
      // twice. `save` itself refuses to overlap, and these two keep the
      // refusals from happening at all.
      if (event.repeat || event.defaultPrevented) return
      event.preventDefault()
      if (dirty) saveNow.current()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [editing, dirty])

  /**
   * What the viewer would lose by leaving — which is **not only bytes**.
   *
   * Text typed into an operand that is not JSON yet lives beside the buffer, so
   * a form whose only edit was one of those was byte-clean: Discard was
   * disabled, the rail showed nothing, and navigating away took the work with
   * no question asked. Dirty stays a byte comparison, because a save writes
   * bytes; this is the wider question of whether there is anything to lose.
   */
  const hasWork = dirty || unwritten > 0
  const discardAll = useCallback(() => {
    buffer.discard()
    setDrafts(new Map())
  }, [buffer])

  usePublishedDirty(path ?? `pack:${packId ?? ''}`, hasWork)
  useDirtyGuard(hasWork, LEAVING)

  /* Try it ----------------------------------------------------------------- */

  const [tryingIt, setTryingIt] = useState(false)
  const [frame, setFrame] = useState<HTMLDivElement | null>(null)
  const box = useMeasuredBox(frame)
  /**
   * The pane goes beside the editor only where the editor keeps its floor.
   * Otherwise it takes the Inspector's place, which is the one other surface
   * wide enough to hold it.
   *
   * **The frame is measured, not the column.** The column is the pane's flex
   * sibling, so placing the pane shrinks the very box the decision was read
   * from: the predicate's input depended on its own output, and between 904
   * and 1304 pixels — which covers a 1440 shell with the Inspector closed —
   * neither answer was a fixed point. It flipped until the browser's
   * `ResizeObserver` loop guard stopped it, in the wrong state: the pane beside
   * a 744px column, which is 352px of editor, under the floor this measurement
   * exists to guarantee. The workspace's own width is the same either way, so
   * the question is now about the room there is rather than about the choice
   * already made.
   */
  const roomInMain = (box?.width ?? 0) - PANE_WIDTH - PANE_GAP >= EDITOR_FLOOR

  const paneNode =
    tryingIt ? (
      <TryItPane
        buffer={bufferText ?? servedText ?? ''}
        packId={packId ?? ''}
        rehearsalSupported={rehearsalSupported}
        connected={status === 'ready'}
        onClose={() => setTryingIt(false)}
      />
    ) : null

  const inspector = useInspectorPortal(
    paneNode !== null && !roomInMain ? (
      paneNode
    ) : pack.data === undefined ? null : (
        <PackInspector
          packId={packId ?? ''}
          document={drawn}
          at={at}
          meta={pack.data.meta}
          fileSha256={file.data?.sha256}
          fileBytes={file.data?.bytes}
          baseSha256={onPath ? buffer.base?.sha256 : undefined}
          dirty={dirty}
          anchored={anchored}
          truncation={truncationNote(report)}
          stale={stale}
          pending={fetching}
          checkedWhat={provenance}
          unavailable={unavailable}
          tab={slot.tab}
          onTabChange={slot.setTab}
        />
      )
  )

  if (pack.isPending) return <Loading what={`pack ${packId}`} />
  /**
   * **The refusal replaces the page only when there is nothing behind it.**
   *
   * `get_pack` refuses bytes it cannot read, and this desk can now write bytes
   * — so the state where the runtime will not serve a pack is a state the
   * editor put the file in, and the editor is the way out of it. The page is
   * over the *file's* bytes, which the chassis returns whatever they say, so a
   * refusal that took the whole route away left an author with a JSON view
   * they could not reach and nothing on screen saying where to go. `AuthorView`
   * has held this rule since it was written; this is the same rule.
   */
  if (pack.error && file.data === undefined) {
    return <ErrorBox title={`Could not load pack ${packId}`} error={pack.error} />
  }
  if (pack.data === undefined && file.data === undefined) return null

  /**
   * The diagnostics the strip prints itself.
   *
   * In the reading document that is the ones that named no block on the page —
   * a pack with no `specVersion` is refused at `/specVersion`, and nothing
   * draws that member. In the **JSON view** it is all of them: the document is
   * one text area, there are no blocks to distribute a diagnostic to, and a
   * report visible only to whoever has the Inspector open is a report the page
   * is keeping to itself. Each row prints the diagnostic's own pointer either
   * way.
   */
  const rootAnchored = rawMode ? anchored : anchored.filter((entry) => entry.anchor === '')

  const digestsDisagree =
    meta?.sha256 !== undefined &&
    file.data?.sha256 !== undefined &&
    meta.sha256 !== file.data.sha256

  const hasMatrix = summary?.matrix === true

  /**
   * The two other views on this pack, and the way into edit mode.
   *
   * Rendered beside the document *and* beside the raw bytes, because the raw
   * bytes are the only view of a file the runtime will not serve — and a page
   * whose only way forward was inside the view it could not draw is a dead end.
   */
  const elsewhere = !editing && (
    <p className={styles.elsewhere}>
      {/*
        A button and not a link: switching mode is `replace`, for the reason
        selecting a member is — how you are looking at a document is not a
        navigation and must not fill the Back stack.
      */}
      <button
        type="button"
        className={styles.elsewhereLink}
        onClick={() => setParams(withEditing(params, true), { replace: true })}
      >
        Edit
      </button>
      <Link
        className={styles.elsewhereLink}
        to={`/packs/${encodeURIComponent(packId ?? '')}/evaluate`}
      >
        Try it
      </Link>
      {hasMatrix && (
        <Link
          className={styles.elsewhereLink}
          to={`/packs/${encodeURIComponent(packId ?? '')}/matrix`}
        >
          Test matrix
        </Link>
      )}
    </p>
  )

  const strip = (
    <CheckStrip
      unavailable={unavailable}
      fetching={fetching}
      stale={stale}
      behind={idle.behind ? CHECK_BEHIND_BUFFER : undefined}
      report={check.data?.report}
      provenance={provenance}
      rootAnchored={rootAnchored}
      digestsDisagree={digestsDisagree}
      disagreement={disagreement}
    >
      {editing && <LockLine paths={(files.data?.files ?? []).map((entry) => entry.path)} />}
    </CheckStrip>
  )

  return (
    <SelectionContext.Provider value={{ at, select }}>
      <EditingContext.Provider value={session}>
        {inspector}
        <div
          className={styles.workspace}
          ref={setFrame}
          style={{ '--tryit-pane-width': `${PANE_WIDTH}px` } as CSSProperties}
        >
          <div className={styles.column}>
            {/*
              The toolbar is edit mode's. A reading page carrying a Check
              button and a Save that can never be pressed is chrome about a
              mode nobody is in; the way *into* the mode is one control beside
              the two standing links, below.
            */}
            {editing && (
              <EditToolbar
                editing={editing}
                shape={shape}
                shapeAvailable={formAvailable}
                dirty={buffer.dirty}
                discardable={hasWork}
                saving={editor.write.isPending}
                checking={fetching}
                tryingIt={tryingIt}
                canUndo={buffer.canUndo}
                unwritten={unwritten}
                onEditing={(next) => setParams(withEditing(params, next), { replace: true })}
                onShape={(next) => setParams(withShape(params, next), { replace: true })}
                onCheck={idle.checkNow}
                onTryIt={() => {
                  setTryingIt((was) => {
                    const next = !was
                    // Where the pane cannot fit beside the editor it takes the
                    // Inspector's place, and a closed Inspector has nowhere to
                    // publish into.
                    if (next && !roomInMain) slot.reveal()
                    return next
                  })
                }}
                onUndo={buffer.undo}
                onDiscard={discardAll}
                onSave={() => save()}
              />
            )}
            {pack.error !== null && (
              <ErrorBox
                title={`The runtime could not read ${path ?? 'this pack'}`}
                error={pack.error}
              />
            )}
            {/*
              A revision for another file, held rather than taken. The path a
              page is about can move under an address nobody navigated — the
              listing re-answers, `get_pack` names a different file — and taking
              those bytes over unsaved work replaces one document with another
              with nothing on screen having asked.
            */}
            {buffer.waiting !== undefined && (
              <AlertPanel
                heading="This page is now about a different file"
                actions={
                  <Button variant="quiet" onClick={buffer.takeWaiting}>
                    Open it and lose these changes
                  </Button>
                }
              >
                <p>
                  The editor is holding unsaved changes to{' '}
                  <code>{buffer.base?.path ?? 'a file'}</code>, and this address now names{' '}
                  <code>{buffer.waiting.path}</code>. Nothing has been replaced and nothing has
                  been written.
                </p>
              </AlertPanel>
            )}
            {/*
              The file moved after these bytes were loaded. The base deliberately
              does not follow a watcher refetch — that is what makes a stale
              write a 409 rather than a silent overwrite — so the honest sentence
              is that this is the revision the editor loaded.
            */}
            {behindDisk && staleWrite === undefined && (
              <AlertPanel
                heading="The file on disk has changed since this was loaded"
                actions={
                  <Button variant="quiet" onClick={reloadNow}>
                    {dirty ? 'Reload, losing these changes' : 'Reload'}
                  </Button>
                }
              >
                <p>
                  What is on screen is the revision this editor loaded. Saving from here states
                  the digest it started from, so the chassis will refuse the write rather than
                  overwrite whatever landed since.
                </p>
              </AlertPanel>
            )}
            {staleWrite !== undefined && (
              <StaleWriteAlert
                stale={staleWrite}
                pending={editor.write.isPending}
                onReload={reloadNow}
                onOverwrite={() => save(true)}
              />
            )}
            {saveFailure !== undefined && (
              <ErrorBox title={`Could not save ${path ?? 'this pack'}`} error={saveFailure} />
            )}
            {editor.reloadError !== undefined && (
              <ErrorBox
                title={`Could not reload ${editor.reloadError.path}`}
                error={editor.reloadError.error}
              />
            )}
            {/*
              What the save actually did, which is the chassis' read-back and not
              an assumption. A read-back that is not what was sent leaves the
              buffer holding the submitted bytes, dirty over the revision that
              landed — so the sentence and the buffer say the same thing.
            */}
            {editor.outcome !== undefined && staleWrite === undefined && (
              <p className={editor.verified ? styles.saved : styles.warning} role="status">
                {editor.verified ? (
                  <>
                    <strong>Saved, and verified.</strong> The chassis replaced the file and read
                    it back off the disk: {editor.outcome.landed.bytes} bytes, sha256{' '}
                    <code>{editor.outcome.landed.sha256.slice(0, 12)}…</code>, byte for byte what
                    was sent.
                  </>
                ) : (
                  <>
                    <strong>Saved, and the read-back does not match.</strong> The write completed
                    and the bytes now on disk are not the bytes that were sent.{' '}
                    {bufferText === editor.outcome.submitted
                      ? 'What is in this editor is what was sent'
                      : 'This editor was not replaced, and holds neither of them'}
                    ; reload before editing further.
                  </>
                )}
              </p>
            )}
            {drawn === undefined || rawMode ? (
              <>
                {elsewhere}
                {strip}
                <RawJsonEditor
                  text={bufferText ?? servedText ?? ''}
                  path={path}
                  dirty={buffer.dirty}
                  problem={withheld}
                  readOnly={!editing || !onPath}
                  onChange={(next) => commit(next, { coalesceKey: 'raw' })}
                />
              </>
            ) : (
              <PackDocumentView document={drawn} active={active}>
                {elsewhere}
                {strip}
              </PackDocumentView>
            )}
          </div>
          {paneNode !== null && roomInMain && <div className={styles.pane}>{paneNode}</div>}
        </div>
      </EditingContext.Provider>
    </SelectionContext.Provider>
  )
}

/**
 * Why the form is withheld, in the words of what is actually wrong.
 *
 * Bytes that do not scan get the position the scanner stopped at, turned into
 * a line and a column because that is what the JSON view's gutter is numbered
 * in. A document that scans but is read differently by `JSON.parse` gets the
 * member the two disagree about.
 */
function formWithheld(
  read: Buffered | undefined,
  text: string | undefined,
  disagreement: readonly { pointer: string; reason: string }[]
): string | undefined {
  if (read === undefined || text === undefined) return undefined
  if (read.index.parseError !== undefined) {
    const offset = /byte (\d+)/.exec(read.index.parseError)
    const where =
      offset === null ? '' : (() => {
        const { line, column } = positionOf(text, Number(offset[1]))
        return ` — line ${line}, column ${column}`
      })()
    return `These bytes are not a document this desk can edit as a form${where}. ${read.index.parseError}.`
  }
  const first = disagreement[0]
  if (first === undefined) return undefined
  return `Form editing is withheld: ${first.reason}${
    first.pointer === '' ? '' : ` at ${first.pointer}`
  }.`
}

/**
 * Why there is no check, where there is none — and never "the document is
 * fine". A runtime that never advertised the tool and a listing that never
 * answered are two different absences, and neither is a verdict.
 */
function checkUnavailable(
  known: boolean,
  validateSupported: boolean,
  error: Error | null,
  bytes: string | undefined
): string | undefined {
  if (error !== null) return `The check did not answer — ${error.message}`
  if (!known) return 'The runtime has not said what it can do, so this document is unchecked.'
  if (!validateSupported) return 'This runtime does not offer validate, so this document is unchecked.'
  if (bytes === undefined || bytes === '') {
    return 'There are no bytes to check yet, so this document is unchecked.'
  }
  return undefined
}

function sameSet(left: ReadonlySet<string>, right: ReadonlySet<string>): boolean {
  if (left.size !== right.size) return false
  for (const value of left) if (!right.has(value)) return false
  return true
}
