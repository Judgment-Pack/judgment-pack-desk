import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { PageHeader } from '../ui/PageLayout'
import { Button } from '../ui/Button'
import { OverflowTooltip } from '../ui/Tooltip'
import { useEffect, useId, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { Empty, ErrorBox, Loading, Pill } from '../components/primitives'
import { StaleWrite, type FileContent } from '../files/client'
import { useFileContent, useFileListing } from '../files/queries'
import { useFileEditing } from '../files/useFileEditing'
import { useOpenRequests, usePublishedDirty } from '../shell/authorBridge'
import { useConfirmDiscard } from '../shell/UnsavedChanges'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { IconChevronLeft, IconDetails } from '../shell/icons'
import { PaneToggle } from '../shell/PaneToggle'
import { PaneDivider } from '../ui/PaneDivider'
import styles from './AuthorView.module.css'

const DEFAULT_FILES_WIDTH = 280
const MIN_FILES_WIDTH = 220
function storedWidth(key: string): number {
  try {
    const value = Number(localStorage.getItem(key))
    if (Number.isFinite(value) && value >= MIN_FILES_WIDTH && value <= 480) return value
  } catch { /* Browser storage is optional. */ }
  return DEFAULT_FILES_WIDTH
}

/**
 * The authoring shell: pick a file, edit its bytes, save them (issue #14,
 * phase 1).
 *
 * **What this is not**, and the boundary is the point. It does not validate, it
 * does not know what a pack is, and it does not read `jpack.json`. The runtime
 * is the only judge of what any of these bytes mean, and it judges after they
 * land — through the tools every other view already uses. Phase 2 attaches
 * schema-guided editing and validate-on-change beside this editor; phase 3 does
 * the same for matrix and rows documents. Both hang off `FileEditor` without
 * changing what the save path does.
 *
 * What it *is* responsible for is what a plain editor gets wrong:
 *
 * - **A base revision that does not move.** The bytes an edit started from are
 *   editor-local and change only when the user acts — an initial load, an
 *   explicit reload, a successful save. The desk invalidates every query when
 *   the watcher sees a file change, and a base derived from that live query
 *   would silently rebase onto bytes the user never saw, so Save would overwrite
 *   them without the 409 that exists to prevent exactly that.
 * - **A save that proves itself against what it sent.** The chassis answers a
 *   write with a read-back from the disk; this compares that to the *submitted
 *   snapshot*, not to the live buffer, so typing after a save cannot turn a true
 *   "verified" into a false "does not match".
 * - **Not losing an edit quietly.** Switching files, leaving the page, and a
 *   file that disappears from underneath all keep the buffer or ask first.
 */
export function AuthorView() {
  useLocale()
  const listing = useFileListing()
  const [selected, setSelected] = useState<string | undefined>(undefined)
  const [dirty, setDirty] = useState(false)
  const frame = useRef<HTMLDivElement>(null), paneId = useId()
  const [room, setRoom] = useState<number>()
  const [width, setWidth] = useState(DEFAULT_FILES_WIDTH)
  const [browsing, setBrowsing] = useState(false)
  const [collapsed, setCollapsed] = useState(false)
  const expandButton = useRef<HTMLButtonElement>(null)
  const widthKey = listing.data ? `jpack.files-pane.v1:${listing.data.root}` : undefined
  useEffect(() => { if (widthKey) setWidth(storedWidth(widthKey)) }, [widthKey])
  useEffect(() => {
    const element = frame.current
    if (!element) return
    const measure = () => {
      const available = element.getBoundingClientRect().width
      if (available > 0) setRoom(available)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const compact = room !== undefined && room < 640
  const maxWidth = Math.min(480, Math.max(MIN_FILES_WIDTH, (room ?? 1200) - 360))
  const paneWidth = Math.min(width, maxWidth)
  const resize = (value: number) => {
    setWidth(value)
    try { if (widthKey) localStorage.setItem(widthKey, String(value)) } catch { /* Keep the session preference. */ }
  }
  const showBrowser = compact && (browsing || !selected)
  const browse = () => {
    setBrowsing(true)
    requestAnimationFrame(() => frame.current?.querySelector<HTMLButtonElement>('button[aria-current="true"]')?.focus())
  }
  const collapse = () => {
    setCollapsed(true)
    requestAnimationFrame(() => expandButton.current?.focus())
  }
  const leading = compact ? <Button variant="quiet" onClick={browse}><IconChevronLeft />{msg('Files')}</Button> : collapsed ? <PaneToggle compact label={msg('Expand files')} expanded={false} controls={paneId} buttonRef={expandButton} onClick={() => setCollapsed(false)} /> : undefined

  const files = listing.data?.files ?? []
  const partial = listing.data?.partial ?? []

  // The selection is not dropped when the listing stops carrying it. A file
  // deleted underneath an open editor is the case the editor most needs to
  // survive: unmounting would throw the buffer away before the user is told.
  const listedNow = files.some((file) => file.path === selected)

  // The one thing the shell needs from this view. Both hooks live in
  // `authorBridge.ts` beside the state they publish to, so what this view
  // carries for the shell is one import and two calls.
  usePublishedDirty(selected ?? '(no file)', dirty)

  // Two guards, because they cover two different exits and neither covers the
  // other — both now in `shell/useDirtyGuard.ts`, so the pack editor holds the
  // same pair rather than a second spelling of it.
  const confirmDiscard = useConfirmDiscard()
  useDirtyGuard(dirty, msg('This file has unsaved changes that will be lost. Leave anyway?'), { name: selected })

  const choose = async (path: string) => {
    if (path === selected) { setBrowsing(false); return }
    if (dirty && !await confirmDiscard(msg('Discard unsaved changes to this file?'), { name: selected })) return
    setDirty(false)
    setSelected(path)
    setBrowsing(false)
  }

  // Routed through `choose`, so the dirty-buffer question is asked for a file
  // the Create dialog opened exactly as it is for one the viewer clicked. It
  // is called here rather than above because `choose` is declared above it.
  useOpenRequests(choose)

  return (
    <article className="detail authoring" data-measure="full" data-layout="page">
      <div ref={frame} className={styles.frame} data-compact={compact || undefined}
        style={{ '--files-width': `${paneWidth}px` } as CSSProperties}>
        <aside id={paneId} className={styles.browser} aria-label={msg('Browse files')} hidden={compact ? !showBrowser : collapsed}>
          <PageHeader title={msg('Project files')} meta={files.length} leading={!compact && <PaneToggle compact label={msg('Collapse files')} expanded controls={paneId} onClick={collapse} />} />
          <div className={styles.fileScroll}>
            {listing.error && !listing.data ? <ErrorBox title={msg("Could not list the project's files")} error={listing.error} />
              : listing.isPending ? <Loading what={msg("the project's files")} /> : <>
              {listing.error && <p className="note note-warn" role="status"><Message text={"<0/> —<1/><2/>. What is shown is the last listing that answered; your edit is untouched."} slots={[<strong>{msg("The file list could not be refreshed")}</strong>, ' ', listing.error.message]} /></p>}
              {partial.length > 0 && <p className="note note-warn" role="status"><Message text={"<0/> The desk could not read everything in the project:<1/><2/>"} slots={[<strong>{msg("This list is incomplete.")}</strong>, <br />, partial.map(problem => <code key={problem} className="partial-reason">{problem}</code>)]} /></p>}
              {files.length === 0 ? <Empty>{partial.length === 0 ? msg("This project directory contains no files.") : msg("Nothing in this project could be read; see above.")}</Empty> :
                <nav aria-label={msg('Files')}><ul className={styles.fileList}>
                  {files.map(file => <li key={file.path}>
                    <OverflowTooltip selector="[data-file-label]" content={file.path}>
                      <button type="button" className={styles.fileEntry} aria-label={file.path}
                        aria-current={file.path === selected ? true : undefined} onClick={() => void choose(file.path)}>
                        <IconDetails />
                        <span className={styles.fileText}>
                          <span data-file-label>{file.path.split('/').at(-1)}</span>
                          {file.path.includes('/') && <small data-file-label>{file.path}</small>}
                        </span>
                      </button>
                    </OverflowTooltip>
                  </li>)}
                </ul></nav>}
            </>}
          </div>
          {!compact && !collapsed && <PaneDivider paneSide="start" label={msg('Resize file pane')} controls={paneId}
            value={paneWidth} min={MIN_FILES_WIDTH} max={maxWidth} onChange={resize}
            onReset={() => resize(Math.min(DEFAULT_FILES_WIDTH, maxWidth))} onCollapse={collapse}
            preview={{element:frame.current, property:'--files-width'}} />}
        </aside>
        <section className={styles.detail} aria-label={msg('Editor')} hidden={showBrowser}>
          {selected ? <FileEditor key={selected} path={selected} listed={listedNow} onDirty={setDirty} leading={leading} /> : <>
            <PageHeader title={msg('Editor')} leading={leading} />
            <div className={styles.empty}><IconDetails /><p>{msg('Choose a file to edit.')}</p></div>
          </>}
        </section>
      </div>
    </article>
  )
}

/**
 * One file, open.
 *
 * Keyed by path at the call site, so switching files remounts rather than
 * carrying one file's buffer into another's — an editor that reused state
 * across that boundary would let a save write one document's text to another
 * document's path.
 *
 * This is the seam phase 2 attaches to: the buffer and its base revision live
 * here, so a validation panel reads `buffer` and a schema form replaces the
 * textarea, without either of them touching the save path below.
 */
function FileEditor({
  path,
  listed,
  onDirty,
  leading
}: {
  path: string
  /** Whether the current listing still carries this path. */
  listed: boolean
  onDirty: (dirty: boolean) => void
  leading?: ReactNode
}) {
  useLocale()
  const confirmDiscard = useConfirmDiscard()
  const loaded = useFileContent(path)
  // The base revision, the save and its proof — `files/useFileEditing.ts`,
  // which is this editor's own discipline lifted out so the pack editor holds
  // one story rather than a second spelling of it.
  const editing = useFileEditing()
  const { write, outcome, reloadError, verified } = editing

  // The revision this edit is against. Editor-local and immutable except where
  // the user acts: seeded once from the first successful load, replaced by an
  // explicit reload or a successful save. Never by a background refetch.
  const [base, setBase] = useState<FileContent | undefined>(undefined)
  const [buffer, setBuffer] = useState<string | undefined>(undefined)

  useEffect(() => {
    if (loaded.data && base === undefined) {
      setBase(loaded.data)
      setBuffer(loaded.data.content)
    }
  }, [loaded.data, base])

  const dirty = useMemo(
    () => buffer !== undefined && base !== undefined && buffer !== base.content,
    [buffer, base]
  )
  useEffect(() => {
    onDirty(dirty)
    return () => onDirty(false)
  }, [dirty, onDirty])

  const stale = write.error instanceof StaleWrite ? write.error : undefined
  const failure = write.error && !stale ? write.error : undefined

  // What the *current* answer from the chassis says, which is a notification
  // and not a rebase. A file changed underneath an open edit is worth saying;
  // adopting it silently is what would defeat the stale-write refusal.
  const changedOnDisk =
    base !== undefined && loaded.data !== undefined && loaded.data.sha256 !== base.sha256
  const deleted = base !== undefined && (!listed || loaded.isError)

  const reload = async () => {
    if (dirty && !await confirmDiscard(msg('Reload and discard changes?'), { name: path })) return
    // This editor is remounted per file (`key={selected}`), so there is no
    // other document a late answer could land in: it takes every read it asked
    // for, and says so.
    editing.reload(path, (fresh) => {
      setBase(fresh)
      setBuffer(fresh.content)
      return true
    })
  }

  const save = (override: boolean) => {
    if (buffer === undefined || base === undefined) return
    editing.save({
      path,
      content: buffer,
      baseSha256: base.sha256,
      override,
      onSaved: (landed) => setBase(landed)
    })
  }

  if (loaded.error && base === undefined) {
    return (
      <><PageHeader title={path.split('/').at(-1)!} leading={leading} /><div className={styles.feedback}>
        <ErrorBox title={msg("Could not read {{value0}}", { value0: path })} error={loaded.error} />
      </div></>
    )
  }
  if (base === undefined || buffer === undefined) {
    return (
      <><PageHeader title={path.split('/').at(-1)!} leading={leading} /><div className={styles.feedback}>
        <Loading what={path} />
      </div></>
    )
  }

  return (
    <>
      <PageHeader title={path.split('/').at(-1)!} leading={leading}
        meta={dirty ? <Pill tone="danger">{msg('unsaved changes')}</Pill> : <span>{msg('saved')}</span>}
        actions={<Button variant="primary" disabled={!dirty || write.isPending} onClick={() => save(false)}>
          {write.isPending ? msg('Saving…') : msg('Save')}
        </Button>} />
      <div className={styles.editorBody}>
        <div className={styles.fileMeta}>
          <OverflowTooltip><code tabIndex={0}>{path}</code></OverflowTooltip>
          <span><Message text={"<0/> bytes"} slots={[base.bytes]} /></span>
          <code className={styles.digest} title={`sha256 ${base.sha256}`}>sha256 {base.sha256.slice(0, 12)}…</code>
        </div>
        <div className={styles.notices}>
        {deleted && (
          <p className="note note-warn" role="alert"><Message text={"<0/> Something else deleted or moved it. Your edit is still here and nothing has been written; saving will recreate the file, and will be refused first because the bytes this edit started from are gone."} slots={[<strong>{msg("This file is no longer in the project.")}</strong>]} /></p>
        )}
        {changedOnDisk && !deleted && (
          <p className="note note-warn"><Message text={"<0/> Your edit is still against the bytes you loaded, and saving will be refused rather than overwrite the change. Reload to start from what is there now — that discards what is in the box."} slots={[<strong>{msg("This file changed on disk since you opened it.")}</strong>]} /></p>
        )}

        </div>
        <label className={styles.editorLabel} htmlFor="authoring-buffer">{msg("File contents")}</label>
        <textarea
          id="authoring-buffer"
          className={`code-editor ${styles.buffer}`}
          spellCheck={false}
          value={buffer}
          onChange={(event) => setBuffer(event.target.value)}
        />

        <div className={styles.editorActions}>
          <Button
            variant="quiet"
            disabled={!dirty || write.isPending}
            onClick={async () => {
              if (!await confirmDiscard(msg('Discard unsaved changes to this file?'), { name: path })) return
              // Discard puts the buffer back *and* clears what the last attempt
              // said about it. A stale conflict notice with a live "Overwrite
              // anyway" beside a buffer that no longer differs is an offer to
              // write something nobody is proposing.
              setBuffer(base.content)
              editing.reset()
            }}
          >{msg("Discard changes")}</Button>
          {/* Disabled while a write is in flight: the PUT cannot be cancelled,
              so reloading during one would replace the base with bytes that are
              about to be superseded by a save already on its way. */}
          <Button
            variant="quiet"
            disabled={write.isPending}
            onClick={reload}
          >{msg("Reload from disk")}</Button>
        </div>

        <div className={styles.notices}>
        {stale && (
          <StaleNotice
            stale={stale}
            pending={write.isPending}
            onReload={reload}
            onOverride={() => save(true)}
          />
        )}
        {failure && <ErrorBox title={msg("Could not save {{value0}}", { value0: path })} error={failure} />}
        {reloadError && (
          <ErrorBox title={msg("Could not reload {{value0}}", { value0: reloadError.path })} error={reloadError.error} />
        )}

        {outcome && !stale && (
          <p className={verified ? 'note' : 'note note-warn'}>
            {verified ? (
              <><Message text={"<0/> The chassis replaced the file and read it back off the disk: <1/> bytes, sha256<2/><3/>, byte for byte what was sent.<4/>"} slots={[<strong>{msg("Saved, and verified.")}</strong>, outcome.landed.bytes, ' ', <code>{outcome.landed.sha256.slice(0, 12)}…</code>, outcome.landed.created ? msg(" The file did not exist before this save.") : '']} /></>
            ) : (
              <><Message text={"<0/> The write completed and the bytes now on disk are not the bytes that were sent. Reload before editing further — what is in this buffer is not what the file holds."} slots={[<strong>{msg("Saved, and the read-back does not match.")}</strong>]} /></>
            )}
          </p>
        )}
        </div>
      </div>
    </>
  )
}

/**
 * A write refused because the file changed underneath the edit.
 *
 * Both digests are shown because both are facts the user needs: the one this
 * edit started from, and the one on disk now. Nothing is written and nothing is
 * lost — the buffer is still here — and the two ways forward are stated as what
 * they are rather than one of them being taken silently.
 */
function StaleNotice({
  stale,
  pending,
  onReload,
  onOverride
}: {
  stale: StaleWrite
  pending: boolean
  onReload: () => void
  onOverride: () => void
}) {
  useLocale()
  return (
    <div className="note note-warn stale-write" role="alert">
      <p><Message text={"<0/><1/><2/><3/>Nothing has been written, and your edit is still in the box above."} slots={[<strong>{msg("Not saved: the file changed since you opened it.")}</strong>, ' ', stale.exists
          ? msg("Something else wrote to it while this edit was open.")
          : msg("The file is no longer on disk — something else deleted or moved it."), ' ']} /></p>
      <p className="meta">
        <span><Message text={"this edit started from <0/>"} slots={[<code>sha256 {shortDigest(stale.expectedSha256)}</code>]} /></span>
        <span><Message text={"on disk now <0/>"} slots={[<code>sha256 {shortDigest(stale.actualSha256)}</code>]} /></span>
      </p>
      <div className="actions">
        <Button disabled={pending} onClick={onReload}>{msg("Reload from disk")}</Button>
        <Button
          variant="danger"
          disabled={pending}
          onClick={onOverride}
        >{msg("Overwrite anyway")}</Button>
      </div>
    </div>
  )
}

function shortDigest(digest: string): string {
  return digest ? `${digest.slice(0, 12)}…` : msg("(no file)")
}
