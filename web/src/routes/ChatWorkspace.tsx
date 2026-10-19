import { sourceHandoff } from '../packs/drafts/sourceHandoff'
import { DefinitionHistory } from '../definitions/DefinitionHistory'
import { DraftCheckStatus } from '../research/DraftCheckStatus'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { usePackChatContext } from '../chat/packChatContext'
import { DefinitionBrief } from '../briefs/DefinitionBrief'
import { TestsContent, carryDraftTests } from '../packs/test-workspace/TestsWorkspace'
import { useReadingDetails } from '../chat/ReadingDetails'
import { DraftActions } from '../packs/drafts/DraftActions'
import { systemMessage } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { msg, useLocale } from '../i18n'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ChatPanel } from '../chat/ChatPanel'
import { useChats } from '../chat/ChatProvider'
import { draftHref, type PackDraft } from '../packs/drafts/model'
import type { ResearchRunBinding } from '../research/useResearchRun'
import { ShowFolders } from '../packs/folders/FolderBrowser'
import { usePackFolders } from '../packs/folders/FolderContext'
import { packFolder } from '../packs/folders/model'
import { homeChatId } from '../chat/navigation'
import type { Chat } from '../chat/store'
import { INITIAL_STATE, canCreateDraft, matrixDocument, researchRecord } from '../research/run'
import { DefinitionUsage } from '../usage/DefinitionUsage'
import { DraftTabs, type Selection } from '../research/ui/DraftPanels'
import { SourceInspector } from '../research/ui/SourceInspector'
import { useDetailsPortal, useDetailsSlot } from '../shell/DetailsSlot'
import { useInspectorPortal, useInspectorControls } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useShellState } from '../shell/paneState'
import { IconFocus } from '../shell/icons'
import { Tooltip } from '../ui/Tooltip'
import { CreatePackDialog } from '../shell/CreatePackDialog'
import { Button } from '../ui/Button'
import { PageHeader } from '../ui/PageLayout'
import styles from '../chat/ChatWorkspace.module.css'

/**
 * Create's rule for a chat's draft: the one rule every mode reads, at rest on
 * a fresh check, on the pin and documents standing now (`basis`).
 */
export function draftReady(chat: Chat, state: typeof INITIAL_STATE, basis: string | undefined): boolean {
  if (state.restored || state.status !== 'ready') return false
  return canCreateDraft(state, chat.mode, basis)
}

// Presentation only: never activated, updated or persisted in ChatStore.
const LOADING_CHAT: Chat = { id: 'loading-history', title: 'New chat', pinned: false,
  archived: false, updatedAt: '', composer: '', model: '', mode: 'draft', view: 'chat' }

export function ChatWorkspace() {
  useLocale()
  const { chatId: linkedChatId } = useParams()
  const location = useLocation()
  const home = location.pathname === '/'
  const chatId = linkedChatId ?? (home ? homeChatId(location.state) : undefined)
  const [params] = useSearchParams()
  const requestedFolder = params.get('folder')
  const targetFolderId = !home && requestedFolder && /^[a-zA-Z0-9-]{1,80}$/.test(requestedFolder) ? requestedFolder : undefined
  const initialMode = params.get('mode') === 'research' ? 'web-research' : undefined
  const { store, chats, drafts, ready, error } = useChats()
  const navigate = useNavigate()
  const created = useRef<string | null>(null)
  useLayoutEffect(() => {
    if (!store || !ready) return
    if (!chatId || (home && ![...chats, ...drafts].some(chat => chat.id === chatId))) {
      if (!store.canCreate) return
      if (!created.current || ![...chats, ...drafts].some(chat => chat.id === created.current)) {
        created.current = store.startChat(undefined, initialMode).id
        if (targetFolderId) store.update(created.current, { targetFolderId })
      }
      navigate('/', { replace: true, state: { homeChatId: created.current } })
    } else { created.current = null; store.activate(chatId) }
  }, [store, ready, chatId, navigate, initialMode, home, chats, drafts, targetFolderId])
  const selectedChat = chats.find(chat => chat.id === chatId) ?? drafts.find(chat => chat.id === chatId)
  // The logo clears route state. Reuse the home draft during that render,
  // before navigation installs its id again, so the panel does not unmount.
  const chat = selectedChat ?? (home && ready && store?.canCreate
    ? drafts.find(chat => !chat.pack && !chat.graph && !chat.input && !chat.draftId && (!initialMode || chat.mode === initialMode)) : undefined)
  const loading = !error && (!ready || !chat && (!chatId || home) && !!store?.canCreate)
  const loadingChat = useMemo(() => ({ ...LOADING_CHAT, mode: initialMode ?? 'draft' } as Chat), [initialMode])
  const [chatHeaderTarget, setChatHeaderTarget] = useState<HTMLDivElement | null>(null)
  return <div data-measure="wide" data-layout="page">
    <header role="presentation" data-page-header className={styles.workspaceHeader}><div className={styles.headerControls} ref={setChatHeaderTarget} /></header>
    {loading || ready && chat
      ? <ChatPanel key={chat?.id ?? loadingChat.id} chat={chat ?? loadingChat} landing={!!chat || home}
          historyLoading={loading} locked={loading} headerTarget={chatHeaderTarget}
          onOpenDraft={chat?.draftId ? () => navigate(draftHref(chat.draftId!)) : undefined} />
      : <div className={styles.blank} role="status">{error ? msg('Chat history unavailable') : ready && chatId && !home
          ? msg("This chat is no longer in history.") : msg("Chat history is full. Export and delete an older chat from Chat history.")}
          {ready && chatId && !home && <Button onClick={() => navigate('/')}>{msg("New chat")}</Button>}
        </div>}
  </div>
}

export function DraftWorkspace({ chat, artifact, fallback }: { chat: Chat; artifact: PackDraft; fallback: ResearchRunBinding }) {
  const [testParams] = useSearchParams()
  const locale = useLocale()
  const { store, bindings } = useChats()
  const live = bindings.get(chat.id)
  const binding = live && chat.draftGeneration===artifact.generation && live.state.candidates.at(-1)?.text === artifact.checkpoint.state.candidates.at(-1)?.text ? live : fallback
  const state = binding.state
  const readSource = useReadingDetails(artifact.id)
  const folders = usePackFolders()
  const folderId = folders ? packFolder(folders.document,artifact.id) : artifact.folderId
  const [selection, setSelection] = useState<Selection>(null)
  const [review, setReview] = useState(false)
  const [testActive, setTestActive] = useState(false)
  const reviewDigest = useRef<string | undefined>(undefined)
  const [reviewNotice, setReviewNotice] = useState('')
  const [writing, setWriting] = useState(false)
  const writingRef = useRef(false)
  const writingChanged = useCallback((value: boolean) => { writingRef.current = value; setWriting(value) }, [])
  useDirtyGuard(false, msg('Saving…'), { busy: writing, shouldBlock: () => true })
  const shell = useShellState()
  const width = shell.inspectorWidth ?? 400
  const [rightOpen, setRightOpen] = useState(true)
  const assistant = useInspectorControls()
  const details = useDetailsSlot()
  const draft = true
  const latest = state.candidates.at(-1)
  const basis = binding.run?.basisNow()
  const passing = draftReady(chat,state,basis) && !store?.getSnapshot().dirty && !store?.getSnapshot().error
  const beginReview = () => { reviewDigest.current = latest?.digest; setReviewNotice(''); setReview(true) }
  useEffect(() => {
    if (review && latest?.digest !== reviewDigest.current) { setReview(false); setReviewNotice(sourceMessage('The draft changed. Review the latest revision before creating it.')) }
  }, [review, latest?.digest])
  const presentation = useMemo(() => ({ title: msg("Assistant"), contextTitle: artifact.title, workspaceTools: draft, available: draft, open: draft && rightOpen,
    onOpenChange: setRightOpen, width, onResize: shell.resizeInspector, onReset: shell.resetInspectorWidth, minimumMainWidth: 480, maximumWidth: 640 }), [draft, rightOpen, width, shell.resizeInspector, shell.resetInspectorWidth, locale, artifact.title])
  useInspectorPresentation(presentation)
  const chatContext = usePackChatContext(artifact.id, latest?.text, artifact.title, true)
  const portal = useInspectorPortal(<div className={styles.retainedChat}><ChatPanel placement="pane" chat={chat} context={chatContext} locked={writing} draftVisible draftActionsInHeader /></div>)
  const detailPortal = useDetailsPortal(binding?.ledger && selection && !testActive ?
    <SourceInspector selection={selection} ledger={binding.ledger} state={state} onSelect={setSelection} /> : null)
  const select = (next: Selection) => { setSelection(next); if (next) details.reveal() }
  const candidate = latest?.document as { title?: unknown; description?: unknown; decision?: { question?: unknown } } | undefined
  // A ready draft has established cases in every mode, and the created pack
  // carries them as its matrix rows beside the record of what they rest on.
  const research = latest && binding?.ledger && passing ? {
    document: latest.document, name: typeof candidate?.title === 'string' ? candidate.title : '',
    description: typeof candidate?.description === 'string' ? candidate.description : '', unknowns: state.unknowns,
    matrix: matrixDocument(state,binding.ledger), research: {...researchRecord(state,binding.ledger,'') as Record<string, unknown>, authoringSources: sourceHandoff(artifact)}
  } : undefined
  return <div className={styles.packWorkspace} data-draft-workspace>
    <PageHeader variant="title" leading={<ShowFolders/>} title={artifact.title} meta={<>{msg('Draft')} · {latest ? msg('revision {{value0}}', { value0: latest.revision }) : msg('no revision yet')}</>} actions={<>
      <DraftCheckStatus state={state}/>
      <DraftActions draft={artifact} sourceText={latest?.text} recheck={{run: () => { if(store)store.perform(chat.id,active=>active.run?.recheck(),false);else binding.run?.recheck() }, disabled: state.status==='running' || writing || !binding.run || Boolean(store?.running && store.running!==chat.id)}}/>
      <Tooltip content={msg('Focus on canvas')}><Button size="icon" variant="quiet" aria-label={msg('Focus on canvas')} onClick={() => assistant.close?.()}><IconFocus /></Button></Tooltip>
      {!review && <Tooltip content={state.restored ? msg('Run checks from Pack options before finalizing.') : undefined}><Button variant="primary" disabled={!passing} onClick={beginReview}>{msg('Review and finalize')}</Button></Tooltip>}
    </>} />
    {portal}{detailPortal}
    {draft && reviewNotice && <p className={styles.reviewNotice} role="status">{systemMessage(reviewNotice)}</p>}
    <div className={styles.workspace}>
      {review && latest ? <div className={styles.review}>
        <CreatePackDialog open presentation="review" submitLabel={msg("Finalize pack")} onOpenChange={open => { if (!open && !writing) setReview(false) }}
          onWritingChange={writingChanged}
          initialFolderId={folderId}
          onFolderChange={targetFolderId => store?.update(chat.id,{targetFolderId})}
          canCreate={() => { const snapshot=store?.getSnapshot(); const current=snapshot?.packDrafts.find(item=>item.id===artifact.id); const active=binding.run?.getSnapshot(); return !snapshot?.dirty && !snapshot?.error && current?.checkpoint.state.candidates.at(-1)?.text===active?.candidates.at(-1)?.text && !!active && active.candidates.at(-1)?.digest===reviewDigest.current && draftReady(chat,active,binding.run?.basisNow()) }}
          reviewDraft={{ trialCount: state.probes?.length ?? 0, caseCount: state.cases.length, document: latest.document, name: artifact.title, description: typeof candidate?.description === 'string' ? candidate.description : '', unknowns: state.unknowns, research,
            citations: { traced: state.citations.filter(c=>c.traced).length, total: state.citations.length },
            cases: { agreeing: latest.check?.cases.filter(c=>c.passed).length ?? 0, total: state.cases.length } }}
          onSaved={async pack => { await carryDraftTests({...artifact,checkpoint:{...artifact.checkpoint,state}},pack.id); store?.finalizeDraft(artifact.id,pack); if(!await store?.flush()) throw new Error(sourceMessage('The pack was finalized, but its draft link could not be saved. Retry saving before leaving.')); writingChanged(false); return `/packs/${encodeURIComponent(pack.id)}` }} />
      </div> : <DraftTabs requestedTab={testParams.get('testProposal') || testParams.get('case') ? {tab:'tests',id:(testParams.get('testProposal') ?? testParams.get('case'))!} : undefined} historyPanel={<DefinitionHistory kind="pack" draft owner={artifact.id} candidates={state.candidates}/>} usagePanel={<DefinitionUsage kind="pack" draft documentId={typeof (latest?.document as {id?:unknown})?.id === 'string' ? (latest!.document as {id:string}).id : undefined}/>} brief={latest && <DefinitionBrief kind="pack-draft" owner={artifact.id} title={artifact.title} content={latest.text} sourceKey={JSON.stringify([latest.text, artifact.updatedAt, store?.getSnapshot().dirty])}/>} onTabChange={tab=>setTestActive(tab==='tests')} testsPanel={latest && candidate ? <TestsContent chatId={chat.id} owner={artifact.id} document={latest.document as import('../mcp/types').PackDocument} text={latest.text} title={artifact.title} active={testActive && !review} draft={{...artifact,checkpoint:{...artifact.checkpoint,state}}}/> : undefined} documents={artifact.documents} files={artifact.sourceFiles} policySources={artifact.policySources} onRead={readSource} hideHeader onSelectInMain={setSelection} state={state} mode={chat.mode} basis={basis} sources={binding?.sources ?? []} selection={selection} onSelect={select} onCreate={beginReview} showCreateAction={false}
        onEstablishCases={() => store?.perform(chat.id, active => active.run?.establishCases())}
        onProposeCorrection={id => store?.perform(chat.id, active => active.run?.proposeExpectationCorrection(id))}
        onApproveCorrection={(id,token) => store?.perform(chat.id, active => active.run?.approveExpectationCorrection(id,token), false)} />}
    </div>
  </div>
}
