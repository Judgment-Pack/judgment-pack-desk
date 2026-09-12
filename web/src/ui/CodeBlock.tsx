import { useEffect, useId, useState } from 'react'
import { Button } from './Button'
import styles from './CodeBlock.module.css'

/** Read-only source: wrapping is presentation; Copy always uses the full text. */
export function CodeBlock({ text, label = 'JSON' }: { text: string; label?: string }) {
  const [wrap, setWrap] = useState(true)
  const [feedback, setFeedback] = useState('')
  const id = useId()
  useEffect(() => { setFeedback('') }, [text])
  return <div className={styles.root}>
    <div className={styles.toolbar}>
      <span>{label}</span>
      <Button variant="quiet" aria-pressed={wrap} aria-controls={id} onClick={() => setWrap(!wrap)}>Wrap</Button>
      <Button variant="quiet" onClick={async () => {
        try { await navigator.clipboard.writeText(text); setFeedback('Copied') }
        catch { setFeedback('Select the text below to copy it.') }
      }}>Copy {label}</Button>
    </div>
    {feedback && <p className={styles.feedback} role="status">{feedback}</p>}
    <pre id={id} className={styles.source} data-wrap={wrap} tabIndex={0} aria-label={label}><code>{text}</code></pre>
  </div>
}
