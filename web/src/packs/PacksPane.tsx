import { graphDraftHref } from '../graphs/drafts'
import { SegmentedControl } from '../ui/SegmentedControl'
import { useSearchParams } from 'react-router-dom'
import { VisuallyHidden } from 'radix-ui'
import { useChats } from '../chat/ChatProvider'
import { draftHref } from './drafts/model'
import { Select } from '../ui/Select'
import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
/** The full-width collection. The parent retains it while a pack is open. */
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ROW_HEIGHT } from '../config/theme'
import { usePacks, useGraphInventory } from '../mcp/queries'
import { useMcp } from '../mcp/McpProvider'
import { useAppearance } from '../shell/appearanceState'
import { useInspectorPortal, useInspectorControls } from '../shell/InspectorSlot'
import { useShellState } from '../shell/paneState'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { SortMenu } from '../ui/SortMenu'
import { IconPreview, IconPack, IconGraph } from '../shell/icons'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { PacksNavigation } from './PacksNavigation'
import { PageHeader } from '../ui/PageLayout'
import { Tooltip, OverflowTooltip } from '../ui/Tooltip'
import { PackPreview, PackPreviewHint } from './PackPreview'
import styles from './PacksPane.module.css'
import { moveFocus, useWindowedRows } from './useWindowedRows'
import { usePackFolders } from './folders/FolderContext'
import { ALL_PACKS, HOME_FOLDER, inFolder, packFolder, folderPath } from './folders/model'
import { FolderLocation, FolderFeedback, SubfolderRows, ShowFolders } from './folders/FolderBrowser'
import { findingWords, packFindings } from './review/findings'
import { useReview } from './review/ReviewContext'
import { UpgradeNote } from './upgrade/UpgradeNote'
import { CollectionHeaderActions, SelectionActions, ItemOptions, DeleteDraftsDialog, type BrowserItem } from './CollectionActions'

function draftDescription(value: unknown): string | undefined { const text=(value as {description?:unknown}|null)?.description;return typeof text==='string'?text:undefined }
function isSpelled(value: string | undefined): value is string {
  return typeof value === 'string' && value.trim() !== ''
}
const SORTS = [
  { value: 'id-asc', get label() { return msg("Name: A–Z") } },
  { value: 'id-desc', get label() { return msg("Name: Z–A") } }
]

export function PacksPane({ active = true }: { active?: boolean }) {
  const locale = useLocale()
  const { data, error, isPending, isSuccess, isFetching, refetch } = usePacks()
  const graphs = useGraphInventory()
  const {graphInventorySupported, status: connectionStatus} = useMcp()
  const graphsUnavailable = connectionStatus === 'ready' && !graphInventorySupported
  const [search, setSearch] = useSearchParams()
  const kind = ['pack', 'graph'].includes(search.get('type') ?? '') ? search.get('type')! : 'all'
  const includePacks = kind !== 'graph', includeGraphs = kind !== 'pack'
  const listedGraphs = graphInventorySupported && !graphs.error ? graphs.data : undefined
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [deleting,setDeleting] = useState<{items:BrowserItem[];opener?:HTMLElement}>()
  const selectAll = useRef<HTMLInputElement>(null)
  const {chats,packDrafts,ready:draftsReady,error:draftError,store}=useChats()
  const drafts = packDrafts.filter(item=>!item.finalized)
  const graphDrafts = chats.flatMap(chat => (chat.graphDrafts ?? []).filter(draft => !draft.saved).map(draft => ({chatId: chat.id, ...draft})))
  const total = (includePacks ? (data?.packs?.length ?? 0) + drafts.length : 0) + (includeGraphs ? (listedGraphs?.graphs?.length ?? 0) + graphDrafts.length : 0)
  const inventoryReady = (!includePacks || isSuccess) && (!includeGraphs || graphInventorySupported && graphs.isSuccess && !graphs.isFetching)
  const folders = usePackFolders()
  const review = useReview()
  const findings = useMemo(() => packFindings(review?.data), [review?.data])
  const folderScope = folders?.query.data && !folders.query.isError ? folders.selected : ALL_PACKS
  const createHref = folders ? `/packs/new?folder=${encodeURIComponent(folders.selected === ALL_PACKS ? HOME_FOLDER : folders.selected)}` : '/packs/new'
  const [filter, setFilter] = useState('')
  const [status,setStatus]=useState('all')
  const [sort, setSort] = useState('id-asc')
  const [previewId, setPreviewId] = useState<string | null>(null)
  const list = useRef<HTMLDivElement | null>(null)
  const scrollPosition = useRef(0)
  const { density } = useAppearance()
  const rowHeight = ROW_HEIGHT[density ?? 'comfortable']
  const slot = useInspectorControls()
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
    title: msg("Item preview"), open: previewOpen, onOpenChange: setPreviewOpen,
    width: previewWidth, onResize: setPreviewWidth, onReset: resetPreviewWidth,
    minimumMainWidth: 560, maximumWidth: 420, closeOnEscape: true
  } : null, [active, previewOpen, previewWidth, resetPreviewWidth, locale])
  useInspectorPresentation(presentation)
  const packs = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const rows = [...(error ? [] : data?.packs ?? []).map(pack=>({...pack,kind:'pack',title:pack.id,status:'finalized',href:`/packs/${encodeURIComponent(pack.id)}`})),...drafts.map(draft=>({kind:'pack',id:draft.id,title:draft.title,status:'draft',href:draftHref(draft.id),description:draftDescription(draft.checkpoint.state.candidates.at(-1)?.document),detail:undefined,packVersion:undefined})), ...(listedGraphs?.graphs ?? []).map(graph => ({kind:'graph', id:'graph:'+graph.id, title:graph.id, status:'finalized', href:`/graphs/${encodeURIComponent(graph.id)}`, description:graph.description, detail:graph.detail, packVersion:graph.graphVersion})), ...graphDrafts.map(draft => ({kind:'graph', id:'graph-draft:'+draft.draftId, title:draft.id, status:'draft', chatId:draft.chatId, draftId:draft.draftId, href:graphDraftHref(draft.chatId,draft.draftId), description:draft.description, detail:undefined,packVersion:undefined}))] as BrowserItem[]
    const matching = rows.filter(pack => (kind==='all'||pack.kind===kind) && (status==='all'||pack.status===status) && (!folders || folderScope === ALL_PACKS || inFolder(folders.document, packFolder(folders.document,pack.id), folderScope, !!needle)) && (!needle ||
      `${pack.id} ${pack.title}`.toLowerCase().includes(needle) || pack.description?.toLowerCase().includes(needle)))
    return matching.sort((left, right) => sort === 'id-desc'
      ? right.title.localeCompare(left.title) : left.title.localeCompare(right.title))
  }, [data, error, packDrafts, chats, listedGraphs, kind, filter, status, sort, folders?.document, folderScope])
  const selected = packs.filter(item => selectedIds.includes(item.id))
  useEffect(() => {
    const visible = new Set(packs.map(item => item.id))
    setSelectedIds(before => before.every(id => visible.has(id)) ? before : before.filter(id => visible.has(id)))
  }, [packs])
  useEffect(() => {if(selectAll.current) selectAll.current.indeterminate = selected.length > 0 && selected.length < packs.length}, [selected.length,packs.length])
  const toggleSelection = (id:string) => setSelectedIds(before => before.includes(id) ? before.filter(value=>value!==id) : [...before,id])
  const requestDelete = (items:BrowserItem[],opener?:HTMLElement) => setDeleting({items,opener})
  const window = useWindowedRows(packs.length, rowHeight)
  const preview = packs.find(pack => pack.id === previewId)
  const portal = useInspectorPortal(active
    ? preview ? preview.status==='draft' || preview.kind==='graph' ? <section className={styles.empty}><h2>{preview.title}</h2><p>{preview.kind==='graph'?msg('Graph'):msg('Pack')} · {preview.status==='draft'?msg('Draft'):msg('Saved')}</p><p>{preview.description}</p><ButtonLink to={preview.href}>{preview.status==='draft'?msg('Open draft'):msg('Open graph')}</ButtonLink></section> : <PackPreview pack={data!.packs!.find(item=>item.id===preview.id)!} /> : <PackPreviewHint />
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

  const [wanted, setWanted] = useState<{ index: number; preview: boolean; selection?: boolean } | undefined>(undefined)
  useEffect(() => {
    if (!wanted) return
    const row = list.current?.querySelector(`[data-row="${wanted.index}"]${wanted.selection ? '[data-select]' : wanted.preview ? '[data-preview]' : ':not([data-preview]):not([data-select])'}`)
    if (row instanceof HTMLElement) { row.focus({ preventScroll: true }); if (previewOpen) slot.reveal() }
    setWanted(undefined)
  }, [wanted, window.start, window.end, previewOpen, slot.reveal])
  const resetScroll = () => {
    scrollPosition.current = 0
    if (list.current) { list.current.scrollTop = 0; list.current.dispatchEvent(new Event('scroll')) }
  }
  const clearFilter = () => { resetScroll(); setFilter(''); setStatus('all') }
  useEffect(() => { resetScroll(); setPreviewId(null); setPreviewOpen(false) }, [folderScope])

  return <article className={styles.pane} data-layout={active ? 'page' : undefined} aria-label={msg("Pack and graph collection")}>
    <PageHeader title={msg("Packs & graphs")} variant="collection"
      navigation={<PacksNavigation leading={<ShowFolders/>} count={inventoryReady ? total : undefined} />}
      actions={<CollectionHeaderActions createHref={createHref} review={Boolean(review)}/>} />
    <div className={styles.controls}><SegmentedControl label={msg('Item type')} value={kind} onValueChange={value => {resetScroll(); setSearch(before => {const next=new URLSearchParams(before); next.set('type', value); return next})}} segments={[{value:'all',label:msg('All')},{value:'pack',label:msg('Packs')},{value:'graph',label:msg('Graphs')}]}/></div>
    <FolderLocation/><FolderFeedback/>
    {review && <UpgradeNote/>}
    <div className={styles.controls} role="group" aria-label={msg("Pack and graph list controls")}>
      <Input className={styles.search} type="search" aria-label={msg("Search packs and graphs")} value={filter}
        placeholder={msg("Search packs and graphs…")} onChange={event => { resetScroll(); setFilter(event.target.value) }} />
      {(filter || status!=='all') && <Button variant="quiet" onClick={clearFilter}>{msg("Clear")}</Button>}
      {folders && folderScope !== ALL_PACKS && <Button variant="quiet" onClick={() => folders.select(ALL_PACKS)}>{msg("Search all packs and graphs")}</Button>}
      {(filter || status!=='all') && inventoryReady && <span className={styles.matchCount} role="status"><Message text={"<0/> of <1/>"} slots={[packs.length, total]} /></span>}
      <div className={styles.sort}>
        <VisuallyHidden.Root asChild><label htmlFor="pack-status">{msg('Status')}</label></VisuallyHidden.Root>
        <Select id="pack-status" quiet value={status} onValueChange={value=>{resetScroll();setStatus(value)}} options={[{value:'all',label:msg('All statuses')},{value:'draft',label:msg('Draft')},{value:'finalized',label:msg('Saved')}]}/>
        <SortMenu label={msg("Sort packs and graphs")} value={sort} onValueChange={value => { resetScroll(); setSort(value) }} options={SORTS} />
      </div>
    </div>
    <SubfolderRows query={filter}/>
    {includeGraphs && graphs.error && <div className={styles.message} role="alert">{msg("Could not list graphs")}: {graphs.error.message}<Button onClick={()=>void graphs.refetch()}>{msg("Retry")}</Button></div>}
    {includeGraphs && graphsUnavailable && <div className={styles.message} role="status">{msg('This runtime cannot list saved graphs. Connect a newer runtime to browse all packs and graphs.')} <ButtonLink to="/graphs">{msg('Open graphs')}</ButtonLink></div>}
    {draftError && <div className={styles.message} role="alert">{draftError}<Button onClick={()=>draftsReady?store?.retrySave():void store?.load()}>{msg('Retry')}</Button></div>}
    {includePacks && error ? <section className={styles.empty} role="alert">
      <h2>{msg("Couldn’t load packs")}</h2><p>{error.message}</p>
      <Button onClick={() => { void refetch() }} disabled={isFetching}>{isFetching ? msg("Retrying…") : msg("Retry")}</Button>
    </section> : includePacks && isPending ? <p className={styles.message} role="status">{msg("Loading packs…")}</p>
    : packs.length === 0 && includeGraphs && !graphsUnavailable && graphs.isPending ? <p className={styles.message} role="status">{msg('Loading packs and graphs…')}</p>
    : packs.length === 0 && includeGraphs && (!graphInventorySupported || graphs.error) ? null
    : packs.length === 0 ? <section className={styles.empty} role="status">
      <h2>{(filter || status!=='all') ? msg("No matching packs or graphs") : msg("No packs or graphs yet")}</h2>
      <p>{(filter || status!=='all') ? msg("Try another search or status.") : msg("Create a pack to define a decision and its rules.")}</p>
      {(filter || status!=='all') ? <Button onClick={clearFilter}>{msg("Reset filters")}</Button> : null}
    </section> : <nav className={styles.collection} aria-label={msg("Packs & graphs")} onKeyDown={event => {
      if(event.key==='Escape'&&selected.length){event.stopPropagation();event.preventDefault();setSelectedIds([])}
      if((event.ctrlKey||event.metaKey)&&event.key==='a'){event.preventDefault();setSelectedIds(packs.map(item=>item.id))}
    }}>
      <SelectionActions items={selected} clear={()=>setSelectedIds([])} onDelete={requestDelete}/>
      <div className={styles.columns}>
        <span className={styles.selection}><input ref={selectAll} data-select-all type="checkbox" aria-label={msg('Select all visible items')} checked={packs.length>0&&selected.length===packs.length} onChange={event=>setSelectedIds(event.target.checked?packs.map(item=>item.id):[])}/></span>
        <span aria-hidden="true">{msg("Name")}</span><span className={styles.descriptionColumn} aria-hidden="true">{msg("Description")}</span><span className={styles.versionColumn} aria-hidden="true">{msg("Version")}</span><span aria-hidden="true"/>
      </div>
      <div className={styles.list} data-pack-list ref={node => { list.current = node; window.ref(node) }}
        onScroll={event => { if (active) scrollPosition.current = event.currentTarget.scrollTop }}>
        <div style={{ height: window.padTop }} aria-hidden="true" />
        <ul className={styles.rows} onKeyDown={event => {
          const focused = document.activeElement as HTMLElement | null
          const pointer = focused?.getAttribute('data-row')
          if (pointer == null) return
          const current = Number(pointer)
          if (!Number.isInteger(current)) return
          if(event.key.toLowerCase()==='x'&&!event.ctrlKey&&!event.metaKey&&!event.altKey){event.preventDefault();if(!event.repeat&&packs[current])toggleSelection(packs[current].id);return}
          if (event.key === ' ' && !focused?.hasAttribute('data-select')) {
            event.preventDefault()
            if (event.repeat) return
            if (previewOpen && previewId === packs[current]?.id) { setPreviewOpen(false); slot.close?.() }
            else if (packs[current]) showPreview(packs[current].id)
            return
          }
          const next = moveFocus(event, packs.length, current)
          if (next === undefined) return
          const preview = focused?.hasAttribute('data-preview') ?? false
          const selection = focused?.hasAttribute('data-select') ?? false
          window.scrollRowIntoView(next)
          const selector = `[data-row="${next}"]${selection ? '[data-select]' : preview ? '[data-preview]' : ':not([data-preview]):not([data-select])'}`
          const already = list.current?.querySelector(selector)
          if (previewOpen && packs[next]) setPreviewId(packs[next].id)
          if (already instanceof HTMLElement) { already.focus({ preventScroll: true }); if (previewOpen) slot.reveal() }
          else setWanted({ index: next, preview, selection })
        }}>
          {packs.slice(window.start, window.end).map((pack, offset) => <li key={pack.id}
            className={styles.row} data-selected={selectedIds.includes(pack.id) || previewOpen && preview?.id === pack.id || undefined}>
            <span className={styles.selection}><input type="checkbox" data-select data-row={window.start+offset} aria-label={msg('Select {{name}}', {name:pack.title})} checked={selectedIds.includes(pack.id)} onChange={()=>toggleSelection(pack.id)}/></span>
            <OverflowTooltip selector="[data-overflow-text]" fallback={msg("Preview this item to read the full description.")}><Link className={styles.link} data-row={window.start + offset} to={pack.href}
              onClick={() => { if (list.current) scrollPosition.current = list.current.scrollTop }}>
              <span className={styles.name}>
                <Tooltip content={pack.kind==='graph'?msg('Graph'):msg('Pack')}><span className={styles.kindIcon} role="img" aria-label={pack.kind==='graph'?msg('Graph'):msg('Pack')}>{pack.kind==='graph'?<IconGraph/>:<IconPack/>}</span></Tooltip>
                <span className={styles.nameText} data-overflow-text>{pack.title}</span>
                {isSpelled(pack.detail) && <span className={styles.issue} role="img" aria-label={pack.detail}>!</span>}
              </span>
              <span className={isSpelled(pack.detail) ? styles.rowDetail : styles.description} data-overflow-text>
                {pack.kind==='pack' && pack.status!=='draft' && findings.get(pack.id)?.map(finding => <small key={finding.name} className={styles.reviewBadge} data-finding={finding.name}>{findingWords(finding.name)}</small>)}
                {folders && (folderScope === ALL_PACKS || filter) && <>{folderPath(folders.document,packFolder(folders.document,pack.id))} · </>}{isSpelled(pack.detail) ? pack.detail : isSpelled(pack.description) ? pack.description : ''}
              </span>
              <span className={styles.version} data-overflow-text>{pack.status==='draft'?<small className={styles.draftBadge}>{msg('Draft')}</small>:isSpelled(pack.packVersion) ? msg("v{{value0}}", { value0: pack.packVersion }) : '—'}</span>
            </Link></OverflowTooltip>
            <ItemOptions item={pack} onDelete={requestDelete}/>
            <Tooltip content={msg("Preview item · Space")}><Button variant="quiet" size="icon" className={styles.preview} data-preview data-row={window.start + offset}
              aria-label={msg("Preview {{value0}}", { value0: pack.title })} aria-pressed={previewOpen && preview?.id === pack.id}
              onClick={() => {
                if (previewOpen && previewId === pack.id) { setPreviewOpen(false); slot.close?.() }
                else showPreview(pack.id)
              }}><IconPreview /></Button></Tooltip>
          </li>)}
        </ul>
        <div style={{ height: window.padBottom }} aria-hidden="true" />
      </div>
    </nav>}
    {deleting && <DeleteDraftsDialog items={deleting.items} opener={deleting.opener} onClose={()=>setDeleting(undefined)}/>}
    {portal}
  </article>
}
