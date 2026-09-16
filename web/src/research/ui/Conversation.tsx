import { useEffect, useRef, useState } from 'react'
import { Button } from '../../ui/Button'
import { TextArea } from '../../ui/TextArea'
import { canRetryExpectationValidation, type RunState } from '../run'
import styles from './ResearchAuthoring.module.css'

/** What the run is doing, in one line, from its own state and never a synthesized narration. */
export function statusLine(state: RunState): string {
  switch (state.status) {
    case 'idle':
      return 'Not started'
    case 'running':
      return state.detail || `Running (${state.phase})`
    case 'ready':
      return 'Ready for review'
    case 'needs-input':
      return 'Needs your input'
    case 'budget':
      return 'Revision budget spent'
    case 'stalled':
      return 'Stalled'
    case 'stopped':
      return 'Stopped'
    case 'failed':
      return 'Failed'
  }
}

export function Conversation({ state, onSend, onStop, onRetryValidation }: { state: RunState; onSend: (text: string) => void; onStop: () => void; onRetryValidation?: () => void }) {
  const [text, setText] = useState('')
  const thread = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const node = thread.current
    if (node && typeof node.scrollTo === 'function') node.scrollTo({ top: node.scrollHeight })
  }, [state.turns.length, state.status])
  const running = state.status === 'running'
  const canSend = !running && state.status !== 'idle' && text.trim() !== ''
  return (
    <section className={styles.pane} aria-label="Conversation" data-pane="conversation">
      <header className={styles.paneHeader}>
        <span>Conversation</span>
        <span className={styles.status} role="status" aria-live="polite">
          {statusLine(state)}
        </span>
      </header>
      <div className={styles.thread} ref={thread}>
        {state.turns.map((turn, index) => (
          <article key={`${turn.at}-${index}`} className={styles.turn} data-role={turn.role} data-kind={turn.kind}>
            <span className={styles.turnMeta}>
              {turn.role === 'user' ? 'You' : 'Assistant'}
              {turn.kind === 'unknowns' ? ' · open questions and assumptions' : turn.kind === 'note' ? ' · note from the desk' : ''}
            </span>
            {turn.text}
          </article>
        ))}
        {state.status !== 'idle' && state.status !== 'running' && state.detail !== '' && (
          <p className={styles.detail} role="status">
            {state.detail}
          </p>
        )}
      </div>
      <div className={styles.composer}>
        <label htmlFor="research-message" className="sr-only">
          Message the assistant
        </label>
        <TextArea
          id="research-message"
          rows={2}
          value={text}
          placeholder={running ? 'The assistant is working…' : 'Ask a question or request a change…'}
          disabled={running || state.status === 'idle'}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && canSend) {
              event.preventDefault()
              onSend(text.trim())
              setText('')
            }
          }}
        />
        <div className={styles.composerRow}>
          <span className={styles.hint}>Enter sends. Shift+Enter for a new line.</span>
          <span className={styles.grow} />
          {/* The way back from a validation that failed on its own account:
              the same proposal, judged again, without paying for the reviewer
              turn that answered the question. */}
          {canRetryExpectationValidation(state) && (
            <Button disabled={running || !onRetryValidation} onClick={() => onRetryValidation?.()}>
              Retry validation
            </Button>
          )}
          {running ? (
            <Button variant="secondary" onClick={onStop}>
              Stop
            </Button>
          ) : (
            <Button
              variant="primary"
              disabled={!canSend}
              onClick={() => {
                onSend(text.trim())
                setText('')
              }}
            >
              Send
            </Button>
          )}
        </div>
      </div>
    </section>
  )
}
