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
import { useEffect, useMemo, useRef, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { usePacks } from '../mcp/queries'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import styles from './PacksPane.module.css'
import { moveFocus, useWindowedRows } from './useWindowedRows'

/** A member the listing actually answered with, rather than left empty. */
function isSpelled(value: string | undefined): value is string {
  return typeof value === 'string' && value !== ''
}

/** One row's height, in pixels, and the number the window arithmetic uses. */
const ROW_HEIGHT = 40

/** How many rows are shown before "Show all N". */
const FIRST_SCREENFUL = 20

const SORTS = [
  { value: 'name-asc', label: 'Name A–Z' },
  { value: 'name-desc', label: 'Name Z–A' }
]

export function PacksPane() {
  const { data, error, isPending, isSuccess } = usePacks()
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
  const window = useWindowedRows(shown.length, ROW_HEIGHT)

  /**
   * A row the keyboard asked for that was not on screen yet.
   *
   * Focus cannot be given to an element that is not rendered, so a key that
   * reaches past the window scrolls first and the row is focused in the render
   * that brings it in. State rather than a frame callback, because the render
   * is what this is waiting for and React is the thing that knows when it
   * happened.
   */
  const [wanted, setWanted] = useState<number | undefined>(undefined)
  useEffect(() => {
    if (wanted === undefined) return
    const row = list.current?.querySelector(`[data-row="${wanted}"]`)
    if (row instanceof HTMLElement) row.focus()
    setWanted(undefined)
  }, [wanted, window.start, window.end])

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
        <div
          className={styles.list}
          ref={(node) => {
            list.current = node
            window.ref(node)
          }}
        >
          <div style={{ height: window.padTop }} aria-hidden="true" />
          <ul
            className={styles.rows}
            onKeyDown={(event) => {
              // **Absolute indices, over the whole list.** The rendered
              // anchors are the window, and navigating by them clamped every
              // key to it: End reached the last *rendered* row and ArrowDown
              // from there prevented the default and moved nothing, so the
              // keyboard could not leave the first screenful of a list a
              // pointer scrolls freely.
              const focused = document.activeElement as HTMLElement | null
              const pointer = focused?.getAttribute('data-row')
              if (pointer === null || pointer === undefined) return
              const current = Number(pointer)
              if (!Number.isInteger(current)) return
              const next = moveFocus(event, shown.length, current)
              if (next === undefined) return
              // Off-window destinations are scrolled to first: a row that is
              // not rendered cannot be focused, and the window follows the
              // scroll rather than the other way round.
              window.scrollRowIntoView(next)
              const already = list.current?.querySelector(`[data-row="${next}"]`)
              if (already instanceof HTMLElement) already.focus()
              // Not rendered yet: the scroll above moves the window, and the
              // row is focused in the render that brings it in.
              else setWanted(next)
            }}
          >
            {shown.slice(window.start, window.end).map((pack, offset) => (
              <li key={pack.id} className={styles.row}>
                <NavLink
                  className={styles.link}
                  // The row's own index in the whole list, so the keyboard can
                  // ask for a row that is not on screen.
                  data-row={window.start + offset}
                  to={`/packs/${encodeURIComponent(pack.id)}`}
                >
                  <span className={styles.name}>{pack.id}</span>
                  {/* An **empty** version is not a version. `list_packs` lists
                      a pack whose document it could not read with `packId` and
                      `packVersion` as empty strings and the reason in
                      `detail`, and a bare "v" beside the name asserted a member
                      of a document nothing could read. */}
                  {isSpelled(pack.packVersion) ? (
                    <span className={styles.version}>v{pack.packVersion}</span>
                  ) : (
                    /* The runtime's own sentence, where it sent one. Quoted and
                       not summarised: what the desk knows about this pack is
                       exactly what the listing said about it. On one line, so
                       every row is still the height the window arithmetic
                       assumes. */
                    isSpelled(pack.detail) && (
                      <span className={styles.rowDetail} title={pack.detail}>
                        {pack.detail}
                      </span>
                    )
                  )}
                </NavLink>
              </li>
            ))}
          </ul>
          <div style={{ height: window.padBottom }} aria-hidden="true" />
        </div>
      )}

      {/*
        **Gated on a listing that succeeded just now.** react-query keeps the
        last good data through a refetch error, so a failed refresh left this
        button under the failure sentence offering to show all N of a listing
        the pane had just said it could not read.
      */}
      {isSuccess && !expanded && packs.length > FIRST_SCREENFUL && (
        <button type="button" className={styles.more} onClick={() => setExpanded(true)}>
          Show all {packs.length}
        </button>
      )}
    </nav>
  )
}
