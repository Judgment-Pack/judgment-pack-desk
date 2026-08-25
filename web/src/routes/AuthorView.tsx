import { useEffect, useMemo, useState } from 'react'
import { Empty, ErrorBox, Loading, Pill, Section } from '../components/primitives'
import { StaleWrite, type FileContent } from '../files/client'
import { useFileContent, useFileListing, useWriteFile } from '../files/queries'

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

  // The selection is not dropped when the listing stops carrying it. A file
  // deleted underneath an open editor is the case the editor most needs to
  // survive: unmounting would throw the buffer away before the user is told.
  const listedNow = files.some((file) => file.path === selected)

  // The browser's own guard. It only fires on a real navigation or close, and
  // only where the page has something to lose.
  useEffect(() => {
    if (!dirty) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

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

      {listing.error ? (
        <ErrorBox title="Could not list the project's files" error={listing.error} />
      ) : listing.isPending ? (
        <Loading what="the project's files" />
      ) : (
        <div className="authoring-panes">
          <Section title="Files" count={files.length}>
            {files.length === 0 ? (
              <Empty>This project directory contains no files.</Empty>
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

  // The revision this edit is against. Editor-local and immutable except where
  // the user acts: seeded once from the first successful load, replaced by an
  // explicit reload or a successful save. Never by a background refetch.
  const [base, setBase] = useState<FileContent | undefined>(undefined)
  const [buffer, setBuffer] = useState<string | undefined>(undefined)
  const [outcome, setOutcome] = useState<SaveOutcome | undefined>(undefined)

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
    void loaded.refetch().then((result) => {
      if (result.data) {
        setBase(result.data)
        setBuffer(result.data.content)
      }
    })
  }

  const save = (override: boolean) => {
    if (buffer === undefined || base === undefined) return
    // The snapshot is captured here, with the request. Everything about
    // verifying this save is judged against it and never against the buffer,
    // which the user is free to keep typing into.
    const submitted = buffer
    write.mutate(
      { path, content: submitted, baseSha256: base.sha256, override },
      {
        onSuccess: (landed) => {
          setOutcome({ submitted, landed })
          setBase(landed)
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
            onClick={() => setBuffer(base.content)}
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

        {stale && <StaleNotice stale={stale} onReload={reload} onOverride={() => save(true)} />}
        {failure && <ErrorBox title={`Could not save ${path}`} error={failure} />}

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
  onReload,
  onOverride
}: {
  stale: StaleWrite
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
        <button type="button" className="button" onClick={onReload}>
          Reload from disk
        </button>
        <button type="button" className="button button-quiet" onClick={onOverride}>
          Overwrite anyway
        </button>
      </div>
    </div>
  )
}

function shortDigest(digest: string): string {
  return digest ? `${digest.slice(0, 12)}…` : '(no file)'
}
