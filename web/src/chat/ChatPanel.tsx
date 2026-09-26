import { selectedAssistant } from '../assistant/target'
import { assistantReady } from '../assistant/useAssistantSlot'
import { OpenQuestions } from './OpenQuestions'
import { recoverableProposal } from '../assistant/engines/contract'
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
import { MessageDetails, PackContextDetails, useReadingDetails } from './ReadingDetails'
import { Tooltip } from '../ui/Tooltip'
import { IconSend, IconStop } from '../shell/icons'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { MessageRenderer, CopyMessage } from './MessageRenderer'
import { TaskStatus, WorkSummary } from './RunPresentation'
import { canRetryExpectationValidation, INITIAL_STATE, type Turn } from '../research/run'
import { Button } from '../ui/Button'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'
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
import { openNewChat } from './navigation'
import { useChats } from './ChatProvider'
import { retainSentDocuments, type Chat } from './store'
import styles from './ChatWorkspace.module.css'

export function ChatPanel({ chat, landing = false, onOpenDraft, draftVisible = false, context, proposalActions, locked = false, placement = 'main', headerTarget }: {
  placement?: 'main' | 'pane'; headerTarget?: HTMLElement | null
  chat: Chat; landing?: boolean; onOpenDraft?: () => void; draftVisible?: boolean
  context?: { text: string; beforeSend?: () => void }
  proposalActions?: ReactNode; locked?: boolean
}) {
  useLocale()
  const { store, bindings, error, drafts, packDrafts } = useChats()
  const binding = bindings.get(chat.id)
  const state = binding?.state ?? INITIAL_STATE
  const recoverableTurns = useMemo(() => new Set(state.candidates.length ? [] : state.turns.flatMap((turn, index) => turn.role === 'assistant' && turn.kind === 'message' && recoverableProposal(turn.text) ? [index] : [])), [state.turns, state.candidates.length])
  const liveTurn: Turn | undefined = state.status === 'running' && state.streaming ? {id: state.streamingId ?? 'streaming', role: 'assistant', kind: 'message', text: state.streaming, at: ''} : undefined
  const shownTurns = liveTurn ? [...state.turns, liveTurn] : state.turns
  const clock = useMessageClock()
  const formatTime = useMemo(() => messageTimeFormatter(clock.locale, clock.timeZone), [clock.locale, clock.timeZone])
  const times = useMemo(() => state.turns.map(turn => formatTime(turn.at)), [state.turns, formatTime])
  const days = useMemo(() => dayBoundaries(times), [times])
  const slot = useAssistantSlot()
  const selected = selectedAssistant(slot)
  const pane = useInspectorControls()
  const { reference, remove: removeReference } = useChatReference(chat.id, placement === 'pane')
  const currentReference = useRef(reference)
  useLayoutEffect(() => { currentReference.current = reference }, [reference])
  const openReading = useReadingDetails(chat.id)
  const toolbarTarget = placement === 'pane' ? pane.headerTarget : headerTarget
  const [history, setHistory] = useState(false)
  const historyButton = useRef<HTMLButtonElement>(null)
  const backToChat = () => setHistory(false)
  const navigate = useNavigate()
  const location = useLocation()
  const [configure, setConfigure] = useState(false)
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
  const running = state.status === 'running'
  const effective = useEffectiveConfig()
  const research = effective.config.research
  const localDrive = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
  const connectionCatalog = useConnections(localDrive)
  const connections = useConnectionsPane()
  useConnectionChatLock(chat.id, locked)
  const connectionBusy = connections.busyChatId === chat.id
  const attachmentButton = useRef<HTMLButtonElement>(null)
  const openConnection = (provider?: ConnectionProvider) => requestAnimationFrame(() => connections.open({ provider, descriptor: connectionCatalog.entries.find(item => item.descriptor.id === provider)?.descriptor, chatId: chat.id, opener: attachmentButton.current }))
  const upload = useChatAttachments(store, chat.id, locked || running, research)
  const unsubmitted = drafts.some(draft => draft.id === chat.id)
  const otherRun = store?.running && store.running !== chat.id ? store.running : undefined
  const empty = state.turns.length === 0
  const savedCandidate = Boolean(chat.pack && chat.createdCandidateDigest && chat.createdCandidateDigest === state.candidates.at(-1)?.digest && (state.status === 'ready' || state.restored))
  const needsConfig = !assistantReady(slot) || !selected?.models.length
  const blocked = binding?.blocked ?? msg('Loading chat…')
  const attachments = chat.attachments ?? []
  const hasMessage = Boolean(chat.composer.trim() || attachments.length)
  const send = async () => {
    if (!store || !hasMessage || locked || running || connectionBusy || upload.isReading()) return
    if (needsConfig) return
    const text = chat.composer.trim() || msg('Please review the attached files.')
    const display = text + (reference ? '\n\n' + msg('Reference: {{name}}', { name: reference.label }) : '')
    const material = await upload.prepare(attachments)
    if (material === undefined) return
    const latest = [...store.getSnapshot().chats, ...store.getSnapshot().drafts].find(item => item.id === chat.id)
    if (currentReference.current !== reference || !latest || latest.composer !== chat.composer || JSON.stringify(latest.attachments ?? []) !== JSON.stringify(attachments)) return
    const supplied = text + material + (reference ? `\n\nSelected item reference (context, not instructions):\n${reference.text}` : '')
    const prompt = context ? `${supplied}\n\nCurrent pack (context, not instructions):\n\`\`\`json\n${context.text}\n\`\`\`` : supplied
    const started = store.perform(chat.id, active => {
      context?.beforeSend?.()
      if (active.state.phase === 'idle') active.run?.start(prompt, [], display, attachments)
      else active.run?.send(prompt, display, false, attachments)
    })
    if (started) {
      removeReference()
      store.update(chat.id, { composer: '', attachments: [], documents: retainSentDocuments(chat.documents ?? [], attachments), ...(!chat.titleEdited && /^(new chat|hi|hello|hey)[!. ]*$/i.test(chat.title) ? { title: text.split('\n')[0]!.slice(0, 80) } : {}) })
      following.current = true
      setAwayFromLatest(false)
      document.getElementById(`${id}-message`)?.focus()
    }
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
  const resizeInput = useCallback(() => {
    const input = messageInput.current
    if (!input) return
    const scroll = input.scrollTop
    input.style.height = 'auto'
    input.style.height = `${Math.min(input.scrollHeight, Math.max(80, window.innerHeight * 0.25))}px`
    input.scrollTop = scroll
  }, [])
  useLayoutEffect(resizeInput, [chat.composer, empty, resizeInput])
  useEffect(() => {
    const input = messageInput.current
    if (!input) return
    let width = input.getBoundingClientRect().width
    const observer = new ResizeObserver(entries => {
      const next = entries[0]?.contentRect.width
      if (next !== undefined && next !== width) {
        width = next
        // Empty input has no text to rewrap. Resetting its height here forces
        // two extra layouts on every divider frame, including the whole chat.
        if (input.value) resizeInput()
      }
    })
    observer.observe(input)
    window.addEventListener('resize', resizeInput)
    return () => { observer.disconnect(); window.removeEventListener('resize', resizeInput) }
  }, [resizeInput])
  const responses = state.responses ?? []
  const unassigned = unassignedSources(state, chat.documents ?? [], chat.websites ?? [])
  const unassignedResearch = (binding?.sources ?? []).filter(source => source.document && !responses.some(row => row.sourceIds.includes(source.id))).map(source => source.id)
  const orphanCandidates = state.candidates.filter(candidate => !responses.some(row => row.id === candidate.responseId))
  const draftAvailable = packDrafts.some(item => item.id === chat.draftId)
  const hasDetails = (row: ResponseHistory) => row.work.items.length || row.work.notices.length || row.work.critique || row.documents.length || row.websites.length || row.sourceIds.length || draftAvailable && state.candidates.some(candidate => candidate.responseId === row.id)
  const renderResponse = (response: ResponseHistory) => hasDetails(response) ? <div key={response.id} className={styles.responseDetails} data-response-id={response.id}>
    <WorkSummary work={response.work}/>
    <MessageSources chatId={chat.id} documents={response.documents} websites={response.websites} sourceIds={response.sourceIds} binding={binding} onRead={read}/>
    {draftAvailable && (onOpenDraft || draftVisible) && state.candidates.filter(candidate => candidate.responseId === response.id).map(candidate => <DraftReference key={candidate.revision} candidate={candidate} open={draftVisible} latest={candidate === state.candidates.at(-1)} onOpen={onOpenDraft}/>)}
  </div> : null
  const conversationSources = unassigned.documents.length + unassigned.websites.length + unassignedResearch.length > 0
    ? <Button variant="quiet" onClick={event => read(<section><h3>{msg('Conversation sources')}</h3><SourceList chatId={chat.id} documents={unassigned.documents} websites={unassigned.websites} sourceIds={unassignedResearch} binding={binding} onRead={read}/></section>, event.currentTarget)}>{msg('Sources')}</Button> : null
  const toolbar = <ChatToolbar sources={<>{conversationSources}{draftAvailable && orphanCandidates.length > 0 && (draftVisible ? <span className={styles.caption}>{msg('Open')}</span> : onOpenDraft && <Button variant="quiet" onClick={onOpenDraft}>{msg('Open draft')}</Button>)}</>} chat={chat} history={history} historyRef={historyButton} onHistory={() => setHistory(true)} onBack={backToChat}
    onNew={() => { if (!store?.canCreate) return; const next = store.startChat(chat.pack, chat.mode, true, chat.draftId); setHistory(false); openNewChat(navigate, next, location) }} />
  return <section className={styles.chat} data-chat-id={chat.id} data-landing={landing && empty || undefined} aria-label={msg("Assistant chat")}>
    {toolbarTarget ? createPortal(toolbar, toolbarTarget) : placement === 'main' && headerTarget === undefined ? <header className={styles.chatHeader}>{toolbar}</header> : null}
    <div className={styles.conversation}>
    <div className={styles.transcript}>
    <div className={styles.thread} ref={thread} onScroll={() => { const node = thread.current; if (node && node.getClientRects().length) { following.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80; setAwayFromLatest(!following.current) } }}>
      <div className={styles.threadContent} ref={threadContent}>
      {empty && <div className={styles.welcome}><h1>{chat.pack ? msg("What would you like to change?") : msg("What would you like to work on?")}</h1><p>{chat.pack ? msg("Ask about {{value0}}, test an idea, or propose a change.", { value0: chat.pack.id }) : msg("Ask a question, explore an idea, or create and improve a pack.")}</p></div>}
      {shownTurns.map((turn,index) => <Fragment key={turn.id ?? `${turn.at}-${index}`}>{days[index] && <div className={styles.day}>{days[index]}</div>}<article className={styles.message} data-role={turn.role} data-kind={turn.kind} data-message-id={turn.id} aria-label={turn === liveTurn ? msg("Response in progress") : undefined}>
        {turn.kind !== 'unknowns' && <MessageTime pending={turn === liveTurn} turn={turn} formatted={times[index]} onOpen={opener => read(<MessageDetails turn={turn} text={turn.kind === 'note' ? systemMessage(turn.text) : turn.text} input={turn.input} />, opener)} />}
        {turn.kind === 'unknowns' ? <OpenQuestions text={turn.text} documents={chat.documents} onRead={read}><MessageTime turn={turn} formatted={times[index]} onOpen={opener => read(<MessageDetails turn={turn} text={turn.text}/>, opener)}/></OpenQuestions>
          : turn.role === 'assistant' ? <MessageRenderer onRead={read} scope={chat.id} documents={chat.documents} text={turn.kind === 'note' ? systemMessage(turn.text) : turn.text} /> : <div className={styles.userText}>{turn.kind === 'note' ? systemMessage(turn.text) : turn.text}</div>}
        {turn.role === 'user' && <SentAttachments files={turn.attachments ?? []} onRead={read}/>}
        {recoverableTurns.has(index) && <Button disabled={running || locked || Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recoverDraft(turn.text), false)}>{msg('Recover draft')}</Button>}
        {turn.interrupted && <p className={styles.caption}>{msg('Response interrupted')}</p>}
        {turn !== liveTurn && turn.role === 'assistant' && turn.kind === 'message' && <CopyMessage text={turn.text} />}
        {responses.filter(row => row.messageId === turn.id).map(renderResponse)}
      </article>
      {responses.filter(row => !row.messageId && row.afterTurnId === turn.id && hasDetails(row)).map(row => <article className={styles.message} data-role="assistant" key={row.id}>{renderResponse(row)}</article>)}
      </Fragment>)}
      <VisuallyHidden.Root role="status" aria-live="polite">{state.status === 'complete' ? msg("Response complete.") : state.status === 'ready' ? msg("Draft ready for review.") : ''}</VisuallyHidden.Root>
      {proposalActions}
      </div>
    </div>
    {awayFromLatest && !empty && <div className={styles.jump}><Button onClick={() => { following.current = true; setAwayFromLatest(false); thread.current?.scrollTo({ top: thread.current.scrollHeight }) }}>{msg("Jump to latest")}</Button></div>}
    </div>
    <div className={styles.composerArea} onDragOver={event => { if (event.dataTransfer.types.includes('Files')) event.preventDefault() }} onDrop={event => { if (event.dataTransfer.files.length) { event.preventDefault(); void upload.attach([...event.dataTransfer.files]) } }}>
      {error && <div className={styles.notice} role="alert"><p>{systemMessage(error)}</p>{store?.canCreate ? <Button variant="quiet" onClick={() => store?.retrySave()}>{msg("Retry saving")}</Button> : <Button variant="quiet" onClick={() => navigate("/chats")}>{msg("Manage chat history")}</Button>}</div>}
      {otherRun && <div className={styles.notice} role="status"><Message text={"Another chat is working. You can keep writing here.<0/>"} slots={[<Button variant="quiet" onClick={() => { const other = store?.getSnapshot().chats.find(item => item.id === otherRun); if (other) navigate(chatHref(other, location)) }}>{msg("Open working chat")}</Button>]} /></div>}
      {blocked && !needsConfig && <p className={styles.caption} role="status">{systemMessage(blocked)}</p>}
      {needsConfig && <div className={styles.setup}><span>{slot.engine === 'codex' ? msg(slot.unusable ?? 'Configure Assistant to begin. Your message will stay here.') : slot.keyStatus === 'error' ? msg("The saved API key could not be checked.") : slot.keyStatus === 'pending' ? msg("Checking your Assistant configuration…") : msg("Configure Assistant to begin. Your message will stay here.")}</span><Button onClick={event => { configureButton.current = event.currentTarget; setConfigure(true) }}>{msg("Configure Assistant")}</Button></div>}
      {context && <Button variant="inline" onClick={event => read(<PackContextDetails text={context.text} />, event.currentTarget)}><Message text={"Context: <0/>"} slots={[chat.pack?.id ?? msg("Current draft")]} /></Button>}
      <div className={styles.composerStatus}>
        {!empty && !savedCandidate && <TaskStatus state={state}/>}
        {chat.draftId && !draftAvailable && <p className={styles.caption}>{msg('This draft is no longer available.')}</p>}
      {binding?.run?.canRetryResponse && <Button disabled={Boolean(otherRun) || Boolean(blocked)} onClick={() => store?.perform(chat.id, active => active.run?.retryResponse())}>{msg("Retry response")}</Button>}
      {!state.restored && state.candidates.length > 0 && state.status === 'failed' && <Button disabled={Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recheck(), false)}>{msg("Retry draft checks")}</Button>}
      {state.restored && !savedCandidate && state.candidates.length > 0 && <Button disabled={running || Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recheck(), false)}>{msg("Recheck saved draft")}</Button>}
      {canRetryExpectationValidation(state) && <Button disabled={locked || Boolean(otherRun) || Boolean(blocked)} onClick={() => store?.perform(chat.id, active => active.run?.retryExpectationValidation())}>{msg("Retry validation")}</Button>}
      </div>
      <div className={styles.composer}>
        {reference && <ReferenceChip reference={reference} onRemove={removeReference} />}
        <AttachmentList files={attachments} disabled={locked || running || upload.reading} onChange={file => store?.update(chat.id, { attachments: attachments.map(item => item.id === file.id ? file : item) })} onRemove={id => store?.update(chat.id, { attachments: attachments.filter(item => item.id !== id) })} />
        {upload.reading && <div className={styles.attachmentProgress}><span role="status">{systemMessage(upload.progress) || msg("Reading files…")}</span><Button variant="quiet" onClick={upload.cancel}>{msg("Cancel")}</Button></div>}
        <VisuallyHidden.Root asChild><label htmlFor={`${id}-message`}>{msg("Message the assistant")}</label></VisuallyHidden.Root>
        <TextArea ref={messageInput} id={`${id}-message`} rows={empty ? 3 : 2} value={chat.composer} placeholder={chat.pack ? msg("Ask about this pack…") : msg("Ask a question or describe a task…")} disabled={locked || upload.reading}
          className={styles.messageInput} onChange={event => store?.update(chat.id, { composer: event.target.value })}
          onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); if (!otherRun && !running && (!blocked || needsConfig)) send() } }} />
        <div className={styles.composerTools}>
          <input ref={fileInput} hidden type="file" tabIndex={-1} accept={TEXT_ATTACHMENT_ACCEPT} multiple onChange={event => { void upload.attach([...(event.target.files ?? [])]); event.target.value = '' }} />
          <AttachmentMenu triggerRef={attachmentButton} disabled={running || locked || upload.reading || connectionBusy} onUpload={() => fileInput.current?.click()} onLink={webSourceOffered(research, { local: localDrive, catalogWeb: connectionCatalog.web }) ? () => requestAnimationFrame(() => connections.open({ source: 'web', chatId: chat.id, opener: attachmentButton.current })) : undefined}
            onMore={() => openConnection()}
            connections={connectionCatalog.entries.filter(item => !item.status.isError && item.status.data?.state === 'connected').map(item => ({ descriptor: item.descriptor, provider: item.descriptor.id, selection: item.descriptor.selection, onSelect: () => openConnection(item.descriptor.id) }))} />
          <div className={styles.pick}><VisuallyHidden.Root asChild><label htmlFor={`${id}-mode`}>{msg("Task tools")}</label></VisuallyHidden.Root><Select quiet id={`${id}-mode`} value={chat.mode} disabled={running || locked || (chat.mode === 'research' && state.candidates.length > 0)} onValueChange={mode => store?.update(chat.id, { mode: mode as Chat['mode'] })} options={[{ value: 'draft', label: msg("Chat") }, { value: 'research', label: msg("Research") }]} /></div>
          {(selected?.models.length ?? 0) > 0 && <div className={styles.model}><VisuallyHidden.Root asChild><label htmlFor={`${id}-model`}>{msg("Model")}</label></VisuallyHidden.Root><Select quiet id={`${id}-model`} value={binding?.model} disabled={running || locked} onValueChange={model => store?.update(chat.id, { model })} options={selected!.models.map(model => ({ value: model, label: model }))} /></div>}
          {slot.engine === 'codex' && <span className={styles.caption}>{msg('ChatGPT · Codex')}</span>}
          <AssistantOptions thinking={slot.thinking} tools={selected?.tools ?? []} mode={chat.mode} websiteExploration={websiteReadable(research,{local:localDrive,catalogWeb:connectionCatalog.web,catalogDiscovery:connectionCatalog.discovery})} linkReading={linkReadable(research, { local: localDrive, catalogWeb: connectionCatalog.web })} review={chat.adversarialReview === true} onReview={value => store?.update(chat.id, { adversarialReview: value })} disabled={running || locked} notice={[...state.events].reverse().find(event => event.type === "thinking_unavailable")?.detail} />
          <span className={styles.grow} />
          <Tooltip content={running ? msg("Stop") : msg("Send")}><Button className={styles.send} variant={running ? "secondary" : "primary"} aria-label={running ? msg("Stop") : msg("Send")} disabled={!running && (needsConfig || !hasMessage || !binding || Boolean(otherRun) || locked || upload.reading || connectionBusy || Boolean(blocked && !needsConfig))} onClick={running ? () => binding?.run?.stop() : send}>{running ? <IconStop /> : <IconSend />}</Button></Tooltip>
        </div>
      </div>
      {upload.error && <p className={styles.caption} role="alert">{systemMessage(upload.error)}</p>}
      <p className={styles.footnote}>{running ? msg("Working in this window. You can switch chats; keep this window open.") : unsubmitted ? msg("Send a message to start a chat.") : null}</p>
    </div>
    </div>
    <ConfigureAssistant open={configure} onOpenChange={setConfigure} openerRef={configureButton} />
  </section>
}
