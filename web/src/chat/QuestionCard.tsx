import { useId, useLayoutEffect, useRef, useState } from 'react'
import type { QuestionDraft, SavedQuestion } from '@judgment-pack/harness/questions'
import { msg, systemMessage, useLocale } from '../i18n'
import {ChatAction} from './ChatActions'
import { Disclosure } from '../ui/Disclosure'
import { TextArea } from '../ui/TextArea'
import styles from './QuestionCard.module.css'
import { submitOnEnter } from './submitOnEnter'

export function QuestionCard({ question, disabled, canContinue, onDraft, onSubmit, onContinue }: {
  question: SavedQuestion; disabled: boolean; canContinue: boolean
  onDraft: (draft: QuestionDraft) => void
  onSubmit: (kind: 'answered' | 'help') => Promise<void>
  onContinue: () => Promise<void>
}) {
  useLocale()
  const id = useId(), input = useRef<HTMLTextAreaElement>(null)
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const { prompt, draft, resolution } = question
  const locked = disabled || busy
  const choices = prompt.options.filter(o => draft.selected.includes(o.id))
  const hint = choices.length ? choices.map(o => o.description).filter(Boolean).join(' ') : prompt.recommendation
  useLayoutEffect(() => {
    if (!input.current) return
    input.current.style.height = '32px'
    input.current.style.height = `${Math.min(120, Math.max(32, input.current.scrollHeight + 2))}px`
  }, [draft.text, resolution])
  async function act(action: () => Promise<void>) {
    if (locked) return
    setBusy(true); setError('')
    try { await action() } catch (cause) { setError((cause as Error).message) } finally { setBusy(false) }
  }
  const feedback = error ? <p className={styles.error} role="alert">{systemMessage(error)}</p> : null
  if (resolution) return <div className={styles.answered}>
    <Disclosure title={prompt.question}>
      <p>{resolution.kind === 'superseded' ? msg('Continued in chat') : resolution.kind === 'help' ? msg('Help requested') : msg('Answer submitted')}</p>
      {resolution.kind === 'answered' && choices.length > 0 && <p>{choices.map(o => o.label).join('; ')}</p>}
      {draft.text && <p className={styles.retainedText}>{draft.text}</p>}
    </Disclosure>
    {feedback}
    {canContinue && <ChatAction disabled={locked} onClick={() => void act(onContinue)}>{busy ? msg('Saving…') : msg('Continue')}</ChatAction>}
  </div>
  return <form className={styles.card} aria-labelledby={`${id}-question`} onSubmit={event => { event.preventDefault(); if (draft.selected.length || draft.text.trim()) void act(() => onSubmit('answered')) }}>
    <div className={styles.caption}><span>{prompt.title}</span><span>{!prompt.options.length ? msg('Write your answer') : prompt.selection === 'multiple' ? msg('Choose any') : msg('Choose one')}</span></div>
    <p className={styles.question} id={`${id}-question`}>{prompt.question}</p>
    {!!prompt.options.length && <fieldset className={styles.options} disabled={locked} aria-labelledby={`${id}-question`} aria-describedby={hint ? `${id}-hint` : undefined}>
      {prompt.options.map(option => <label key={option.id} className={styles.option}>
        <input type={prompt.selection === 'single' ? 'radio' : 'checkbox'} name={`${id}-choice`} value={option.id} checked={draft.selected.includes(option.id)}
          onKeyDown={event => submitOnEnter(event, () => event.currentTarget.form?.requestSubmit())}
          onChange={event => onDraft({...draft,selected:prompt.selection === 'single' ? [option.id] : event.target.checked ? [...draft.selected,option.id] : draft.selected.filter(value => value !== option.id)})}/>
        <span>{option.label}</span>{option.id === prompt.recommendedOptionId && <span className={styles.badge}>{msg('Recommended')}</span>}
      </label>)}
    </fieldset>}
    {hint && <p id={`${id}-hint`} className={styles.hint}>{hint}</p>}
    <label className={styles.textLabel} htmlFor={`${id}-answer`}>{prompt.options.length ? <>{msg('Add context or your own answer')} <span>{msg('(optional)')}</span></> : msg('Your answer')}</label>
    <TextArea ref={input} id={`${id}-answer`} className={styles.text} rows={1} maxLength={4000} value={draft.text} disabled={locked}
      placeholder={msg('Write an answer or add context…')} aria-describedby={`${id}-shortcut`}
      onKeyDown={event => submitOnEnter(event, () => event.currentTarget.form?.requestSubmit())}
      onChange={event => onDraft({...draft,text:event.target.value})}/>
    <p id={`${id}-shortcut`} className={styles.shortcut}>{msg('Enter to submit · Shift+Enter for a new line')}</p>
    {feedback}
    <div className={styles.actions}>
      <ChatAction variant="quiet" disabled={locked} onClick={() => void act(() => onSubmit('help'))}>{msg('Help me decide')}</ChatAction>
      <div className={styles.right}>
        {!!draft.selected.length && <ChatAction variant="quiet" disabled={locked} onClick={() => onDraft({...draft,selected:[]})}>{msg('Clear choice')}</ChatAction>}
        <ChatAction variant="primary" type="submit" disabled={locked || !draft.selected.length && !draft.text.trim()}>{busy ? msg('Saving…') : msg('Submit answer')}</ChatAction>
      </div>
    </div>
  </form>
}
