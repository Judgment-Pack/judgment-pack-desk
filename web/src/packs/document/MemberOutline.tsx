/**
 * The document's own outline, on one line under the standfirst.
 *
 * Identity, Decision, Outcomes 2, Applicability (not declared), and so on.
 *
 * Every entry is a link to its member's block, except the ones the document
 * does not declare: those carry "not declared" and no link, because a link to
 * a block that is not there is a link that does nothing.
 *
 * It is also the scroll-spy's readout — one line rather than a second column,
 * because the desk already has a rail and a pane, and a third fixed column
 * would leave the document less room than the frame it sits in.
 */
import { Link } from 'react-router-dom'
import styles from './PackDocument.module.css'

export interface OutlineEntry {
  id: string
  label: string
  pointer: string
  present: boolean
  count?: number
}

export function MemberOutline({
  entries,
  active
}: {
  entries: readonly OutlineEntry[]
  active: string | null
}) {
  return (
    <nav className={styles.outline} aria-label="Members">
      <ul className={styles.outlineList}>
        {entries.map((entry) => (
          <li key={entry.id} className={styles.outlineItem}>
            {entry.present ? (
              <Link
                className={styles.outlineLink}
                to={{ hash: `#${entry.pointer}` }}
                aria-current={active === entry.pointer ? 'true' : undefined}
              >
                {entry.label}
                {entry.count !== undefined && (
                  <span className={styles.outlineCount}> {entry.count}</span>
                )}
              </Link>
            ) : (
              <span className={styles.outlineAbsent}>{entry.label} — not declared</span>
            )}
          </li>
        ))}
      </ul>
    </nav>
  )
}
