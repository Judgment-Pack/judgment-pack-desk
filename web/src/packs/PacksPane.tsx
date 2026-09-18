import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
/** The full-width collection. The parent retains it while a pack is open. */
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ROW_HEIGHT } from '../config/theme'
import { usePacks } from '../mcp/queries'
import { useAppearance } from '../shell/appearanceState'
import { useInspectorPortal, useInspectorSlot } from '../shell/InspectorSlot'
import { useShellState } from '../shell/paneState'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { SortMenu } from '../ui/SortMenu'
import { IconPreview } from '../shell/icons'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { PacksNavigation } from './PacksNavigation'
import { PageHeader } from '../ui/PageLayout'
import { Tooltip, OverflowTooltip } from '../ui/Tooltip'
import { PackPreview, PackPreviewHint } from './PackPreview'
import styles from './PacksPane.module.css'
import { moveFocus, useWindowedRows } from './useWindowedRows'

function isSpelled(value: string | undefined): value is string {
  return typeof value === 'string' && value.trim() !== ''
}
const SORTS = [
  { value: 'id-asc', get label() { return msg("Pack ID: A–Z") } },
  { value: 'id-desc', get label() { return msg("Pack ID: Z–A") } }
]

export function PacksPane({ active = true }: { active?: boolean }) {
  const locale = useLocale()
  const { data, error, isPending, isSuccess, isFetching, refetch } = usePacks()
  const total = data?.packs?.length ?? 0
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState('id-asc')
  const [previewId, setPreviewId] = useState<string | null>(null)
  const list = useRef<HTMLDivElement | null>(null)
  const scrollPosition = useRef(0)
  const { density } = useAppearance()
  const rowHeight = ROW_HEIGHT[density ?? 'comfortable']
  const slot = useInspectorSlot()
  const shell = useShellState()
  const [previewOpen, setPreviewOpen] = useState(false)
  const [previewWidth, setPreviewWidth] = useState(360)
  const resetPreviewWidth = useCallback(() => setPreviewWidth(360), [])
  const previewOwner = useRef<string | undefined>(undefined)
  useLayoutEffect(() => {
    if (!shell.keyResolved) return
    if (previewOwner.current !== undefined && previewOwner.current !== shell.storageKey) {
      setPreviewOpen(false); setPreviewId(null); setPreviewWidth(360)
    }
    previewOwner.current = shell.storageKey
  }, [shell.storageKey, shell.keyResolved])
  const presentation = useMemo(() => active ? {
    title: msg("Pack preview"), open: previewOpen, onOpenChange: setPreviewOpen,
    width: previewWidth, onResize: setPreviewWidth, onReset: resetPreviewWidth,
    minimumMainWidth: 720, maximumWidth: 420, closeOnEscape: true
  } : null, [active, previewOpen, previewWidth, resetPreviewWidth, locale])
  useInspectorPresentation(presentation)
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
    ? preview ? <PackPreview pack={preview} /> : <PackPreviewHint />
    : null)
  useEffect(() => {
    if (previewId && !preview && !isPending) { setPreviewOpen(false); setPreviewId(null) }
  }, [previewId, preview, isPending])
  const showPreview = (id: string) => {
    setPreviewId(id)
    setPreviewOpen(true)
    slot.reveal()
  }

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
    if (row instanceof HTMLElement) { row.focus({ preventScroll: true }); if (previewOpen) slot.reveal() }
    setWanted(undefined)
  }, [wanted, window.start, window.end, previewOpen, slot.reveal])
  const resetScroll = () => {
    scrollPosition.current = 0
    if (list.current) { list.current.scrollTop = 0; list.current.dispatchEvent(new Event('scroll')) }
  }
  const clearFilter = () => { resetScroll(); setFilter('') }

  return <article className={styles.pane} data-layout={active ? 'page' : undefined} aria-label={msg("Pack collection")}>
    <PageHeader title={msg("Packs")} variant="collection"
      navigation={<PacksNavigation current="packs" count={isSuccess ? total : undefined} />}
      actions={<ButtonLink to="/create-pack" variant="primary">{msg("Create pack")}</ButtonLink>} />
    <div className={styles.controls} role="group" aria-label={msg("Pack list controls")}>
      <Input className={styles.search} type="search" aria-label={msg("Search packs")} value={filter}
        placeholder={msg("Search packs…")} onChange={event => { resetScroll(); setFilter(event.target.value) }} />
      {filter && <Button variant="quiet" onClick={clearFilter}>{msg("Clear")}</Button>}
      {filter && isSuccess && <span className={styles.matchCount} role="status"><Message text={"<0/> of <1/>"} slots={[packs.length, total]} /></span>}
      <div className={styles.sort}>
        <SortMenu label={msg("Sort packs")} value={sort} onValueChange={value => { resetScroll(); setSort(value) }} options={SORTS} />
      </div>
    </div>
    {error ? <section className={styles.empty} role="alert">
      <h2>{msg("Couldn’t load packs")}</h2><p>{error.message}</p>
      <Button onClick={() => { void refetch() }} disabled={isFetching}>{isFetching ? msg("Retrying…") : msg("Retry")}</Button>
    </section> : isPending ? <p className={styles.message} role="status">{msg("Loading packs…")}</p>
    : packs.length === 0 ? <section className={styles.empty} role="status">
      <h2>{total ? msg("No matching packs") : msg("No packs yet")}</h2>
      <p>{total ? msg("Try another pack ID or description.") : msg("Create a pack to define a decision and its rules.")}</p>
      {total ? <Button onClick={clearFilter}>{msg("Clear search")}</Button> : null}
    </section> : <nav className={styles.collection} aria-label={msg("Packs")}>
      <div className={styles.columns} aria-hidden="true"><span>{msg("Pack")}</span><span className={styles.descriptionColumn}>{msg("Description")}</span><span>{msg("Version")}</span><span /></div>
      <div className={styles.list} data-pack-list ref={node => { list.current = node; window.ref(node) }}
        onScroll={event => { if (active) scrollPosition.current = event.currentTarget.scrollTop }}>
        <div style={{ height: window.padTop }} aria-hidden="true" />
        <ul className={styles.rows} onKeyDown={event => {
          const focused = document.activeElement as HTMLElement | null
          const pointer = focused?.getAttribute('data-row')
          if (pointer == null) return
          const current = Number(pointer)
          if (!Number.isInteger(current)) return
          if (event.key === ' ') {
            event.preventDefault()
            if (event.repeat) return
            if (previewOpen && previewId === packs[current]?.id) { setPreviewOpen(false); slot.close?.() }
            else if (packs[current]) showPreview(packs[current].id)
            return
          }
          const next = moveFocus(event, packs.length, current)
          if (next === undefined) return
          const preview = focused?.hasAttribute('data-preview') ?? false
          window.scrollRowIntoView(next)
          const selector = `[data-row="${next}"]${preview ? '[data-preview]' : ':not([data-preview])'}`
          const already = list.current?.querySelector(selector)
          if (previewOpen && packs[next]) setPreviewId(packs[next].id)
          if (already instanceof HTMLElement) { already.focus({ preventScroll: true }); if (previewOpen) slot.reveal() }
          else setWanted({ index: next, preview })
        }}>
          {packs.slice(window.start, window.end).map((pack, offset) => <li key={pack.id}
            className={styles.row} data-selected={previewOpen && preview?.id === pack.id || undefined}>
            <OverflowTooltip selector="[data-overflow-text]" fallback={msg("Preview this pack to read the full description.")}><Link className={styles.link} data-row={window.start + offset} to={`/packs/${encodeURIComponent(pack.id)}`}
              onClick={() => { if (list.current) scrollPosition.current = list.current.scrollTop }}>
              <span className={styles.name}>
                <span className={styles.nameText} data-overflow-text>{pack.id}</span>
                {isSpelled(pack.detail) && <span className={styles.issue} role="img" aria-label={pack.detail}>!</span>}
              </span>
              <span className={isSpelled(pack.detail) ? styles.rowDetail : styles.description} data-overflow-text>
                {isSpelled(pack.detail) ? pack.detail : isSpelled(pack.description) ? pack.description : ''}
              </span>
              <span className={styles.version} data-overflow-text>{isSpelled(pack.packVersion) ? msg("v{{value0}}", { value0: pack.packVersion }) : '—'}</span>
            </Link></OverflowTooltip>
            <Tooltip content={msg("Preview pack · Space")}><Button variant="quiet" className={styles.preview} data-preview data-row={window.start + offset}
              aria-label={msg("Preview {{value0}}", { value0: pack.id })} aria-pressed={previewOpen && preview?.id === pack.id}
              onClick={() => {
                if (previewOpen && previewId === pack.id) { setPreviewOpen(false); slot.close?.() }
                else showPreview(pack.id)
              }}><IconPreview /></Button></Tooltip>
          </li>)}
        </ul>
        <div style={{ height: window.padBottom }} aria-hidden="true" />
      </div>
    </nav>}
    {portal}
  </article>
}
