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
 * **The document on screen is the buffer.** Both modes draw
 * `indexDocument(buffer).value`, so a keystroke in the JSON view and an edit in
 * a form move the reading document the same way, and the form is never over one
 * revision while the page is over another. The served document is the fallback
 * where the file did not load or its bytes do not scan — and the digest
 * sentence still says when the runtime served a different revision from the one
 * the editor holds.
 *
 * **Save is never gated on the check.** The chassis writes bytes and the
 * runtime judges them, in that order. Outstanding diagnostics stay on screen
 * through a save, because a desk that decided what may exist on disk would be
 * the authority this whole surface exists not to be.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom'
import { ErrorBox, Loading } from '../components/primitives'
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
import { SelectionContext } from '../packs/document/Block'
import { EditToolbar } from '../packs/edit/EditToolbar'
import { EditingContext, declaredIds, type EditingSession } from '../packs/edit/editingContext'
import { editShape, isEditing, withEditing, withShape } from '../packs/edit/editMode'
import { LockLine } from '../packs/edit/LockLine'
import { RawJsonEditor, positionOf } from '../packs/edit/RawJsonEditor'
import { StaleWriteAlert } from '../packs/edit/StaleWriteAlert'
import { TryItPane } from '../packs/edit/TryItPane'
import { useDocumentBuffer } from '../packs/edit/useDocumentBuffer'
import { useIdleCheck } from '../packs/edit/useIdleCheck'
import { buffered, type Buffered } from '../packs/edit/writes'
import { PackInspector } from '../packs/inspector/PackInspector'
import { outlineRepresentatives, readingOrder } from '../packs/document/members'
import { pointerFromHash } from '../packs/pointers'
import { useDocumentSpy } from '../packs/useDocumentSpy'
import { useInspectorPortal, useInspectorSlot } from '../shell/InspectorSlot'
import { usePublishedDirty } from '../shell/authorBridge'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { useMeasuredBox } from '../shell/measured'
import styles from './PackView.module.css'

/** What the what-if pane needs, and what the editor must keep beside it. */
const PANE_WIDTH = 392
const EDITOR_FLOOR = 512

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
  const meta = pack.data?.meta
  const path = meta?.path
  const file = useFileContent(path)
  const slot = useInspectorSlot()

  const editing = isEditing(params)
  const askedShape = editShape(params)

  // The revision, the write, and the verdict the last write left behind. The
  // buffer's discard clears that verdict, which is why the two are wired
  // together here rather than each holding half of it.
  const editor = useFileEditing()
  const buffer = useDocumentBuffer(file.data, editor.reset)
  const bufferText = buffer.text

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
  const whichBytes =
    bufferText !== undefined
      ? buffer.dirty
        ? 'the bytes in the editor'
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
  const formAvailable = disagreement.length === 0 && read?.index.value !== undefined
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
  const drawn: PackDocument | undefined =
    (read?.index.value as PackDocument | undefined) ?? pack.data?.document

  // Which blocks are actually on screen. Read off the DOM rather than derived
  // from the document, because "nearest **rendered** ancestor" is a claim about
  // what is on the page.
  const [rendered, setRendered] = useState<ReadonlySet<string>>(new Set(['']))
  const rawMode = editing && shape === 'json'
  const documentKey = `${shownText ?? ''}|${editing ? shape : 'read'}`
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
    element.focus()
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

  const summary = (listing.data?.packs ?? []).find((entry) => entry.id === packId)
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
      ids: declaredIds(drawn ?? {}, summary?.consultedFactPaths ?? [])
    }),
    [editing, shape, read, commit, byPointer, drawn, summary?.consultedFactPaths]
  )

  /* Saving ----------------------------------------------------------------- */

  const staleWrite = editor.write.error instanceof StaleWrite ? editor.write.error : undefined

  const save = useCallback(
    (override?: boolean) => {
      if (path === undefined || bufferText === undefined || buffer.base === undefined) return
      // The check runs before the save and does **not** gate it. Sending the
      // buffer now means the diagnostics on screen afterwards are about the
      // bytes that were written rather than about whatever was last idle.
      idle.checkNow()
      editor.save({
        path,
        content: bufferText,
        baseSha256: buffer.base.sha256,
        override,
        onSaved: (landed) => {
          buffer.rebase(landed)
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

  const onEditorKey = (event: KeyboardEvent<HTMLElement>) => {
    // **Mod+S fires inside a text field**, which is the one place the shell's
    // own chords are suppressed — `isTypingTarget` exists so typing an `s`
    // never opens a pane. So it is registered on this subtree rather than
    // through `installShortcuts`, and the shell's rule stays as written.
    if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 's') return
    if (!editing || !buffer.dirty) return
    event.preventDefault()
    save()
  }

  usePublishedDirty(path ?? `pack:${packId ?? ''}`, buffer.dirty)
  useDirtyGuard(buffer.dirty, LEAVING)

  /* Try it ----------------------------------------------------------------- */

  const [tryingIt, setTryingIt] = useState(false)
  const [column, setColumn] = useState<HTMLDivElement | null>(null)
  const box = useMeasuredBox(column)
  // The pane goes beside the editor only where the editor keeps its floor.
  // Otherwise it takes the Inspector's place, which is the one other surface
  // wide enough to hold it.
  const roomInMain = (box?.width ?? 0) - PANE_WIDTH >= EDITOR_FLOOR

  const paneNode =
    tryingIt && pack.data !== undefined ? (
      <TryItPane
        buffer={bufferText ?? servedText ?? ''}
        packId={packId ?? ''}
        rehearsalSupported={rehearsalSupported}
        connected={status === 'ready'}
        onClose={() => setTryingIt(false)}
      />
    ) : null

  const inspector = useInspectorPortal(
    pack.data === undefined ? null : (
      paneNode !== null && !roomInMain ? (
        paneNode
      ) : (
        <PackInspector
          packId={packId ?? ''}
          document={drawn ?? pack.data.document}
          at={at}
          meta={pack.data.meta}
          fileSha256={file.data?.sha256}
          fileBytes={file.data?.bytes}
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
  )

  if (pack.isPending) return <Loading what={`pack ${packId}`} />
  if (pack.error) return <ErrorBox title={`Could not load pack ${packId}`} error={pack.error} />
  if (!pack.data) return null

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

  const hasMatrix = (listing.data?.packs ?? []).some(
    (entry) => entry.id === packId && entry.matrix === true
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
        <div className={styles.workspace}>
          <div className={styles.column} ref={setColumn} onKeyDown={onEditorKey}>
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
                saving={editor.write.isPending}
                checking={fetching}
                tryingIt={tryingIt}
                canUndo={buffer.canUndo}
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
                onDiscard={buffer.discard}
                onSave={() => save()}
              />
            )}
            {staleWrite !== undefined && (
              <StaleWriteAlert
                stale={staleWrite}
                pending={editor.write.isPending}
                onReload={() => {
                  if (path === undefined) return
                  editor.reload(path, (fresh) => buffer.rebase(fresh))
                }}
                onOverwrite={() => save(true)}
              />
            )}
            {editing && shape === 'json' ? (
              <>
                {strip}
                <RawJsonEditor
                  text={bufferText ?? servedText ?? ''}
                  path={path}
                  dirty={buffer.dirty}
                  problem={withheld}
                  readOnly={buffer.base === undefined}
                  onChange={(next) => commit(next, { coalesceKey: 'raw' })}
                />
              </>
            ) : (
              <PackDocumentView document={drawn ?? pack.data.document} active={active}>
                {!editing && (
                  <p className={styles.elsewhere}>
                    {/*
                      A button and not a link: switching mode is `replace`, for
                      the reason selecting a member is — how you are looking at
                      a document is not a navigation and must not fill the Back
                      stack.
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
                )}
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
