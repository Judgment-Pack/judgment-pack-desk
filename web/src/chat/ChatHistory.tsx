import { useRef, useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { DropdownMenu } from 'radix-ui'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Input } from '../ui/Input'
import { OverflowTooltip } from '../ui/Tooltip'
import { IconChevronDown, IconPlus } from '../shell/icons'
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
export function ChatMenu({ chat, onNew }: { chat: Chat; onNew: () => void }) {
  const { store, chats } = useChats()
  const navigate = useNavigate()
  const location = useLocation()
  const [history, setHistory] = useState(false)
  const opener = useRef<HTMLButtonElement>(null)
  const recent = chats.filter(item => !item.archived && item.pack?.id === chat.pack?.id).slice(0, 6)
  return <div className={styles.chatMenu}>
    <DropdownMenu.Root>
      <DropdownMenu.Trigger ref={opener} className={styles.menuTrigger}><span>{chat.title}</span><IconChevronDown /></DropdownMenu.Trigger>
      <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="start" sideOffset={6} collisionPadding={12}>
        <DropdownMenu.Label className="desk-menu-label">{chat.pack ? `Chats for ${chat.pack.id}` : 'Draft conversations'}</DropdownMenu.Label>
        {recent.map(item => <DropdownMenu.Item key={item.id} className="desk-menu-item" onSelect={() => navigate(chatHref(item, location))}>{item.title}</DropdownMenu.Item>)}
        <DropdownMenu.Separator className="desk-menu-separator" />
        <DropdownMenu.Item className="desk-menu-item" disabled={!store?.canCreate} onSelect={onNew}>New chat</DropdownMenu.Item>
        <DropdownMenu.Item className="desk-menu-item" onSelect={() => setHistory(true)}>View all chats</DropdownMenu.Item>
      </DropdownMenu.Content></DropdownMenu.Portal>
    </DropdownMenu.Root>
    <HistoryDialog open={history} onOpenChange={setHistory} openerRef={opener} packId={chat.pack?.id} />
  </div>
}
export function RecentChats({ onNavigate }: { onNavigate?: () => void }) {
  const { store, chats, ready, bindings } = useChats()
  const [history, setHistory] = useState(false)
  const opener = useRef<HTMLButtonElement>(null)
  const navigate = useNavigate()
  const location = useLocation()
  if (!store || !ready) return null
  const recent = chats.filter(chat => !chat.archived && (chat.checkpoint?.state.turns.length || chat.composer)).sort((a,b) => Number(b.pinned) - Number(a.pinned) || b.updatedAt.localeCompare(a.updatedAt)).slice(0,5)
  return <section className={styles.recent} aria-label="Recent chats">
    <span className={styles.caption}>Recent chats</span>
    {recent.length === 0 && <p className={styles.caption}>Your conversations appear here.</p>}
    {recent.map(chat => <OverflowTooltip key={chat.id} content={chat.title}><button className="desk-nav-item" type="button" aria-current={(location.pathname === `/chats/${chat.id}` || new URLSearchParams(location.search).get('chat') === chat.id) ? 'page' : undefined} onClick={() => { navigate(chatHref(chat, location)); onNavigate?.() }}><span className={styles.ellipsis}>{chat.title}</span>{bindings.get(chat.id)?.state.status === 'running' && <span className={styles.caption}>Working</span>}</button></OverflowTooltip>)}
    <Button ref={opener} variant="quiet" onClick={() => setHistory(true)}>View all chats</Button>
    <HistoryDialog open={history} onOpenChange={setHistory} openerRef={opener} onNavigate={onNavigate} />
  </section>
}
export function HistoryDialog({ open, onOpenChange, openerRef, packId, onNavigate }: {
  open: boolean; onOpenChange: (open: boolean) => void; openerRef: React.RefObject<HTMLButtonElement | null>; packId?: string; onNavigate?: () => void
}) {
  const { store, chats, bindings } = useChats()
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
  return <Dialog open={open} onOpenChange={onOpenChange} title="Chats" openerRef={openerRef} description="Conversations saved on this computer, for this project.">
    <Input aria-label="Search chats" value={query} onChange={event => setQuery(event.target.value)} placeholder="Search title or messages…" />
    <div className={styles.historyFilters}>
      {packId && <label><input type="checkbox" checked={all} onChange={event => setAll(event.target.checked)} /> All packs</label>}
      <label><input type="checkbox" checked={archived} onChange={event => setArchived(event.target.checked)} /> Include archived</label>
    </div>
    <ul className={styles.history}>
      {shown.map(chat => <li key={chat.id}>
        {rename === chat.id ? <form className={styles.historyRename} onSubmit={event => { event.preventDefault(); if (name.trim()) { store?.update(chat.id, { title: name.trim() }); setRename(null) } }}>
          <Input aria-label="Chat name" value={name} autoFocus maxLength={120} onChange={event => setName(event.target.value)} /><Button type="submit" disabled={!name.trim()}>Save</Button><Button variant="quiet" onClick={() => setRename(null)}>Cancel</Button>
        </form> : <>
          <button type="button" className={styles.historyOpen} onClick={() => { navigate(chatHref(chat, location)); onOpenChange(false); onNavigate?.() }}>
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
        {deleting === chat.id && <div className={styles.confirm}><p>Delete this conversation? Its pack and research files will remain.</p><Button variant="danger" onClick={() => { const current = location.pathname === `/chats/${chat.id}` || new URLSearchParams(location.search).get('chat') === chat.id; store?.remove(chat.id); setDeleting(null); if (current) { navigate(chat.pack ? `/packs/${encodeURIComponent(chat.pack.id)}` : '/create-pack'); onOpenChange(false) } }}>Delete chat</Button><Button onClick={() => setDeleting(null)}>Cancel</Button></div>}
      </li>)}
    </ul>
    {!shown.length && <p className={styles.caption}>No chats match this search.</p>}
    {store && !store.canCreate && <p className={styles.caption}>Chat history is full. Export and delete an older chat to start another.</p>}
    <DialogActions><Button onClick={() => onOpenChange(false)}>Close</Button><Button variant="primary" disabled={!store?.canCreate} onClick={() => { if (!store) return; const chat = store.create(); navigate(chatHref(chat, location)); onOpenChange(false); onNavigate?.() }}><IconPlus /> New chat</Button></DialogActions>
  </Dialog>
}
