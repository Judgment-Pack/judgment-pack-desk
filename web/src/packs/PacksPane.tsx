/** The full-width collection. The parent retains it while a pack is open. */
import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { VisuallyHidden } from 'radix-ui'
import { ROW_HEIGHT } from '../config/theme'
import { usePacks } from '../mcp/queries'
import { useAppearance } from '../shell/appearanceState'
import { useInspectorPortal, useInspectorSlot } from '../shell/InspectorSlot'
import { useShellState } from '../shell/paneState'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { PageHeader } from '../ui/PageLayout'
import { PackPreview, PackPreviewHint } from './PackPreview'
import styles from './PacksPane.module.css'
import { moveFocus, useWindowedRows } from './useWindowedRows'

function isSpelled(value: string | undefined): value is string {
  return typeof value === 'string' && value.trim() !== ''
}
const SORTS = [
  { value: 'id-asc', label: 'Pack ID: A–Z' },
  { value: 'id-desc', label: 'Pack ID: Z–A' }
]

export function PacksPane({ active = true }: { active?: boolean }) {
  const { data, error, isPending, isSuccess, isFetching, refetch } = usePacks()
  const total = data?.packs?.length ?? 0
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState('id-asc')
  const [previewId, setPreviewId] = useState<string | null>(null)
  const list = useRef<HTMLDivElement | null>(null)
  const scrollPosition = useRef(0)
  const sortId = useId()
  const { density } = useAppearance()
  const rowHeight = ROW_HEIGHT[density ?? 'comfortable']
  const slot = useInspectorSlot()
  const shell = useShellState()
  const packs = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const matching = (data?.packs ?? []).filter(pack => !needle ||
      pack.id.toLowerCase().includes(needle) || pack.description?.toLowerCase().includes(needle))
    return matching.sort((left, right) => sort === 'id-desc'
      ? right.id.localeCompare(left.id) : left.id.localeCompare(right.id))
  }, [data, filter, sort])
  const window = useWindowedRows(packs.length, rowHeight)
  const preview = isSuccess ? packs.find(pack => pack.id === previewId) : undefined
  const portal = useInspectorPortal(active
    ? preview ? <PackPreview pack={preview} onOpen={() => {
      // Opening the document from a modal preview must reveal the main page.
      if (slot.open && slot.target?.closest('[role="dialog"]')) shell.toggleInspector()
    }} /> : <PackPreviewHint />
    : null)

  // A hidden collection must not replace the selected pack's Inspector or
  // overwrite its own saved scroll position with a zero-height measurement.
  useLayoutEffect(() => {
    if (active && list.current) {
      list.current.scrollTop = scrollPosition.current
      list.current.dispatchEvent(new Event('scroll'))
    }
  }, [active])

  const [wanted, setWanted] = useState<{ index: number; preview: boolean } | undefined>(undefined)
  useEffect(() => {
    if (!wanted) return
    const row = list.current?.querySelector(`[data-row="${wanted.index}"]${wanted.preview ? '[data-preview]' : ':not([data-preview])'}`)
    if (row instanceof HTMLElement) row.focus({ preventScroll: true })
    setWanted(undefined)
  }, [wanted, window.start, window.end])
  const resetScroll = () => {
    scrollPosition.current = 0
    if (list.current) { list.current.scrollTop = 0; list.current.dispatchEvent(new Event('scroll')) }
  }
  const clearFilter = () => { resetScroll(); setFilter('') }

  return <article className={styles.pane} data-layout={active ? 'page' : undefined} aria-label="Pack collection">
    <PageHeader title="Packs" meta={isSuccess ? packs.length === total
      ? total : `${packs.length} of ${total}` : undefined}
      actions={<ButtonLink to="/create-pack" variant="primary">Create pack</ButtonLink>} />
    <div className={styles.controls} role="group" aria-label="Pack list controls">
      <Input className={styles.search} type="search" aria-label="Search packs" value={filter}
        placeholder="Search packs…" onChange={event => { resetScroll(); setFilter(event.target.value) }} />
      {filter && <Button variant="quiet" onClick={clearFilter}>Clear</Button>}
      <div className={styles.sort}>
        <VisuallyHidden.Root asChild><label htmlFor={sortId}>Sort packs</label></VisuallyHidden.Root>
        <Select id={sortId} value={sort} onValueChange={value => { resetScroll(); setSort(value) }} options={SORTS} />
      </div>
    </div>
    {error ? <section className={styles.empty} role="alert">
      <h2>Couldn’t load packs</h2><p>{error.message}</p>
      <Button onClick={() => { void refetch() }} disabled={isFetching}>{isFetching ? 'Retrying…' : 'Retry'}</Button>
    </section> : isPending ? <p className={styles.message} role="status">Loading packs…</p>
    : packs.length === 0 ? <section className={styles.empty} role="status">
      <h2>{total ? 'No matching packs' : 'No packs yet'}</h2>
      <p>{total ? 'Try another pack ID or description.' : 'Create a pack to define a decision and its rules.'}</p>
      {total ? <Button onClick={clearFilter}>Clear search</Button> : null}
    </section> : <nav className={styles.collection} aria-label="Packs">
      <div className={styles.columns} aria-hidden="true"><span>Pack</span><span>Version</span><span /></div>
      <div className={styles.list} data-pack-list ref={node => { list.current = node; window.ref(node) }}
        onScroll={event => { if (active) scrollPosition.current = event.currentTarget.scrollTop }}>
        <div style={{ height: window.padTop }} aria-hidden="true" />
        <ul className={styles.rows} onKeyDown={event => {
          const focused = document.activeElement as HTMLElement | null
          const pointer = focused?.getAttribute('data-row')
          if (pointer == null) return
          const current = Number(pointer)
          if (!Number.isInteger(current)) return
          const next = moveFocus(event, packs.length, current)
          if (next === undefined) return
          const preview = focused?.hasAttribute('data-preview') ?? false
          window.scrollRowIntoView(next)
          const selector = `[data-row="${next}"]${preview ? '[data-preview]' : ':not([data-preview])'}`
          const already = list.current?.querySelector(selector)
          if (already instanceof HTMLElement) already.focus({ preventScroll: true })
          else setWanted({ index: next, preview })
        }}>
          {packs.slice(window.start, window.end).map((pack, offset) => <li key={pack.id}
            className={styles.row} data-selected={slot.open && preview?.id === pack.id || undefined}>
            <Link className={styles.link} data-row={window.start + offset} to={`/packs/${encodeURIComponent(pack.id)}`}
              onClick={() => { if (list.current) scrollPosition.current = list.current.scrollTop }}>
              <span className={styles.identity}><span className={styles.name} title={pack.id}>{pack.id}</span>
                {isSpelled(pack.detail) ? <span className={styles.rowDetail} title={pack.detail}>{pack.detail}</span>
                  : isSpelled(pack.description) && <span className={styles.description} title={pack.description}>{pack.description}</span>}
              </span>
              <span className={styles.version}>{isSpelled(pack.packVersion) ? `v${pack.packVersion}` : '—'}</span>
            </Link>
            <Button variant="quiet" className={styles.preview} data-preview data-row={window.start + offset}
              aria-label={`Preview ${pack.id}`} aria-pressed={slot.open && preview?.id === pack.id}
              onClick={() => { setPreviewId(pack.id); slot.reveal() }}>Preview</Button>
          </li>)}
        </ul>
        <div style={{ height: window.padBottom }} aria-hidden="true" />
      </div>
    </nav>}
    {portal}
  </article>
}
