import { useRef, useState } from 'react'
import { DropdownMenu } from 'radix-ui'
import { Link } from 'react-router-dom'
import { msg, systemMessage } from '../i18n'
import { useChats } from '../chat/ChatProvider'
import { Button, ButtonLink } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Tooltip } from '../ui/Tooltip'
import { Alert } from '../ui/Alert'
import { IconChevronDown, IconMore, IconPack, IconGraph, IconFolder, IconTrash } from '../shell/icons'
import { usePackFolders } from './folders/FolderContext'
import { ALL_PACKS } from './folders/model'
import styles from './PacksPane.module.css'

export interface BrowserItem {
  id: string; title: string; kind: 'pack' | 'graph'; status: 'draft' | 'finalized'; href: string
  description?: string; detail?: string; packVersion?: string; chatId?: string; draftId?: string
}

export function CollectionHeaderActions({ createHref, review }: {createHref: string; review: boolean}) {
  const folders = usePackFolders(), options = useRef<HTMLButtonElement>(null)
  return <>
    {folders || review ? <DropdownMenu.Root>
      <Tooltip content={msg('Collection options')}><DropdownMenu.Trigger asChild><Button ref={options} variant="quiet" size="icon" aria-label={msg('Collection options')}><IconMore/></Button></DropdownMenu.Trigger></Tooltip>
      <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" sideOffset={6}
        onCloseAutoFocus={event => {if (folders?.editing) event.preventDefault()}}>
        {folders && <DropdownMenu.Item className="desk-menu-item" disabled={!folders.query.data || folders.query.isError || folders.mutation.isPending}
          onSelect={() => folders.edit({kind:'create', parentId:folders.selected === ALL_PACKS ? null : folders.selected}, options.current ?? undefined)}><IconFolder/>{msg('New folder')}</DropdownMenu.Item>}
        {review && <DropdownMenu.Item asChild className="desk-menu-item"><Link to="/packs/_review">{msg('Review and lock')}</Link></DropdownMenu.Item>}
      </DropdownMenu.Content></DropdownMenu.Portal>
    </DropdownMenu.Root> : null}
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild><Button variant="primary">{msg('Create')}<IconChevronDown/></Button></DropdownMenu.Trigger>
      <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" sideOffset={6}>
        <DropdownMenu.Item asChild className="desk-menu-item"><Link to={createHref}><IconPack/>{msg('Create pack')}</Link></DropdownMenu.Item>
        <DropdownMenu.Item asChild className="desk-menu-item"><Link to="/graphs?view=compose"><IconGraph/>{msg('Create graph')}</Link></DropdownMenu.Item>
      </DropdownMenu.Content></DropdownMenu.Portal>
    </DropdownMenu.Root>
  </>
}

export function SelectionActions({items, clear, onDelete}: {items: BrowserItem[]; clear: () => void; onDelete: (items: BrowserItem[], opener?:HTMLElement) => void}) {
  const folders = usePackFolders()
  if (!items.length) return null
  const compose = items.every(item => item.kind === 'pack' && item.status === 'finalized')
  const drafts = items.every(item => item.status === 'draft')
  return <div className={styles.bulkActions} role="group" aria-label={msg('Selected item actions')}>
    <span className={styles.selectedCount} role="status">{msg('{{count}} selected', {count:items.length})}</span>
    {folders && <Button variant="quiet" disabled={!folders.query.data || folders.query.isError || folders.mutation.isPending}
      onClick={event => folders.edit({kind:'items',ids:items.map(item => item.id)}, event.currentTarget)}><IconFolder/>{msg('Move to…')}</Button>}
    {compose ? <ButtonLink variant="quiet" to={'/graphs?view=compose&'+items.map(item => 'pack='+encodeURIComponent(item.id)).join('&')}><IconGraph/>{msg('Compose graph')}</ButtonLink>
      : <Tooltip content={msg('Select saved packs to compose a graph. Save drafts first; graphs cannot be nested.')}><Button variant="quiet" aria-disabled onClick={event => event.preventDefault()}><IconGraph/>{msg('Compose graph')}</Button></Tooltip>}
    {drafts ? <Button variant="quiet" onClick={event => onDelete(items,event.currentTarget)}><IconTrash/>{msg('Delete drafts')}</Button>
      : <Tooltip content={msg('Select only drafts to delete them. Saved files may be referenced by graphs or jobs.')}><Button variant="quiet" aria-disabled onClick={event => event.preventDefault()}><IconTrash/>{msg('Delete drafts')}</Button></Tooltip>}
    <Button variant="quiet" className={styles.clearSelection} onClick={clear}>{msg('Clear selection')}</Button>
  </div>
}

export function ItemOptions({item, onDelete}: {item: BrowserItem; onDelete: (items: BrowserItem[], opener?:HTMLElement) => void}) {
  const folders = usePackFolders(), opener = useRef<HTMLButtonElement>(null)
  return <DropdownMenu.Root>
    <Tooltip content={msg('Item options')}><DropdownMenu.Trigger asChild><Button ref={opener} variant="quiet" size="icon" className={styles.rowOptions} aria-label={msg('Options for {{name}}', {name:item.title})}><IconMore/></Button></DropdownMenu.Trigger></Tooltip>
    <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" sideOffset={4}
      onCloseAutoFocus={event => {if (folders?.editing) event.preventDefault()}}>
      <DropdownMenu.Item asChild className="desk-menu-item"><Link to={item.href}>{msg('Open')}</Link></DropdownMenu.Item>
      {folders && <DropdownMenu.Item className="desk-menu-item" disabled={!folders.query.data || folders.query.isError || folders.mutation.isPending}
        onSelect={() => folders.edit({kind:'pack',id:item.id},opener.current ?? undefined)}><IconFolder/>{msg('Move to folder…')}</DropdownMenu.Item>}
      {item.status === 'draft' && <><DropdownMenu.Separator className="desk-menu-separator"/><DropdownMenu.Item className="desk-menu-item" onSelect={() => onDelete([item],opener.current ?? undefined)}><IconTrash/>{msg('Delete draft')}</DropdownMenu.Item></>}
    </DropdownMenu.Content></DropdownMenu.Portal>
  </DropdownMenu.Root>
}

/** Remove only draft artifacts. Project files and their references have another lifecycle. */
export function DeleteDraftsDialog({items, opener:trigger, onClose}: {items: BrowserItem[]; opener?:HTMLElement; onClose: () => void}) {
  const {store} = useChats()
  const [busy,setBusy] = useState(false), [error,setError] = useState(''), [applied,setApplied] = useState(false)
  const opener = useRef(trigger ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null))
  const remove = async () => {
    if (!store || busy) return
    setBusy(true); setError('')
    try {
      // Another autosave owns the write revision until it settles. Do not report
      // deletion as durable when flush skipped an in-flight write.
      const settle = async () => {if (store.getSnapshot().saving) await new Promise<void>(resolve => {
        const unsubscribe = store.subscribe(() => {if (!store.getSnapshot().saving) {unsubscribe();resolve()}})
      })}
      await settle()
      if (!applied) {
        const removed = store.removeDraftItems(items.map(item => item.kind === 'pack'
          ? {kind:'pack' as const,id:item.id}
          : {kind:'graph' as const,chatId:item.chatId!,id:item.draftId!}))
        if (!removed) throw new Error(msg('The drafts changed or a conversation is running. Close this dialog and review your selection.'))
        setApplied(true)
      }
      if (store.getSnapshot().error) {store.retrySave();await settle()}
      if (!await store.flush()) throw new Error(store.getSnapshot().error || msg('Draft changes could not be saved. Try again.'))
      onClose()
    } catch (cause) {setError(cause instanceof Error ? systemMessage(cause.message) : msg('Draft changes could not be saved. Try again.'))}
    finally {setBusy(false)}
  }
  return <Dialog open title={msg('Delete selected drafts?')} description={msg('Conversations and saved project files will remain.')} openerRef={opener} onCloseAutoFocus={event=>{event.preventDefault();(opener.current?.isConnected?opener.current:document.querySelector<HTMLElement>('[data-select-all]'))?.focus()}} onOpenChange={open => {if (!open && !busy) onClose()}}>
    <ul className={styles.deleteList}>{items.map(item => <li key={item.id}>{item.title}</li>)}</ul>
    {error && <Alert>{error}</Alert>}
    <DialogActions><Button variant="quiet" disabled={busy} onClick={onClose}>{msg('Cancel')}</Button><Button variant="danger" disabled={busy || !store} onClick={() => void remove()}>{busy ? msg('Saving…') : applied ? msg('Retry saving') : msg('Delete drafts')}</Button></DialogActions>
  </Dialog>
}
