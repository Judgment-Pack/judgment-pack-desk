import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useBlocker } from 'react-router-dom'
import { Empty, ErrorBox, Loading, Pill, Section } from '../components/primitives'
import {
  StaleWrite,
  readFile,
  type FileContent,
  type FileEntry,
  type FileListing
} from '../files/client'
import { useFileContent, useFileListing, useWriteFile } from '../files/queries'
import { publishDirty, takeRequestedOpen, useRequestedOpen } from '../shell/authorBridge'

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
  const listing = useFileListing()
  const [selected, setSelected] = useState<string | undefined>(undefined)
  const [dirty, setDirty] = useState(false)

  const files = listing.data?.files ?? []
  const partial = listing.data?.partial ?? []

  // The selection is not dropped when the listing stops carrying it. A file
  // deleted underneath an open editor is the case the editor most needs to
  // survive: unmounting would throw the buffer away before the user is told.
  const listedNow = files.some((file) => file.path === selected)

  // The two lines the shell needs from this view, and nothing else changes
  // here. `dirty` is component-local and the selection has no URL parameter,
  // so the rail's dot and the Create-pack dialog's open-the-new-file cannot
  // work without them.
  useEffect(() => {
    publishDirty(dirty)
    return () => publishDirty(false)
  }, [dirty])
  // Driven by the published value rather than by mount, because creating a
  // pack while this view is already open navigates to the route it is already
  // on: the element does not remount, and a `[]`-dependency effect would never
  // run again. It routes through `choose` so the dirty-buffer question is
  // asked here too, rather than only where a file is clicked.
  //
  // Still an effect and not a lazy `useState` initializer: StrictMode invokes
  // an initializer twice and would swallow a consume-once take in the run
  // whose state React discards. An effect's mount → cleanup → mount preserves
  // state, so the second run simply finds nothing to do.
  const requestedOpen = useRequestedOpen()
  useEffect(() => {
    const requested = takeRequestedOpen()
    if (requested) choose(requested)
    // `choose` is re-created every render and is not a dependency: this effect
    // fires on a new request, never on a re-render of the editor.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestedOpen])

  // Two guards, because they cover two different exits and neither covers the
  // other. `beforeunload` is the browser's, and it fires only when the document
  // itself goes — a reload, a close, a link off the site. Everything inside this
  // application is same-document routing, which that event never sees: Back out
  // of the editor, or follow any in-app link, and the component simply unmounts
  // with the buffer in it.
  useEffect(() => {
    if (!dirty) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    dirty && currentLocation.pathname !== nextLocation.pathname
  )
  useEffect(() => {
    if (blocker.state !== 'blocked') return
    if (window.confirm('This file has unsaved changes that will be lost. Leave anyway?')) {
      blocker.proceed()
    } else {
      blocker.reset()
    }
  }, [blocker])

  const choose = (path: string) => {
    if (path === selected) return
    if (
      dirty &&
      !window.confirm(
        `${selected} has unsaved changes that will be lost. Open ${path} anyway?`
      )
    ) {
      return
    }
    setDirty(false)
    setSelected(path)
  }

  return (
    <article className="detail authoring">
      <header className="detail-head">
        <h1>Author</h1>
        <p className="ids">
          <Pill tone="quiet">phase 1</Pill>
          <span className="quiet">edit the project's files; the runtime judges them</span>
        </p>
        {listing.data?.root && (
          <p className="meta">
            <code>{listing.data.root}</code>
          </p>
        )}
      </header>

      <p className="note">
        <strong>The desk owns writes; the runtime does not.</strong> The runtime is a
        stateless judge with no write tools by design (ADR-0006), so saving happens
        here, over loopback, inside the project directory only. Nothing on this page
        validates anything — not even that a file is JSON: what these bytes mean is
        the runtime's answer, and every other view asks it. Schema-guided editing and
        validate-on-change are phase 2 of{' '}
        <a href="https://github.com/Judgment-Pack/judgment-pack-desk/issues/14">#14</a>.
      </p>

      {/* An error replaces the pane only when there is nothing behind it.
          TanStack keeps the previous listing after a failed refetch, and the
          file watcher refetches on every change — so treating any error as
          fatal would unmount an open editor, and its buffer with it, because
          something unrelated failed once. */}
      {listing.error && !listing.data ? (
        <ErrorBox title="Could not list the project's files" error={listing.error} />
      ) : listing.isPending ? (
        <Loading what="the project's files" />
      ) : (
        <div className="authoring-panes">
          {listing.error && (
            <p className="note note-warn authoring-wide" role="status">
              <strong>The file list could not be refreshed</strong> —{' '}
              {listing.error.message}. What is shown is the last listing that
              answered; your edit is untouched.
            </p>
          )}
          <Section title="Files" count={files.length}>
            {partial.length > 0 && (
              <p className="note note-warn" role="status">
                <strong>This list is incomplete.</strong> The desk could not read
                everything in the project:
                <br />
                {partial.map((problem) => (
                  <code key={problem} className="partial-reason">
                    {problem}
                  </code>
                ))}
              </p>
            )}
            {files.length === 0 && partial.length === 0 ? (
              <Empty>This project directory contains no files.</Empty>
            ) : files.length === 0 ? (
              // Not "no files" — nothing readable. The difference is the whole
              // reason `partial` exists, and stating the definite version here
              // would report a permission error as an empty project.
              <Empty>Nothing in this project could be read; see above.</Empty>
            ) : (
              <ul className="file-list">
                {files.map((file) => (
                  <li key={file.path}>
                    <button
                      type="button"
                      className={`file-entry${file.path === selected ? ' file-entry-on' : ''}`}
                      aria-current={file.path === selected ? 'true' : undefined}
                      onClick={() => choose(file.path)}
                    >
                      <code>{file.path}</code>
                      <span className="quiet">{file.bytes} bytes</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Section>

          {selected ? (
            <FileEditor key={selected} path={selected} listed={listedNow} onDirty={setDirty} />
          ) : (
            <Section title="Editor">
              <Empty>Choose a file to edit.</Empty>
            </Section>
          )}
        </div>
      )}
    </article>
  )
}

/** What the last save produced, kept apart from the live buffer. */
interface SaveOutcome {
  /** The bytes that were submitted, captured at the moment of the request. */
  submitted: string
  /** What the chassis read back off the disk afterwards. */
  landed: FileContent
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
  onDirty
}: {
  path: string
  /** Whether the current listing still carries this path. */
  listed: boolean
  onDirty: (dirty: boolean) => void
}) {
  const loaded = useFileContent(path)
  const write = useWriteFile()
  const queryClient = useQueryClient()

  // The revision this edit is against. Editor-local and immutable except where
  // the user acts: seeded once from the first successful load, replaced by an
  // explicit reload or a successful save. Never by a background refetch.
  const [base, setBase] = useState<FileContent | undefined>(undefined)
  const [buffer, setBuffer] = useState<string | undefined>(undefined)
  const [outcome, setOutcome] = useState<SaveOutcome | undefined>(undefined)
  const [reloadError, setReloadError] = useState<Error | undefined>(undefined)

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

  const reload = () => {
    write.reset()
    setOutcome(undefined)
    // A direct read, not a refetch. `refetch()` reports success from cache
    // when the watcher's broad `cancelQueries()` cancels the request in flight,
    // so its success is not proof that anything was fetched — and installing
    // cached bytes as the new base is how a reload replaces an edit with what
    // it was already showing. Only this request's own answer counts.
    setReloadError(undefined)
    void readFile(path)
      .then((fresh) => {
        setBase(fresh)
        setBuffer(fresh.content)
        queryClient.setQueryData(['desk-file', path], fresh)
      })
      .catch((cause: unknown) => {
        setReloadError(cause instanceof Error ? cause : new Error(String(cause)))
      })
  }

  const save = (override: boolean) => {
    if (buffer === undefined || base === undefined) return
    // A previous verdict does not survive into a new attempt: leaving "Saved,
    // and verified" on screen while the next save is pending or failing states
    // something about bytes that are no longer the question.
    setOutcome(undefined)
    // The snapshot is captured here, with the request. Everything about
    // verifying this save is judged against it and never against the buffer,
    // which the user is free to keep typing into.
    const submitted = buffer
    // When this save was issued, measured against the file query's own clock.
    const startedAt = queryClient.getQueryState(['desk-file', path])?.dataUpdatedAt ?? 0
    write.mutate(
      { path, content: submitted, baseSha256: base.sha256, override },
      {
        onSuccess: (landed) => {
          setOutcome({ submitted, landed })
          setBase(landed)
          // The read-back is authoritative about the bytes this save wrote, and
          // *not* about anything that happened afterwards. A watcher refetch
          // that completed while this PUT was in flight is newer than this
          // answer, and installing over it would replace a fresher read and
          // clear the invalidation that fetched it.
          const state = queryClient.getQueryState(['desk-file', path])
          if (state !== undefined && state.dataUpdatedAt > startedAt) return
          // The read-back is the authority on what is now on disk, so the
          // caches are told rather than left to disagree with it. Without this
          // the page can say "Saved, and verified" beside a "changed on disk"
          // warning derived from the pre-save cache — both from the same save.
          queryClient.setQueryData(['desk-file', path], landed)
          queryClient.setQueryData(['desk-files'], (previous: FileListing | undefined) =>
            previous === undefined
              ? previous
              : {
                  ...previous,
                  files: upsertListed(previous.files, {
                    path: landed.path,
                    bytes: landed.bytes,
                    sha256: landed.sha256
                  })
                }
          )
        }
      }
    )
  }

  if (loaded.error && base === undefined) {
    return (
      <Section title="Editor">
        <ErrorBox title={`Could not read ${path}`} error={loaded.error} />
      </Section>
    )
  }
  if (base === undefined || buffer === undefined) {
    return (
      <Section title="Editor">
        <Loading what={path} />
      </Section>
    )
  }

  // The proof, not the assumption: the chassis read the file back off the disk
  // after the rename, and this compares that to the bytes that were sent.
  const verified = outcome !== undefined && outcome.landed.content === outcome.submitted

  return (
    <Section title="Editor">
      <>
        <p className="meta">
          <code>{path}</code>
          <span>{base.bytes} bytes</span>
          <code>sha256 {base.sha256.slice(0, 12)}…</code>
          {dirty ? <Pill tone="danger">unsaved changes</Pill> : <Pill tone="quiet">saved</Pill>}
        </p>

        {deleted && (
          <p className="note note-warn" role="alert">
            <strong>This file is no longer in the project.</strong> Something else
            deleted or moved it. Your edit is still here and nothing has been written;
            saving will recreate the file, and will be refused first because the bytes
            this edit started from are gone.
          </p>
        )}
        {changedOnDisk && !deleted && (
          <p className="note note-warn">
            <strong>This file changed on disk since you opened it.</strong> Your edit is
            still against the bytes you loaded, and saving will be refused rather than
            overwrite the change. Reload to start from what is there now — that discards
            what is in the box.
          </p>
        )}

        <label className="editor-label" htmlFor="authoring-buffer">
          File contents
        </label>
        <textarea
          id="authoring-buffer"
          className="code-editor"
          rows={22}
          spellCheck={false}
          value={buffer}
          onChange={(event) => setBuffer(event.target.value)}
        />

        <div className="actions">
          <button
            type="button"
            className="button"
            disabled={!dirty || write.isPending}
            onClick={() => save(false)}
          >
            {write.isPending ? 'Saving…' : 'Save'}
          </button>
          <button
            type="button"
            className="button button-quiet"
            disabled={!dirty || write.isPending}
            onClick={() => {
              // Discard puts the buffer back *and* clears what the last attempt
              // said about it. A stale conflict notice with a live "Overwrite
              // anyway" beside a buffer that no longer differs is an offer to
              // write something nobody is proposing.
              setBuffer(base.content)
              setOutcome(undefined)
              write.reset()
            }}
          >
            Discard changes
          </button>
          {/* Disabled while a write is in flight: the PUT cannot be cancelled,
              so reloading during one would replace the base with bytes that are
              about to be superseded by a save already on its way. */}
          <button
            type="button"
            className="link-button"
            disabled={write.isPending}
            onClick={reload}
          >
            Reload from disk
          </button>
        </div>

        {stale && (
          <StaleNotice
            stale={stale}
            pending={write.isPending}
            onReload={reload}
            onOverride={() => save(true)}
          />
        )}
        {failure && <ErrorBox title={`Could not save ${path}`} error={failure} />}
        {reloadError && <ErrorBox title={`Could not reload ${path}`} error={reloadError} />}

        {outcome && !stale && (
          <p className={verified ? 'note' : 'note note-warn'}>
            {verified ? (
              <>
                <strong>Saved, and verified.</strong> The chassis replaced the file and
                read it back off the disk: {outcome.landed.bytes} bytes, sha256{' '}
                <code>{outcome.landed.sha256.slice(0, 12)}…</code>, byte for byte what
                was sent.
                {outcome.landed.created ? ' The file did not exist before this save.' : ''}
              </>
            ) : (
              <>
                <strong>Saved, and the read-back does not match.</strong> The write
                completed and the bytes now on disk are not the bytes that were sent.
                Reload before editing further — what is in this buffer is not what the
                file holds.
              </>
            )}
          </p>
        )}
      </>
    </Section>
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
  return (
    <div className="note note-warn stale-write" role="alert">
      <p>
        <strong>Not saved: the file changed since you opened it.</strong>{' '}
        {stale.exists
          ? 'Something else wrote to it while this edit was open.'
          : 'The file is no longer on disk — something else deleted or moved it.'}{' '}
        Nothing has been written, and your edit is still in the box above.
      </p>
      <p className="meta">
        <span>
          this edit started from <code>sha256 {shortDigest(stale.expectedSha256)}</code>
        </span>
        <span>
          on disk now <code>sha256 {shortDigest(stale.actualSha256)}</code>
        </span>
      </p>
      <div className="actions">
        <button type="button" className="button" disabled={pending} onClick={onReload}>
          Reload from disk
        </button>
        <button
          type="button"
          className="button button-quiet"
          disabled={pending}
          onClick={onOverride}
        >
          Overwrite anyway
        </button>
      </div>
    </div>
  )
}

function shortDigest(digest: string): string {
  return digest ? `${digest.slice(0, 12)}…` : '(no file)'
}

/** The listing with one entry replaced, or added where it was not there. */
function upsertListed(files: FileEntry[], entry: FileEntry): FileEntry[] {
  const without = files.filter((file) => file.path !== entry.path)
  return [...without, entry].sort((a, b) => a.path.localeCompare(b.path))
}
