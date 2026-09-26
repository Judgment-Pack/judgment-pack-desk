import { useTestStorage } from '../packs/test-workspace/store'
import { TestsContent, carryDraftTests } from '../packs/test-workspace/TestsWorkspace'
import { useReadingDetails } from '../chat/ReadingDetails'
import { DraftActions } from '../packs/drafts/DraftActions'
import { systemMessage } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { msg, useLocale } from '../i18n'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useBlocker, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ChatPanel } from '../chat/ChatPanel'
import { useChats } from '../chat/ChatProvider'
import { chatHref } from '../chat/ChatHistory'
import { draftHref, type PackDraft } from '../packs/drafts/model'
import type { ResearchRunBinding } from '../research/useResearchRun'
import { MovePackButton, ShowFolders } from '../packs/folders/FolderBrowser'
import { usePackFolders } from '../packs/folders/FolderContext'
import { packFolder } from '../packs/folders/model'
import { homeChatId } from '../chat/navigation'
import type { Chat } from '../chat/store'
import { INITIAL_STATE, canCreateResearchDraft, matrixDocument, researchRecord } from '../research/run'
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

export function draftReady(chat: Chat, state: typeof INITIAL_STATE): boolean {
  if (state.restored || state.status !== 'ready') return false
  if (chat.mode === 'research') return canCreateResearchDraft(state)
  const candidate = state.candidates.at(-1)
  return Boolean(candidate?.check?.valid && candidate.check.documentDigest === candidate.digest)
}

export function ChatWorkspace() {
  useLocale()
  const { chatId: linkedChatId } = useParams()
  const location = useLocation()
  const home = location.pathname === '/'
  const chatId = linkedChatId ?? (home ? homeChatId(location.state) : undefined)
  const [params] = useSearchParams()
  const requestedFolder = params.get('folder')
  const targetFolderId = !home && requestedFolder && /^[a-zA-Z0-9-]{1,80}$/.test(requestedFolder) ? requestedFolder : undefined
  const initialMode = params.get('mode') === 'research' ? 'research' : undefined
  const { store, chats, drafts, ready, error } = useChats()
  const navigate = useNavigate()
  const created = useRef<string | null>(null)
  useEffect(() => {
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
  const chat = chats.find(chat => chat.id === chatId) ?? drafts.find(chat => chat.id === chatId)
  const [chatHeaderTarget, setChatHeaderTarget] = useState<HTMLDivElement | null>(null)
  return <div data-measure="wide" data-layout="page">
    {!ready || !chat ? <div className={styles.blank} role="status">{error || (ready && chatId && !home ? msg("This chat is no longer in history.") : ready && !store?.canCreate ? msg("Chat history is full. Export and delete an older chat from Chat history.") : msg("Loading chat history…"))}{error && <Button onClick={() => void store?.load()}>{msg("Retry")}</Button>}{ready && chatId && !home && <Button onClick={() => navigate('/')}>{msg("New chat")}</Button>}</div>
      : <><header role="presentation" data-page-header className={styles.workspaceHeader}><div className={styles.headerControls} ref={setChatHeaderTarget} /></header><ChatPanel key={chat.id} chat={chat} landing headerTarget={chatHeaderTarget} onOpenDraft={chat.draftId ? () => navigate(draftHref(chat.draftId!)) : undefined} /></>}
  </div>
}

export function DraftWorkspace({ chat, artifact, fallback }: { chat: Chat; artifact: PackDraft; fallback: ResearchRunBinding }) {
  const locale = useLocale()
  const { store, bindings } = useChats()
  const live = bindings.get(chat.id)
  const binding = live && chat.draftGeneration===artifact.generation && live.state.candidates.at(-1)?.text === artifact.checkpoint.state.candidates.at(-1)?.text ? live : fallback
  const state = binding.state
  const savedTests=useTestStorage(artifact.id)
  const readSource = useReadingDetails(artifact.id)
  const navigate = useNavigate()
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
  const blocker = useBlocker(() => writingRef.current)
  useEffect(() => { if (blocker.state === 'blocked') blocker.reset() }, [blocker])
  useEffect(() => {
    if (!writing) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [writing])
  const shell = useShellState()
  const width = shell.inspectorWidth ?? 400
  const [rightOpen, setRightOpen] = useState(true)
  const assistant = useInspectorControls()
  const details = useDetailsSlot()
  const draft = true
  const latest = state.candidates.at(-1)
  const passing = draftReady(chat,state) && !store?.getSnapshot().dirty && !store?.getSnapshot().error
  const beginReview = () => { reviewDigest.current = latest?.digest; setReviewNotice(''); setReview(true) }
  useEffect(() => {
    if (review && latest?.digest !== reviewDigest.current) { setReview(false); setReviewNotice(sourceMessage('The draft changed. Review the latest revision before creating it.')) }
  }, [review, latest?.digest])
  const presentation = useMemo(() => ({ title: msg("Assistant"), contextTitle: artifact.title, workspaceTools: draft, available: draft, open: draft && rightOpen,
    onOpenChange: setRightOpen, width, onResize: shell.resizeInspector, onReset: shell.resetInspectorWidth, minimumMainWidth: 480, maximumWidth: 640 }), [draft, rightOpen, width, shell.resizeInspector, shell.resetInspectorWidth, locale, artifact.title])
  useInspectorPresentation(testActive && !review ? null : presentation)
  const portal = useInspectorPortal(draft ? <div hidden={testActive && !review} style={{height:"100%"}}><ChatPanel placement="pane" chat={chat} locked={writing} draftVisible /></div> : null)
  const detailPortal = useDetailsPortal(binding?.ledger && selection && !testActive ?
    <SourceInspector selection={selection} ledger={binding.ledger} state={state} onSelect={setSelection} /> : null)
  const select = (next: Selection) => { setSelection(next); if (next) details.reveal() }
  const candidate = latest?.document as { title?: unknown; description?: unknown; decision?: { question?: unknown } } | undefined
  const research = latest && binding?.ledger && chat.mode === 'research' && passing ? {
    document: latest.document, name: typeof candidate?.title === 'string' ? candidate.title : '',
    description: typeof candidate?.description === 'string' ? candidate.description : '', unknowns: state.unknowns,
    matrix: matrixDocument(state,binding.ledger), research: researchRecord(state,binding.ledger,'')
  } : undefined
  return <div className={styles.packWorkspace} data-draft-workspace>
    <PageHeader leading={<ShowFolders/>} title={msg("Packs")} titleHref="/packs" context={artifact.title} meta={<>{msg('Draft')} · {latest ? msg('revision {{value0}}', { value0: latest.revision }) : msg('no revision yet')}</>} actions={<>
      <DraftActions draft={artifact} onChat={() => store?.getSnapshot().chats.some(item=>item.id===chat.id) ? navigate(chatHref(chat)) : navigate('/',{state:{homeChatId:chat.id}})}><MovePackButton id={artifact.id}/></DraftActions>
      <Tooltip content={msg('Focus on canvas')}><Button size="icon" variant="quiet" aria-label={msg('Focus on canvas')} onClick={() => assistant.close?.()}><IconFocus /></Button></Tooltip>
      {state.restored && <Button disabled={state.status==='running'} onClick={() => binding.run?.recheck()}>{msg('Recheck saved draft')}</Button>}
      {!review && !state.restored && <Button variant="primary" disabled={!passing} onClick={beginReview}>{msg('Review and finalize')}</Button>}
    </>} />
    {portal}{detailPortal}
    {draft && reviewNotice && <p className={styles.reviewNotice} role="status">{systemMessage(reviewNotice)}</p>}
    <div className={styles.workspace}>
      {review && latest ? <div className={styles.review}>
        <CreatePackDialog open presentation="review" submitLabel={msg("Finalize pack")} onOpenChange={open => { if (!open && !writing) setReview(false) }}
          onWritingChange={writingChanged}
          initialFolderId={folderId}
          onFolderChange={targetFolderId => store?.update(chat.id,{targetFolderId})}
          canCreate={() => { const snapshot=store?.getSnapshot(); const current=snapshot?.packDrafts.find(item=>item.id===artifact.id); const active=binding.run?.getSnapshot(); return !snapshot?.dirty && !snapshot?.error && current?.checkpoint.state.candidates.at(-1)?.text===active?.candidates.at(-1)?.text && !!active && active.candidates.at(-1)?.digest===reviewDigest.current && draftReady(chat,active) }}
          reviewDraft={{ trialCount: state.probes?.length ?? 0, caseCount: new Set([...state.cases.map(c=>c.id),...savedTests.suite.cases.map(c=>c.id)]).size, document: latest.document, name: artifact.title, description: typeof candidate?.description === 'string' ? candidate.description : '', unknowns: state.unknowns, research }}
          onSaved={async pack => { await carryDraftTests({...artifact,checkpoint:{...artifact.checkpoint,state}},pack.id); store?.finalizeDraft(artifact.id,pack); if(!await store?.flush()) throw new Error(sourceMessage('The pack was finalized, but its draft link could not be saved. Retry saving before leaving.')); writingChanged(false); return `/packs/${encodeURIComponent(pack.id)}` }} />
      </div> : <DraftTabs onTabChange={tab=>setTestActive(tab==='tests')} testsPanel={latest && candidate ? <TestsContent owner={artifact.id} document={latest.document as import('../mcp/types').PackDocument} text={latest.text} title={artifact.title} active={testActive && !review} draft={{...artifact,checkpoint:{...artifact.checkpoint,state}}}/> : undefined} documents={artifact.documents} files={artifact.sourceFiles} onRead={readSource} hideHeader onSelectInMain={setSelection} state={state} mode={chat.mode} sources={binding?.sources ?? []} selection={selection} onSelect={select} onCreate={beginReview} showCreateAction={false}
        onProposeCorrection={id => store?.perform(chat.id, active => active.run?.proposeExpectationCorrection(id))}
        onApproveCorrection={(id,token) => store?.perform(chat.id, active => active.run?.approveExpectationCorrection(id,token), false)} />}
    </div>
  </div>
}
