/**
 * The project's packs, in main's left pane.
 *
 * They used to be in the rail. A project can carry hundreds of packs and a
 * rail cannot: the old list capped at thirty and handed the rest to the
 * project home, which is a list that stops being a list at the point it starts
 * being useful. So the rail keeps one **destination** with a count, and the
 * list lives here — filtered, sorted, windowed, and beside the document rather
 * than instead of it.
 *
 * **A refused listing shows the failure, not an empty project.** "This project
 * declares no packs" and "the listing did not answer" are two different
 * statements and only one of them is about the project. That rule moved here
 * from the rail with the list.
 *
 * Sorting offers name ascending and descending and nothing else, because
 * nothing else is a fact the desk has: `list_packs` reports no date, no size
 * and no order of its own, and a "recently changed" sort would be the desk
 * inventing an ordering out of a file listing that answers a different
 * question.
 *
 * It is a `<nav>` with its own name. The pane is a list of navigations, so
 * that is the correct markup — and it means a route adds a landmark inside
 * main, which the README's region table now says.
 */
import { useMemo, useRef, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { usePacks } from '../mcp/queries'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import styles from './PacksPane.module.css'
import { moveFocus, useWindowedRows } from './useWindowedRows'

/** One row's height, in pixels, and the number the window arithmetic uses. */
const ROW_HEIGHT = 40

/** How many rows are shown before "Show all N". */
const FIRST_SCREENFUL = 20

const SORTS = [
  { value: 'name-asc', label: 'Name A–Z' },
  { value: 'name-desc', label: 'Name Z–A' }
]

export function PacksPane() {
  const { data, error, isPending } = usePacks()
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState('name-asc')
  const [expanded, setExpanded] = useState(false)
  const list = useRef<HTMLDivElement | null>(null)

  const packs = useMemo(() => {
    const all = data?.packs ?? []
    const needle = filter.trim().toLowerCase()
    const matching =
      needle === '' ? [...all] : all.filter((pack) => pack.id.toLowerCase().includes(needle))
    matching.sort((left, right) =>
      sort === 'name-desc' ? right.id.localeCompare(left.id) : left.id.localeCompare(right.id)
    )
    return matching
  }, [data, filter, sort])

  const shown = expanded ? packs : packs.slice(0, FIRST_SCREENFUL)
  const window = useWindowedRows(list, shown.length, ROW_HEIGHT)

  return (
    <nav className={styles.pane} aria-label="Packs">
      <div className={styles.controls}>
        <Field label="Filter">
          {(wiring) => (
            <Input
              {...wiring}
              type="search"
              value={filter}
              placeholder="pack id"
              onChange={(event) => setFilter(event.target.value)}
            />
          )}
        </Field>
        <Field label="Sort">
          {(wiring) => (
            <Select id={wiring.id} value={sort} onValueChange={setSort} options={SORTS} />
          )}
        </Field>
      </div>

      {error ? (
        <p className={styles.failure}>The pack listing did not answer — {error.message}</p>
      ) : isPending ? (
        <p className={styles.quiet}>Reading the project&rsquo;s packs&hellip;</p>
      ) : packs.length === 0 ? (
        <p className={styles.quiet}>
          {(data?.packs ?? []).length === 0
            ? 'This project declares no packs.'
            : 'No pack id contains that.'}
        </p>
      ) : (
        <div className={styles.list} ref={list}>
          <div style={{ height: window.padTop }} aria-hidden="true" />
          <ul
            className={styles.rows}
            onKeyDown={(event) => {
              const rows = [...(list.current?.querySelectorAll('a') ?? [])] as HTMLElement[]
              const current = rows.indexOf(document.activeElement as HTMLElement)
              if (current < 0) return
              const next = moveFocus(event, rows, current)
              if (next !== undefined) rows[next]?.focus()
            }}
          >
            {shown.slice(window.start, window.end).map((pack) => (
              <li key={pack.id} className={styles.row}>
                <NavLink className={styles.link} to={`/packs/${encodeURIComponent(pack.id)}`}>
                  <span className={styles.name}>{pack.id}</span>
                  {pack.packVersion !== undefined && (
                    <span className={styles.version}>v{pack.packVersion}</span>
                  )}
                </NavLink>
              </li>
            ))}
          </ul>
          <div style={{ height: window.padBottom }} aria-hidden="true" />
        </div>
      )}

      {!expanded && packs.length > FIRST_SCREENFUL && (
        <button type="button" className={styles.more} onClick={() => setExpanded(true)}>
          Show all {packs.length}
        </button>
      )}
    </nav>
  )
}
