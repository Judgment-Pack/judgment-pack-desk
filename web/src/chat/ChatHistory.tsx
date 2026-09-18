import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage, formatDate } from '../i18n'
import { Fragment, useState, type RefObject } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { DropdownMenu } from 'radix-ui'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { OverflowTooltip, Tooltip } from '../ui/Tooltip'
import { Popover } from '../ui/Popover'
import { IconHistory, IconMore, IconPlus } from '../shell/icons'
import { useChats } from './ChatProvider'
import type { Chat } from './store'
import { readDocumentObject } from '../documents/client'
import { chatHref, chatTitle, hasChatContent, homeChatId } from './navigation'
export { chatHref } from './navigation'
import styles from './ChatWorkspace.module.css'

/** The same compact controls are portalled into main or the Assistant title bar. */
export function ChatToolbar({ chat, history, onHistory, onBack, onNew, historyRef }: {
  chat: Chat; history: boolean; onHistory: () => void; onBack: () => void; onNew: () => void
  historyRef: RefObject<HTMLButtonElement | null>
}) {
  useLocale()
  const { store, bindings } = useChats()
  const state = bindings.get(chat.id)?.state ?? chat.checkpoint?.state
  const empty = !chat.composer.trim() && !chat.attachments?.length && !state?.turns.length && !state?.candidates.length
  return <div className={styles.chatToolbar} role="group" aria-label={msg("Chat actions")}>
    <Tooltip content={msg("New chat")}><button type="button" className="desk-icon-button" aria-label={msg("New chat")} disabled={!store?.canCreate || empty} onClick={onNew}><IconPlus /></button></Tooltip>
    <Popover title={msg("Chat history")} variant="list" triggerTooltip={msg("Chat history")} open={history} onOpenChange={open => open ? onHistory() : onBack()}
      onEscapeKeyDown={event => event.stopPropagation()}
      trigger={<button ref={historyRef} type="button" className="desk-icon-button" aria-label={msg("Chat history")} aria-pressed={history}><IconHistory /></button>}>
      <ChatHistoryList packId={chat.pack?.id} activeId={chat.id} onNavigate={onBack} compact />
    </Popover>
  </div>
}
export function historyGroup(chat: Chat, now = new Date()): string {
  if (chat.pinned) return msg("Pinned")
  const date = new Date(chat.updatedAt)
  const day = (value: Date) => Date.UTC(value.getFullYear(), value.getMonth(), value.getDate())
  const elapsed = (day(now) - day(date)) / 86_400_000
  return elapsed <= 0 ? msg("Today") : elapsed === 1 ? msg("Yesterday") : elapsed < 7 ? msg("Previous 7 days") : msg("Older")
}
function attention(status: string | undefined): string {
  return status === 'running' ? msg("Working") : status === 'stopped' ? msg("Interrupted") : status === 'failed' || status === 'stalled' || status === 'budget' ? msg("Needs attention") : ''
}
export function ChatHistoryList({ packId, activeId, onNavigate, compact = false }: {
  packId?: string; activeId?: string; onNavigate?: () => void; compact?: boolean
}) {
  useLocale()
  const { store, chats, bindings, ready, error } = useChats()
  const navigate = useNavigate()
  const location = useLocation()
  const [query, setQuery] = useState('')
  const [all, setAll] = useState(false)
  const [archived, setArchived] = useState(false)
  const [rename, setRename] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [deleting, setDeleting] = useState<string | null>(null)
  const [exportError, setExportError] = useState('')
  const [exporting, setExporting] = useState(false)
  const [actionChat, setActionChat] = useState<string | null>(null)
  const shown = chats.filter(chat => hasChatContent(chat) && (all || !packId || chat.pack?.id === packId) && (archived || !chat.archived)
    && `${chatTitle(chat)} ${chat.composer} ${chat.pack?.id ?? ''} ${chat.checkpoint?.state.turns.map(turn => turn.text).join(' ') ?? ''}`.toLowerCase().includes(query.toLowerCase()))
    .sort((a,b) => Number(b.pinned) - Number(a.pinned) || b.updatedAt.localeCompare(a.updatedAt))
  const exportChat = async (chat: Chat) => {
    if (exporting) return
    setExporting(true); setExportError('')
    try {
      const references = [...new Map([...(chat.documents ?? []), ...(chat.attachments ?? [])].filter(file => file.document).map(file => [file.document!.id, file.document!])).values()]
      const objects: Record<string, unknown> = {}
      let bytes = new TextEncoder().encode(JSON.stringify(chat)).length
      for (const ref of references) {
        const object = await readDocumentObject(ref.id)
        bytes += new TextEncoder().encode(JSON.stringify(object)).length
        if (bytes > 128 * 1024 * 1024) throw new Error(msg('This chat exceeds the 128 MiB export limit.'))
        objects[ref.id] = object
      }
      const output = references.length ? { version: 1, chat, documentObjects: objects } : chat
      const url = URL.createObjectURL(new Blob([JSON.stringify(output)], { type: 'application/json' }))
      const link = document.createElement('a'); link.href = url; link.download = `chat-${chat.id}.json`; link.click()
      setTimeout(() => URL.revokeObjectURL(url), 0)
    } catch (cause) { setExportError((cause as Error).message) }
    finally { setExporting(false) }
  }
  return <section className={styles.historyBody} data-compact={compact || undefined} aria-label={msg("Chat history")}>
    {exportError && <p role="alert">{systemMessage(exportError)}</p>}
    {exporting && <p role="status">{msg("Preparing export…")}</p>}
    {error && <div role="alert"><p>{systemMessage(error)}</p><Button onClick={() => ready ? store?.retrySave() : void store?.load()}>{msg("Retry")}</Button></div>}
    {!ready && !error && <p role="status">{msg("Loading chat history…")}</p>}
    <div className={styles.historySearch}>
    <Input autoFocus aria-label={msg("Search chats")} value={query} onChange={event => setQuery(event.target.value)} placeholder={msg("Search chats…")} />
    <div className={styles.historyFilters}>
      {packId && <label className="checkbox"><Message text={"<0/> All packs"} slots={[<input type="checkbox" checked={all} onChange={event => setAll(event.target.checked)} />]} /></label>}
      <label className="checkbox"><Message text={"<0/> Include archived"} slots={[<input type="checkbox" checked={archived} onChange={event => setArchived(event.target.checked)} />]} /></label>
    </div>
    </div>
    <ul className={styles.history}>
      {shown.map((chat, index) => <Fragment key={chat.id}>{(index === 0 || historyGroup(shown[index - 1]!) !== historyGroup(chat)) && <li className={styles.historyGroup}>{historyGroup(chat)}</li>}<li data-current={activeId === chat.id || undefined}>
        {rename === chat.id ? <form className={styles.historyRename} onSubmit={event => { event.preventDefault(); if (name.trim()) { store?.update(chat.id, { title: name.trim(), titleEdited: true }); setRename(null) } }}>
          <Input aria-label={msg("Chat name")} value={name} autoFocus maxLength={120} onChange={event => setName(event.target.value)} /><Button type="submit" disabled={!name.trim()}>{msg("Save")}</Button><Button variant="quiet" onClick={() => setRename(null)}>{msg("Cancel")}</Button>
        </form> : <>
          <OverflowTooltip content={chatTitle(chat)} selector="[data-chat-title]"><button type="button" className={styles.historyOpen} aria-current={activeId === chat.id ? 'true' : undefined} onClick={() => { navigate(chatHref(chat, location)); onNavigate?.() }}>
            <span data-chat-title>{chat.pinned ? msg("Pinned · ") : ''}{chatTitle(chat)}</span><small>{chat.pack?.id ?? (chat.checkpoint?.state.candidates.length ? msg("Draft") : msg("Conversation"))} · {attention(bindings.get(chat.id)?.state.status ?? chat.checkpoint?.state.status) || formatDate(new Date(chat.updatedAt))}{chat.archived ? msg(" · Archived") : ''}</small>
          </button></OverflowTooltip>
          <DropdownMenu.Root open={actionChat === chat.id} onOpenChange={open => setActionChat(open ? chat.id : null)}><Tooltip content={msg("Chat actions")} openOnFocus={false} disabled={actionChat === chat.id}><DropdownMenu.Trigger className={`desk-icon-button ${styles.historyActions}`} aria-label={msg("Actions for {{value0}}", { value0: chatTitle(chat) })}><IconMore /></DropdownMenu.Trigger></Tooltip>
            <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" side="bottom" align="end" sideOffset={6} collisionPadding={16} onCloseAutoFocus={event => { if (rename || deleting) event.preventDefault() }}>
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => { setRename(chat.id); setName(chat.title) }}>{msg("Rename")}</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => store?.update(chat.id, { pinned: !chat.pinned })}>{chat.pinned ? msg("Unpin") : msg("Pin")}</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => store?.update(chat.id, { archived: !chat.archived })}>{chat.archived ? msg("Unarchive") : msg("Archive")}</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" disabled={exporting} onSelect={() => void exportChat(chat)}>{msg("Export chat")}</DropdownMenu.Item>
              <DropdownMenu.Item className="desk-menu-item" disabled={bindings.get(chat.id)?.run?.running} onSelect={() => setDeleting(chat.id)}>{msg("Delete chat…")}</DropdownMenu.Item>
            </DropdownMenu.Content></DropdownMenu.Portal>
          </DropdownMenu.Root>
        </>}
        {deleting === chat.id && <div className={styles.confirm}><p>{msg("Delete this chat? Its pack and research files will remain.")}</p><Button variant="danger" onClick={() => { const current = location.pathname === `/chats/${chat.id}` || homeChatId(location.state) === chat.id || new URLSearchParams(location.search).get('chat') === chat.id; store?.remove(chat.id); setDeleting(null); if (current) { navigate(chat.pack ? `/packs/${encodeURIComponent(chat.pack.id)}` : '/'); onNavigate?.() } }}>{msg("Delete chat")}</Button><Button onClick={() => setDeleting(null)}>{msg("Cancel")}</Button></div>}
      </li></Fragment>)}
    </ul>
    {ready && !shown.length && <p className={styles.historyEmpty}>{query || archived || packId ? msg("No chats match this search.") : msg("Your conversations will appear here after you send a message.")}</p>}
    {store && !store.canCreate && <p className={styles.caption}>{msg("Chat history is full. Export and delete an older chat to start another.")}</p>}
  </section>
}
