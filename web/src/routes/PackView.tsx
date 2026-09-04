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
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom'
import { ErrorBox, Loading } from '../components/primitives'
import { useFileContent } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { usePack, usePacks, useValidate } from '../mcp/queries'
import { agreesWithParse, indexDocument } from '../packs/documentText'
import { anchor, isStale, layersReached, truncationNote } from '../packs/checks'
import { PackDocumentView } from '../packs/document/PackDocumentView'
import { SelectionContext } from '../packs/document/Block'
import { PackInspector } from '../packs/inspector/PackInspector'
import { outlineRepresentatives, readingOrder } from '../packs/document/members'
import { pointerFromHash } from '../packs/pointers'
import { useDocumentSpy } from '../packs/useDocumentSpy'
import { useInspectorPortal, useInspectorSlot } from '../shell/InspectorSlot'
import styles from './PackView.module.css'

export function PackView() {
  const { packId } = useParams<{ packId: string }>()
  const { hash, key: locationKey } = useLocation()
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
    // The reveal is not here. Selecting writes `?at`, which is a navigation —
    // a replacing one, but a new history entry all the same — and the effect
    // below opens the pane for exactly that. Two callers meant two calls, and
    // while `reveal` is idempotent now and two is harmless, one path is one
    // rule: the pane opens when the address gains a selection, however it
    // gained one.
  }

  /**
   * Arriving with a selection is arriving at a link someone sent, so the pane
   * it addresses is opened.
   *
   * **Once per arrival, and an arrival is a `location.key`.** Mount was the
   * wrong unit in both directions. `/packs/a` → `/packs/a?at=/rules/0` reuses
   * this component, so a link followed from the References panel — or from
   * anywhere inside the same pack — mounted nothing and revealed nothing. And
   * StrictMode mounts twice, which made two calls out of one arrival; that half
   * is now harmless because `reveal` sets rather than flips, but the unit is
   * still wrong.
   *
   * The key changes on every history entry and on nothing else, so this fires
   * for a navigation and not for a rerender — which is what keeps it from
   * fighting a viewer who has closed the pane and stayed where they are.
   */
  const visited = useRef<string | undefined>(undefined)
  useEffect(() => {
    // **Every arrival is recorded, whether or not it carries a selection.** It
    // used to record only the ones that revealed, so an entry without `?at`
    // returned before writing anything down: select a member (entry K), follow a
    // link that drops the selection (entry U), close the pane, press Back to K —
    // and K was still the last key this remembered, so the arrival that Back
    // *is* looked like the rerender it is not, and the pane stayed shut on an
    // address that names a member.
    if (visited.current === locationKey) return
    visited.current = locationKey
    if (at === null) return
    slot.reveal()
  }, [at, locationKey, slot])

  // The bytes the check ran over: the file's where they loaded, the served
  // document's where they did not. Which one it is is printed, because the two
  // are different artifacts and a sentence about "the document" that does not
  // say which would be a sentence about neither.
  const fileText = file.data?.content
  const checkedText = fileText ?? pack.data?.raw
  const whichBytes =
    fileText !== undefined
      ? `the bytes of ${meta?.path ?? 'the file on disk'}`
      : 'the document the runtime served'
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

  /**
   * Whether this report is about the bytes on screen.
   *
   * **It was hard-coded false, and that was a claim rather than a check.** The
   * check runs over the file on disk where it loaded and over the served
   * document where it did not; the page draws the served document either way.
   * Where the chassis read revision B after `get_pack` served A, every one of
   * B's diagnostics was anchored onto A's blocks — a `/rules/0` diagnostic
   * landing on a rule that is not the rule it is about, which is worse than no
   * diagnostic at all because it looks like an answer.
   *
   * The report carries the bytes it checked, and they are compared with the
   * bytes being rendered. Not the digests: the digest warning below is about
   * two answers from two sources, and it is possible for that to be quiet while
   * these bytes still differ.
   */
  const servedText = pack.data?.raw
  const stale = isStale(check.data?.checkedBytes, servedText)

  /**
   * Which bytes the check is about — in the tense of what has actually
   * happened.
   *
   * "checked against the bytes of packs/x.pack.json" was printed unconditionally
   * and stood beside "This runtime does not offer validate, so this document is
   * unchecked", beside "There are no bytes to check yet", and under "Checking…".
   * A line that says a check ran, next to a line that says no check ran, is one
   * of them lying and the reader cannot tell which. So: nothing at all where
   * there is no check, the present tense while one is in flight, and the past
   * tense only for a report that has arrived.
   */
  const unavailable = checkUnavailable(known, validateSupported, check.error, checkedText)
  const fetching = check.fetchStatus === 'fetching'
  const provenance =
    unavailable !== undefined
      ? undefined
      : fetching
        ? `checking against ${whichBytes}`
        : check.data !== undefined
          ? `checked against ${whichBytes}`
          : undefined

  // **No diagnostics at all while stale.** Not "fewer", not "the ones that
  // still anchor": every one of them is about a document that is not the one on
  // the page, and there is no way to tell from a pointer which of them would
  // still be right. The strip says the report is about other bytes; the blocks
  // say nothing.
  const report = stale ? undefined : check.data?.report
  const anchored = useMemo(() => anchor(report, rendered), [report, rendered])

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

  const order = useMemo(
    () => (pack.data === undefined ? [] : readingOrder(pack.data.document)),
    [pack.data]
  )
  const outlinePointers = useMemo(() => order.map((unit) => unit.pointer), [order])
  /**
   * The outline lists `Identity` once and the page draws its five members
   * separately, so the spy answers in pointers the nav does not carry.
   *
   * `MemberOutline` marks the entry whose pointer *equals* the active one, and
   * four of the five identity members equal no entry — so reading the version,
   * or following a link to `/id`, left the nav marking nothing at all. The
   * answer is translated to the entry that lists it; the entry is the thing the
   * reader sees.
   */
  const representative = useMemo(
    () =>
      pack.data === undefined
        ? new Map<string, string>()
        : outlineRepresentatives(pack.data.document, order),
    [pack.data, order]
  )
  const seen = useDocumentSpy(outlinePointers, at, documentKey)
  const active = seen === null ? null : (representative.get(seen) ?? seen)

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
            {unavailable ??
              // Progress is `fetchStatus`, which is about a request being in
              // flight. `isPending` is about there being no data, which a
              // disabled query satisfies for ever.
              (fetching
                ? 'Checking…'
                : stale
                  ? 'This check ran over different bytes from the ones shown, so nothing it found is placed on this document.'
                  : layersReached(check.data?.report).text)}
          </p>
          {provenance !== undefined && <p className={styles.checkWhat}>{provenance}</p>}
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
  error: Error | null,
  bytes: string | undefined
): string | undefined {
  if (error !== null) return `The check did not answer — ${error.message}`
  if (!known) return 'The runtime has not said what it can do, so this document is unchecked.'
  if (!validateSupported) return 'This runtime does not offer validate, so this document is unchecked.'
  // **Empty bytes are a reason, not a wait.** `useValidate` disables itself for
  // an empty buffer, and a disabled query reports `isPending` for ever — so the
  // strip said "Checking…" about a check that was never going to start. There
  // is nothing to check, and that is a sentence.
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
