import { useCallback, useEffect, useLayoutEffect, useRef, type ReactNode, type RefObject } from 'react'
import { VisuallyHidden } from 'radix-ui'
import { msg } from '../i18n'
import {ChatAction} from './ChatActions'
import { TextArea } from '../ui/TextArea'
import { Tooltip } from '../ui/Tooltip'
import { IconSend, IconStop } from '../shell/icons'
import { submitOnEnter } from './submitOnEnter'
import styles from './ChatWorkspace.module.css'

/** The composer shared by chat and reviewed, task-specific assistant workflows. */
export function ChatComposer({ id, inputRef, value, onChange, onSend, onStop, running,
  disabled = false, sendDisabled = false, placeholder, label = msg('Message the assistant'),
  rows = 2, describedBy, children, readiness, controls, feedback,
}: {
  id: string
  inputRef?: RefObject<HTMLTextAreaElement | null>
  value: string
  onChange: (value: string) => void
  onSend: () => void
  onStop: () => void
  running: boolean
  disabled?: boolean
  sendDisabled?: boolean
  placeholder: string
  label?: string
  rows?: number
  describedBy?: string
  children?: ReactNode
  readiness?: ReactNode
  controls: ReactNode
  feedback?: ReactNode
}) {
  const localRef = useRef<HTMLTextAreaElement>(null)
  const input = inputRef ?? localRef
  const resizeInput = useCallback(() => {
    const node = input.current
    if (!node) return
    const scroll = node.scrollTop
    node.style.height = 'auto'
    node.style.height = `${Math.min(node.scrollHeight, Math.max(80, window.innerHeight * 0.25))}px`
    node.scrollTop = scroll
  }, [input])
  useLayoutEffect(resizeInput, [value, rows, resizeInput])
  useEffect(() => {
    const node = input.current
    if (!node) return
    let width = node.getBoundingClientRect().width
    const observer = new ResizeObserver(entries => {
      const next = entries[0]?.contentRect.width
      if (next !== undefined && next !== width) {
        width = next
        // Avoid forcing layout on each divider frame for an empty composer.
        if (node.value) resizeInput()
      }
    })
    observer.observe(node)
    window.addEventListener('resize', resizeInput)
    return () => { observer.disconnect(); window.removeEventListener('resize', resizeInput) }
  }, [input, resizeInput])
  const cannotSend = disabled || sendDisabled
  const send = () => { if (!running && !cannotSend) onSend() }
  return <>
    <form data-chat-composer className={styles.composer} onSubmit={event => { event.preventDefault(); send() }}>
      {children}
      <VisuallyHidden.Root asChild><label htmlFor={`${id}-message`}>{label}</label></VisuallyHidden.Root>
      <TextArea ref={input} id={`${id}-message`} className={styles.messageInput} rows={rows}
        value={value} placeholder={placeholder} disabled={disabled}
        aria-describedby={[describedBy, `${id}-composer-help`].filter(Boolean).join(' ')}
        onChange={event => onChange(event.target.value)} onKeyDown={event => submitOnEnter(event, send)} />
      {readiness}
      <div className={styles.composerTools}>
        <div className={styles.composerControls}>{controls}</div>
        <Tooltip content={running ? msg('Stop') : msg('Send')} shortcut={running ? undefined : 'Enter'}>
          <ChatAction size="icon" className={styles.send} variant={running ? 'secondary' : 'primary'}
            aria-label={running ? msg('Stop') : msg('Send')} disabled={!running && cannotSend}
            type={running ? 'button' : 'submit'} onClick={running ? onStop : undefined}>
            {running ? <IconStop /> : <IconSend />}
          </ChatAction>
        </Tooltip>
      </div>
    </form>
    {feedback}
    <p id={`${id}-composer-help`} className={`${styles.footnote} ${styles.composerShortcut}`}>
      {running ? msg('Working in this window. You can switch chats; keep this window open.') : msg('Enter to send · Shift+Enter for a new line')}
    </p>
  </>
}
