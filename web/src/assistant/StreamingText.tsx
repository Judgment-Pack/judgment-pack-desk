import { msg, useLocale } from '../i18n'
import styles from './StreamingText.module.css'

/**
 * The prose the assistant has written so far, while it writes it.
 *
 * It is the engine's `message_progress` (`@judgment-pack/harness`: the answer's
 * prose so far, a proposal's fenced document left out), shown as text and
 * replaced by the answer when that is whole. It says nothing a proposal says:
 * the document, its checks and its review arrive with the run's own events.
 */
export function StreamingText({ text }: { text: string }) {
  useLocale()
  if (text === '') return null
  return (
    <p className={styles.streaming} role="status" aria-live="polite" aria-label={msg('The assistant is writing')}>
      {text}
    </p>
  )
}
