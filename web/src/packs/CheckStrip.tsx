/**
 * One sentence about the check, in the tense of what has actually happened —
 * printed identically whether the document is being read or edited.
 *
 * It was inside `PackView`, which meant edit mode would have had a second
 * spelling of it. There is one: the runtime's own status and layer rows, the
 * diagnostics that named no member the page draws, and the two sentences about
 * bytes — the runtime serving a different revision from the file, and this
 * desk's own reading of the file disagreeing with `JSON.parse`.
 *
 * Nothing here is a verdict. Every word is either the runtime's own or a
 * statement about which bytes were looked at.
 */
import type { ReactNode } from 'react'
import type { AnchoredDiagnostic } from './checks'
import { layersReached } from './checks'
import type { ValidationReport } from '../mcp/types'
import styles from './CheckStrip.module.css'

export function CheckStrip({
  unavailable,
  fetching,
  stale,
  behind,
  report,
  provenance,
  rootAnchored,
  digestsDisagree,
  disagreement,
  children
}: {
  /** Why there is no check, where there is none. */
  unavailable?: string
  fetching: boolean
  /** The report describes bytes other than the ones on screen. */
  stale: boolean
  /**
   * The words for a stale report, which differ by why it is stale: a check
   * behind a buffer somebody is typing into is not the same event as a report
   * about a revision the page never showed.
   */
  behind?: string
  report: ValidationReport | undefined
  provenance?: string
  rootAnchored: readonly AnchoredDiagnostic[]
  digestsDisagree: boolean
  disagreement: { pointer: string; reason: string }[]
  /** The lock line, where the project's listing carries one. */
  children?: ReactNode
}) {
  return (
    <div className={styles.strip}>
      <p className={styles.check}>
        {unavailable ??
          // Progress is `fetchStatus`, which is about a request being in
          // flight. `isPending` is about there being no data, which a disabled
          // query satisfies for ever.
          (fetching
            ? 'Checking…'
            : stale
              ? (behind ??
                'This check ran over different bytes from the ones shown, so nothing it found is placed on this document.')
              : layersReached(report).text)}
      </p>
      {provenance !== undefined && <p className={styles.checkWhat}>{provenance}</p>}
      {/*
        The diagnostics that landed on the document itself. `anchor()` sends a
        diagnostic here when neither its own pointer nor any ancestor of it is
        on the page — a pack with no `specVersion` is refused at
        `/specVersion`, and nothing renders that member — and the strip is the
        block whose pointer is the empty string.
      */}
      {rootAnchored.length > 0 && (
        <ul className={styles.rootDiagnostics}>
          {rootAnchored.map((entry, index) => (
            <li key={`${entry.diagnostic.code}-${index}`}>
              <code>{entry.diagnostic.code}</code> {entry.diagnostic.message}{' '}
              <code>{entry.named === '' ? 'the document' : entry.named}</code>
            </li>
          ))}
        </ul>
      )}
      {digestsDisagree && (
        <p className={styles.warning} role="status">
          The runtime served bytes with a different digest from the file on disk. These are two
          answers about one file, and they do not describe one revision.
        </p>
      )}
      {disagreement.length > 0 && (
        <p className={styles.warning} role="status">
          The desk will not edit around this file: {disagreement[0]!.reason} at{' '}
          <code>{disagreement[0]!.pointer === '' ? 'the document' : disagreement[0]!.pointer}</code>.
        </p>
      )}
      {children}
    </div>
  )
}
