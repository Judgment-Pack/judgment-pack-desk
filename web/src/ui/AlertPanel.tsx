/**
 * A block-level announcement: a heading, what happened, what can be done, and
 * the detail behind a disclosure.
 *
 * `Alert` beside this is a `<p>`, which is the right shape for one sentence
 * and the wrong one here: a `<details>` and two buttons inside a paragraph is
 * markup the browser re-parents, so the disclosure would land outside the
 * element that announces. They are siblings rather than one component with a
 * mode, and `Alert` is untouched.
 *
 * The disclosure is where the digests go. Both of them are facts the reader
 * needs and neither is the first thing to say: "this file changed and nothing
 * was written" is, and sixty-four hex characters ahead of it buries the
 * sentence that matters.
 */
import type { ReactNode } from 'react'
import styles from './AlertPanel.module.css'

export function AlertPanel({
  heading,
  children,
  detailLabel,
  detail,
  actions
}: {
  heading: string
  children: ReactNode
  /** The disclosure's summary, where there is one. */
  detailLabel?: string
  detail?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div role="alert" className={styles.panel}>
      <p className={styles.heading}>{heading}</p>
      <div className={styles.body}>{children}</div>
      {detail !== undefined && (
        <details className={styles.details}>
          <summary className={styles.summary}>{detailLabel ?? 'details'}</summary>
          <div className={styles.detail}>{detail}</div>
        </details>
      )}
      {actions !== undefined && <div className={styles.actions}>{actions}</div>}
    </div>
  )
}
