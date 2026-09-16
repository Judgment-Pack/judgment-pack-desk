import { useState, type RefObject } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { DropdownMenu } from 'radix-ui'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { OverflowTooltip, Tooltip } from '../ui/Tooltip'
import { IconChevronLeft, IconHistory, IconPlus } from '../shell/icons'
import { useChats } from './ChatProvider'
import type { Chat } from './store'
import styles from './ChatWorkspace.module.css'

export function chatHref(chat: Chat, location?: { pathname: string; search: string }): string {
  if (!chat.pack) return `/chats/${chat.id}`
  const path = `/packs/${encodeURIComponent(chat.pack.id)}`
  const params = new URLSearchParams(location?.pathname === path ? location.search : '')
  params.set('chat', chat.id)
  return `${path}?${params}`
}
/** The same compact controls are portalled into main or the Assistant title bar. */
export function ChatToolbar({ chat, history, onHistory, onBack, onNew, historyRef }: {
  chat: Chat; history: boolean; onHistory: () => void; onBack: () => void; onNew: () => void
  historyRef: RefObject<HTMLButtonElement | null>
}) {
  const { store } = useChats()
  return <div className={styles.chatToolbar}>
    {history && <Tooltip content="Back to chat"><button type="button" className="desk-icon-button" aria-label="Back to chat" onClick={onBack}><IconChevronLeft /></button></Tooltip>}
    <OverflowTooltip content={history ? 'Chat history' : chat.title}><span className={styles.chatTitle}>{history ? 'Chat history' : chat.title}</span></OverflowTooltip>
    <Tooltip content="New chat"><button type="button" className="desk-icon-button" aria-label="New chat" disabled={!store?.canCreate} onClick={onNew}><IconPlus /></button></Tooltip>
    <Tooltip content="Chat history" openOnFocus={false}><button ref={historyRef} type="button" className="desk-icon-button" aria-label="Chat history" aria-pressed={history} onClick={history ? onBack : onHistory}><IconHistory /></button></Tooltip>
  </div>
}
export function RecentChats({ onNavigate }: { onNavigate?: () => void }) {
  const { store, chats, ready, bindings } = useChats()
  const navigate = useNavigate()
  const location = useLocation()
  if (!store || !ready) return null
  const recent = chats.filter(chat => !chat.archived && (chat.checkpoint?.state.turns.length || chat.composer)).sort((a,b) => Number(b.pinned) - Number(a.pinned) || b.updatedAt.localeCompare(a.updatedAt)).slice(0,5)
  return <section className={styles.recent} aria-label="Recent chats">
    <span className={styles.caption}>Recent chats</span>
    {recent.length === 0 && <p className={styles.caption}>Your chats appear here.</p>}
    {recent.map(chat => <OverflowTooltip key={chat.id} content={chat.title}><button className="desk-nav-item" type="button" aria-current={(location.pathname === `/chats/${chat.id}` || new URLSearchParams(location.search).get('chat') === chat.id) ? 'page' : undefined} onClick={() => { navigate(chatHref(chat, location)); onNavigate?.() }}><span className={styles.ellipsis}>{chat.title}</span>{bindings.get(chat.id)?.state.status === 'running' && <span className={styles.caption}>Working</span>}</button></OverflowTooltip>)}
    <ButtonLink variant="quiet" to="/chats" onClick={onNavigate}>Chat history</ButtonLink>
  </section>
}
export function ChatHistoryList({ packId, activeId, onNavigate }: {
  packId?: string; activeId?: string; onNavigate?: () => void
}) {
  const { store, chats, bindings, ready, error } = useChats()
  const navigate = useNavigate()
  const location = useLocation()
  const [query, setQuery] = useState('')
  const [all, setAll] = useState(false)
  const [archived, setArchived] = useState(false)
  const [rename, setRename] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [deleting, setDeleting] = useState<string | null>(null)
  const shown = chats.filter(chat => (all || !packId || chat.pack?.id === packId) && (archived || !chat.archived)
    && `${chat.title} ${chat.pack?.id ?? ''} ${chat.checkpoint?.state.turns.map(turn => turn.text).join(' ') ?? ''}`.toLowerCase().includes(query.toLowerCase()))
    .sort((a,b) => Number(b.pinned) - Number(a.pinned) || b.updatedAt.localeCompare(a.updatedAt))
  const exportChat = (chat: Chat) => {
    const url = URL.createObjectURL(new Blob([JSON.stringify(chat, null, 2)], { type: 'application/json' }))
    const link = document.createElement('a'); link.href = url; link.download = `chat-${chat.id}.json`; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }
  return <section className={styles.historyBody} aria-label="Chat history">
    {error && <div role="alert"><p>{error}</p><Button onClick={() => ready ? store?.retrySave() : void store?.load()}>Retry</Button></div>}
    {!ready && !error && <p role="status">Loading chat history…</p>}
    <Input autoFocus aria-label="Search chats" value={query} onChange={event => setQuery(event.target.value)} placeholder="Search title or messages…" />
    <div className={styles.historyFilters}>
      {packId && <label><input type="checkbox" checked={all} onChange={event => setAll(event.target.checked)} /> All packs</label>}
      <label><input type="checkbox" checked={archived} onChange={event => setArchived(event.target.checked)} /> Include archived</label>
    </div>
    <ul className={styles.history}>
      {shown.map(chat => <li key={chat.id}>
        {rename === chat.id ? <form className={styles.historyRename} onSubmit={event => { event.preventDefault(); if (name.trim()) { store?.update(chat.id, { title: name.trim() }); setRename(null) } }}>
          <Input aria-label="Chat name" value={name} autoFocus maxLength={120} onChange={event => setName(event.target.value)} /><Button type="submit" disabled={!name.trim()}>Save</Button><Button variant="quiet" onClick={() => setRename(null)}>Cancel</Button>
        </form> : <>
          <button type="button" className={styles.historyOpen} aria-current={activeId === chat.id ? 'true' : undefined} onClick={() => { navigate(chatHref(chat, location)); onNavigate?.() }}>
            <span>{chat.pinned ? 'Pinned · ' : ''}{chat.title}</span><small>{chat.pack?.id ?? 'Unpublished draft'} · {bindings.get(chat.id)?.state.status === 'running' ? 'Working' : new Date(chat.updatedAt).toLocaleDateString()}{chat.archived ? ' · Archived' : ''}</small>
          </button>
          <DropdownMenu.Root><DropdownMenu.Trigger className="desk-icon-button" aria-label={`Actions for ${chat.title}`}>…</DropdownMenu.Trigger>
            <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" collisionPadding={12}>
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => { setRename(chat.id); setName(chat.title) }}>Rename</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => store?.update(chat.id, { pinned: !chat.pinned })}>{chat.pinned ? 'Unpin' : 'Pin'}</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => store?.update(chat.id, { archived: !chat.archived })}>{chat.archived ? 'Unarchive' : 'Archive'}</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => exportChat(chat)}>Export chat</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" disabled={bindings.get(chat.id)?.run?.running} onSelect={() => setDeleting(chat.id)}>Delete chat…</DropdownMenu.Item>
            </DropdownMenu.Content></DropdownMenu.Portal>
          </DropdownMenu.Root>
        </>}
        {deleting === chat.id && <div className={styles.confirm}><p>Delete this chat? Its pack and research files will remain.</p><Button variant="danger" onClick={() => { const current = location.pathname === `/chats/${chat.id}` || new URLSearchParams(location.search).get('chat') === chat.id; store?.remove(chat.id); setDeleting(null); if (current) { navigate(chat.pack ? `/packs/${encodeURIComponent(chat.pack.id)}` : '/create-pack'); onNavigate?.() } }}>Delete chat</Button><Button onClick={() => setDeleting(null)}>Cancel</Button></div>}
      </li>)}
    </ul>
    {ready && !shown.length && <p className={styles.caption}>No chats match this search.</p>}
    {store && !store.canCreate && <p className={styles.caption}>Chat history is full. Export and delete an older chat to start another.</p>}
  </section>
}
