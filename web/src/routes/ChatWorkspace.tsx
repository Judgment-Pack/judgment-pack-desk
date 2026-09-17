import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useBlocker, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ChatPanel } from '../chat/ChatPanel'
import { useChats } from '../chat/ChatProvider'
import { chatHref } from '../chat/ChatHistory'
import { homeChatId } from '../chat/navigation'
import type { Chat } from '../chat/store'
import { INITIAL_STATE, canCreateResearchDraft, matrixDocument, researchRecord } from '../research/run'
import { DraftTabs, type Selection } from '../research/ui/DraftPanels'
import { SourceInspector } from '../research/ui/SourceInspector'
import { useDetailsPortal, useDetailsSlot } from '../shell/DetailsSlot'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useShellState } from '../shell/paneState'
import { useMediaQuery } from '../shell/useMediaQuery'
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
  const { chatId: linkedChatId } = useParams()
  const location = useLocation()
  const home = location.pathname === '/'
  const chatId = linkedChatId ?? (home ? homeChatId(location.state) : undefined)
  const [params] = useSearchParams()
  const initialMode = params.get('mode') === 'research' ? 'research' : undefined
  const { store, chats, drafts, ready, error } = useChats()
  const navigate = useNavigate()
  const created = useRef<string | null>(null)
  useEffect(() => {
    if (!store || !ready) return
    if (!chatId || (home && ![...chats, ...drafts].some(chat => chat.id === chatId))) {
      if (!store.canCreate) return
      if (!created.current || ![...chats, ...drafts].some(chat => chat.id === created.current)) created.current = store.startChat(undefined, initialMode).id
      navigate('/', { replace: true, state: { homeChatId: created.current } })
    } else { created.current = null; store.activate(chatId) }
  }, [store, ready, chatId, navigate, initialMode, home, chats, drafts])
  const chat = chats.find(chat => chat.id === chatId) ?? drafts.find(chat => chat.id === chatId)
  useEffect(() => { if (chat?.pack) navigate(chatHref(chat), { replace: true }) }, [chat?.pack?.id, chat?.id, navigate])
  return <div data-measure="wide" data-layout="page">
    {!ready || !chat ? <div className={styles.blank} role="status">{error || (ready && chatId && !home ? 'This chat is no longer in history.' : ready && !store?.canCreate ? 'Chat history is full. Export and delete an older chat from Chat history.' : 'Loading chat history…')}{error && <Button onClick={() => void store?.load()}>Retry</Button>}{ready && chatId && !home && <Button onClick={() => navigate('/')}>New chat</Button>}</div>
      : <DraftWorkspace key={chat.id} chat={chat} />}
  </div>
}

function DraftWorkspace({ chat }: { chat: Chat }) {
  const { store, bindings } = useChats()
  const binding = bindings.get(chat.id)
  const state = binding?.state ?? INITIAL_STATE
  const [chatHeaderTarget, setChatHeaderTarget] = useState<HTMLDivElement | null>(null)
  const [selection, setSelection] = useState<Selection>(null)
  const [review, setReview] = useState(false)
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
  const narrow = useMediaQuery('(max-width: 1100px)')
  const details = useDetailsSlot()
  const draft = chat.view === 'draft'
  const latest = state.candidates.at(-1)
  const passing = draftReady(chat,state)
  const beginReview = () => { reviewDigest.current = latest?.digest; setReviewNotice(''); setReview(true) }
  useEffect(() => {
    if (review && latest?.digest !== reviewDigest.current) { setReview(false); setReviewNotice('The draft changed. Review the latest revision before creating it.') }
  }, [review, latest?.digest])
  const presentation = useMemo(() => ({ title: 'Assistant', available: draft && !narrow, open: draft && !narrow && rightOpen,
    onOpenChange: setRightOpen, width, onResize: shell.resizeInspector, onReset: shell.resetInspectorWidth, minimumMainWidth: 480, maximumWidth: 640 }), [draft, narrow, rightOpen, width, shell.resizeInspector, shell.resetInspectorWidth])
  useInspectorPresentation(presentation)
  const openDraft = () => { setReview(false); setRightOpen(true); store?.update(chat.id, { view: 'draft' }) }
  const portal = useInspectorPortal(draft && !narrow ? <ChatPanel placement="pane" chat={chat} locked={writing} /> : null)
  const detailPortal = useDetailsPortal(binding?.ledger && selection ? <SourceInspector selection={selection} ledger={binding.ledger} state={state} onSelect={setSelection} /> : null)
  const select = (next: Selection) => { setSelection(next); if (next) details.reveal() }
  const candidate = latest?.document as { title?: unknown; description?: unknown; decision?: { question?: unknown } } | undefined
  const digest = latest?.digest
  const research = latest && binding?.ledger && chat.mode === 'research' && passing ? {
    document: latest.document, name: typeof candidate?.title === 'string' ? candidate.title : '',
    description: typeof candidate?.description === 'string' ? candidate.description : '', unknowns: state.unknowns,
    matrix: matrixDocument(state,binding.ledger), research: researchRecord(state,binding.ledger,'')
  } : undefined
  return <>
    {!draft ? <header role="presentation" data-page-header className={styles.workspaceHeader}><div className={styles.headerControls} ref={setChatHeaderTarget} /></header> : <PageHeader title="Draft" actions={<>
      {draft && <Button variant="quiet" disabled={writing} onClick={() => { setReview(false); store?.update(chat.id,{ view: 'chat' }) }}>Chat</Button>}
      {draft && !narrow && !rightOpen && <Button onClick={() => setRightOpen(true)}>Show Assistant</Button>}
      {draft && !review && <Button variant="primary" disabled={!passing} onClick={beginReview}>Review and create</Button>}
    </>} />}
    {portal}{detailPortal}
    {draft && reviewNotice && <p className={styles.reviewNotice} role="status">{reviewNotice}</p>}
    <div className={styles.workspace}>
      {!draft ? <ChatPanel chat={chat} headerTarget={chatHeaderTarget} landing onOpenDraft={openDraft} /> : review && latest ? <div className={styles.review}>
        <CreatePackDialog open presentation="review" onOpenChange={open => { if (!open && !writing) setReview(false) }}
          onWritingChange={writingChanged}
          canCreate={() => { const active = store?.getSnapshot().bindings.get(chat.id)?.state; return !!active && active.candidates.at(-1)?.digest === reviewDigest.current && draftReady(chat,active) }}
          reviewDraft={{ document: latest.document, name: typeof candidate?.title === 'string' ? candidate.title : '', description: typeof candidate?.description === 'string' ? candidate.description : '', unknowns: state.unknowns, research }}
          onSaved={async pack => { writingChanged(false); store?.update(chat.id,{ pack, view: 'chat', createdCandidateDigest: digest }); await store?.flush(); return chatHref({ ...chat, pack }) }} />
      </div> : <DraftTabs state={state} mode={chat.mode} sources={binding?.sources ?? []} selection={selection} onSelect={select} onCreate={beginReview} showCreateAction={false}
        onProposeCorrection={id => store?.perform(chat.id, active => active.run?.proposeExpectationCorrection(id))}
        onApproveCorrection={(id,token) => store?.perform(chat.id, active => active.run?.approveExpectationCorrection(id,token), false)} />}
    </div>
  </>
}
