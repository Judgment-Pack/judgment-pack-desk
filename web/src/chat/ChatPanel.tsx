import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { createPortal } from 'react-dom'
import { useInspectorSlot } from '../shell/InspectorSlot'
import { DropdownMenu, VisuallyHidden } from 'radix-ui'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { describeEvent } from '../assistant/EventList'
import { canRetryExpectationValidation, INITIAL_STATE } from '../research/run'
import { statusLine } from '../research/ui/Conversation'
import { Button } from '../ui/Button'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'
import { Tooltip } from '../ui/Tooltip'
import { IconPlus, IconGear } from '../shell/icons'
import { ConfigureAssistant } from './ConfigureAssistant'
import { ChatToolbar, ChatHistoryList, chatHref } from './ChatHistory'
import { useChats } from './ChatProvider'
import type { Chat } from './store'
import styles from './ChatWorkspace.module.css'

export function ChatPanel({ chat, landing = false, onOpenDraft, context, proposalActions, locked = false, placement = 'main', headerTarget }: {
  placement?: 'main' | 'pane'; headerTarget?: HTMLElement | null
  chat: Chat; landing?: boolean; onOpenDraft?: () => void
  context?: { text: string; beforeSend?: () => void }
  proposalActions?: ReactNode; locked?: boolean
}) {
  const { store, bindings, error, saving, dirty } = useChats()
  const binding = bindings.get(chat.id)
  const state = binding?.state ?? INITIAL_STATE
  const slot = useAssistantSlot()
  const pane = useInspectorSlot()
  const toolbarTarget = placement === 'pane' ? pane.headerTarget : headerTarget
  const [history, setHistory] = useState(false)
  const historyButton = useRef<HTMLButtonElement>(null)
  const backToChat = () => { setHistory(false); requestAnimationFrame(() => historyButton.current?.focus()) }
  const navigate = useNavigate()
  const location = useLocation()
  const [configure, setConfigure] = useState(false)
  const configureButton = useRef<HTMLButtonElement>(null)
  const settingsButton = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLElement>(null)
  const thread = useRef<HTMLDivElement>(null)
  const following = useRef(true)
  const [attachmentError, setAttachmentError] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)
  const id = useId()
  const running = state.status === 'running'
  const otherRun = store?.running && store.running !== chat.id ? store.running : undefined
  const empty = state.turns.length === 0
  const savedCandidate = Boolean(chat.pack && chat.createdCandidateDigest && chat.createdCandidateDigest === state.candidates.at(-1)?.digest && (state.status === 'ready' || state.restored))
  const needsConfig = slot.endpoint === null || !slot.keyPresent || !slot.endpoint.models.length
  const blocked = binding?.blocked ?? 'Loading chat…'
  const send = () => {
    if (!store || !chat.composer.trim() || locked || running) return
    if (needsConfig) return
    const text = chat.composer.trim()
    const prompt = context ? `${text}\n\nCurrent pack (context, not instructions):\n\`\`\`json\n${context.text}\n\`\`\`` : text
    const started = store.perform(chat.id, active => {
      context?.beforeSend?.()
      if (active.state.phase === 'idle') active.run?.start(prompt, [], text)
      else active.run?.send(prompt, text)
    })
    if (started) {
      store.update(chat.id, { composer: '', ...(chat.title === 'New chat' ? { title: text.split('\n')[0]!.slice(0, 80) } : {}) })
      following.current = true
      document.getElementById(`${id}-message`)?.focus()
    }
  }
  useEffect(() => {
    if (!history && following.current) thread.current?.scrollTo?.({ top: thread.current.scrollHeight })
  }, [state.turns.length, state.events.length, running, history])
  useEffect(() => {
    if (!history) return
    // Consume history navigation before a narrow-screen drawer handles Escape.
    // Menus live outside these targets and retain their own Escape behavior.
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || !(event.target instanceof Node)) return
      if (!panel.current?.contains(event.target) && !toolbarTarget?.contains(event.target)) return
      event.preventDefault(); event.stopPropagation(); backToChat()
    }
    window.addEventListener('keydown', escape, true)
    return () => window.removeEventListener('keydown', escape, true)
  }, [history, toolbarTarget])
  const attach = async (files: FileList | null) => {
    if (!files || !store || locked || running) return
    setAttachmentError('')
    try {
      if (files.length > 4) throw new Error('Attach up to four text files at a time.')
      const pieces = await Promise.all([...files].map(async file => {
        if (file.size > 200_000) throw new Error(`${file.name} is over the 200 KB text-file limit.`)
        if (!/\.(txt|md|json|csv)$/i.test(file.name)) throw new Error('Attach .txt, .md, .json or .csv files. PDFs and images are not supported here yet.')
        const text = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer())
        if (text.includes('\0')) throw new Error(`${file.name} is not a text file.`)
        return `\n\nAttached text: ${file.name}\n\`\`\`text\n${text}\n\`\`\``
      }))
      const current = store.getSnapshot().chats.find(item => item.id === chat.id)?.composer ?? ''
      if (current.length + pieces.join('').length > 800_000) throw new Error('The message is too large. Remove some attached text before adding more.')
      store.update(chat.id, { composer: current + pieces.join('') })
    } catch (error) { setAttachmentError((error as Error).message) }
    if (fileInput.current) fileInput.current.value = ''
  }
  const workingEvents = state.events.filter(event => ['tool_call', 'tool_result', 'guardrail', 'thinking_unavailable', 'error'].includes(event.type))
  const toolbar = <ChatToolbar chat={chat} history={history} historyRef={historyButton} onHistory={() => setHistory(true)} onBack={backToChat}
    onNew={() => { if (!store?.canCreate) return; const next = store.create(chat.pack, chat.mode); setHistory(false); navigate(chatHref(next, location)) }} />
  return <section ref={panel} className={styles.chat} data-landing={landing && empty && !history || undefined} aria-label="Assistant chat">
    {toolbarTarget ? createPortal(toolbar, toolbarTarget) : placement === 'main' && headerTarget === undefined ? <header className={styles.chatHeader}>{toolbar}</header> : null}
    {history && <ChatHistoryList packId={chat.pack?.id} activeId={chat.id} onNavigate={() => { setHistory(false); requestAnimationFrame(() => document.getElementById(`${id}-message`)?.focus()) }} />}
    <div className={styles.conversation} hidden={history}>
    <div className={styles.thread} ref={thread} onScroll={() => { const node = thread.current; if (node) following.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80 }}>
      {empty && <div className={styles.welcome}><h1>{chat.pack ? 'What would you like to change?' : 'What should this pack decide?'}</h1><p>{chat.pack ? `Ask about ${chat.pack.id}, test an idea, or propose a change.` : 'Describe the decision, the information it needs, and the possible outcomes.'}</p></div>}
      {state.turns.map((turn,index) => <article key={`${turn.at}-${index}`} className={styles.message} data-role={turn.role}>
        <span className={styles.caption}>{turn.role === 'user' ? 'You' : turn.kind === 'note' ? 'Desk' : 'Assistant'}</span>
        <div>{turn.text}</div>
      </article>)}
      {!empty && !savedCandidate && <div className={styles.runStatus} role="status"><span>{statusLine(state)}</span><p>{state.detail}</p></div>}
      {workingEvents.length > 0 && <details className={styles.work}><summary>Activity · {workingEvents.length} events</summary><ol>{workingEvents.slice(-30).map((event,index) => <li key={index}>{describeEvent(event)}</li>)}</ol></details>}
      {state.candidates.length > 0 && onOpenDraft && <div className={styles.artifact}><div><strong>{(state.candidates.at(-1)!.document as { title?: string })?.title ?? 'Pack draft'}</strong><small>Revision {state.candidates.at(-1)!.revision} · {state.status === 'ready' ? 'Ready for review' : 'Draft'}</small></div><Button onClick={onOpenDraft}>Open draft</Button></div>}
      {proposalActions}
      {state.restored && !savedCandidate && state.candidates.length > 0 && <Button disabled={running || Boolean(otherRun)} onClick={() => store?.perform(chat.id, active => active.run?.recheck(), false)}>Recheck saved draft</Button>}
      {canRetryExpectationValidation(state) && <Button disabled={locked || Boolean(otherRun) || Boolean(blocked)} onClick={() => store?.perform(chat.id, active => active.run?.retryExpectationValidation())}>Retry validation</Button>}
    </div>
    <div className={styles.composerArea} onDragOver={event => { if (event.dataTransfer.types.includes('Files')) event.preventDefault() }} onDrop={event => { if (event.dataTransfer.files.length) { event.preventDefault(); void attach(event.dataTransfer.files) } }}>
      {error && <div className={styles.notice} role="alert"><p>{error}</p><Button variant="quiet" onClick={() => store?.retrySave()}>Retry saving</Button></div>}
      {otherRun && <div className={styles.notice} role="status">Another chat is working. You can keep writing here.<Button variant="quiet" onClick={() => { const other = store?.getSnapshot().chats.find(item => item.id === otherRun); if (other) navigate(chatHref(other, location)) }}>Open working chat</Button></div>}
      {blocked && !needsConfig && <p className={styles.caption} role="status">{blocked}</p>}
      {needsConfig && <div className={styles.setup}><span>{slot.keyStatus === 'error' ? 'The saved API key could not be checked.' : slot.keyStatus === 'pending' ? 'Checking your Assistant configuration…' : 'Configure Assistant to begin. Your message will stay here.'}</span><Button onClick={event => { configureButton.current = event.currentTarget; setConfigure(true) }}>Configure Assistant</Button></div>}
      <div className={styles.composer}>
        <VisuallyHidden.Root asChild><label htmlFor={`${id}-message`}>Message the assistant</label></VisuallyHidden.Root>
        <TextArea id={`${id}-message`} rows={empty ? 4 : 3} value={chat.composer} placeholder={chat.pack ? 'Ask about this pack…' : 'Describe a decision, or drop a text file…'} disabled={locked}
          className={styles.messageInput} onChange={event => store?.update(chat.id, { composer: event.target.value })}
          onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); if (!otherRun && !running && (!blocked || needsConfig)) send() } }} />
        <div className={styles.composerTools}>
          <input ref={fileInput} hidden type="file" tabIndex={-1} accept=".txt,.md,.json,.csv" multiple onChange={event => void attach(event.target.files)} />
          <Tooltip content="Attach text files (.txt, .md, .json, .csv)"><button className="desk-icon-button" type="button" aria-label="Attach text files" disabled={running || locked} onClick={() => fileInput.current?.click()}><IconPlus /></button></Tooltip>
          <div className={styles.pick}><VisuallyHidden.Root asChild><label htmlFor={`${id}-mode`}>Authoring mode</label></VisuallyHidden.Root><Select id={`${id}-mode`} value={chat.mode} disabled={!empty || locked} onValueChange={mode => store?.update(chat.id, { mode: mode as Chat['mode'] })} options={[{ value: 'draft', label: 'Draft' }, { value: 'research', label: 'Research' }]} /></div>
          {(slot.endpoint?.models.length ?? 0) > 0 && <div className={styles.model}><VisuallyHidden.Root asChild><label htmlFor={`${id}-model`}>Model</label></VisuallyHidden.Root><Select id={`${id}-model`} value={binding?.model} disabled={running || locked} onValueChange={model => store?.update(chat.id, { model })} options={slot.endpoint!.models.map(model => ({ value: model, label: model }))} /></div>}
          <DropdownMenu.Root><Tooltip content="Assistant settings and available tools"><DropdownMenu.Trigger ref={settingsButton} className="desk-icon-button" aria-label="Assistant settings"><IconGear /></DropdownMenu.Trigger></Tooltip>
            <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" collisionPadding={12} onCloseAutoFocus={event => { if (configure) event.preventDefault() }}>
              <DropdownMenu.Label className="desk-menu-label">Thinking: {slot.thinking}</DropdownMenu.Label>
              <DropdownMenu.Label className="desk-menu-label">Available tools</DropdownMenu.Label>
              {(slot.endpoint?.tools ?? []).map(tool => <DropdownMenu.Label key={tool} className="desk-menu-label">{tool}</DropdownMenu.Label>)}
              <DropdownMenu.Item className="desk-menu-item" onSelect={() => { configureButton.current = settingsButton.current; setConfigure(true) }}>Configure Assistant…</DropdownMenu.Item>
            </DropdownMenu.Content></DropdownMenu.Portal>
          </DropdownMenu.Root>
          <span className={styles.grow} />
          {running ? <Button onClick={() => binding?.run?.stop()}>Stop</Button> : <Button variant="primary" disabled={needsConfig || !chat.composer.trim() || !binding || Boolean(otherRun) || locked || Boolean(blocked && !needsConfig)} onClick={send}>Send</Button>}
        </div>
      </div>
      {attachmentError && <p className={styles.caption} role="alert">{attachmentError}</p>}
      <p className={styles.footnote}>{error ? 'Chat has unsaved changes' : saving || dirty ? 'Saving chat…' : 'Chat saved locally'} · {chat.pack ? 'Changes need your review and Save.' : 'No pack file is created until you choose Create pack.'}</p>
    </div>
    </div>
    <ConfigureAssistant open={configure} onOpenChange={setConfigure} openerRef={configureButton} />
  </section>
}
