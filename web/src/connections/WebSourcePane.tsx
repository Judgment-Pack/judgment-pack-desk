import { useEffect, useRef, useState, type FormEvent } from 'react'
import { createPortal } from 'react-dom'
import { useChats } from '../chat/ChatProvider'
import { useChatAttachments } from '../chat/useChatAttachments'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { validWebURL } from '../documents/record'
import { msg, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Alert } from '../ui/Alert'
import { useConnections } from './catalog'
import type { ConnectionPaneRequest } from './ConnectionPaneContext'
import styles from './ConnectionsPane.module.css'

/** Keep form and pending ingestion outside the portal when dock/drawer changes. */
export function WebSourcePane({ request, target, onClose, onAttached, onBusy }: {
 request: ConnectionPaneRequest; target: HTMLElement | null
 onClose: () => void; onAttached: () => void; onBusy: (chatId?: string) => void
}) {
 useLocale()
 const effective = useEffectiveConfig()
 const local = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
 const catalog = useConnections(local)
 const { store, chats, drafts, bindings } = useChats()
 const chat = [...chats, ...drafts].find(item => item.id === request.chatId)
 const running = Boolean(request.chatId && bindings.get(request.chatId)?.run?.running)
 const available = catalog.web && Boolean(effective.config.research.documents?.enabled)
 const upload = useChatAttachments(store, request.chatId ?? '', running || !available, effective.config.research)
 const [url, setURL] = useState(''), [invalid, setInvalid] = useState(false)
 const input = useRef<HTMLInputElement>(null)
 const capacity = Boolean(chat && (chat.attachments?.length ?? 0) < 4)
 useEffect(() => { input.current?.focus({ preventScroll: true }) }, [target])
 useEffect(() => { if (!chat || running) onClose() }, [Boolean(chat), running, onClose])
 useEffect(() => { onBusy(upload.reading ? request.chatId : undefined); return () => onBusy(undefined) }, [upload.reading, request.chatId, onBusy])
 async function attach(event: FormEvent) {
  event.preventDefault()
  if (!available || !capacity || upload.isReading()) return
  const chosen = url.trim()
  if (!validWebURL(chosen)) { setInvalid(true); input.current?.focus(); return }
  setInvalid(false)
  if (await upload.attachWeb(chosen)) onAttached()
 }
 const content = <form className={styles.pane} onSubmit={event => void attach(event)} aria-label={msg('Add link')}>
  <div className={styles.body}>
   <p>{msg('Attach a snapshot of a public web page, PDF, or text file. Nothing is sent until you send your message.')}</p>
   <label className={styles.linkLabel} htmlFor="web-source-url">{msg('Link')}</label>
   <Input ref={input} id="web-source-url" value={url} onChange={event => { setURL(event.target.value); setInvalid(false) }} disabled={upload.reading} inputMode="url" autoComplete="off" aria-invalid={invalid || undefined} aria-describedby="web-source-hint" />
   <p id="web-source-hint" className={styles.linkHint}>{msg('Public HTTPS links only, up to 4 MiB. Sign-in pages and JavaScript content are not supported.')}</p>
   {invalid && <Alert>{msg('Enter a public HTTPS link without a sign-in or fragment.')}</Alert>}
   {!capacity && <Alert>{msg('Attach up to four files at a time.')}</Alert>}
   {!available && !catalog.loading && <Alert>{msg('Local processing is unavailable. Check the details in Admin → Storage & data.')}</Alert>}
   {upload.error && <Alert>{upload.error}</Alert>}
  </div>
  <div className={styles.footer}>
   {upload.reading && <p role="status">{upload.progress}</p>}
   <div className={styles.actions}>
    <Button type="button" variant="quiet" onClick={() => { upload.cancel(); onClose() }}>{msg('Cancel')}</Button>
    <Button type="submit" variant="primary" disabled={!url.trim() || !available || !capacity || upload.reading}>{msg('Attach link')}</Button>
   </div>
  </div>
 </form>
 return target ? createPortal(content, target) : null
}
