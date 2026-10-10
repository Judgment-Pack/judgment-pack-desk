import { useEffect, useMemo, useRef, useState } from 'react'
import { msg, systemMessage } from '../i18n'
import { useChats } from '../chat/ChatProvider'
import { ChatPanel } from '../chat/ChatPanel'
import { useInspectorControls } from '../shell/InspectorSlot'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { diffProposal } from '../assistant/proposalDiff'
import { ProposalDiffView } from '../assistant/ProposalDiffView'
import type { GraphProposal } from './author'
import styles from './GraphWorkspace.module.css'

/** Same project conversations and ChatPanel as packs; never bind a graph as a pack. */
export function GraphAssistant({ proposal, workspace, chatId, onChat, onApply, busy, selection }: {
  proposal: GraphProposal; workspace: string; chatId?: string; onChat: (id: string) => void
  onApply: (next: Partial<GraphProposal>) => void; busy: boolean; selection: string
}) {
  const {store, ready, chats, drafts, bindings, error} = useChats()
  const slot = useInspectorControls()
  const available = [...chats, ...drafts]
  const chat = available.find(item => item.id === chatId && !item.pack && !item.draftId && (!item.graph || item.graph.workspace === workspace))
    ?? available.filter(item => item.graph?.workspace === workspace && !item.archived).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))[0]
  const creating = useRef(false)
  useEffect(() => {
    if (!store || !ready) return
    if (chat) {
      creating.current = false
      // A graph draft may originate in an unscoped home conversation. Bind it
      // before another turn so New chat and candidate persistence stay graph-scoped.
      if (!chat.graph) store.update(chat.id, {graph: {id: proposal.id, path: proposal.path, workspace}})
      store.activate(chat.id); onChat(chat.id)
    }
    else if (slot.open && store.canCreate && !creating.current) {
      creating.current = true
      const next = store.startChat(undefined, 'draft', false, undefined, {id: proposal.id, path: proposal.path, workspace})
      onChat(next.id)
    }
  }, [store, ready, chat?.id, slot.open, proposal.id, proposal.path, workspace, onChat])
  const [baseline, setBaseline] = useState<{chatId: string; proposal: string; content: string; drafts: string[]}>()
  const [accepted, setAccepted] = useState('')
  const state = chat ? bindings.get(chat.id)?.state : undefined
  const candidate = baseline?.chatId === chat?.id ? chat?.graphDrafts?.filter(item => !baseline.drafts.includes(item.draftId) && !item.saved).at(-1) : undefined
  const unchanged = baseline?.proposal === JSON.stringify(proposal)
  // A new conversation or restored proposal has no send-time baseline, so it cannot apply.
  const eligible = !!candidate && unchanged && !busy && state?.status !== 'running' && !state?.restored
    && (!proposal.baseSha256 || candidate.id === proposal.id && candidate.path === proposal.path)
  const document = useMemo(() => {try {return candidate ? JSON.parse(candidate.content) : undefined} catch {return undefined}}, [candidate?.content])
  const diff = useMemo(() => document === undefined ? null : diffProposal(baseline?.content, document), [baseline?.content, document])
  if (error) return <div className={styles.feedback} role="alert"><p>{systemMessage(error)}</p><Button onClick={() => void store?.load()}>{msg('Retry')}</Button></div>
  if (!chat) return <p className={styles.feedback}>{ready ? msg('Open Assistant to discuss this graph.') : msg('Loading chats…')}</p>
  const actions = candidate && state?.status !== 'running' ? <Disclosure title={msg('Review proposed changes')}>
    {diff && <ProposalDiffView diff={diff} onBaseline={unchanged}/>}
    {!unchanged && <p>{msg('The document changed since this request. Send another message against the current draft.')}</p>}
    <Button disabled={!eligible || document === undefined || accepted === candidate.draftId} onClick={() => {
      if (!eligible || document === undefined) return
      onApply({content: candidate.content, ...(!proposal.baseSha256 ? {id: candidate.id, path: candidate.path, description: candidate.description} : {})})
      setAccepted(candidate.draftId)
    }}>{accepted === candidate.draftId ? msg('Applied to draft') : msg('Apply to draft')}</Button>
    <p>{msg('Review the draft, then use Review & save. Applying a proposal is one undo step.')}</p>
  </Disclosure> : undefined
  const contextKind = 'graph' as const
  return <div className={styles.assistant}>
    <p className={styles.context}>{proposal.id || msg('New graph')}{selection ? ` · ${selection}` : ''}</p>
    <ChatPanel key={chat.id} chat={chat} placement="pane" locked={busy} proposalActions={actions}
      context={{kind: contextKind, text: JSON.stringify({id: proposal.id, path: proposal.path, selection, content: proposal.content}), beforeSend: () => {
        setBaseline({chatId: chat.id, proposal: JSON.stringify(proposal), content: proposal.content, drafts: (chat.graphDrafts ?? []).map(item => item.draftId)}); setAccepted('')
      }}}/>
  </div>
}
