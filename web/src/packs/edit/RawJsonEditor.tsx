/**
 * The buffer as the bytes it is.
 *
 * A third view of the **same** text, not a second document: a keystroke here
 * moves the buffer the forms write into and the reading document draws from,
 * and a form edit is in this box the moment it is made. Two buffers with a
 * synchronization step between them is the shape that produces a page and a
 * form over different revisions.
 *
 * Where the bytes do not scan this is the only view offered, and the reason is
 * printed with the position the scanner stopped at — a byte offset turned into
 * a line and column, because the gutter beside the text is numbered in lines.
 */
import { CodeArea } from '../../ui/CodeArea'
import styles from './RawJsonEditor.module.css'

export function RawJsonEditor({
  text,
  path,
  dirty,
  problem,
  readOnly,
  onChange
}: {
  text: string
  /** The declared path, for the foot line. */
  path: string | undefined
  dirty: boolean
  /** Why form mode is withheld, where it is. */
  problem?: string
  /**
   * True while there is no base revision — the chassis has not answered, or
   * could not.
   *
   * **Bytes with no base are bytes with no save.** The text shown is then the
   * runtime's served copy, and typing into it would edit something that is
   * about to be replaced by the file when it arrives, with nothing to write it
   * back to in the meantime. So it is read-only and says which bytes these
   * are, rather than accepting an edit it cannot keep.
   */
  readOnly?: boolean
  onChange: (next: string) => void
}) {
  return (
    <div className={styles.raw}>
      {problem !== undefined && (
        <p className={styles.problem} role="status">
          {problem}
        </p>
      )}
      {readOnly === true && (
        <p className={styles.problem} role="status">
          These are the bytes the runtime served. The file itself has not been read, so there is
          nothing here to save.
        </p>
      )}
      <label className={styles.label} htmlFor="pack-raw">
        The document&rsquo;s bytes
      </label>
      <CodeArea
        id="pack-raw"
        aria-label="The document's bytes"
        value={text}
        readOnly={readOnly}
        onChange={(event) => onChange(event.target.value)}
      />
      <p className={styles.foot}>
        {path !== undefined && <code>{path}</code>}
        <span>{byteLength(text)} bytes</span>
        <span>{dirty ? 'unsaved' : 'saved'}</span>
      </p>
    </div>
  )
}

/**
 * The size a save would write, which is bytes and not characters.
 *
 * `text.length` is UTF-16 code units, so a document with one emoji in a
 * description reported a number the chassis never agrees with — and the
 * chassis' own count is what the foot line is next to on every other surface.
 */
export function byteLength(text: string): number {
  return new TextEncoder().encode(text).length
}

/**
 * Where the scanner stopped, in the terms the gutter is numbered in.
 *
 * The scanner reports a byte offset, which is the honest thing for it to
 * report and is not a place a reader can find. This turns one into a line and
 * a column so the sentence names a line the editor shows.
 */
export function positionOf(text: string, offset: number): { line: number; column: number } {
  const upTo = text.slice(0, Math.max(0, Math.min(offset, text.length)))
  const lines = upTo.split('\n')
  return { line: lines.length, column: (lines[lines.length - 1] ?? '').length + 1 }
}
