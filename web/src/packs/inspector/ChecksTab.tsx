/**
 * The diagnostics that name this member.
 *
 * Each one prints the runtime's own words: `code`, `layer`, `severity`,
 * `codeStability` and the message, with the pointer at the foot. Nothing is
 * translated and nothing is coloured by a rule of this desk's own — the
 * severity is the runtime's word for how bad it is, and a second opinion in a
 * colour would be a verdict.
 *
 * **An empty set is not a clean bill**, and the panel does not dress it as
 * one. It says no other diagnostic named this member — and it does not even
 * say that where `diagnosticsTruncated` is set, because the runtime stopped at
 * its own limit and the desk cannot claim what is not there.
 *
 * The footer names which bytes were checked. In read mode that is the file's
 * bytes, or the served document where the file could not be read; the "bytes
 * now in the editor" wording arrives with the editor.
 */
import type { AnchoredDiagnostic } from '../checks'
import styles from './PackInspector.module.css'

export function ChecksTab({
  diagnostics,
  truncation,
  stale,
  checkedWhat,
  unavailable
}: {
  diagnostics: readonly AnchoredDiagnostic[]
  /** The sentence to print instead of "no other diagnostic", where the list was cut. */
  truncation: string | undefined
  /** True where the check describes bytes other than the ones on screen. */
  stale: boolean
  /** Which bytes the check ran over. */
  checkedWhat: string
  /** Why there is no check at all, where there is none. */
  unavailable?: string
}) {
  if (unavailable !== undefined) {
    return <p className={styles.empty}>{unavailable}</p>
  }
  return (
    <div className={styles.panel}>
      {stale && (
        <p className={styles.stale}>
          These diagnostics were computed against other bytes. Nothing below is anchored to what
          is on screen.
        </p>
      )}
      {diagnostics.length > 0 && (
        <ul className={styles.diagnostics}>
          {diagnostics.map((entry, index) => (
            <li key={`${entry.diagnostic.code}-${index}`} className={styles.diagnostic}>
              <p className={styles.diagnosticHead}>
                <code className={styles.code}>{entry.diagnostic.code}</code>
                {entry.diagnostic.layer !== undefined && (
                  <span className={styles.word}>{entry.diagnostic.layer}</span>
                )}
                {entry.diagnostic.severity !== undefined && (
                  <span className={styles.word}>{entry.diagnostic.severity}</span>
                )}
                {entry.diagnostic.codeStability !== undefined && (
                  <span className={styles.word}>{entry.diagnostic.codeStability}</span>
                )}
              </p>
              <p className={styles.message}>{entry.diagnostic.message}</p>
              <p className={styles.pointer}>
                <code>{entry.named}</code>
              </p>
            </li>
          ))}
        </ul>
      )}
      {truncation === undefined ? (
        <p className={styles.empty}>No other diagnostic names this member.</p>
      ) : (
        <p className={styles.empty}>{truncation}</p>
      )}
      <p className={styles.footer}>{checkedWhat}</p>
    </div>
  )
}
