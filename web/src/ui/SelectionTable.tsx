import type { ComponentPropsWithRef, ReactNode, RefObject } from 'react'
import { msg } from '../i18n'
import { IconChevronDown, IconClose, IconDownload } from '../shell/icons'
import { Button, type ButtonProps } from './Button'
import { Menu } from './Menu'
import { Tooltip } from './Tooltip'
import type { CollectionSelection } from './useCollectionSelection'
import styles from './SelectionTable.module.css'

/** Native collection table, also used by collections without bulk selection. */
export function CollectionTable({className,variant,...table}:ComponentPropsWithRef<'table'> & {variant?:'index'}) {
  return <table {...table} data-collection-index={variant==='index'||undefined} className={[styles.table,className].filter(Boolean).join(' ')}/>
}
/** Native table semantics with Library's selection reveal and scoped shortcuts. */
export function SelectionTable({ selection, className, onKeyDown, variant, ...table }: ComponentPropsWithRef<'table'> & { selection: CollectionSelection; variant?: 'index' }) {
  return <CollectionTable {...table} variant={variant} className={className} data-selecting={selection.count > 0 || undefined} onKeyDown={event => {
    onKeyDown?.(event)
    if (event.defaultPrevented || (event.target instanceof Element && event.target.closest('input:not([type="checkbox"]),textarea,select,[contenteditable="true"]'))) return
    if (event.key === 'Escape' && selection.count) { event.preventDefault(); event.stopPropagation(); selection.clear() }
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'a' && !event.altKey) { event.preventDefault(); if (!selection.all) selection.toggleAll() }
  }}/>
}
export function SelectionHeader({ selection, complete = true }: { selection: CollectionSelection; complete?: boolean }) {
  return <th className={styles.cell} scope="col" data-selection-cell><label className={styles.check}>
    <input ref={element => {selection.header.current = element; if (element) element.indeterminate = selection.mixed}}
      data-select-all type="checkbox" aria-label={selection.all ? msg('Clear selection') : complete ? msg('Select all matches') : msg('Select all loaded matches')}
      disabled={selection.empty} checked={selection.all} onChange={selection.toggleAll}/>
  </label></th>
}
export function SelectionCell({ selection, id, name }: { selection: CollectionSelection; id: string; name: string }) {
  return <td className={styles.cell} data-row-ignore data-selection-cell><label className={styles.check}>
    <input type="checkbox" aria-label={msg('Select {{name}}', {name})} checked={selection.has(id)} onChange={event => selection.toggle(id, (event.nativeEvent as MouseEvent).shiftKey)}/>
  </label></td>
}
/** One selection indicator for Library and native collection tables. */
export function SelectionCountButton({ count, compact = false, className, ...props }: ButtonProps & { count: number; compact?: boolean }) {
  return <Button {...props} variant="quiet" className={[styles.count, className].filter(Boolean).join(' ')} aria-label={msg('{{count}} selected', {count})}>
    <span role="status">{compact ? count : msg('{{count}} selected', {count})}</span><IconChevronDown/>
  </Button>
}
export function SelectionActions({ selection, onExport, children, triggerRef, compact=false, tight=false }: {
  selection: CollectionSelection; onExport?: () => void; children?: ReactNode; triggerRef?: RefObject<HTMLButtonElement | null>; compact?:boolean; tight?:boolean
}) {
  if (!selection.count) return null
  const clear = () => { selection.clear(); requestAnimationFrame(() => selection.header.current?.focus()) }
  return <div className={styles.actions} role="group" aria-label={msg('Selected item actions')}>
    <Menu.Root><Menu.Trigger asChild><SelectionCountButton ref={triggerRef} count={selection.count} compact={tight}/></Menu.Trigger>
      <Menu.Portal><Menu.Content align="end" collisionPadding={16}>
        {children}
        {onExport && <Menu.Item icon={<IconDownload/>} onSelect={onExport}>{msg('Export selected rows')}</Menu.Item>}
        {(children || onExport) && <Menu.Separator/>}
        <Menu.Item icon={<IconClose/>} onSelect={clear}>{msg('Clear selection')}</Menu.Item>
      </Menu.Content></Menu.Portal>
    </Menu.Root>
    {!compact&&<Tooltip content={msg('Clear selection')}><Button variant="quiet" size="icon" aria-label={msg('Clear selection')} onClick={clear}><IconClose/></Button></Tooltip>}
  </div>
}
