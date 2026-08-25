import { useQueryClient } from '@tanstack/react-query'
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
 * What it *is* responsible for is the two things a plain editor gets wrong:
 *
 * - **Dirty state that is honest.** The buffer is compared to the bytes that
 *   were loaded, not to the last thing typed, so returning a file to its
 *   original contents is not "modified".
 * - **A save that proves itself.** The chassis answers a write with a read-back
 *   from the disk, and this compares that read-back to what it sent. "Saved" is
 *   a claim about bytes on a filesystem, and the only honest way to make it is
 *   to have read them.
 */
export function AuthorView() {
  const listing = useFileListing()
  const [selected, setSelected] = useState<string | undefined>(undefined)

  const files = listing.data?.files ?? []
  // Derived, not merely stored: a listing that no longer holds the selected
  // path (someone deleted it) must not leave the editor pointed at nothing.
  const current = files.some((file) => file.path === selected) ? selected : undefined

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
        validates anything: what these bytes mean is the runtime's answer, and every
        other view asks it. Schema-guided editing and validate-on-change are phase 2
        of <a href="https://github.com/Judgment-Pack/judgment-pack-desk/issues/14">#14</a>.
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
                      className={`file-entry${file.path === current ? ' file-entry-on' : ''}`}
                      aria-current={file.path === current ? 'true' : undefined}
                      onClick={() => setSelected(file.path)}
                    >
                      <code>{file.path}</code>
                      <span className="quiet">{file.bytes} bytes</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Section>

          {current ? (
            <FileEditor key={current} path={current} />
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

/**
 * One file, open.
 *
 * Keyed by path at the call site, so switching files remounts rather than
 * carrying one file's buffer into another's — an editor that reused state
 * across that boundary would let a save write one document's text to another
 * document's path.
 *
 * This is the seam phase 2 attaches to: the buffer and its dirty state live
 * here, so a validation panel reads `buffer` and a schema form replaces the
 * textarea, without either of them touching the save path below.
 */
function FileEditor({ path }: { path: string }) {
  const loaded = useFileContent(path)
  const write = useWriteFile()
  const queryClient = useQueryClient()

  const [buffer, setBuffer] = useState<string | undefined>(undefined)
  // What the last successful save put on disk, as the chassis read it back.
  const [saved, setSaved] = useState<FileContent | undefined>(undefined)

  // The bytes this edit started from: the loaded file, or the last save. Both
  // the dirty comparison and the write's `baseSha256` come from here, so they
  // can never disagree about which revision is being edited.
  const base = saved ?? loaded.data

  useEffect(() => {
    if (loaded.data && buffer === undefined) setBuffer(loaded.data.content)
  }, [loaded.data, buffer])

  const dirty = useMemo(
    () => buffer !== undefined && base !== undefined && buffer !== base.content,
    [buffer, base]
  )

  const stale = write.error instanceof StaleWrite ? write.error : undefined
  const failure = write.error && !stale ? write.error : undefined

  // Reload seeds the buffer from the *refetched* bytes rather than clearing it
  // and letting the mount effect re-seed. Clearing seeds from whatever is in
  // the cache at that instant, which is the revision being reloaded away from —
  // so a reload would put the stale bytes straight back and report them as
  // current.
  const reload = () => {
    write.reset()
    setSaved(undefined)
    void loaded.refetch().then((result) => {
      if (result.data) setBuffer(result.data.content)
      void queryClient.invalidateQueries({ queryKey: ['desk-files'] })
    })
  }

  const save = (override: boolean) => {
    if (buffer === undefined || base === undefined) return
    write.mutate(
      { path, content: buffer, baseSha256: base.sha256, override },
      {
        onSuccess: (result) => {
          setSaved(result)
          void queryClient.invalidateQueries({ queryKey: ['desk-files'] })
          void queryClient.invalidateQueries({ queryKey: ['desk-file', path] })
        }
      }
    )
  }

  if (loaded.error) {
    return (
      <Section title="Editor">
        <ErrorBox title={`Could not read ${path}`} error={loaded.error} />
      </Section>
    )
  }
  if (loaded.isPending || buffer === undefined || base === undefined) {
    return (
      <Section title="Editor">
        <Loading what={path} />
      </Section>
    )
  }

  // The proof, not the assumption: the chassis read the file back off the disk
  // after the rename, and this compares that to what was sent.
  const verified = saved !== undefined && saved.content === buffer

  return (
    <Section title="Editor">
      <>
        <p className="meta">
          <code>{path}</code>
          <span>{base.bytes} bytes</span>
          <code>sha256 {base.sha256.slice(0, 12)}…</code>
          {dirty ? <Pill tone="danger">unsaved changes</Pill> : <Pill tone="quiet">saved</Pill>}
        </p>

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
          <button type="button" className="link-button" onClick={reload}>
            Reload from disk
          </button>
        </div>

        {stale && <StaleNotice stale={stale} onReload={reload} onOverride={() => save(true)} />}
        {failure && <ErrorBox title={`Could not save ${path}`} error={failure} />}

        {saved && !stale && (
          <p className={verified ? 'note' : 'note note-warn'}>
            {verified ? (
              <>
                <strong>Saved, and verified.</strong> The chassis replaced the file
                atomically and read it back off the disk: {saved.bytes} bytes, sha256{' '}
                <code>{saved.sha256.slice(0, 12)}…</code>, byte for byte what was sent.
                {saved.created ? ' The file did not exist before this save.' : ''}
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
