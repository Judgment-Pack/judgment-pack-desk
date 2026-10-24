import { CodeBlock } from '../ui/CodeBlock'
import {PolicyInterpretationProposals} from '../policies/PolicyInterpretation'
import {usePolicyChatContext} from '../policies/chatContext'
import {PolicyChatProposals} from '../policies/PolicyChatProposals'
import {pendingPublication} from '../dialects/publicationProposal'
import {LinkedPackProposal} from './PackProposalCard'
import {ExecutionProgress} from '../assistant/execution/ExecutionProgress'
import {MAX_REQUEST_ALLOWANCE,REQUEST_INCREMENT} from '@judgment-pack/harness/execution/state'
import {RetryResponseButton} from './RetryResponseButton'
import {PlanCard} from '../assistant/planning/PlanCard'
import {ChatMemoryReview} from '../assistant/memory/MemorySettings'
import { ChatTestProposals, useChatTestProposals } from '../packs/test-workspace/ChatTestProposals'
import {AssistantApprovalControl} from '../assistant/approvals/AssistantApprovalControl'
import { ResponseRecovery } from './ResponseRecovery'
import { QuestionCard } from './QuestionCard'
import { ChatComposer } from './ChatComposer'
import { submitQuestion, continueQuestion } from './questionActions'
import {DialectActionNotice,DialectChatProgress} from '../dialects/DialectActionArtifacts'
import {useDialectChatContext} from '../dialects/dialectChatContext'
import {RelatedChatWork, type WorkContext} from './RelatedChatWork'
import {DialectRecipeNotice} from '../dialects/DialectRecipeArtifacts'
import { MessageNavigator } from './MessageNavigator'
import { ResearchSuggestion } from './ResearchSuggestion'
import { researchIntent, type ResearchChoice } from './researchChoice'
import { useSearchPreference } from '../search/connections'
import { LinkedMessageText } from './LinkedMessageText'
import { ConfigureSearch } from '../search/ConfigureSearch'
import { ChatSearchRecovery } from '../search/ChatSearchRecovery'
import { ChatWebAccessRecovery } from '../connections/WebAccessSettings'
import { ComposerContext } from './ComposerContext'
import { GraphDraftNotice } from '../graphs/GraphArtifacts'
import { ModelControl } from './ModelControl'
import { chatReasoningAgent } from './reasoning'
import { composeMessageInput } from './messageInput'
import { searchReferenceKey } from '../search/step'
import { ChatSearchOptions } from '../search/ChatSearchOptions'
import { selectedAssistant } from '../assistant/target'
import { assistantReady } from '../assistant/useAssistantSlot'
import { OpenQuestions } from './OpenQuestions'
import { recoverableProposal } from '@judgment-pack/harness/engines/contract'
import { ReferenceChip, useChatReference } from './AssistantReference'
import { SentAttachments, MessageSources, SourceList } from './MessageSources'
import { DraftReference } from './DraftReference'
import { unassignedSources } from './messageOwnership'
import type { ResponseHistory } from './responseHistory'
import { MessageTime, useMessageClock } from './MessageTime'
import { messageTimeFormatter, dayBoundaries } from './timestamps'
import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
import { Fragment, useCallback, useEffect, useId, useMemo, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { createPortal } from 'react-dom'
import { useInspectorControls } from '../shell/InspectorSlot'
import { VisuallyHidden } from 'radix-ui'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { MessageDetails, ReadingDetails, useReadingDetails } from './ReadingDetails'
import { IconLink } from '../shell/icons'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { MessageRenderer, CopyMessage } from './MessageRenderer'
import { ComposerTaskStatus, WorkSummary } from './RunPresentation'
import { canRetryExpectationValidation, INITIAL_STATE, type Turn } from '../research/run'
import {ChatAction} from './ChatActions'
import { Select } from '../ui/Select'
import { AttachmentList } from './AttachmentList'
import { useConnectionsPane, useConnectionChatLock } from '../connections/ConnectionPaneContext'
import type { ConnectionProvider } from '../connections/client'
import { useConnections } from '../connections/catalog'
import { AttachmentMenu } from './AttachmentMenu'
import { TEXT_ATTACHMENT_ACCEPT, useChatAttachments } from './useChatAttachments'
import { linkReadable, websiteReadable, webSourceOffered } from './linkTools'
import { ConfigureAssistant } from './ConfigureAssistant'
import { AssistantOptions } from './AssistantOptions'
import { ChatToolbar, chatHref } from './ChatHistory'
import { chatTitle, openNewChat, type ChatScope } from './navigation'
import { useChats } from './ChatProvider'
import { criticChoice, retainSentDocuments, type Chat } from './store'
import styles from './ChatWorkspace.module.css'

export function ChatPanel({ chat, landing = false, onOpenDraft, draftVisible = false, context: suppliedContext, proposalActions, locked: suppliedLocked = false, inputActionsLocked = false, placement = 'main', headerTarget, historyLoading = false, active = true, draftActionsInHeader = false, openGraphDraftId, scope }: {
  scope?: ChatScope
  placement?: 'main' | 'pane'; headerTarget?: HTMLElement | null; openGraphDraftId?: string
  historyLoading?: boolean
  active?: boolean; draftActionsInHeader?: boolean
  chat: Chat; landing?: boolean; onOpenDraft?: () => void; draftVisible?: boolean
  context?: { text: string; title?: string; detail?: string; tests?: string; kind?: 'pack' | 'graph' | 'input' | 'policy'; prepare?:()=>Promise<{text:string;beforeSend?:()=>void}>; beforeSend?: () => void }
  proposalActions?: ReactNode; locked?: boolean; inputActionsLocked?: boolean
}) {
  useLocale()
  const inputContext = useDialectChatContext(suppliedContext ? undefined : chat.input?.id)
  const policyContext=usePolicyChatContext(chat.policy?.id,chat.id)
  const context: typeof suppliedContext = suppliedContext ?? policyContext ?? inputContext
  const locked = suppliedLocked || Boolean(inputContext?.busy)||Boolean(policyContext?.busy)
  const isInput = context?.kind === 'input' || Boolean(chat.input)
  const { store, bindings, packDrafts } = useChats()
  const binding = bindings.get(chat.id)
  const state = binding?.state ?? INITIAL_STATE
  const recoverableTurns = useMemo(() => new Set(state.candidates.length ? [] : state.turns.flatMap((turn, index) => turn.role === 'assistant' && turn.kind === 'message' && recoverableProposal(turn.text) ? [index] : [])), [state.turns, state.candidates.length])
  const liveTurn: Turn | undefined = state.status === 'running' && state.streaming ? {id: state.streamingId ?? 'streaming', role: 'assistant', kind: 'message', text: state.streaming, at: ''} : undefined
  const shownTurns = liveTurn ? [...state.turns, liveTurn] : state.turns
  const clock = useMessageClock()
  const formatTime = useMemo(() => messageTimeFormatter(clock.locale, clock.timeZone), [clock.locale, clock.timeZone])
  const times = useMemo(() => state.turns.map(turn => formatTime(turn.at)), [state.turns, formatTime])
  const days = useMemo(() => dayBoundaries(times), [times])
  const slot = useAssistantSlot(chat.aiConnection)
  const selected = selectedAssistant(slot)
  const pane = useInspectorControls()
  const openInputId = placement==='pane'&&scope?.kind==='input'&&scope.id!=='list'?scope.id:undefined
  const title = chatTitle(chat)
  const requestTitle = pane.requestTitle
  useEffect(() => {
    if (placement === 'pane' && active) return requestTitle?.(title)
  }, [placement, active, requestTitle, title])
  const { reference, remove: removeReference } = useChatReference(chat.id, placement === 'pane' && active)
  const currentReference = useRef(reference)
  useLayoutEffect(() => { currentReference.current = reference }, [reference])
  const openReading = useReadingDetails(chat.id)
  const toolbarTarget = placement === 'pane' ? pane.headerTarget : headerTarget
  const [history, setHistory] = useState(false)
  const [messagesOpen, setMessagesOpen] = useState(false)
  const [jumpedMessage, setJumpedMessage] = useState<string>()
  const [suggestedInput, setSuggestedInput] = useState<string>()
  const historyButton = useRef<HTMLButtonElement>(null)
  const backToChat = () => setHistory(false)
  const navigate = useNavigate()
  const location = useLocation()
  const [configure, setConfigure] = useState(false)
  const [configureSearch, setConfigureSearch] = useState(false)
  const searchSetupButton = useRef<HTMLElement | null>(null)
  const configureButton = useRef<HTMLButtonElement>(null)
  const thread = useRef<HTMLDivElement>(null)
  const threadContent = useRef<HTMLDivElement>(null)
  const following = useRef(true)
  const read = useCallback((node: ReactNode, opener: HTMLElement | null) => {
    // Opening a reader is a reading gesture, not a request to jump to the tail.
    following.current = false
    openReading(node, opener)
  }, [openReading])
  const [awayFromLatest, setAwayFromLatest] = useState(false)
  const messageInput = useRef<HTMLTextAreaElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const id = useId()

  const graphContext = context?.kind === 'graph' || Boolean(chat.graph)
  const running = state.status === 'running'
  const effective = useEffectiveConfig()
  const research = effective.config.research
  const searchPreference = useSearchPreference()
  const searchAllowed = Boolean(searchPreference.data && !searchPreference.isError && (chat.researchMode ?? searchPreference.data.value.mode) === 'auto')
  const localDrive = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
  const connectionCatalog = useConnections(localDrive)
  const connections = useConnectionsPane()
  useConnectionChatLock(chat.id, locked)
  const connectionBusy = connections.busyChatId === chat.id
  const attachmentButton = useRef<HTMLButtonElement>(null)
  const openConnection = (provider?: ConnectionProvider) => requestAnimationFrame(() => connections.open({ provider, descriptor: connectionCatalog.entries.find(item => item.descriptor.id === provider)?.descriptor, chatId: chat.id, opener: attachmentButton.current }))
  const upload = useChatAttachments(store, chat.id, locked || running, research)
  const otherRun = store?.running && store.running !== chat.id ? store.running : undefined
  const empty = state.turns.length === 0
  const idleEmpty = empty && state.status === 'idle' && !state.responses?.length && !state.events.length && !state.candidates.length
    && !chat.graphDrafts?.length && !chat.inputDrafts?.length && !chat.inputActions?.length && !proposalActions
  const savedCandidate = Boolean(chat.pack && chat.createdCandidateDigest && chat.createdCandidateDigest === state.candidates.at(-1)?.digest && (state.status === 'ready' || state.restored))
  const needsConnection = !!effective.aiConnections && !chat.aiConnection && !!chat.checkpoint?.state.turns.length
  const needsConfig = !assistantReady(slot) || !selected?.models.length
  const blocked = binding?.blocked ?? msg('Loading chat…')
  // All readiness belongs to the input it blocks. Run and history notices
  // retain their separate placement above the composer.
  const setupMessage = slot.unusable ? systemMessage(slot.unusable)
    : slot.engine !== 'codex' && slot.keyStatus === 'error' ? msg('The saved API key could not be checked.')
    : slot.engine !== 'codex' && slot.keyStatus === 'pending' ? msg('Checking your Assistant configuration…')
    : msg('Configure Assistant to begin. Your message will stay here.')
  const composerReadiness = !historyLoading && !running ? needsConfig ? setupMessage : systemMessage(blocked) : ''
  const readinessAction = composerReadiness && needsConfig ? slot.recovery ?? (assistantReady(slot) ? 'configure' : undefined) : undefined
  const attachments = chat.attachments ?? []
  const hasMessage = Boolean(chat.composer.trim() || attachments.length)
  const inputKey = JSON.stringify([chat.composer, attachments, chat.mode])
  const showResearchSuggestion = suggestedInput === inputKey && searchAllowed && chat.mode === 'draft'
  const send = async (choice?: ResearchChoice) => {
    if (!store || !hasMessage || locked || running || otherRun || blocked || connectionBusy || upload.isReading()) return
    if (needsConfig) return
    const planCommand=/^\/plan(?:\s|$)/i.test(chat.composer.trim())
    const message=planCommand?chat.composer.trim().replace(/^\/plan\s*/i,''):chat.composer
    if(planCommand){store.update(chat.id,{interaction:'plan'});if(!message.trim()){store.update(chat.id,{composer:''});return}}
    const intent = researchIntent(message)
    if (!choice && searchAllowed && chat.mode === 'draft' && intent === 'suggest') { setSuggestedInput(inputKey); return }
    const chosen: ResearchChoice = !searchAllowed || intent === 'provided' ? 'provided' : choice ?? (chat.mode === 'web-research' || intent === 'research' ? 'research' : 'ask')
    if(slot.connectionId)store.update(chat.id,{aiConnection:slot.connectionId,aiConnectionName:slot.connectionName,model:binding?.model||selected?.model||chat.model})
    const text = message.trim() || msg('Please review the attached files.')
    const material = await upload.prepare(attachments)
    if (material === undefined) return
    const latest = [...store.getSnapshot().chats, ...store.getSnapshot().drafts].find(item => item.id === chat.id)
    if (currentReference.current !== reference || !latest || latest.composer !== chat.composer || JSON.stringify(latest.attachments ?? []) !== JSON.stringify(attachments)) return
    let currentContext=context?.text
    let prepared: {text:string;beforeSend?:()=>void}|undefined
    try{if(context?.prepare){prepared=await context.prepare();currentContext=prepared.text}}catch{store.problem(msg('The saved Policy could not be read. Sign in or reload before sending.'));return}
    const admitted=[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(item=>item.id===chat.id)
    if(!admitted||admitted.composer!==chat.composer||JSON.stringify(admitted.attachments??[])!==JSON.stringify(attachments)||currentReference.current!==reference)return
    const { prompt, display, statement } = composeMessageInput(text, material, reference, currentContext, context?.kind, context?.tests)
    const packContext = chat.pack && context?.text !== undefined && (!context.kind || context.kind === 'pack') ? {packId:chat.pack.id,path:chat.pack.path,text:context.text} : undefined
    const started = store.perform(chat.id, active => {
      prepared?.beforeSend?.()
      context?.beforeSend?.()
      if (active.state.phase === 'idle') active.run?.start(prompt, [], display, attachments, statement, chosen, packContext)
      else active.run?.send(prompt, display, false, attachments, statement, chosen, packContext)
    })
    if (started) {
      setSuggestedInput(undefined)
      removeReference()
      store.update(chat.id, { composer: '', attachments: [], documents: retainSentDocuments(chat.documents ?? [], attachments), ...(!chat.titleEdited && /^(new chat|hi|hello|hey)[!. ]*$/i.test(chat.title) ? { title: text.split('\n')[0]!.slice(0, 80) } : {}) })
      following.current = true
      setAwayFromLatest(false)
      document.getElementById(`${id}-message`)?.focus()
    }
  }
  const jumpToMessage = (messageId: string) => {
    const scroller = thread.current
    const target = scroller && [...scroller.querySelectorAll<HTMLElement>('[data-message-id]')].find(node=>node.dataset.messageId===messageId)
    if (!scroller || !target) return
    following.current = false
    setJumpedMessage(messageId)
    target.focus({preventScroll:true})
    scroller.scrollTo({top:scroller.scrollTop+target.getBoundingClientRect().top-scroller.getBoundingClientRect().top-8,behavior:'instant'})
    setAwayFromLatest(scroller.scrollHeight-scroller.scrollTop-scroller.clientHeight>=80)
  }
  useLayoutEffect(() => {
    if (!history && (placement !== 'pane' || pane.open) && following.current) thread.current?.scrollTo?.({ top: thread.current.scrollHeight })
  }, [state.turns.length, state.streaming, running, history, placement, pane.open])
  useLayoutEffect(() => {
    const scroller = thread.current, content = threadContent.current
    if (!scroller || !content) return
    const observer = new ResizeObserver(() => {
      if (!scroller.getClientRects().length) return
      if (!history && following.current) scroller.scrollTo?.({ top: scroller.scrollHeight })
      if (!history) setAwayFromLatest(scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight >= 80)
    })
    observer.observe(scroller); observer.observe(content)
    return () => observer.disconnect()
  }, [history])
  const responses = state.responses ?? []
  const lastResponse = responses.at(-1)
  const lastUser = [...state.turns].reverse().find(turn => turn.role === 'user')
  const canChooseResearch = !!binding?.run?.chooseResearch && searchAllowed && !running && lastUser?.researchChoice !== 'provided' && lastUser?.researchChoice !== 'research' && lastResponse?.afterTurnId === lastUser?.id && !!lastResponse?.work.items.some(item=>item.researchSuggestion)
  const actionDisabled = locked || running || Boolean(blocked) || Boolean(otherRun) || connectionBusy || upload.reading
  const continueExecution = () => {
    if(!store||actionDisabled||!binding?.run?.prepareBudgetContinuation())return
    store.report(chat.id,{...binding,state:binding.run.getSnapshot()})
    void store.checkpoint(new AbortController().signal).then(()=>{store.perform(chat.id,active=>active.run?.continueExecution(),false)}).catch(()=>{/* Shared persistence status reports failures; no dispatch. */})
  }
  const retryResponse = () => {
    if(actionDisabled || !binding?.run?.canRetryResponse)return
    if(store?.perform(chat.id,active=>active.run?.retryResponse(),false)){following.current=true;setAwayFromLatest(false)}
  }
  const lastSearch = lastResponse?.afterTurnId === lastUser?.id ? lastResponse && [...lastResponse.work.items].reverse().find(item => item.name === 'search_sources') : undefined
  const continueSearch = (query: string) => {
    if (locked || running || blocked || otherRun || connectionBusy || upload.isReading()) return
    // An explicit new, read-only research turn preserves unsent composer text
    // and attachments; it does not replay a previous authoring action.
    const started = store?.perform(chat.id, active => active.run?.send(
      `Continue the web research for my previous request. Retry search_sources with this query: ${JSON.stringify(query)}. Read relevant sources and answer the pending question. Do not repeat previous actions, create or edit packs or graphs, or send messages to anyone.`,
      msg('Continue web research'), false, [], '', 'research'))
    if (started) { following.current = true; setAwayFromLatest(false) }
  }
  const unassigned = unassignedSources(state, chat.documents ?? [], chat.websites ?? [])
  const unassignedResearch = (binding?.sources ?? []).filter(source => source.document && !responses.some(row => row.sourceIds.includes(source.id))).map(source => source.id)
  const questionsById = new Map((state.questions ?? []).map(question => [question.id, question]))
  const draftAvailable = packDrafts.some(item => item.id === chat.draftId)
  const hasDetails = (row: ResponseHistory) => row.searches?.length || row.work.items.length || row.work.notices.length || row.work.critique || row.documents.length || row.websites.length || row.sourceIds.length || draftAvailable && state.candidates.some(candidate => candidate.responseId === row.id)
  const renderResponse = (response: ResponseHistory) => hasDetails(response) ? <div key={response.id} className={styles.responseDetails} data-response-id={response.id}>
    <WorkSummary work={response.work} documents={response.documents} onRead={read}/>
    <MessageSources chatId={chat.id} documents={response.documents} websites={response.websites} searches={response.searches?.filter(ref=>!response.work.items.some(row=>row.search?.reference&&searchReferenceKey(row.search.reference)===searchReferenceKey(ref)))} sourceIds={response.sourceIds} binding={binding} onRead={read}/>
    {draftAvailable && (onOpenDraft || draftVisible) && state.candidates.filter(candidate => candidate.responseId === response.id).map(candidate => <DraftReference key={candidate.revision} candidate={candidate} open={draftVisible} latest={candidate === state.candidates.at(-1)} onOpen={onOpenDraft}/>)}
  </div> : null
  const unassignedSearches=(chat.searches??[]).filter(s=>!responses.some(r=>r.searches?.some(ref=>ref.id===s.id)))
  const hasChatSources = unassignedSearches.length + unassigned.documents.length + unassigned.websites.length + unassignedResearch.length > 0
  const workContext: WorkContext | undefined = context ? {
    kind: chat.policy?'policy':isInput ? 'input' : graphContext ? 'graph' : 'pack',
    id: chat.policy?.id??(isInput ? (scope?.kind === 'input' ? scope.id : chat.input?.id) ?? 'list' : graphContext ? chat.graph?.id ?? '' : chat.pack?.id ?? chat.draftId ?? ''),
    title: context.title ?? '', detail: context.detail, text: context.text, tests: context.tests, open: placement === 'pane',
  } : undefined
  const testProposals = useChatTestProposals(chat.id)
  const hasRelatedWork = !!(testProposals.length || context || chat.policy || chat.input || chat.pack || chat.draftId || chat.graph || chat.inputDrafts?.length || chat.inputActions?.length || chat.graphDrafts?.length)
  const openRelatedWork = (opener: HTMLElement | null) => read(<RelatedChatWork chatId={chat.id} current={workContext} openGraphDraftId={openGraphDraftId} disabled={locked||inputActionsLocked}/>, opener)
  const inputContextHeader = isInput && context && placement === 'pane'
  const toolbar = <ChatToolbar loading={historyLoading} scope={scope}
    navigator={<MessageNavigator turns={state.turns} open={messagesOpen} onOpenChange={open=>{setMessagesOpen(open);if(open)setHistory(false)}} onJump={jumpToMessage} currentId={jumpedMessage} threadRef={thread} disabled={historyLoading}/>}
    onSources={hasChatSources ? opener => read(<ReadingDetails title={msg('Chat sources')}><SourceList chatId={chat.id} documents={unassigned.documents} websites={unassigned.websites} searches={unassignedSearches} sourceIds={unassignedResearch} binding={binding} onRead={read}/></ReadingDetails>, opener) : undefined}
    work={hasRelatedWork && !inputContextHeader ? <ChatAction variant="quiet" onClick={event=>openRelatedWork(event.currentTarget)}><IconLink/>{msg('Related work')}</ChatAction> : undefined}
    chat={chat} history={history} historyRef={historyButton} onHistory={() => {setMessagesOpen(false);setHistory(true)}} onBack={backToChat}
    onNew={() => { if (!store?.canCreate) return; const next = scope?.kind === 'input' ? store.startChat(undefined, chat.mode, true, undefined, undefined, {id:scope.id}) : store.startChat(chat.pack, chat.mode, true, chat.draftId, chat.graph, chat.input,chat.policy); setHistory(false); openNewChat(navigate, next, location) }} />
  const welcome = idleEmpty && (!historyLoading || landing) ? <div className={styles.welcome}><h1>{chat.policy || isInput || chat.pack || draftVisible || graphContext ? msg("What would you like to change?") : msg("What would you like to work on?")}</h1><p>{chat.policy?msg('Ask about this policy, explain its wording, or propose a draft change.'):isInput ? msg("Ask about this dialect, update the recipe, or prepare records.") : graphContext ? msg("Ask about this graph, connect packs, or propose a change.") : chat.pack ? msg("Ask about {{value0}}, test an idea, or propose a change.", { value0: context?.title || chat.pack.id }) : draftVisible ? msg("Ask about this pack, test an idea, or propose a change.") : msg("Ask a question, explore an idea, or create and improve a pack.")}</p></div> : null
  return <section className={styles.chat} data-chat-id={chat.id} data-placement={placement} data-history-loading={historyLoading || undefined} data-landing={landing && idleEmpty || undefined} aria-label={msg("Assistant chat")}>
    {active && toolbarTarget ? createPortal(toolbar, toolbarTarget) : active && placement === 'main' && headerTarget === undefined ? <header className={styles.chatHeader}>{toolbar}</header> : null}
    {inputContextHeader && <ComposerContext title={context.title||msg('Dialects')} kind="input" detail={context.detail} openLabel={msg('Related work')} onOpen={event=>openRelatedWork(event.currentTarget)}/>}
    <div className={styles.conversation}>
    <div className={styles.transcript}>
    <div className={styles.thread} ref={thread} onScroll={() => { const node = thread.current; if (node && node.getClientRects().length) { following.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80; setAwayFromLatest(!following.current) } }}>
      <div className={styles.threadContent} ref={threadContent}>
      {!isInput && welcome}
      {shownTurns.map((turn,index) => <Fragment key={turn.id ?? `${turn.at}-${index}`}>{days[index] && <div className={styles.day}>{days[index]}</div>}<article className={styles.message} data-role={turn.role} data-kind={turn.kind} data-message-id={turn.id ?? `message-${index}`} tabIndex={-1} data-jump-target={jumpedMessage===(turn.id ?? `message-${index}`)||undefined} aria-label={turn === liveTurn ? msg("Response in progress") : undefined}>
        {turn.kind !== 'unknowns' && <div className={turn.role === 'user' ? styles.userHeader : undefined}>
          <MessageTime pending={turn === liveTurn} turn={turn} formatted={times[index]} onOpen={opener => read(<MessageDetails turn={turn} text={turn.kind === 'note' ? systemMessage(turn.text) : turn.text} input={turn.input} />, opener)} />
          {turn.role === 'user' && <CopyMessage text={turn.text} label={msg('Copy message')}>
            {turn.researchChoice==='research'&&<span>{msg('Research')}</span>}
            {turn === lastUser && state.status!=='failed' && binding?.run?.canRetryResponse && <RetryResponseButton disabled={actionDisabled} onClick={retryResponse}/>}
          </CopyMessage>}
        </div>}
        {turn.kind === 'question' && questionsById.get(turn.id ?? '') ? <QuestionCard
          question={questionsById.get(turn.id ?? '')!} disabled={actionDisabled || needsConfig || historyLoading}
          canContinue={!!questionsById.get(turn.id ?? '')?.resolution && !questionsById.get(turn.id ?? '')?.resolution?.continued && questionsById.get(turn.id ?? '')?.resolution?.kind !== 'superseded' && questionsById.get(turn.id ?? '')?.resolution?.turnId === state.turns.at(-1)?.id}
          onDraft={draft => binding?.run?.updateQuestionDraft(turn.id!,draft)}
          onSubmit={async kind => { if (!store) return; await submitQuestion(store,chat.id,turn.id!,kind); following.current=true;setAwayFromLatest(false);messageInput.current?.focus({preventScroll:true}) }}
          onContinue={async () => { if (!store) return; await continueQuestion(store,chat.id,turn.id!); following.current=true;setAwayFromLatest(false);messageInput.current?.focus({preventScroll:true}) }}/>
          : turn.kind === 'unknowns' ? <OpenQuestions text={turn.text} documents={chat.documents} onRead={read}><MessageTime turn={turn} formatted={times[index]} onOpen={opener => read(<MessageDetails turn={turn} text={turn.text}/>, opener)}/></OpenQuestions>
          : turn.role === 'assistant' ? <MessageRenderer onRead={read} scope={chat.id} documents={chat.documents} text={turn.kind === 'note' ? systemMessage(turn.text) : turn.text} /> : <div className={styles.userText}><LinkedMessageText text={turn.kind === 'note' ? systemMessage(turn.text) : turn.text}/></div>}
        {turn.role === 'user' && <SentAttachments files={turn.attachments ?? []} onRead={read}/>}
        {binding?.run?.recoverDraft && recoverableTurns.has(index) && <ChatAction disabled={running || locked || Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recoverDraft?.(turn.text), false)}>{msg('Recover draft')}</ChatAction>}
        {turn.interrupted && <p className={styles.caption}>{msg('Response interrupted')}</p>}
        {turn !== liveTurn && turn.role === 'assistant' && turn.kind === 'message' && <CopyMessage text={turn.text} />}
        {responses.filter(row => row.messageId === turn.id).map(renderResponse)}
      </article>
      {responses.filter(row => !row.messageId && row.afterTurnId === turn.id && hasDetails(row)).map(row => <article className={styles.message} data-role="assistant" key={row.id}>{renderResponse(row)}</article>)}
      </Fragment>)}
      <VisuallyHidden.Root role="status" aria-live="polite">{state.questions?.some(q => !q.resolution) ? msg("Waiting for your answer") : state.status === 'complete' ? msg("Response complete.") : state.status === 'ready' ? msg("Draft ready for review.") : ''}</VisuallyHidden.Root>
      {!running && lastResponse && lastSearch?.status === 'failed' && lastSearch.failure && <ChatSearchRecovery key={lastResponse.id} chat={chat} failure={lastSearch}
        disabled={locked || Boolean(blocked) || Boolean(otherRun) || connectionBusy || upload.reading} onChange={patch => store?.update(chat.id, patch)} onContinue={continueSearch}/>}
      {!running && lastResponse?.afterTurnId===lastUser?.id && lastResponse?.work.items.some(item=>item.redirectPolicy==='google-grounding') && <ChatWebAccessRecovery disabled={locked || running || Boolean(otherRun) || connectionBusy || upload.reading}/>}
      <GraphDraftNotice chatId={chat.id} drafts={chat.graphDrafts ?? []} openDraftId={openGraphDraftId}/>
      <DialectRecipeNotice chatId={chat.id} drafts={(chat.inputDrafts??[]).filter(draft=>scope?.kind!=='input'||scope.id==='list'||draft.savedInputId===scope.id||!draft.savedInputId)} disabled={locked||inputActionsLocked}/>
      <DialectActionNotice chatId={chat.id} actions={(chat.inputActions??[]).filter(action=>scope?.kind!=='input'||scope.id==='list'||action.inputId===scope.id)} disabled={locked}/>
      {isInput&&<DialectChatProgress inputId={scope?.kind==='input'?scope.id:chat.input?.id} emptyFallback={welcome} presentation={openInputId&&!pane.mainCovered?'hidden':'compact'}/>}
      <PolicyChatProposals chatId={chat.id} disabled={locked||running}/><PolicyInterpretationProposals chatId={chat.id} disabled={locked||running}/>
      <ChatTestProposals chatId={chat.id}/>
      <PlanCard chat={chat} store={store} disabled={actionDisabled} onRevise={()=>messageInput.current?.focus()} onStart={()=>{
        if(!store)return false
        const plan=chat.plans?.at(-1)
        if(!plan)return false
        return store.perform(chat.id,active=>{const prompt=msg('Start work on reviewed plan {{revision}}.',{revision:plan.revision});if(active.state.phase==='idle')active.run?.start(prompt,[]);else active.run?.send(prompt)})
      }}/>

      <ExecutionProgress execution={state.execution} report={state.responses?.at(-1)?.work.report} disabled={actionDisabled} onStop={()=>store?.perform(chat.id,active=>active.run?.keepExecution(),false)} onContinue={continueExecution}/>
      <ChatMemoryReview chatId={chat.id}/>
      {proposalActions === undefined && chat.pack ? <LinkedPackProposal chat={chat} state={state} busy={running || Boolean(otherRun)} onCheck={()=>store?.perform(chat.id, active=>active.run?.recheck(),false)}/> : proposalActions}
      {state.status!=='running' && state.streaming && <section aria-label={msg('Written so far')}><p className={styles.caption}>{msg('Written so far')}</p><MessageRenderer text={state.streaming}/></section>}
      {binding?.run?.pendingApproval && <div role="group" aria-label={msg('Approval required')}><p>{binding.run.pendingApproval.name}</p><CodeBlock label={msg('Arguments')} text={JSON.stringify(binding.run.pendingApproval.args,null,2)}/><ChatAction disabled={actionDisabled} onClick={()=>binding.run?.decideApproval?.(true)}>{msg('Approve')}</ChatAction><ChatAction disabled={actionDisabled} onClick={()=>binding.run?.decideApproval?.(false)}>{msg('Decline')}</ChatAction></div>}
      {binding?.run?.needsReconnect && binding.run.reconnect && <ChatAction disabled={running || locked || Boolean(otherRun)} onClick={()=>void binding.run?.reconnect?.()}>{msg('Reconnect')}</ChatAction>}
      {state.status==='failed' && <ResponseRecovery failure={state.failure} detail={state.detail} disabled={actionDisabled} onRetry={binding?.run?.canExtendResponse?continueExecution:binding?.run?.canRetryResponse?retryResponse:undefined} additionalRequests={binding?.run?.canExtendResponse?Math.min(REQUEST_INCREMENT,MAX_REQUEST_ALLOWANCE-state.execution!.limit):undefined}/>}
      </div>
    </div>
    {awayFromLatest && !empty && <div className={styles.jump}><ChatAction onClick={() => { following.current = true; setAwayFromLatest(false); thread.current?.scrollTo({ top: thread.current.scrollHeight }) }}>{msg("Jump to latest")}</ChatAction></div>}
    </div>
    <div className={styles.composerArea} onDragOver={event => { if (event.dataTransfer.types.includes('Files')) event.preventDefault() }} onDrop={event => { if (event.dataTransfer.files.length) { event.preventDefault(); void upload.attach([...event.dataTransfer.files]) } }}>
      {otherRun && <div className={styles.notice} role="status"><Message text={"Another chat is working. You can keep writing here.<0/>"} slots={[<ChatAction variant="quiet" onClick={() => { const other = store?.getSnapshot().chats.find(item => item.id === otherRun); if (other) navigate(chatHref(other, location)) }}>{msg("Open working chat")}</ChatAction>]} /></div>}

      <div className={styles.composerStatus}>
        <ComposerTaskStatus state={state} empty={empty} savedCandidate={savedCandidate || Boolean(chat.pack)}/>
        {chat.draftId && !draftAvailable && <p className={styles.caption}>{msg('This draft is no longer available.')}</p>}
      {binding?.run?.ownership!=='server' && !draftActionsInHeader && !chat.pack && !savedCandidate && state.candidates.length > 0 && <ChatAction disabled={running || Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recheck(), false)}>{msg("Run checks")}</ChatAction>}
      {binding?.run?.retryExpectationValidation && canRetryExpectationValidation(state) && <ChatAction disabled={locked || Boolean(otherRun) || Boolean(blocked)} onClick={() => store?.perform(chat.id, active => active.run?.retryExpectationValidation?.())}>{msg("Retry validation")}</ChatAction>}
      </div>
      {(showResearchSuggestion || canChooseResearch) && <ResearchSuggestion disabled={actionDisabled} onChoose={choice=>{
        if(actionDisabled)return
        if(showResearchSuggestion)void send(choice)
        else if(store?.perform(chat.id,active=>active.run?.chooseResearch?.(choice),false)){following.current=true;setAwayFromLatest(false)}
      }}/>}
      <ChatComposer id={id} inputRef={messageInput} rows={empty && placement === 'main' ? 3 : 2}
        value={chat.composer} onChange={composer => store?.update(chat.id, { composer })}
        placeholder={(!readinessAction && composerReadiness) || (isInput ? msg("Ask about this dialect…") : graphContext ? msg("Ask about this graph…") : chat.pack || draftVisible ? msg("Ask about this pack…") : msg("Ask a question or describe a task…"))}
        describedBy={composerReadiness ? `${id}-readiness` : undefined}
        disabled={locked || upload.reading} running={running}
        sendDisabled={needsConfig || !hasMessage || !binding || Boolean(otherRun) || connectionBusy || Boolean(blocked && !needsConfig)}
        onSend={() => void send()} onStop={() => binding?.run?.stop()}
        readiness={composerReadiness && (chat.composer || readinessAction || needsConnection
          ? <div className={styles.composerReadiness}>
            <span id={`${id}-readiness`} role="status">{composerReadiness}</span>
            {readinessAction === 'retry' && <ChatAction variant="inline" disabled={slot.retrying} onClick={slot.retryKey}>{msg('Check again')}</ChatAction>}
            {readinessAction === 'configure' && <ChatAction variant="inline" onClick={event => { configureButton.current = event.currentTarget; setConfigure(true) }}>{msg('Assistant settings')}</ChatAction>}
          </div>
          : <VisuallyHidden.Root id={`${id}-readiness`} role="status">{composerReadiness}</VisuallyHidden.Root>)}
        controls={<>
          <input ref={fileInput} hidden type="file" tabIndex={-1} accept={TEXT_ATTACHMENT_ACCEPT} multiple onChange={event => { void upload.attach([...(event.target.files ?? [])]); event.target.value = '' }} />
          <AttachmentMenu triggerRef={attachmentButton} disabled={running || locked || upload.reading || connectionBusy} onUpload={() => fileInput.current?.click()} onLink={webSourceOffered(research, { local: localDrive, catalogWeb: connectionCatalog.web }) ? () => requestAnimationFrame(() => connections.open({ source: 'web', chatId: chat.id, opener: attachmentButton.current })) : undefined}
            onMore={() => openConnection()}
            connections={connectionCatalog.entries.filter(item => !item.status.isError && item.status.data?.state === 'connected').map(item => ({ descriptor: item.descriptor, provider: item.descriptor.id, selection: item.descriptor.selection, onSelect: () => openConnection(item.descriptor.id) }))} />
          <div className={styles.pick}><VisuallyHidden.Root asChild><label htmlFor={`${id}-mode`}>{msg("Task tools")}</label></VisuallyHidden.Root><Select size="small" quiet id={`${id}-mode`} value={chat.interaction==='plan'?'plan':chat.mode} disabled={running || locked} onValueChange={mode => store?.update(chat.id, mode==='plan'?{interaction:'plan'}:{interaction:'work',mode:mode as Chat['mode']})} options={[{value:'plan',label:msg('Plan')},{ value: 'draft', label: msg("Chat") }, { value: chat.mode === 'research' ? 'research' : 'web-research', label: msg("Research") }]} /></div>
          {chat.interaction==='plan'&&<Select size="small" quiet id={`${id}-plan-sources`} aria-label={msg('Plan sources')} value={chat.mode} disabled={running||locked} onValueChange={mode=>store?.update(chat.id,{mode:mode as Chat['mode']})} options={[{value:'draft',label:msg('Chat sources')},{value:chat.mode==='research'?'research':'web-research',label:msg('Research sources')}]}/>}
          {chat.interaction==='plan'?<span className={styles.caption}>{msg('Approvals apply when work starts')}</span>:chat.policy?<span className={styles.caption}>{msg('Draft updates require a reviewed proposal')}</span>:<AssistantApprovalControl publication={scope?.kind==='pack'?undefined:pendingPublication(chat.inputActions,scope?.kind==='input'&&scope.id!=='list'?scope.id:chat.input?.id!=='list'?chat.input?.id:undefined)} chatId={chat.id} pack={!scope||scope.kind==='pack'?chat.pack:undefined} checkpoint={()=>store?.checkpoint(new AbortController().signal)??Promise.resolve()} inputId={scope?.kind==='input'&&scope.id!=='list'?scope.id:chat.input?.id!=='list'?chat.input?.id:undefined} disabled={running||locked||inputActionsLocked}/> }
          {((selected?.models.length ?? 0)>0||effective.aiConnections?.data?.connections.some(c=>c.enabled))&&<ModelControl requiredSelection={needsConnection} id={`${id}-model`} model={binding?.model||selected?.model||''} models={selected?.models??[]} thinking={slot.thinking} thinkingOverride={chat.apiThinking} onThinkingChange={apiThinking=>store?.update(chat.id,{apiThinking})} connectionId={chat.aiConnection??slot.connectionId} onConnectionChange={(aiConnection,aiConnectionName,model)=>store?.update(chat.id,{aiConnection,aiConnectionName,model,apiThinking:undefined,reasoning:{model,effort:null}})} agent={slot.agent} value={chat.reasoning} codex={slot.engine==='codex'} enabled={assistantReady(slot)} disabled={running||locked} onModelChange={model=>store?.update(chat.id,{model,...(slot.engine==='codex'?{reasoning:{model,effort:null}}:{})})} onReasoningChange={reasoning=>store?.update(chat.id,{reasoning})}/>}
          <AssistantOptions criticReasoning={chat.criticReasoning} onCriticReasoning={criticReasoning=>store?.update(chat.id,{criticReasoning})} codex={slot.engine==='codex'} connectionId={slot.connectionId} criticModel={slot.agent?.criticModel??slot.endpoint?.criticModel??binding?.model??selected?.model??''} reasoning={chatReasoningAgent(slot.agent, binding?.model || selected?.model || '', chat.reasoning)?.effort} researchMode={chat.researchMode} searchConnection={chat.searchConnection} searchOptions={chat.mode !== 'research' && <ChatSearchOptions onConfigure={element => { searchSetupButton.current = element; setConfigureSearch(true) }} chat={chat} disabled={running||locked} onChange={patch=>store?.update(chat.id,patch)}/>} thinking={slot.thinking} tools={selected?.tools ?? []} mode={chat.mode} websiteExploration={websiteReadable(research,{local:localDrive,catalogWeb:connectionCatalog.web,catalogDiscovery:connectionCatalog.discovery})} linkReading={linkReadable(research, { local: localDrive, catalogWeb: connectionCatalog.web })} review={chat.critic ?? (chat.adversarialReview === undefined ? undefined : chat.adversarialReview ? 'on' : 'off')} onReview={value => store?.update(chat.id, criticChoice(value))} disabled={running || locked} notice={[...state.events].reverse().find(event => event.type === "thinking_unavailable")?.detail} />
        </>}
        feedback={upload.error && <p className={styles.caption} role="alert">{systemMessage(upload.error)}</p>}
      >
        {reference && <ReferenceChip reference={reference} onRemove={removeReference} />}
        <AttachmentList files={attachments} disabled={locked || running || upload.reading} onChange={file => store?.update(chat.id, { attachments: attachments.map(item => item.id === file.id ? file : item) })} onRemove={id => store?.update(chat.id, { attachments: attachments.filter(item => item.id !== id) })} />
        {upload.reading && <div className={styles.attachmentProgress}><span role="status">{systemMessage(upload.progress) || msg("Reading files…")}</span><ChatAction variant="quiet" onClick={upload.cancel}>{msg("Cancel")}</ChatAction></div>}
      </ChatComposer>
    </div>
    </div>
    <ConfigureSearch open={configureSearch} onClose={() => setConfigureSearch(false)} openerRef={searchSetupButton} fallbackRef={messageInput} selected={chat.searchConnection} onSelect={id => { store?.update(chat.id, {searchConnection: id}); setConfigureSearch(false) }}/>
    <ConfigureAssistant open={configure} onOpenChange={setConfigure} openerRef={configureButton} />
  </section>
}
