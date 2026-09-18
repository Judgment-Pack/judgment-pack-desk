import { sourceMessage } from '../i18n/source'
import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
import { useEffect, useId, useState } from 'react'
import { Button } from './Button'
import styles from './CodeBlock.module.css'

/** Read-only source: wrapping is presentation; Copy always uses the full text. */
export function CodeBlock({ text, label = 'JSON' }: { text: string; label?: string }) {
  useLocale()
  const [wrap, setWrap] = useState(true)
  const [feedback, setFeedback] = useState('')
  const id = useId()
  useEffect(() => { setFeedback('') }, [text])
  return <div className={styles.root}>
    <div className={styles.toolbar}>
      <span>{label}</span>
      <Button variant="quiet" aria-pressed={wrap} aria-controls={id} onClick={() => setWrap(!wrap)}>{msg("Wrap")}</Button>
      <Button variant="quiet" onClick={async () => {
        try { await navigator.clipboard.writeText(text); setFeedback('Copied') }
        catch { setFeedback(sourceMessage("Select the text below to copy it.")) }
      }}><Message text={"Copy <0/>"} slots={[label]} /></Button>
    </div>
    {feedback && <p className={styles.feedback} role="status">{systemMessage(feedback)}</p>}
    <pre id={id} className={styles.source} data-wrap={wrap} tabIndex={0} aria-label={label}><code>{text}</code></pre>
  </div>
}
