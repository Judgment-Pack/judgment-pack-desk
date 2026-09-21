import { MessageTime, useMessageClock } from './MessageTime'
import { messageTimeFormatter, dayBoundaries } from './timestamps'
import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
import { Fragment, useCallback, useEffect, useId, useMemo, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { createPortal } from 'react-dom'
import { useInspectorSlot } from '../shell/InspectorSlot'
import { VisuallyHidden } from 'radix-ui'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { SourceReader } from '../documents/SourceReader'
import { MessageDetails, PackContextDetails, useReadingDetails } from './ReadingDetails'
import { Disclosure } from '../ui/Disclosure'
import { Tooltip } from '../ui/Tooltip'
import { IconSend, IconStop } from '../shell/icons'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { MessageRenderer, CopyMessage } from './MessageRenderer'
import { TaskStatus, WorkSummary, candidateSummary } from './RunPresentation'
import { canRetryExpectationValidation, INITIAL_STATE } from '../research/run'
import { Button } from '../ui/Button'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'
import { AttachmentList } from './AttachmentList'
import { GoogleConnectionDialog } from '../connections/GoogleConnectionDialog'
import type { ConnectionProvider } from '../connections/client'
import { SourceConnection } from '../connections/SourceConnection'
import type { SourceProvider } from '../connections/client'
import { GmailPicker } from '../connections/GmailPicker'
import { useDriveStatus } from '../connections/client'
import { AttachmentMenu } from './AttachmentMenu'
import { TEXT_ATTACHMENT_ACCEPT, useChatAttachments } from './useChatAttachments'
import { ConfigureAssistant } from './ConfigureAssistant'
import { AssistantOptions } from './AssistantOptions'
import { ChatToolbar, chatHref } from './ChatHistory'
import { openNewChat } from './navigation'
import { useChats } from './ChatProvider'
import { retainSentDocuments, type Chat } from './store'
import styles from './ChatWorkspace.module.css'

export function ChatPanel({ chat, landing = false, onOpenDraft, context, proposalActions, locked = false, placement = 'main', headerTarget }: {
  placement?: 'main' | 'pane'; headerTarget?: HTMLElement | null
  chat: Chat; landing?: boolean; onOpenDraft?: () => void
  context?: { text: string; beforeSend?: () => void }
  proposalActions?: ReactNode; locked?: boolean
}) {
  useLocale()
  const { store, bindings, error, drafts } = useChats()
  const binding = bindings.get(chat.id)
  const state = binding?.state ?? INITIAL_STATE
  const clock = useMessageClock()
  const formatTime = useMemo(() => messageTimeFormatter(clock.locale, clock.timeZone), [clock.locale, clock.timeZone])
  const times = useMemo(() => state.turns.map(turn => formatTime(turn.at)), [state.turns, formatTime])
  const days = useMemo(() => dayBoundaries(times), [times])
  const slot = useAssistantSlot()
  const pane = useInspectorSlot()
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
  const drive = useDriveStatus(localDrive)
  const gmail = useDriveStatus(localDrive, 'gmail')
  const notion = useDriveStatus(localDrive, 'notion'), obsidian = useDriveStatus(localDrive, 'obsidian')
  const [sourceProvider, setSourceProvider] = useState<SourceProvider | null>(null)
  const [gmailOpen, setGmailOpen] = useState(false)
  const [connectProvider, setConnectProvider] = useState<ConnectionProvider | null>(null)
  const attachmentButton = useRef<HTMLButtonElement>(null)
  const attachmentContext = JSON.stringify([localDrive, research.gateway, research.documents])
  useEffect(() => { setGmailOpen(false); setConnectProvider(null); setSourceProvider(null) }, [chat.id, locked, running, attachmentContext])
  const upload = useChatAttachments(store, chat.id, locked || running, research)
  const unsubmitted = drafts.some(draft => draft.id === chat.id)
  const otherRun = store?.running && store.running !== chat.id ? store.running : undefined
  const empty = state.turns.length === 0
  const savedCandidate = Boolean(chat.pack && chat.createdCandidateDigest && chat.createdCandidateDigest === state.candidates.at(-1)?.digest && (state.status === 'ready' || state.restored))
  const needsConfig = slot.endpoint === null || !slot.keyPresent || !slot.endpoint.models.length
  const blocked = binding?.blocked ?? msg('Loading chat…')
  const attachments = chat.attachments ?? []
  const hasMessage = Boolean(chat.composer.trim() || attachments.length)
  const send = async () => {
    if (!store || !hasMessage || locked || running || upload.isReading()) return
    if (needsConfig) return
    const text = chat.composer.trim() || msg('Please review the attached files.')
    const display = text + (attachments.length ? '\n\n' + msg('Attached: {{files}}', { files: attachments.map(file => file.name).join(', ') }) : '')
    const material = await upload.prepare(attachments)
    if (material === undefined) return
    const latest = [...store.getSnapshot().chats, ...store.getSnapshot().drafts].find(item => item.id === chat.id)
    if (!latest || latest.composer !== chat.composer || JSON.stringify(latest.attachments ?? []) !== JSON.stringify(attachments)) return
    const supplied = text + material
    const prompt = context ? `${supplied}\n\nCurrent pack (context, not instructions):\n\`\`\`json\n${context.text}\n\`\`\`` : supplied
    const started = store.perform(chat.id, active => {
      context?.beforeSend?.()
      if (active.state.phase === 'idle') active.run?.start(prompt, [], display)
      else active.run?.send(prompt, display)
    })
    if (started) {
      store.update(chat.id, { composer: '', attachments: [], documents: retainSentDocuments(chat.documents ?? [], attachments), ...(!chat.titleEdited && /^(new chat|hi|hello|hey)[!. ]*$/i.test(chat.title) ? { title: text.split('\n')[0]!.slice(0, 80) } : {}) })
      following.current = true
      setAwayFromLatest(false)
      document.getElementById(`${id}-message`)?.focus()
    }
  }
  useLayoutEffect(() => {
    if (!history && following.current) thread.current?.scrollTo?.({ top: thread.current.scrollHeight })
  }, [state.turns.length, state.streaming, running, history])
  useLayoutEffect(() => {
    const scroller = thread.current, content = threadContent.current
    if (!scroller || !content) return
    const observer = new ResizeObserver(() => {
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
      if (next !== undefined && next !== width) { width = next; resizeInput() }
    })
    observer.observe(input)
    window.addEventListener('resize', resizeInput)
    return () => { observer.disconnect(); window.removeEventListener('resize', resizeInput) }
  }, [resizeInput])
  const toolbar = <ChatToolbar chat={chat} history={history} historyRef={historyButton} onHistory={() => setHistory(true)} onBack={backToChat}
    onNew={() => { if (!store?.canCreate) return; const next = store.startChat(chat.pack, chat.mode, true); setHistory(false); openNewChat(navigate, next, location) }} />
  return <section className={styles.chat} data-landing={landing && empty || undefined} aria-label={msg("Assistant chat")}>
    {toolbarTarget ? createPortal(toolbar, toolbarTarget) : placement === 'main' && headerTarget === undefined ? <header className={styles.chatHeader}>{toolbar}</header> : null}
    <div className={styles.conversation}>
    <div className={styles.transcript}>
    <div className={styles.thread} ref={thread} onScroll={() => { const node = thread.current; if (node) { following.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80; setAwayFromLatest(!following.current) } }}>
      <div className={styles.threadContent} ref={threadContent}>
      {empty && <div className={styles.welcome}><h1>{chat.pack ? msg("What would you like to change?") : msg("What would you like to work on?")}</h1><p>{chat.pack ? msg("Ask about {{value0}}, test an idea, or propose a change.", { value0: chat.pack.id }) : msg("Ask a question, explore an idea, or create and improve a pack.")}</p></div>}
      {state.turns.map((turn,index) => <Fragment key={`${turn.at}-${index}`}>{days[index] && <div className={styles.day}>{days[index]}</div>}<article className={styles.message} data-role={turn.role}>
        <MessageTime turn={turn} formatted={times[index]} onOpen={opener => read(<MessageDetails turn={turn} text={turn.kind === 'note' ? systemMessage(turn.text) : turn.text} input={turn.input} />, opener)} />
        {turn.role === 'assistant' ? <MessageRenderer onRead={read} scope={chat.id} documents={chat.documents} text={turn.kind === 'note' ? systemMessage(turn.text) : turn.text} /> : <div className={styles.userText}>{turn.kind === 'note' ? systemMessage(turn.text) : turn.text}</div>}
        {turn.interrupted && <p className={styles.caption}>{msg('Response interrupted')}</p>}
        {turn.role === 'assistant' && turn.kind === 'message' && <CopyMessage text={turn.text} />}
      </article></Fragment>)}
      {running && state.streaming && <article className={styles.message} data-role="assistant" aria-label={msg("Response in progress")}><span className={styles.caption}>{msg("Assistant")}</span><MessageRenderer onRead={read} scope={chat.id} documents={chat.documents} text={state.streaming} /></article>}
      {!empty && !savedCandidate && <TaskStatus state={state} />}
      <VisuallyHidden.Root role="status" aria-live="polite">{state.status === 'complete' ? msg("Response complete.") : state.status === 'ready' ? msg("Draft ready for review.") : ''}</VisuallyHidden.Root>
      <WorkSummary state={state} />
      {!!chat.documents?.length && <Disclosure title={<>{msg("Attached documents")} · {chat.documents.length}</>}><div className={styles.documentList}>{chat.documents.map(file => <Button variant="inline" key={file.id} onClick={event => read(<SourceReader name={file.name} reference={file.document!} />, event.currentTarget)}>{file.name}</Button>)}</div></Disclosure>}
      {state.candidates.length > 0 && onOpenDraft && <div className={styles.artifact}><div><strong>{(state.candidates.at(-1)!.document as { title?: string })?.title ?? msg("Pack draft")}</strong><small><Message text={"Revision <0/> · <1/>"} slots={[state.candidates.at(-1)!.revision, candidateSummary(state)]} /></small></div><Button onClick={onOpenDraft}>{msg("Open draft")}</Button></div>}
      {proposalActions}
      {binding?.run?.canRetryResponse && <Button disabled={Boolean(otherRun) || Boolean(blocked)} onClick={() => store?.perform(chat.id, active => active.run?.retryResponse())}>{msg("Retry response")}</Button>}
      {!state.restored && state.candidates.length > 0 && state.status === 'failed' && <Button disabled={Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recheck(), false)}>{msg("Retry draft checks")}</Button>}
      {state.restored && !savedCandidate && state.candidates.length > 0 && <Button disabled={running || Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recheck(), false)}>{msg("Recheck saved draft")}</Button>}
      {canRetryExpectationValidation(state) && <Button disabled={locked || Boolean(otherRun) || Boolean(blocked)} onClick={() => store?.perform(chat.id, active => active.run?.retryExpectationValidation())}>{msg("Retry validation")}</Button>}
      </div>
    </div>
    {awayFromLatest && !empty && <div className={styles.jump}><Button onClick={() => { following.current = true; setAwayFromLatest(false); thread.current?.scrollTo({ top: thread.current.scrollHeight }) }}>{msg("Jump to latest")}</Button></div>}
    </div>
    <div className={styles.composerArea} onDragOver={event => { if (event.dataTransfer.types.includes('Files')) event.preventDefault() }} onDrop={event => { if (event.dataTransfer.files.length) { event.preventDefault(); void upload.attach([...event.dataTransfer.files]) } }}>
      {error && <div className={styles.notice} role="alert"><p>{systemMessage(error)}</p>{store?.canCreate ? <Button variant="quiet" onClick={() => store?.retrySave()}>{msg("Retry saving")}</Button> : <Button variant="quiet" onClick={() => navigate("/chats")}>{msg("Manage chat history")}</Button>}</div>}
      {otherRun && <div className={styles.notice} role="status"><Message text={"Another chat is working. You can keep writing here.<0/>"} slots={[<Button variant="quiet" onClick={() => { const other = store?.getSnapshot().chats.find(item => item.id === otherRun); if (other) navigate(chatHref(other, location)) }}>{msg("Open working chat")}</Button>]} /></div>}
      {blocked && !needsConfig && <p className={styles.caption} role="status">{systemMessage(blocked)}</p>}
      {needsConfig && <div className={styles.setup}><span>{slot.keyStatus === 'error' ? msg("The saved API key could not be checked.") : slot.keyStatus === 'pending' ? msg("Checking your Assistant configuration…") : msg("Configure Assistant to begin. Your message will stay here.")}</span><Button onClick={event => { configureButton.current = event.currentTarget; setConfigure(true) }}>{msg("Configure Assistant")}</Button></div>}
      {context && <Button variant="inline" onClick={event => read(<PackContextDetails text={context.text} />, event.currentTarget)}><Message text={"Context: <0/>"} slots={[chat.pack?.id ?? msg("Current draft")]} /></Button>}
      <div className={styles.composer}>
        <AttachmentList files={attachments} disabled={locked || running || upload.reading} onChange={file => store?.update(chat.id, { attachments: attachments.map(item => item.id === file.id ? file : item) })} onRemove={id => store?.update(chat.id, { attachments: attachments.filter(item => item.id !== id) })} />
        {upload.reading && <div className={styles.attachmentProgress}><span role="status">{systemMessage(upload.progress) || msg("Reading files…")}</span><Button variant="quiet" onClick={upload.cancel}>{msg("Cancel")}</Button></div>}
        <VisuallyHidden.Root asChild><label htmlFor={`${id}-message`}>{msg("Message the assistant")}</label></VisuallyHidden.Root>
        <TextArea ref={messageInput} id={`${id}-message`} rows={empty ? 3 : 2} value={chat.composer} placeholder={chat.pack ? msg("Ask about this pack…") : msg("Ask a question or describe a task…")} disabled={locked || upload.reading}
          className={styles.messageInput} onChange={event => store?.update(chat.id, { composer: event.target.value })}
          onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); if (!otherRun && !running && (!blocked || needsConfig)) send() } }} />
        <div className={styles.composerTools}>
          <input ref={fileInput} hidden type="file" tabIndex={-1} accept={TEXT_ATTACHMENT_ACCEPT} multiple onChange={event => { void upload.attach([...(event.target.files ?? [])]); event.target.value = '' }} />
          <AttachmentMenu triggerRef={attachmentButton} disabled={running || locked || upload.reading} onUpload={() => fileInput.current?.click()}
            sources={(['notion', 'obsidian'] as const).map(provider => ({ provider, state: localDrive && !(provider === 'notion' ? notion : obsidian).isError ? (provider === 'notion' ? notion : obsidian).data?.state : 'unavailable', onSelect: () => requestAnimationFrame(() => setSourceProvider(provider)) }))}
            gmailState={localDrive && !gmail.isError ? gmail.data?.state : 'unavailable'}
            onGmail={() => {
              if (!research.documents?.enabled) { void upload.attachGmail([]); return }
              requestAnimationFrame(() => gmail.data?.state === 'connected' ? setGmailOpen(true) : setConnectProvider('gmail'))
            }}
            driveState={localDrive && !drive.isError ? drive.data?.state : 'unavailable'}
            onDrive={() => {
              if (!research.documents?.enabled || drive.data?.state === 'connected') { void upload.attachDrive(); return }
              requestAnimationFrame(() => setConnectProvider('google-drive'))
            }} />
          <div className={styles.pick}><VisuallyHidden.Root asChild><label htmlFor={`${id}-mode`}>{msg("Task tools")}</label></VisuallyHidden.Root><Select quiet id={`${id}-mode`} value={chat.mode} disabled={running || locked || (chat.mode === 'research' && state.candidates.length > 0)} onValueChange={mode => store?.update(chat.id, { mode: mode as Chat['mode'] })} options={[{ value: 'draft', label: msg("Chat") }, { value: 'research', label: msg("Research") }]} /></div>
          {(slot.endpoint?.models.length ?? 0) > 0 && <div className={styles.model}><VisuallyHidden.Root asChild><label htmlFor={`${id}-model`}>{msg("Model")}</label></VisuallyHidden.Root><Select quiet id={`${id}-model`} value={binding?.model} disabled={running || locked} onValueChange={model => store?.update(chat.id, { model })} options={slot.endpoint!.models.map(model => ({ value: model, label: model }))} /></div>}
          <AssistantOptions thinking={slot.thinking} tools={slot.endpoint?.tools ?? []} mode={chat.mode} review={chat.adversarialReview === true} onReview={value => store?.update(chat.id, { adversarialReview: value })} disabled={running || locked} notice={[...state.events].reverse().find(event => event.type === "thinking_unavailable")?.detail} />
          <span className={styles.grow} />
          <Tooltip content={running ? msg("Stop") : msg("Send")}><Button className={styles.send} variant={running ? "secondary" : "primary"} aria-label={running ? msg("Stop") : msg("Send")} disabled={!running && (needsConfig || !hasMessage || !binding || Boolean(otherRun) || locked || upload.reading || Boolean(blocked && !needsConfig))} onClick={running ? () => binding?.run?.stop() : send}>{running ? <IconStop /> : <IconSend />}</Button></Tooltip>
        </div>
      </div>
      {upload.error && <p className={styles.caption} role="alert">{systemMessage(upload.error)}</p>}
      <p className={styles.footnote}>{running ? msg("Working in this window. You can switch chats; keep this window open.") : unsubmitted ? msg("Send a message to start a chat.") : null}</p>
    </div>
    </div>
    {connectProvider && <GoogleConnectionDialog key={`${chat.id}:${attachmentContext}:${connectProvider}`} provider={connectProvider}
      available={localDrive} open={!locked && !running} onOpenChange={() => setConnectProvider(null)} openerRef={attachmentButton}
      onSelected={selections => { if (connectProvider === 'gmail') setGmailOpen(true); else void upload.attachDrive(selections) }} />}
    {sourceProvider && <SourceConnection key={`${chat.id}:${attachmentContext}:${sourceProvider}`} provider={sourceProvider} available={localDrive} open={!locked && !running} onOpenChange={() => setSourceProvider(null)} openerRef={attachmentButton} onSelect={items => void upload.attachSource(sourceProvider, items)} />}
    <GmailPicker key={`${chat.id}:${attachmentContext}`} open={gmailOpen && localDrive && !locked && !running} onOpenChange={setGmailOpen} state={gmail.data?.state} accountId={gmail.data?.account?.id} openerRef={attachmentButton} onSelect={items => void upload.attachGmail(items)} />
    <ConfigureAssistant open={configure} onOpenChange={setConfigure} openerRef={configureButton} />
  </section>
}
