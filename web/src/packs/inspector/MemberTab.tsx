/**
 * The selected member's own JSON subtree, and where the bytes came from.
 *
 * The subtree is printed rather than described. It is the member as the
 * document carries it, pretty-printed for reading, in a container that scrolls
 * sideways — a long condition is long, and wrapping it would change where the
 * lines break in a document whose line breaks are someone's.
 *
 * The provenance group lives here rather than in a fourth tab, which is how
 * board 1 draws it: the path, the byte length, the digest of the loaded
 * document, and one sentence — **printed only when the two digests are
 * equal**. `get_pack`'s `sha256` and the file read's are two answers about one
 * file, and only equality proves they describe one revision. Printing the
 * sentence unconditionally would be the desk asserting a binding it never
 * checked.
 *
 * **And only while the editor holds those bytes.** Both digests are about the
 * file; an unsaved edit is about neither. Saying "matches the file the editor
 * holds" over a buffer that has moved states the one thing this whole group
 * exists to be honest about, falsely — so while the buffer is dirty the
 * sentence is replaced by what these figures actually describe.
 */
import type { PackFileMeta } from '../../mcp/types'
import styles from './PackInspector.module.css'

export function MemberTab({
  pointer,
  subtree,
  meta,
  fileSha256,
  fileBytes,
  baseSha256,
  dirty
}: {
  pointer: string
  /** The member at that pointer, or undefined where the document has none. */
  subtree: unknown
  meta: PackFileMeta
  /** The digest the chassis reported for the same path, where it answered. */
  fileSha256: string | undefined
  fileBytes: number | undefined
  /**
   * The digest of the revision the editor actually loaded.
   *
   * A third answer, and the one the subtree above is drawn from. The other two
   * move with a watcher refetch and this one deliberately does not, so a file
   * changed underneath an open editor made the other two agree with each other
   * about bytes that are **not** on screen — and the sentence below said the
   * page matched a file it had never read.
   */
  baseSha256?: string | undefined
  /** True where the editor holds bytes that are not on disk. */
  dirty?: boolean
}) {
  const bound =
    dirty !== true &&
    meta.sha256 !== undefined &&
    fileSha256 !== undefined &&
    meta.sha256 === fileSha256 &&
    (baseSha256 === undefined || baseSha256 === fileSha256)

  return (
    <div className={styles.panel}>
      <p className={styles.pointer}>
        <code>{pointer}</code>
      </p>
      {subtree === undefined ? (
        <p className={styles.empty}>The document declares no member at this pointer.</p>
      ) : (
        <pre className={styles.json}>
          <code>{JSON.stringify(subtree, null, 2)}</code>
        </pre>
      )}

      <h3 className={styles.groupHead}>Provenance</h3>
      <dl className={styles.provenance}>
        {meta.path !== undefined && (
          <div className={styles.row}>
            <dt>path</dt>
            <dd>
              <code>{meta.path}</code>
            </dd>
          </div>
        )}
        {meta.bytes !== undefined && (
          <div className={styles.row}>
            <dt>bytes</dt>
            <dd>{meta.bytes.toLocaleString()}</dd>
          </div>
        )}
        {meta.sha256 !== undefined && (
          <div className={styles.row}>
            <dt>sha256</dt>
            <dd>
              <code title={meta.sha256}>{meta.sha256}</code>
            </dd>
          </div>
        )}
        {fileBytes !== undefined && fileBytes !== meta.bytes && (
          <div className={styles.row}>
            <dt>file bytes</dt>
            <dd>{fileBytes.toLocaleString()}</dd>
          </div>
        )}
      </dl>
      {bound && <p className={styles.bound}>matches the file the editor holds</p>}
      {dirty === true && (
        <p className={styles.unbound}>
          These figures are the file on disk. The editor holds changes that are not in it.
        </p>
      )}
      {dirty !== true && baseSha256 !== undefined && fileSha256 !== undefined && baseSha256 !== fileSha256 && (
        <p className={styles.unbound}>
          These figures are the file on disk. The editor is showing the revision it loaded,
          which is not that one.
        </p>
      )}
    </div>
  )
}
