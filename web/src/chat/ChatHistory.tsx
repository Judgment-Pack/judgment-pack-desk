import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage, formatDate } from '../i18n'
import { Fragment, useRef, useState, type RefObject, type ReactNode } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { VisuallyHidden } from 'radix-ui'
import { Menu as DropdownMenu } from '../ui/Menu'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { OverflowTooltip, Tooltip } from '../ui/Tooltip'
import { Popover } from '../ui/Popover'
import { IconHistory, IconMore, IconPlus, IconPaperclip } from '../shell/icons'
import { useChats } from './ChatProvider'
import type { Chat } from './store'
import { readDocumentObject } from '../documents/client'
import { assistantChatHref, chatHref, chatTitle, hasChatContent, homeChatId, chatScope, inChatScope, type ChatScope } from './navigation'
export { chatHref } from './navigation'
import styles from './ChatWorkspace.module.css'

/** The same compact controls are portalled into main or the Assistant title bar. */
export function ChatToolbar({ chat, history, onHistory, onBack, onNew, historyRef, actions = [], onSources, loading = false }: {
  actions?: { label: string; icon: ReactNode; onSelect: (opener: HTMLButtonElement | null) => void }[]; loading?: boolean
  onSources?: (opener: HTMLButtonElement | null) => void
  chat: Chat; history: boolean; onHistory: () => void; onBack: () => void; onNew: () => void
  historyRef: RefObject<HTMLButtonElement | null>
}) {
  useLocale()
  const { store, bindings } = useChats()
  const optionsButton = useRef<HTMLButtonElement>(null)
  const startAfterClose = useRef(false)
  const pendingAction = useRef<((opener: HTMLButtonElement | null) => void) | null>(null)
  const state = bindings.get(chat.id)?.state ?? chat.checkpoint?.state
  const empty = !chat.composer.trim() && !chat.attachments?.length && !state?.turns.length
  return <div className={styles.chatToolbar} role="group" aria-label={msg("Chat actions")}>
    <Popover title={msg("Chat history")} variant="list" triggerTooltip={msg("Chat history")} open={history} onOpenChange={open => open ? onHistory() : onBack()}
      onCloseAutoFocus={event => {
        if (startAfterClose.current) { startAfterClose.current = false; event.preventDefault(); onNew() }
      }}
      onEscapeKeyDown={event => event.stopPropagation()}
      trigger={<button ref={historyRef} type="button" className="desk-icon-button" aria-label={msg("Chat history")} aria-pressed={history} aria-busy={loading || undefined} disabled={loading}>{loading ? <span className={styles.historySpinner} aria-hidden="true" /> : <IconHistory />}</button>}>
      <div className={styles.historyCreate}><Button variant="quiet" disabled={loading || !store?.canCreate || empty} onClick={() => { startAfterClose.current = true; onBack() }}><IconPlus />{msg('New chat')}</Button></div>
      <ChatHistoryList scope={chatScope(chat)} activeId={chat.id} onNavigate={onBack} compact />
    </Popover>
    {(onSources || actions.length > 0) && <DropdownMenu.Root>
      <Tooltip content={msg('Chat options')}><DropdownMenu.Trigger asChild><button ref={optionsButton} type="button" className="desk-icon-button" aria-label={msg('Chat options')}><IconMore /></button></DropdownMenu.Trigger></Tooltip>
      <DropdownMenu.Portal><DropdownMenu.Content align="end" onCloseAutoFocus={event => {
        if (pendingAction.current) {
          const run = pendingAction.current
          pendingAction.current = null
          event.preventDefault()
          run(optionsButton.current)
        }
      }}>
        {onSources && <DropdownMenu.Item icon={<IconPaperclip />} onSelect={() => { pendingAction.current = onSources }}>{msg('Chat sources')}</DropdownMenu.Item>}
        {actions.map(action => <DropdownMenu.Item key={action.label} icon={action.icon} onSelect={() => { pendingAction.current = action.onSelect }}>{action.label}</DropdownMenu.Item>)}
      </DropdownMenu.Content></DropdownMenu.Portal>
    </DropdownMenu.Root>}
    {loading && <VisuallyHidden.Root role="status">{msg("Loading chat history…")}</VisuallyHidden.Root>}
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
export function ChatHistoryList({ packId, scope: owner, activeId, onNavigate, compact = false }: {
  scope?: ChatScope; packId?: string; activeId?: string; onNavigate?: () => void; compact?: boolean
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
  const scope = owner ?? (packId ? {kind: 'pack' as const, id: packId} : undefined)
  const shown = chats.filter(chat => hasChatContent(chat) && (all || !scope || inChatScope(chat, scope)) && (archived || !chat.archived)
    && `${chatTitle(chat)} ${chat.composer} ${chat.pack?.id ?? chat.graph?.id ?? ''} ${chat.checkpoint?.state.turns.map(turn => turn.text).join(' ') ?? ''}`.toLowerCase().includes(query.toLowerCase()))
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
      {scope && <label className="checkbox"><Message text={"<0/> All chats"} slots={[<input type="checkbox" checked={all} onChange={event => setAll(event.target.checked)} />]} /></label>}
      <label className="checkbox"><Message text={"<0/> Include archived"} slots={[<input type="checkbox" checked={archived} onChange={event => setArchived(event.target.checked)} />]} /></label>
    </div>
    </div>
    <ul className={styles.history}>
      {shown.map((chat, index) => <Fragment key={chat.id}>{(index === 0 || historyGroup(shown[index - 1]!) !== historyGroup(chat)) && <li className={styles.historyGroup}>{historyGroup(chat)}</li>}<li data-current={activeId === chat.id || undefined}>
        {rename === chat.id ? <form className={styles.historyRename} onSubmit={event => { event.preventDefault(); if (name.trim()) { store?.update(chat.id, { title: name.trim(), titleEdited: true }); setRename(null) } }}>
          <Input aria-label={msg("Chat name")} value={name} autoFocus maxLength={120} onChange={event => setName(event.target.value)} /><Button type="submit" disabled={!name.trim()}>{msg("Save")}</Button><Button variant="quiet" onClick={() => setRename(null)}>{msg("Cancel")}</Button>
        </form> : <>
          <OverflowTooltip content={chatTitle(chat)} selector="[data-chat-title]"><button type="button" className={styles.historyOpen} aria-current={activeId === chat.id ? 'true' : undefined} onClick={() => { navigate(scope && inChatScope(chat, scope) ? assistantChatHref(chat,location) : chatHref(chat)); onNavigate?.() }}>
            <span data-chat-title>{chat.pinned ? msg("Pinned · ") : ''}{chatTitle(chat)}</span><small>{chat.pack?.id ?? chat.graph?.id ?? (chat.checkpoint?.state.candidates.length ? msg("Draft") : msg("Conversation"))} · {attention(bindings.get(chat.id)?.state.status ?? chat.checkpoint?.state.status) || formatDate(new Date(chat.updatedAt))}{chat.archived ? msg(" · Archived") : ''}</small>
          </button></OverflowTooltip>
          <DropdownMenu.Root open={actionChat === chat.id} onOpenChange={open => setActionChat(open ? chat.id : null)}><Tooltip content={msg("Chat actions")} openOnFocus={false} disabled={actionChat === chat.id}><DropdownMenu.Trigger className={`desk-icon-button ${styles.historyActions}`} aria-label={msg("Actions for {{value0}}", { value0: chatTitle(chat) })}><IconMore /></DropdownMenu.Trigger></Tooltip>
            <DropdownMenu.Portal><DropdownMenu.Content side="bottom" align="end" sideOffset={6} collisionPadding={16} onCloseAutoFocus={event => { if (rename || deleting) event.preventDefault() }}>
              <DropdownMenu.Item onSelect={() => { setRename(chat.id); setName(chat.title) }}>{msg("Rename")}</DropdownMenu.Item>
              <DropdownMenu.Item onSelect={() => store?.update(chat.id, { pinned: !chat.pinned })}>{chat.pinned ? msg("Unpin") : msg("Pin")}</DropdownMenu.Item>
              <DropdownMenu.Item onSelect={() => store?.update(chat.id, { archived: !chat.archived })}>{chat.archived ? msg("Unarchive") : msg("Archive")}</DropdownMenu.Item>
              <DropdownMenu.Item disabled={exporting} onSelect={() => void exportChat(chat)}>{msg("Export chat")}</DropdownMenu.Item>
              <DropdownMenu.Item disabled={bindings.get(chat.id)?.run?.running} onSelect={() => setDeleting(chat.id)}>{msg("Delete chat…")}</DropdownMenu.Item>
            </DropdownMenu.Content></DropdownMenu.Portal>
          </DropdownMenu.Root>
        </>}
        {deleting === chat.id && <div className={styles.confirm}><p>{msg("Delete this chat? Its pack and research files will remain.")}</p><Button variant="danger" onClick={() => { const current = location.pathname === `/chats/${chat.id}` || homeChatId(location.state) === chat.id || new URLSearchParams(location.search).get('chat') === chat.id; store?.remove(chat.id); setDeleting(null); if (current) { if (scope && inChatScope(chat, scope)) { const params = new URLSearchParams(location.search); params.delete('chat'); navigate(`${location.pathname}${params.size ? '?' + params : ''}`) } else navigate(chat.pack ? `/packs/${encodeURIComponent(chat.pack.id)}` : '/'); onNavigate?.() } }}>{msg("Delete chat")}</Button><Button onClick={() => setDeleting(null)}>{msg("Cancel")}</Button></div>}
      </li></Fragment>)}
    </ul>
    {ready && !shown.length && <p className={styles.historyEmpty}>{query || archived || scope ? msg("No chats match this search.") : msg("Your conversations will appear here after you send a message.")}</p>}
    {store && !store.canCreate && <p className={styles.caption}>{msg("Chat history is full. Export and delete an older chat to start another.")}</p>}
  </section>
}
