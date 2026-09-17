import { msg, useLocale } from '../i18n'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useEditing } from '../packs/edit/editingContext'
import type { BufferIdentity } from '../packs/edit/useDocumentBuffer'
import { applyProposal } from '../assistant/acceptProposal'
import { diffProposal } from '../assistant/proposalDiff'
import { ProposalDiffView } from '../assistant/ProposalDiff'
import { AssistantPane } from '../assistant/AssistantPane'
import { useInspectorSlot } from '../shell/InspectorSlot'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { ChatPanel } from './ChatPanel'
import { useChats } from './ChatProvider'
import { chatHref } from './ChatHistory'
import styles from './ChatWorkspace.module.css'

export function PackAssistant({ packId, path, digest, draft, editing, identity, busy, diagnostics, onEdit }: {
  packId: string; path?: string; digest?: string; draft?: string; editing: boolean; identity?: BufferIdentity; busy: () => string; diagnostics: { count: number; bytes: string } | undefined; onEdit?: () => void
}) {
  useLocale()
  const { store, ready, chats, drafts, bindings } = useChats()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const slot = useInspectorSlot()
  const session = useEditing()
  const explicit = params.get('chat')
  const available = [...chats, ...drafts]
  const chat = (explicit ? available.find(chat => chat.id === explicit && chat.pack?.id === packId) : undefined)
    ?? available.filter(chat => chat.pack?.id === packId && !chat.archived).sort((a,b) => b.updatedAt.localeCompare(a.updatedAt))[0]
  const creating = useRef(false)
  useEffect(() => {
    if (!store || !ready) return
    if (chat) { creating.current = false; store.activate(chat.id) }
    else if (slot.open && store.canCreate && !creating.current) {
      creating.current = true
      const next = store.startChat({ id: packId, path: path ?? '', digest: digest ?? '' })
      navigate(chatHref(next), { replace: true })
    }
  }, [store, ready, chat?.id, packId, path, digest, slot.open, navigate])
  const [baseline, setBaseline] = useState<{ chatId: string; bytes: string; revision: number; identity?: BufferIdentity; fromView?: boolean; path?: string } | null>(null)
  useEffect(() => {
    if (editing && baseline?.fromView && baseline.chatId === chat?.id && baseline.path === path && baseline.bytes === draft && identity) {
      setBaseline({ ...baseline, identity, fromView: false })
    }
  }, [editing, baseline, chat?.id, path, draft, identity])
  const [accepted, setAccepted] = useState('')
  const candidate = chat ? bindings.get(chat.id)?.state.candidates.at(-1) : undefined
  const state = chat ? bindings.get(chat.id)?.state : undefined
  const proposed = baseline?.chatId === chat?.id && candidate && candidate.revision > baseline.revision && state?.status !== 'running' ? candidate.document : undefined
  const diff = useMemo(() => proposed === undefined ? null : diffProposal(baseline?.bytes, proposed), [baseline?.bytes, proposed])
  const unchanged = !!baseline && baseline.bytes === draft && baseline.identity?.path === identity?.path && baseline.identity?.generation === identity?.generation && baseline.identity?.revision === identity?.revision
  const eligible = editing && session.editing && unchanged && !busy() && session.pending.size === 0 && !state?.restored && state?.status !== 'running' && proposed !== undefined
  // Standalone embeddings retain the existing assistant. The application
  // provides the project store above all routes.
  if (!store) return <AssistantPane draft={draft} editing={editing} identity={identity} busy={busy} diagnostics={diagnostics} />
  if (!chat) return <p className={styles.blank}>{ready ? !store.canCreate ? msg("Chat history is full. Export and delete an older chat from Chat history.") : msg("Open Assistant to start a conversation about this pack.") : msg("Loading chats…")}</p>
  const actions = proposed === undefined ? undefined : <Disclosure title={msg("Review proposed changes")}>
    {diff && <ProposalDiffView diff={diff} onBaseline={unchanged} />}
    {!editing && <><p className={styles.caption}>{msg("Open Edit to review and apply this proposal. Your pack is unchanged until Save.")}</p>{onEdit && <Button onClick={onEdit}>{msg("Review in Edit")}</Button>}</>}
    {editing && !unchanged && <p className={styles.caption}>{msg("The document changed since this request. Send another message against the current draft.")}</p>}
    <Button disabled={!eligible || accepted === candidate?.digest} onClick={() => {
      if (!eligible || proposed === undefined || !candidate) return
      session.write(current => current.text === baseline?.bytes ? applyProposal(current,proposed) : current, { coalesceKey: `chat-accept:${chat.id}:${candidate.digest}` })
      setAccepted(candidate.digest)
    }}>{accepted === candidate?.digest ? msg("Applied to draft") : msg("Apply to draft")}</Button>
    <p className={styles.caption}>{msg("Review the changes in the main pane, then use Save. Applying a proposal is one undo step.")}</p>
  </Disclosure>
  return <ChatPanel placement="pane" key={chat.id} chat={chat} proposalActions={actions} context={draft === undefined ? undefined : { text: draft, beforeSend: () => { if (baseline?.chatId === chat.id && baseline.bytes === draft && (unchanged || baseline.fromView && !editing)) return; setBaseline({ chatId: chat.id, bytes: draft, revision: candidate?.revision ?? 0, identity, fromView: !editing, path }); setAccepted('') } }} locked={Boolean(busy())} />
}
