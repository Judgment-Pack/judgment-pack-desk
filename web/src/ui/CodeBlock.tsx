import { sourceMessage } from '../i18n/source'
import { msg, useLocale, systemMessage } from '../i18n'
import { useEffect, useId, useState } from 'react'
import { Button } from './Button'
import styles from './CodeBlock.module.css'
import { IconCopy, IconCheck } from '../shell/icons'
import { Tooltip } from './Tooltip'
import { VisuallyHidden } from 'radix-ui'

/** Read-only source: wrapping is presentation; Copy always uses the full text. */
export function CodeBlock({ text, label = 'JSON' }: { text: string; label?: string }) {
  useLocale()
  const [wrap, setWrap] = useState(true)
  const [feedback, setFeedback] = useState('')
  const id = useId()
  useEffect(() => { setFeedback('') }, [text])
  useEffect(() => { if (feedback !== 'Copied') return; const timer = setTimeout(() => setFeedback(''), 2000); return () => clearTimeout(timer) }, [feedback])
  return <div className={styles.root}>
    <div className={styles.toolbar}>
      <span>{label}</span>
      <Button variant="quiet" aria-pressed={wrap} aria-controls={id} onClick={() => setWrap(!wrap)}>{msg("Wrap")}</Button>
      <Tooltip content={feedback ? systemMessage(feedback) : msg("Copy {{label}}", { label })}><button type="button" className="desk-icon-button" aria-label={msg("Copy {{label}}", { label })} onClick={async () => {
        try { await navigator.clipboard.writeText(text); setFeedback('Copied') }
        catch { setFeedback(sourceMessage("Select the text below to copy it.")) }
      }}>{feedback === 'Copied' ? <IconCheck /> : <IconCopy />}</button></Tooltip>
    </div>
    {feedback === 'Copied' && <VisuallyHidden.Root role="status">{systemMessage(feedback)}</VisuallyHidden.Root>}
    {feedback && feedback !== 'Copied' && <p className={styles.feedback} role="status">{systemMessage(feedback)}</p>}
    <pre id={id} className={styles.source} data-wrap={wrap} tabIndex={0} aria-label={label}><code>{text}</code></pre>
  </div>
}
