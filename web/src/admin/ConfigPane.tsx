/**
 * What is in the file, in the **right pane** — Admin's read-only context.
 *
 * **The bytes left the main column, and where they went is the change.** The
 * Content disclosure sat under every card: a `details` a reader opened, a code
 * block that pushed the fields below the fold, and one more thing stacked into
 * a vertical scroll that already carried four sections. It is context rather
 * than a setting — nobody edits it here — and context is what the Inspector is
 * for. So the page claims the slot the pack routes claim, on the same
 * mechanism, and the file is beside the form instead of underneath it.
 *
 * **A refused file shows no bytes, and that rule did not move with them.** The
 * decoder refuses a whole file for one credential-shaped member, and the point
 * of refusing it is that the desk will not act on it; rendering its bytes in
 * the pane would put the very member the refusal is about into the DOM of the
 * page that reported the refusal — the same disclosure, one pane over. The gate
 * is `showsContent`, imported from the card rather than spelled again here,
 * because two spellings of one rule are invisible to a harness that breaks one
 * of them.
 *
 * **And what is shown is the member's own bytes, never a re-serialisation.**
 * `1e2` is not `100`, an integer past a float64's precision is not what it
 * round-trips to, and `idBase` gains a separator at decode — so a pane that
 * stringified the decoded value would be showing a reader a file that is not on
 * disk while a Location row underneath it says where that file is. Where the
 * bytes cannot be established `memberBytes` answers nothing, and this says so
 * in one line rather than offering the decode in their place.
 */
import { memberBytes } from './memberBytes'
import { showsContent, StatusLine, type SourceStatus } from './SourceCard'
import styles from './ConfigPane.module.css'
import type { ReactNode } from 'react'

/**
 * The four states `memberBytes` deliberately does not tell apart, in one
 * sentence: the text is not one JSON object, it is malformed, the member is
 * absent, or the member is written twice. The caller does the same thing in all
 * four, so the page says the same thing about all four.
 */
export const NO_BYTES_SAYS = 'the member’s own bytes could not be established in this file'

/** The digest, as much of it as identifies a revision on sight. */
export function digestSays(sha256: string): string {
  return `sha256 ${sha256.slice(0, 12)}…`
}

export function ConfigPane({
  title,
  location,
  status,
  digest,
  text,
  member
}: {
  /** `<file> › <member>`, or the file alone where the pane is about all of it. */
  title: string
  /** The path, from the chassis — never composed here. */
  location: ReactNode
  status: SourceStatus
  /** The digest of the bytes this read saw, where a file was read. */
  digest?: string
  /** The file's own bytes, where this page read them. */
  text?: string
  /** The top-level member to quote, or the whole file where absent. */
  member?: string
}) {
  // **One gate, and it is the card's.** A refused file's bytes are the thing
  // the refusal is about, and an unread file's are bytes this page never had.
  const quotable = showsContent(status) && text !== undefined
  const bytes = !quotable
    ? undefined
    : member === undefined
      ? text
      : memberBytes(text, member)
  return (
    <section className={styles.pane} aria-labelledby="admin-file-title">
      <h2 className={styles.title} id="admin-file-title">
        {title}
      </h2>
      <dl className={styles.head}>
        <div className={styles.row}>
          <dt className={styles.key}>Location</dt>
          <dd className={styles.value}>{location}</dd>
        </div>
        {/* The empty string is the chassis saying "there is no file", which is
            a digest a write may state and not one a reader can be shown. The
            Status row is what says which of those this is. */}
        {digest !== undefined && digest !== '' && (
          <div className={styles.row}>
            <dt className={styles.key}>Digest</dt>
            <dd className={styles.value}>
              <code>{digestSays(digest)}</code>
            </dd>
          </div>
        )}
        <div className={styles.row}>
          <dt className={styles.key}>Status</dt>
          <dd className={styles.value}>
            <StatusLine status={status} />
          </dd>
        </div>
      </dl>
      {bytes !== undefined && (
        <pre className={styles.bytes}>
          <code>{bytes}</code>
        </pre>
      )}
      {quotable && bytes === undefined && <p className={styles.none}>{NO_BYTES_SAYS}</p>}
    </section>
  )
}
