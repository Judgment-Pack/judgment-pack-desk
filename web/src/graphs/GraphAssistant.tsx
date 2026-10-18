import {samePolicySources,validPolicySources,type PolicyAuthoringSource} from '../policies/authoringSource'
import {ChatPlaceholder} from '../chat/ChatPlaceholder'
import { useEffect, useMemo, useRef, useState } from 'react'
import { msg } from '../i18n'
import { useGraphMatrix } from '../mcp/queries'
import { useChats } from '../chat/ChatProvider'
import { ChatPanel } from '../chat/ChatPanel'
import { useInspectorControls } from '../shell/InspectorSlot'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { diffProposal } from '../assistant/proposalDiff'
import { ProposalDiffView } from '../assistant/ProposalDiffView'
import type { GraphProposal } from './author'
import {graphDraftWorkspaceMatches,type GraphDraftIdentity} from './identity'
import styles from './GraphWorkspace.module.css'

/** Same project conversations and ChatPanel as packs; never bind a graph as a pack. */
export function GraphAssistant({ proposal, workspace, chatId, onChat, onApply, busy, selection, openDraftId,policySources=[],draftIdentities=[] }: {
  proposal: GraphProposal; workspace: string;policySources?:PolicyAuthoringSource[]; openDraftId?: string; chatId?: string; onChat: (id: string) => void
  onApply?: (next: Partial<GraphProposal>) => void; busy: boolean; selection: string;draftIdentities?:GraphDraftIdentity[]
}) {
  const {store, ready, chats, drafts, bindings, error} = useChats()
  const slot = useInspectorControls()
  const tests = useGraphMatrix(proposal.baseSha256 ? proposal.id : undefined, false, false).data
  // Model reference material, not UI copy or a source of expected results.
  const testContext = useMemo(() => tests ? JSON.stringify({observedRun: tests, note: 'Cached observations may be historical. Compare their graph and pack digests before using them. Observations do not establish expected results.'}) : undefined, [tests])
  const available = [...chats, ...drafts]
  const basisValid=validPolicySources(policySources)
  const compatible=(item:typeof available[number])=>!item.policySources?.length||samePolicySources(item.policySources,policySources)
  const [originChatId,draftId]=workspace.split(':')
  const known=[...draftIdentities,...available.flatMap(item=>(item.graphDrafts??[]).map(d=>({originChatId:item.id,draftId:d.draftId})))]
  const matches=(item:typeof available[number])=>item.graph?.workspace===workspace||!!draftId&&graphDraftWorkspaceMatches({originChatId,draftId,id:proposal.id,path:proposal.path},item,known)
  const chat = available.find(item => compatible(item)&&item.id === chatId && !item.pack && !item.draftId && (!item.graph || matches(item)))
    ?? available.filter(item => compatible(item)&&item.graph?.workspace === workspace && !item.archived).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))[0]
  const creating = useRef(false)
  useEffect(() => {
    if (!store || !ready || !basisValid) return
    if (chat) {
      creating.current = false
      // A graph draft may originate in an unscoped home conversation. Bind it
      // before another turn so New chat and candidate persistence stay graph-scoped.
      if (!chat.graph||chat.graph.workspace!==workspace||!samePolicySources(policySources,chat.policySources??[])) store.update(chat.id, {graph: {id: proposal.id, path: proposal.path, workspace},...(policySources.length?{policySources}:{})})
      store.activate(chat.id); onChat(chat.id)
    }
    else if (slot.open && store.canCreate && !creating.current) {
      creating.current = true
      // Preserve incompatible historical chats and their unsent composers.
      const historical=available.some(item=>item.graph?.workspace===workspace&&!compatible(item))
      const next = historical ? store.create(undefined,'draft') : store.startChat(undefined, 'draft', false, undefined, {id: proposal.id, path: proposal.path, workspace})
      if(historical||policySources.length)store.update(next.id,{...(historical?{graph:{id:proposal.id,path:proposal.path,workspace}}:{}),...(policySources.length?{policySources}:{})})
      onChat(next.id)
    }
  }, [store, ready, chat?.id, slot.open, proposal.id, proposal.path, workspace, onChat,policySources,basisValid])
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
  if(!basisValid)return <p role="alert">{msg('Review the graph Policy sources before opening an assistant. The exact basis must contain at most eight distinct, nonconflicting sources.')}</p>
  if (!chat) return <ChatPlaceholder loading={!ready} error={!!error}>{msg('Open Assistant to discuss this graph.')}</ChatPlaceholder>
  const actions = onApply && candidate && state?.status !== 'running' ? <Disclosure title={msg('Review proposed changes')}>
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
    <ChatPanel key={chat.id} chat={chat} placement="pane" locked={busy} proposalActions={actions} openGraphDraftId={accepted || openDraftId}
      context={{kind: contextKind, tests: testContext, title: proposal.id || msg('New graph'), detail: selection ? msg('Graph · {{selection}}', { selection }) : msg('Graph'), text: JSON.stringify({id: proposal.id, path: proposal.path, selection, content: proposal.content}), beforeSend: () => {
        setBaseline({chatId: chat.id, proposal: JSON.stringify(proposal), content: proposal.content, drafts: (chat.graphDrafts ?? []).map(item => item.draftId)}); setAccepted('')
      }}}/>
  </div>
}
