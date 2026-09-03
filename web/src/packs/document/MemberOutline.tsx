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
import { Link, useLocation } from 'react-router-dom'
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
  // The current search, carried through. A `to` object naming only a hash
  // clears the query, and the two ways of choosing what to inspect — an
  // outline entry and a block — would then produce different addresses for the
  // same choice, one of them missing the token the URL was opened with.
  const { search } = useLocation()
  return (
    <nav className={styles.outline} aria-label="Members">
      <ul className={styles.outlineList}>
        {entries.map((entry) => (
          <li key={entry.id} className={styles.outlineItem}>
            {entry.present ? (
              <Link
                className={styles.outlineLink}
                to={{ search, hash: `#${entry.pointer}` }}
                // Choosing what to inspect is not a navigation, and the block
                // beside it replaces. Two paths to one act, one history entry.
                replace
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
