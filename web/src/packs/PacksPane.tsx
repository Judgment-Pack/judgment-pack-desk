import {PageLoading} from '../ui/PageLoading'
import { QueryStatusNotice } from '../shell/QueryStatusNotice'
import { graphDraftHref,graphDraftsQuery,retainedGraphDrafts,graphDraftFolderId } from '../graphs/drafts'
import {useQuery} from '@tanstack/react-query'
import {policyListQuery} from '../policies/client'
import {dialectListQuery} from '../dialects/queries'
import {dialectDraftListQuery} from '../dialects/drafts'
import {policyLibraryItems, policyLibraryStatus, dialectLibraryItems, folderAssignmentKey, libraryKind, libraryKindLabel} from '../library/items'
import { useSearchParams } from 'react-router-dom'
import { useChats } from '../chat/ChatProvider'
import { draftHref } from './drafts/model'
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
import { IconPreview, IconPack, IconGraph, IconMatrix, IconDetails } from '../shell/icons'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { PageHeader } from '../ui/PageLayout'
import { Pagination } from '../ui/Pagination'
import { usePagination } from '../ui/usePagination'
import { Tooltip, OverflowTooltip } from '../ui/Tooltip'
import { PackPreview, PackPreviewHint } from './PackPreview'
import styles from './PacksPane.module.css'
import { moveFocus, useWindowedRows } from './useWindowedRows'
import { usePackFolders } from './folders/FolderContext'
import { ALL_PACKS, HOME_FOLDER, inFolder, packFolder, folderPath } from './folders/model'
import { LibraryLocation, FolderFeedback, SubfolderRows } from './folders/FolderBrowser'
import { findingWords, packFindings } from './review/findings'
import { useReview } from './review/ReviewContext'
import { CollectionToolbar } from './CollectionToolbar'
import { CollectionHeaderActions, ItemOptions, DeleteDraftsDialog, type BrowserItem } from './CollectionActions'

function draftDescription(value: unknown): string | undefined { const text=(value as {description?:unknown}|null)?.description;return typeof text==='string'?text:undefined }
function isSpelled(value: string | undefined): value is string {
  return typeof value === 'string' && value.trim() !== ''
}


export function PacksPane({ active = true }: { active?: boolean }) {
  const locale = useLocale()
  const { data, error, isPending, isSuccess, isFetching, refetch } = usePacks()
  const graphs = useGraphInventory()
  const {graphInventorySupported, status: connectionStatus} = useMcp()
  const graphsUnavailable = connectionStatus === 'ready' && !graphInventorySupported
  const folders = usePackFolders()
  const [search, setSearch] = useSearchParams()
  const routeKind = search.has('type') ? libraryKind(search.get('type')) : folders?.prefs.kind ?? 'all'
  // Document URLs have no collection filter; retain it while the list is hidden.
  const retainedKind = useRef(routeKind)
  useEffect(() => { if (active) {retainedKind.current = routeKind;if(folders&&folders.prefs.kind!==routeKind)folders.preferences({kind:routeKind})} }, [active, routeKind, folders?.prefs.kind])
  const kind = active ? routeKind : retainedKind.current
  const includePacks = kind === 'all' || kind === 'pack', includeGraphs = kind === 'all' || kind === 'graph', includeDialects = kind === 'all' || kind === 'dialect', includePolicies = kind === 'all' || kind === 'policy'
  const dialects = useQuery({...dialectListQuery(), enabled: active && includeDialects, refetchInterval: active && includeDialects ? 5000 : false})
  const dialectDrafts = useQuery({...dialectDraftListQuery(), enabled: active && includeDialects, refetchInterval: active && includeDialects ? 5000 : false})
  const retainedGraphs=useQuery({...graphDraftsQuery(),enabled:active&&includeGraphs,refetchInterval:active&&includeGraphs?10000:false})
  const policies = useQuery({...policyListQuery(),enabled:active&&includePolicies,refetchInterval:active&&includePolicies?10000:false})
  const listedGraphs = graphInventorySupported ? graphs.data : undefined
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [deleting,setDeleting] = useState<{items:BrowserItem[];opener?:HTMLElement}>()
  const selectAll = useRef<HTMLInputElement>(null)
  const {chats,packDrafts}=useChats()
  const drafts = packDrafts.filter(item=>!item.finalized)
  const graphDrafts=retainedGraphDrafts(retainedGraphs.data?.content,chats).filter(d=>!d.saved).map(d=>({...d,chatId:d.originChatId??''}))
  const inventoryReady = (!includePacks || isSuccess) && (!includeGraphs || graphInventorySupported && graphs.isSuccess && retainedGraphs.isSuccess) && (!includeDialects || dialects.isSuccess && dialectDrafts.isSuccess) && (!includePolicies || policies.isSuccess)
  const review = useReview()
  const findings = useMemo(() => packFindings(review?.data), [review?.data])
  const folderScope = folders?.query.data && !folders.query.isError ? folders.selected : ALL_PACKS
  const createHref = folders ? `/packs/new?folder=${encodeURIComponent(folders.selected === ALL_PACKS ? HOME_FOLDER : folders.selected)}` : '/packs/new'
  const [filter, setFilter] = useState('')
  const [searchEverywhere, setSearchEverywhere] = useState(false)
  const listScope = searchEverywhere ? ALL_PACKS : folderScope
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
    const rows: BrowserItem[] = [...(data?.packs ?? []).map(pack=>({...pack,key:'pack:'+pack.id,kind:'pack' as const,title:pack.id,status:'finalized' as const,href:`/packs/${encodeURIComponent(pack.id)}`})),...drafts.map(draft=>({key:'pack-draft:'+draft.id,kind:'pack' as const,id:draft.id,title:draft.title,status:'draft' as const,href:draftHref(draft.id),description:draftDescription(draft.checkpoint.state.candidates.at(-1)?.document)})), ...(listedGraphs?.graphs ?? []).map(graph => ({key:'graph:'+graph.id,kind:'graph' as const, id:'graph:'+graph.id, title:graph.id, status:'finalized' as const, href:`/graphs/${encodeURIComponent(graph.id)}`, description:graph.description, detail:graph.detail, packVersion:graph.graphVersion})), ...graphDrafts.map(draft => ({key:graphDraftFolderId(draft),kind:'graph' as const, id:graphDraftFolderId(draft), title:draft.id, status:'draft' as const, chatId:draft.chatId, draftId:draft.draftId, href:graphDraftHref(draft.chatId,draft.draftId), description:draft.description})), ...dialectLibraryItems(dialects.data?.items??[],dialectDrafts.data?.items??[]),...policyLibraryItems(policies.data?.items??[])]
    const matching = rows.filter(pack => (kind==='all'||pack.kind===kind) && (status==='all'||(status==='saved'?pack.status!=='draft':pack.status===status)) && (!folders || listScope === ALL_PACKS || inFolder(folders.document, folderAssignmentKey(pack)===undefined?HOME_FOLDER:packFolder(folders.document,pack.id), listScope, !!needle)) && (!needle ||
      `${pack.id} ${pack.title}`.toLowerCase().includes(needle) || pack.description?.toLowerCase().includes(needle)))
    return matching.sort((left, right) => sort === 'id-desc'
      ? right.title.localeCompare(left.title) : left.title.localeCompare(right.title))
  }, [data, error, packDrafts, chats, retainedGraphs.data, listedGraphs, dialects.data, dialectDrafts.data, policies.data, kind, filter, status, sort, folders?.document, listScope, locale])
  const pagination = usePagination(packs.length, JSON.stringify([shell.storageKey, listScope, filter, kind, status, sort]))
  const pageItems = packs.slice(pagination.start, pagination.end)
  const selected = packs.filter(item => selectedIds.includes(item.key))
  const allSelected = packs.length > 0 && selected.length === packs.length
  useEffect(() => {
    const visible = new Set(packs.map(item => item.key))
    setSelectedIds(before => before.every(id => visible.has(id)) ? before : before.filter(id => visible.has(id)))
  }, [packs])
  useEffect(() => {if(selectAll.current) selectAll.current.indeterminate = selected.length > 0 && selected.length < packs.length}, [selected.length,packs.length])
  const toggleSelection = (id:string) => setSelectedIds(before => before.includes(id) ? before.filter(value=>value!==id) : [...before,id])
  const requestDelete = (items:BrowserItem[],opener?:HTMLElement) => setDeleting({items,opener})
  const window = useWindowedRows(pageItems.length, rowHeight)
  const preview = packs.find(pack => pack.key === previewId)
  const portal = useInspectorPortal(active
    ? preview ? preview.status==='draft' || preview.kind!=='pack' ? <section className={styles.empty}><h2>{preview.title}</h2><p>{libraryKindLabel(preview)} · {preview.kind==='policy'?policyLibraryStatus(preview):preview.status==='draft'?msg('Draft'):msg('Saved')}</p><p>{preview.description}</p>{preview.kind==='dialect'&&<p>{msg('Dialect folder placement is not available. Find dialects in Home or across all folders.')}</p>}<ButtonLink to={preview.href}>{preview.kind==='policy'?msg('Open policy'):preview.status==='draft'?msg('Open draft'):preview.kind==='dialect'?msg('Open dialect'):msg('Open graph')}</ButtonLink></section> : <PackPreview pack={data!.packs!.find(item=>item.id===preview.id)!} /> : <PackPreviewHint />
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
  useLayoutEffect(() => { resetScroll() }, [pagination.page, pagination.size])
  const changeKind = (value: string) => { resetScroll(); setSearch(before => {const next = new URLSearchParams(before); next.set('type', value); return next}) }
  const filtered = Boolean(filter || status !== 'all' || kind !== 'all')
  const clearFilter = () => { resetScroll(); setFilter(''); setStatus('all'); changeKind('all') }
  useEffect(() => { resetScroll(); setSearchEverywhere(false); setPreviewId(null); setPreviewOpen(false) }, [folderScope])

  const loading = !packs.length && (includePacks && isPending || includeGraphs && !graphsUnavailable && graphs.isPending || includeDialects && (dialects.isPending || dialectDrafts.isPending) || includePolicies && policies.isPending)
  const unavailable = includePacks && error || includeGraphs && (graphsUnavailable || graphs.error) || includeDialects && (dialects.error || dialectDrafts.error) || includePolicies && policies.error
  return <article className={styles.pane} data-layout={active ? 'page' : undefined} aria-label={msg("Library collection")}>
    <PageHeader title={msg("Library")} variant="collection"
      navigation={<LibraryLocation/>}
      actions={<CollectionHeaderActions createHref={createHref}/>} />
    <FolderFeedback/>
    <CollectionToolbar filter={filter} onFilter={value => {resetScroll();setFilter(value)}} kind={kind} onKind={changeKind}
      status={status} onStatus={value=>{resetScroll();setStatus(value)}} sort={sort} onSort={value=>{resetScroll();setSort(value)}}
      canScope={Boolean(folders && folderScope !== ALL_PACKS)} everywhere={searchEverywhere} onScope={value=>{resetScroll();setSearchEverywhere(value)}}
      filtered={filtered} reset={clearFilter} selected={selected} clearSelection={()=>setSelectedIds([])} onDelete={requestDelete}/>
    {!searchEverywhere && <SubfolderRows query={filter}/>}
    {includeGraphs && <><QueryStatusNotice id="graph-drafts" label={msg('Graph drafts unavailable')} error={retainedGraphs.error} onRetry={()=>retainedGraphs.refetch()}/>
      <QueryStatusNotice id="graphs" label={msg("Graphs unavailable")} error={graphs.error} retrying={graphs.isFetching} onRetry={()=>graphs.refetch()}>{msg("Graphs could not be refreshed. Previously loaded graphs may be out of date.")}</QueryStatusNotice></>}
    {includeGraphs && graphsUnavailable && <div className={styles.message} role="status">{msg('This runtime cannot list saved graphs. Connect a newer runtime to browse all packs and graphs.')} <ButtonLink to="/graphs">{msg('Open graphs')}</ButtonLink></div>}
    {includePacks && <QueryStatusNotice id="packs" label={msg("Packs unavailable")} error={error} retrying={isFetching} onRetry={()=>refetch()}>{msg("Packs could not be refreshed. Previously loaded packs may be out of date.")}</QueryStatusNotice>}
    {includeDialects && <QueryStatusNotice id="library-dialects" label={msg('Dialects unavailable')} error={dialects.error} retrying={dialects.isFetching} onRetry={()=>dialects.refetch()}>{msg('Dialects could not be refreshed. Previously loaded dialects may be out of date.')}</QueryStatusNotice>}
    {includeDialects && <QueryStatusNotice id="library-dialect-drafts" label={msg('Dialect drafts unavailable')} error={dialectDrafts.error} retrying={dialectDrafts.isFetching} onRetry={()=>dialectDrafts.refetch()}/>}
    {includePolicies && <QueryStatusNotice id="library-policies" label={msg('Policies unavailable')} error={policies.error} retrying={policies.isFetching} onRetry={()=>policies.refetch()}>{msg('Policies could not be refreshed. Previously loaded policies may be out of date.')}</QueryStatusNotice>}
    {loading ? <PageLoading>{msg('Loading library…')}</PageLoading>
    : packs.length === 0 && unavailable ? null
    : packs.length === 0 ? <section className={styles.empty} role="status">
      <h2>{filtered ? msg("No matching library items") : msg("No library items yet")}</h2>
      <p>{filtered ? msg("Try another search or status.") : msg("Create a pack, graph, dialect or policy to get started.")}</p>
      {filtered ? <Button onClick={clearFilter}>{msg("Reset filters")}</Button> : null}
    </section> : <nav className={styles.collection} data-selecting={selected.length > 0 || undefined} aria-label={msg("Library")} onKeyDown={event => {
      if(event.key==='Escape'&&selected.length){event.stopPropagation();event.preventDefault();setSelectedIds([])}
      if((event.ctrlKey||event.metaKey)&&event.key==='a'){event.preventDefault();setSelectedIds(packs.map(item=>item.key))}
    }}>
      <div className={styles.columns} style={{ paddingInlineEnd: `calc(var(--collection-gutter) + ${window.scrollbarWidth}px)` }}>
        <span className={styles.selection}><input ref={selectAll} data-select-all type="checkbox" aria-label={allSelected ? msg('Clear selection') : msg('Select all matches')} checked={allSelected} onChange={event=>setSelectedIds(event.target.checked?packs.map(item=>item.key):[])}/></span>
        <span aria-hidden="true">{msg("Name")}</span><span className={styles.descriptionColumn} aria-hidden="true">{msg("Description")}</span><span className={styles.versionColumn} aria-hidden="true">{msg("Version or status")}</span><span aria-hidden="true"/>
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
          if(event.key.toLowerCase()==='x'&&!event.ctrlKey&&!event.metaKey&&!event.altKey){event.preventDefault();if(!event.repeat&&pageItems[current])toggleSelection(pageItems[current].key);return}
          if (event.key === ' ' && !focused?.hasAttribute('data-select')) {
            event.preventDefault()
            if (event.repeat) return
            if (previewOpen && previewId === pageItems[current]?.key) { setPreviewOpen(false); slot.close?.() }
            else if (pageItems[current]) showPreview(pageItems[current].key)
            return
          }
          const next = moveFocus(event, pageItems.length, current)
          if (next === undefined) return
          const preview = focused?.hasAttribute('data-preview') ?? false
          const selection = focused?.hasAttribute('data-select') ?? false
          window.scrollRowIntoView(next)
          const selector = `[data-row="${next}"]${selection ? '[data-select]' : preview ? '[data-preview]' : ':not([data-preview]):not([data-select])'}`
          const already = list.current?.querySelector(selector)
          if (previewOpen && pageItems[next]) setPreviewId(pageItems[next].key)
          if (already instanceof HTMLElement) { already.focus({ preventScroll: true }); if (previewOpen) slot.reveal() }
          else setWanted({ index: next, preview, selection })
        }}>
          {pageItems.slice(window.start, window.end).map((pack, offset) => <li key={pack.key}
            className={styles.row} data-selection-selected={selectedIds.includes(pack.key) || undefined} data-selected={selectedIds.includes(pack.key) || previewOpen && preview?.key === pack.key || undefined}>
            <span className={styles.selection}><input type="checkbox" data-select data-row={window.start+offset} aria-label={msg('Select {{name}}', {name:pack.title})} checked={selectedIds.includes(pack.key)} onChange={()=>toggleSelection(pack.key)}/></span>
            <OverflowTooltip selector="[data-overflow-text]" fallback={msg("Preview this item to read the full description.")}><Link className={styles.link} aria-keyshortcuts="Space" data-row={window.start + offset} to={pack.href}
              onClick={() => { if (list.current) scrollPosition.current = list.current.scrollTop }}>
              <span className={styles.name}>
                <Tooltip content={libraryKindLabel(pack)}><span className={styles.kindIcon} role="img" aria-label={libraryKindLabel(pack)}>{pack.kind==='graph'?<IconGraph/>:pack.kind==='dialect'?<IconMatrix/>:pack.kind==='policy'?<IconDetails/>:<IconPack/>}</span></Tooltip>
                <span className={styles.nameText} data-overflow-text>{pack.title}</span>
                {isSpelled(pack.detail) && <span className={styles.issue} role="img" aria-label={pack.detail}>!</span>}
              </span>
              <span className={isSpelled(pack.detail) ? styles.rowDetail : styles.description} data-overflow-text>
                {pack.kind==='pack' && pack.status!=='draft' && findings.get(pack.id)?.map(finding => <small key={finding.name} className={styles.reviewBadge} data-finding={finding.name}>{findingWords(finding.name)}</small>)}
                {folders && (listScope === ALL_PACKS || filter) && <>{folderPath(folders.document,folderAssignmentKey(pack)===undefined?HOME_FOLDER:packFolder(folders.document,pack.id))} · </>}{isSpelled(pack.detail) ? pack.detail : isSpelled(pack.description) ? pack.description : ''}
              </span>
              <span className={styles.version} data-overflow-text>{pack.kind==='policy'?policyLibraryStatus(pack):pack.status==='draft'?<small className={styles.draftBadge}>{msg('Draft')}</small>:pack.kind==='dialect'?pack.records?msg('Ready to review'):msg('No records'):isSpelled(pack.packVersion) ? pack.packVersion : '—'}</span>
            </Link></OverflowTooltip>
            <ItemOptions item={pack} onDelete={requestDelete}/>
            <Tooltip content={msg("Preview item")} shortcut={msg("Spacebar")}><Button variant="quiet" size="icon" className={styles.preview} data-preview data-row={window.start + offset}
              aria-label={msg("Preview {{value0}}", { value0: pack.title })} aria-keyshortcuts="Space" aria-pressed={previewOpen && preview?.key === pack.key}
              onClick={() => {
                if (previewOpen && previewId === pack.key) { setPreviewOpen(false); slot.close?.() }
                else showPreview(pack.key)
              }}><IconPreview /></Button></Tooltip>
          </li>)}
        </ul>
        <div style={{ height: window.padBottom }} aria-hidden="true" />
      </div>
    </nav>}
    {(inventoryReady || packs.length > 0) && <Pagination pagination={pagination} complete={inventoryReady} className={styles.pagination}/>}
    {deleting && <DeleteDraftsDialog items={deleting.items} opener={deleting.opener} onClose={()=>setDeleting(undefined)}/>}
    {portal}
  </article>
}
