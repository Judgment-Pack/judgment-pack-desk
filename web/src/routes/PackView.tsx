/**
 * One pack: the document, the check under it, and the Inspector beside it.
 *
 * This route does six things and no more.
 *
 * 1. Reads the **served** document — `get_pack` gives the parsed pack, its raw
 *    text, and the metadata beside it: the declared path, the byte count and
 *    the digest.
 * 2. Reads the **file** at that path through the chassis, which is the bytes
 *    an editor would hold and the base digest a save would state. One read
 *    serves both modes.
 * 3. **Binds the two.** They are two answers about one file and they only
 *    describe one revision when the digests are equal. Where they are not,
 *    this says so plainly rather than picking one.
 * 4. Runs `validate` over the file's bytes where they loaded, and over the
 *    served text where they did not — and labels which.
 * 5. Renders the document with the check strip under its outline.
 * 6. Publishes the Inspector through the shell's slot.
 *
 * `?at` is the selection and `#pointer` is the deep link, and both are the
 * pointer address space. Selection writes are `replace: true`: choosing what
 * to inspect is not a navigation and must not fill the Back stack.
 *
 * `?edit` is not read here at all. The route tolerates it and does nothing
 * with it; the mode helper lands with the forms, so it is written once against
 * a real caller.
 */
import { useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom'
import { ErrorBox, Loading } from '../components/primitives'
import { useFileContent } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { usePack, usePacks, useValidate } from '../mcp/queries'
import { agreesWithParse, indexDocument } from '../packs/documentText'
import { anchor, layersReached, truncationNote } from '../packs/checks'
import { PackDocumentView } from '../packs/document/PackDocumentView'
import { SelectionContext } from '../packs/document/Block'
import { PackInspector } from '../packs/inspector/PackInspector'
import { readingOrder } from '../packs/document/members'
import { pointerFromHash } from '../packs/pointers'
import { useDocumentSpy } from '../packs/useDocumentSpy'
import { useInspectorPortal, useInspectorSlot } from '../shell/InspectorSlot'
import styles from './PackView.module.css'

export function PackView() {
  const { packId } = useParams<{ packId: string }>()
  const { hash } = useLocation()
  const [params, setParams] = useSearchParams()
  const { known, validateSupported } = useMcp()
  const pack = usePack(packId)
  const listing = usePacks()
  const meta = pack.data?.meta
  const file = useFileContent(meta?.path)
  const slot = useInspectorSlot()

  const at = params.get('at')
  const select = (pointer: string) => {
    const next = new URLSearchParams(params)
    next.set('at', pointer)
    setParams(next, { replace: true })
    // Selecting is asking to inspect. With the pane closed — the shell's
    // default in a fresh profile — this otherwise filled a panel behind a
    // closed pane and changed nothing on screen but the block's own border.
    slot.reveal()
  }

  // The bytes the check ran over: the file's where they loaded, the served
  // document's where they did not. Which one it is is printed, because the two
  // are different artifacts and a sentence about "the document" that does not
  // say which would be a sentence about neither.
  const fileText = file.data?.content
  const checkedText = fileText ?? pack.data?.raw
  const checkedWhat =
    fileText !== undefined
      ? `checked against the bytes of ${meta?.path ?? 'the file on disk'}`
      : 'checked against the document the runtime served'
  const check = useValidate(checkedText)

  // Which blocks are actually on screen. Read off the DOM rather than derived
  // from the document, because "nearest **rendered** ancestor" is a claim
  // about what is on the page: a derivation would be a second model of the
  // renderer, free to drift from it.
  const [rendered, setRendered] = useState<ReadonlySet<string>>(new Set(['']))
  const documentKey = pack.data?.raw ?? ''
  useEffect(() => {
    const article = document.querySelector('[data-pointer=""]')
    if (article === null) return
    const found = new Set<string>([''])
    for (const element of article.querySelectorAll('[data-pointer]')) {
      const pointer = element.getAttribute('data-pointer')
      if (pointer !== null) found.add(pointer)
    }
    setRendered((previous) => (sameSet(previous, found) ? previous : found))
  }, [documentKey, at])

  const anchored = useMemo(() => anchor(check.data, rendered), [check.data, rendered])
  // **Never stale in read mode, by construction**, and said here rather than
  // computed: the check's query key *is* the buffer, so a report only ever
  // exists for the bytes on screen. `isStale` is the rule the editor phase
  // wires up, where the buffer moves under a report that has already answered;
  // calling it here with one expression on both sides would read as a live
  // guard and could never fire.
  const stale = false

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
    // The fragment is the trigger and the only one: a re-render must not drag
    // the reader back to a member they have scrolled away from.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hash, documentKey])

  const outlinePointers = useMemo(
    () => (pack.data === undefined ? [] : readingOrder(pack.data.document).map((unit) => unit.pointer)),
    [pack.data]
  )
  const active = useDocumentSpy(outlinePointers, at)

  // The desk's own reading of the file bytes, compared to `JSON.parse`'s. A
  // disagreement is what withholds form mode next phase, and the sentence is
  // written now because the reason for it does not change.
  const disagreement = useMemo(() => {
    if (fileText === undefined) return []
    return agreesWithParse(fileText, indexDocument(fileText))
  }, [fileText])

  const inspector = useInspectorPortal(
    pack.data === undefined ? null : (
      <PackInspector
        packId={packId ?? ''}
        document={pack.data.document}
        at={at}
        meta={pack.data.meta}
        fileSha256={file.data?.sha256}
        fileBytes={file.data?.bytes}
        anchored={anchored}
        truncation={truncationNote(check.data)}
        stale={stale}
        pending={check.isPending}
        checkedWhat={checkedWhat}
        unavailable={checkUnavailable(known, validateSupported, check.error)}
        tab={slot.tab}
        onTabChange={slot.setTab}
      />
    )
  )

  if (pack.isPending) return <Loading what={`pack ${packId}`} />
  if (pack.error) return <ErrorBox title={`Could not load pack ${packId}`} error={pack.error} />
  if (!pack.data) return null

  const rootAnchored = anchored.filter((entry) => entry.anchor === '')

  const digestsDisagree =
    meta?.sha256 !== undefined &&
    file.data?.sha256 !== undefined &&
    meta.sha256 !== file.data.sha256

  // The document is one of three views on a pack, and this is the only one with
  // a route to the other two. `PackDetail` carried the links and the rail
  // carried a per-pack Evaluate child; both went with this rewrite, and the
  // what-if view was reachable only by typing its URL.
  const hasMatrix = (listing.data?.packs ?? []).some(
    (summary) => summary.id === packId && summary.matrix === true
  )

  return (
    <SelectionContext.Provider value={{ at, select }}>
      {inspector}
      <PackDocumentView document={pack.data.document} active={active}>
        <p className={styles.elsewhere}>
          <Link className={styles.elsewhereLink} to={`/packs/${encodeURIComponent(packId ?? '')}/evaluate`}>
            Try it
          </Link>
          {hasMatrix && (
            <Link className={styles.elsewhereLink} to={`/packs/${encodeURIComponent(packId ?? '')}/matrix`}>
              Test matrix
            </Link>
          )}
        </p>
        <div className={styles.strip}>
          <p className={styles.check}>
            {checkUnavailable(known, validateSupported, check.error) ??
              (check.isPending ? 'Checking…' : layersReached(check.data).text)}
          </p>
          <p className={styles.checkWhat}>{checkedWhat}</p>
          {/*
            The diagnostics that landed on the document itself. `anchor()` sends
            a diagnostic here when neither its own pointer nor any ancestor of
            it is on the page — a pack with no `specVersion` is refused at
            `/specVersion`, and nothing renders that member — and the strip is
            the block whose pointer is the empty string. Until this list, those
            were counted in the sentence above and printed nowhere at all.
          */}
          {rootAnchored.length > 0 && (
            <ul className={styles.rootDiagnostics}>
              {rootAnchored.map((entry, index) => (
                <li key={`${entry.diagnostic.code}-${index}`}>
                  <code>{entry.diagnostic.code}</code> {entry.diagnostic.message}{' '}
                  <code>{entry.named === '' ? 'the document' : entry.named}</code>
                </li>
              ))}
            </ul>
          )}
          {digestsDisagree && (
            <p className={styles.warning} role="status">
              The runtime served bytes with a different digest from the file on disk. These are
              two answers about one file, and they do not describe one revision.
            </p>
          )}
          {disagreement.length > 0 && (
            <p className={styles.warning} role="status">
              The desk will not edit around this file: {disagreement[0]!.reason} at{' '}
              <code>{disagreement[0]!.pointer === '' ? 'the document' : disagreement[0]!.pointer}</code>.
            </p>
          )}
        </div>
      </PackDocumentView>
    </SelectionContext.Provider>
  )
}

/**
 * Why there is no check, where there is none — and never "the document is
 * fine". A runtime that never advertised the tool and a listing that never
 * answered are two different absences, and neither is a verdict.
 */
function checkUnavailable(
  known: boolean,
  validateSupported: boolean,
  error: Error | null
): string | undefined {
  if (error !== null) return `The check did not answer — ${error.message}`
  if (!known) return 'The runtime has not said what it can do, so this document is unchecked.'
  if (!validateSupported) return 'This runtime does not offer validate, so this document is unchecked.'
  return undefined
}

function sameSet(left: ReadonlySet<string>, right: ReadonlySet<string>): boolean {
  if (left.size !== right.size) return false
  for (const value of left) if (!right.has(value)) return false
  return true
}
