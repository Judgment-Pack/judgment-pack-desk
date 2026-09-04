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
 */
import type { PackFileMeta } from '../../mcp/types'
import styles from './PackInspector.module.css'

export function MemberTab({
  pointer,
  subtree,
  meta,
  fileSha256,
  fileBytes
}: {
  pointer: string
  /** The member at that pointer, or undefined where the document has none. */
  subtree: unknown
  meta: PackFileMeta
  /** The digest the chassis reported for the same path, where it answered. */
  fileSha256: string | undefined
  fileBytes: number | undefined
}) {
  const bound =
    meta.sha256 !== undefined && fileSha256 !== undefined && meta.sha256 === fileSha256

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
    </div>
  )
}
