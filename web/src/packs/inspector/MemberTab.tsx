/**
 * The selected member's own JSON subtree, and where the bytes came from.
 *
 * Strings are readable prose; structured values keep their full JSON shape.
 * Soft wrapping never changes the value copied from the document.
 *
 * The provenance disclosure holds the path, byte length and digest of the loaded
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
import { CodeBlock } from '../../ui/CodeBlock'
import { MemberValue } from './MemberValue'
import styles from './PackInspector.module.css'

export function MemberTab({
  pointer,
  subtree,
  meta,
  fileSha256,
  fileBytes,
  baseSha256,
  dirty,
  metadataOnly = false
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
  /** The Logic Inspector already renders this member. */
  metadataOnly?: boolean
}) {
  // **All three, and each of them defined.** An absent base was read as
  // agreement, so a page whose editor holds no revision of this file at all —
  // the read has not answered, or it failed — printed "matches the file the
  // editor holds" on the strength of the other two agreeing with each other.
  const bound =
    dirty !== true &&
    meta.sha256 !== undefined &&
    fileSha256 !== undefined &&
    meta.sha256 === fileSha256 &&
    // An undefined base is not equal to anything, which is the point: it was
    // written as "undefined **or** equal", so a page whose editor holds no
    // revision of this file at all printed the sentence on the strength of the
    // other two agreeing with each other.
    baseSha256 === fileSha256

  return (
    <div className={styles.panel}>
      {!metadataOnly && <p className={styles.pointer}>
        <code>{pointer}</code>
      </p>}
      {!metadataOnly && (subtree === undefined ? (
        <p className={styles.empty}>The document declares no member at this pointer.</p>
      ) : (
        <>
          <MemberValue value={subtree} />
          {(subtree === null || typeof subtree !== 'object') && <details className={styles.disclosure}>
            <summary>Exact value JSON</summary><CodeBlock text={JSON.stringify(subtree, null, 2)} />
          </details>}
        </>
      ))}

      <details className={styles.disclosure}>
      <summary>Provenance</summary>
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
              <code>{meta.sha256}</code>
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
      </details>
      {dirty === true && (
        <p className={styles.unbound}>
          These figures are the file on disk. The editor holds changes that are not in it.
        </p>
      )}
      {dirty !== true &&
        baseSha256 !== undefined &&
        fileSha256 !== undefined &&
        baseSha256 !== fileSha256 && (
        <p className={styles.unbound}>
          These figures are the file on disk. The editor is showing the revision it loaded,
          which is not that one.
        </p>
      )}
    </div>
  )
}
