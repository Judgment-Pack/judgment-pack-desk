/** Compact, portaled table of contents. Absent members remain reachable;
 * choosing a link preserves the route query and replaces its fragment. */
import { Link, useLocation } from 'react-router-dom'
import { Button } from '../../ui/Button'
import { Popover, PopoverClose } from '../../ui/Popover'
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
  const current = entries.find(entry => entry.pointer === active)?.label
  return (
    <div className={styles.outline}>
      <Popover title="On this page" trigger={<Button variant="inline">On this page{current ? ` · ${current}` : ''} ▾</Button>}>
      <nav aria-label="Members">
      <ul className={styles.outlineList}>
        {entries.map((entry) => (
          <li key={entry.id} className={styles.outlineItem}>
            {/*
              **An omission is a place too.** Every entry is a link, present or
              not, because the document renders an addressed block for an
              omitted member — that is what `OmittedMember` is for — and an
              outline entry that could not reach it was the only line in this
              nav that named something you could not go to. "not declared" is
              kept: the link goes to the statement of absence, and the entry
              still says which it is.
            */}
            <PopoverClose><Link
              className={entry.present ? styles.outlineLink : styles.outlineAbsentLink}
              to={{ search, hash: `#${entry.pointer}` }}
              // Choosing what to inspect is not a navigation, and the block
              // beside it replaces. Two paths to one act, one history entry.
              replace
              aria-current={active === entry.pointer ? 'true' : undefined}
            >
              {entry.label}
              {entry.present ? (
                entry.count !== undefined && (
                  <span className={styles.outlineCount}> {entry.count}</span>
                )
              ) : (
                <span className={styles.outlineAbsent}> — not declared</span>
              )}
            </Link></PopoverClose>
          </li>
        ))}
      </ul>
      </nav>
      </Popover>
    </div>
  )
}
