import { useEffect, useState } from 'react'
import { formattingLocale, msg, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
import type { Turn } from '../research/run'
import type { MessageTimeFormat } from './timestamps'
import styles from './MessageTime.module.css'

export function messageSpeaker(turn: Pick<Turn, 'role' | 'kind'>) {
  return turn.role === 'user' ? msg('You') : turn.kind === 'note' ? msg('Desk') : msg('Assistant')
}
/** Re-read regional settings when returning to the app, without ticking labels
 * or live announcements in a conversation someone is reading. */
export function useMessageClock() {
  useLocale()
  const [, refresh] = useState(0)
  useEffect(() => {
    const update = () => refresh(value => value + 1)
    const visible = () => { if (document.visibilityState === 'visible') update() }
    window.addEventListener('focus', update)
    window.addEventListener('languagechange', update)
    document.addEventListener('visibilitychange', visible)
    return () => { window.removeEventListener('focus', update); window.removeEventListener('languagechange', update); document.removeEventListener('visibilitychange', visible) }
  }, [])
  return { locale: formattingLocale(), timeZone: new Intl.DateTimeFormat().resolvedOptions().timeZone }
}

export function MessageTime({ turn, formatted, onOpen }: { turn: Turn; formatted?: MessageTimeFormat; onOpen: (opener: HTMLButtonElement) => void }) {
  useLocale()
  const full = formatted?.full ?? msg('Time unavailable')
  return <div className={styles.caption}><span>{messageSpeaker(turn)}</span><span aria-hidden="true">·</span>
    <Tooltip content={full}><button type="button" className={styles.time} aria-label={msg('Message details: {{time}}', { time: full })} onClick={event => onOpen(event.currentTarget)}>
      {formatted ? <time dateTime={turn.at}>{formatted.short}</time> : <span>{msg('Time unavailable')}</span>}
    </button></Tooltip>
  </div>
}
