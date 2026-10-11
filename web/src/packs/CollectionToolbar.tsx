import { useLayoutEffect, useRef, useState } from 'react'
import { Menu as DropdownMenu, MenuChoice } from '../ui/Menu'
import { msg } from '../i18n'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { SortMenu } from '../ui/SortMenu'
import { Tooltip } from '../ui/Tooltip'
import { IconChevronDown, IconClose, IconSearch } from '../shell/icons'
import { SelectionActions, type BrowserItem } from './CollectionActions'
import styles from './PacksPane.module.css'

interface Props {
  filter: string; onFilter: (value: string) => void
  kind: string; onKind: (value: string) => void
  status: string; onStatus: (value: string) => void
  sort: string; onSort: (value: string) => void
  canScope: boolean; everywhere: boolean; onScope: (value: boolean) => void
  filtered: boolean; reset: () => void
  selected: BrowserItem[]; clearSelection: () => void
  onDelete: (items: BrowserItem[], opener?: HTMLElement) => void
}

/** One stable row; narrow panes put view controls in a single menu. */
export function CollectionToolbar(props: Props) {
  const { filter, onFilter, kind, onKind, status, onStatus, sort, onSort, canScope, everywhere, onScope, filtered, reset, selected, clearSelection, onDelete } = props
  const toolbar = useRef<HTMLDivElement>(null)
  const [compact, setCompact] = useState(false)
  const [tight, setTight] = useState(false)
  useLayoutEffect(() => {
    const element = toolbar.current
    if (!element) return
    const measure = () => {
      const width = element.getBoundingClientRect().width
      if (width > 0) { setCompact(width < 800); setTight(width < 480) }
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const types = [{ value: 'all', label: msg('All types') }, { value: 'pack', label: msg('Packs') }, { value: 'graph', label: msg('Graphs') }]
  const statuses = [{ value: 'all', label: msg('All statuses') }, { value: 'draft', label: msg('Draft') }, { value: 'finalized', label: msg('Saved') }]
  const sorts = [{ value: 'id-asc', label: msg('Name: A–Z') }, { value: 'id-desc', label: msg('Name: Z–A') }]
  const scope = everywhere ? msg('All folders') : msg('This folder')
  const activeFilters = Number(kind !== 'all') + Number(status !== 'all')
  return <div ref={toolbar} className={styles.toolbar} data-compact={compact || undefined} data-tight={tight || undefined} role="group" aria-label={msg('Pack and graph list controls')}>
    <div className={styles.searchField}>
      <IconSearch/>
      <Input className={styles.search} type="search" aria-label={msg('Search packs and graphs')} value={filter} placeholder={msg('Search…')} onChange={event => onFilter(event.target.value)}/>
      {canScope && <DropdownMenu.Root>
        <Tooltip content={msg('Search scope: {{scope}}', {scope})}><DropdownMenu.Trigger asChild><Button variant="quiet" className={styles.scope} aria-label={msg('Search scope: {{scope}}', {scope})}><span>{scope}</span><IconChevronDown/></Button></DropdownMenu.Trigger></Tooltip>
        <DropdownMenu.Portal><DropdownMenu.Content align="start" sideOffset={4} collisionPadding={16}>
          <DropdownMenu.RadioGroup value={everywhere ? 'all' : 'folder'} onValueChange={value => onScope(value === 'all')}>
            {[{value:'folder',label:msg('This folder')},{value:'all',label:msg('All folders')}].map(option => <DropdownMenu.RadioItem key={option.value} value={option.value} ><span>{option.label}</span></DropdownMenu.RadioItem>)}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Content></DropdownMenu.Portal>
      </DropdownMenu.Root>}
    </div>
    {compact ? <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild><Button variant="quiet" aria-label={msg('View options')}>{msg('View')}{activeFilters > 0 && <span className={styles.filterCount}>{activeFilters}</span>}<IconChevronDown/></Button></DropdownMenu.Trigger>
      <DropdownMenu.Portal><DropdownMenu.Content align="end" sideOffset={4} collisionPadding={16}>
        {[{label:msg('Item type'),value:kind,onChange:onKind,options:types},{label:msg('Status'),value:status,onChange:onStatus,options:statuses},{label:msg('Sort'),value:sort,onChange:onSort,options:sorts}].map(group => <DropdownMenu.Group key={group.label}>
          <DropdownMenu.Label>{group.label}</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={group.value} onValueChange={group.onChange} aria-label={group.label}>
            {group.options.map(option => <DropdownMenu.RadioItem key={option.value} value={option.value}  onSelect={event => event.preventDefault()}><span>{option.label}</span></DropdownMenu.RadioItem>)}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Group>)}
        {filtered && <><DropdownMenu.Separator/><DropdownMenu.Item icon={<IconClose/>} onSelect={reset}><span>{msg('Clear filters')}</span></DropdownMenu.Item></>}
      </DropdownMenu.Content></DropdownMenu.Portal>
    </DropdownMenu.Root> : <>
      <MenuChoice label={msg('Item type')} value={kind} onValueChange={onKind} options={types}/>
      <MenuChoice label={msg('Status')} value={status} onValueChange={onStatus} options={statuses}/>
      <SortMenu label={msg('Sort packs and graphs')} value={sort} onValueChange={onSort} options={sorts}/>
      {filtered && <Tooltip content={msg('Clear filters')}><Button variant="quiet" size="icon" aria-label={msg('Clear filters')} onClick={reset}><IconClose/></Button></Tooltip>}
    </>}
    <SelectionActions items={selected} clear={clearSelection} onDelete={onDelete} compact={compact} tight={tight}/>
  </div>
}
