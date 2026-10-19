import { sourceMessage } from '../i18n/source'
import { msg, useLocale, systemMessage } from '../i18n'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { Button } from './Button'
import { formatJson } from '../files/formatJson'
import { Dialog, DialogActions, DialogClose } from './Dialog'
import styles from './CodeBlock.module.css'
import { IconCopy, IconCheck, IconFocus } from '../shell/icons'
import { Tooltip } from './Tooltip'
import { VisuallyHidden } from 'radix-ui'

/** Read-only source: JSON indentation and wrapping are presentation only; Copy preserves the original text. */
export function CodeBlock({ text, label = 'JSON', lang, expandable = false, defaultWrap = true, expanded = false, verbatim = false }: { text: string; label?: string; lang?: string; expandable?: boolean; defaultWrap?: boolean; expanded?: boolean; verbatim?: boolean }) {
  useLocale()
  const displayText = useMemo(() => {
    // Reuse the file editor's lossless formatter, including its expansion bound.
    // Prose, incomplete streaming JSON and non-JSON code stay as received.
    if (verbatim || text.length > (4 << 20) || !/^\s*[\[{]/.test(text)) return text
    const formatted = formatJson(text)
    return formatted.ok ? formatted.content.slice(0, -1) : text
  }, [text, verbatim])
  const [wrap, setWrap] = useState(defaultWrap)
  const [show, setShow] = useState(false)
  const opener = useRef<HTMLButtonElement>(null)
  const [feedback, setFeedback] = useState('')
  const id = useId()
  useEffect(() => { setFeedback('') }, [text])
  useEffect(() => { if (feedback !== 'Copied') return; const timer = setTimeout(() => setFeedback(''), 2000); return () => clearTimeout(timer) }, [feedback])
  return <div className={styles.root} data-expandable={expandable || expanded || undefined}>
    <div className={styles.toolbar}>
      <span>{label}</span>
      {expandable && <Tooltip content={msg("Show code")} disabled={show}><Button size="small-icon" variant="quiet" ref={opener} type="button" aria-label={msg("Show code")} aria-haspopup="dialog" onClick={() => setShow(true)}><IconFocus /></Button></Tooltip>}
      <Button size="small" variant="quiet" aria-pressed={wrap} aria-controls={id} onClick={() => setWrap(!wrap)}>{msg("Wrap")}</Button>
      <Tooltip content={feedback ? systemMessage(feedback) : msg("Copy {{label}}", { label })}><Button size="small-icon" variant="quiet" type="button" aria-label={msg("Copy {{label}}", { label })} onClick={async () => {
        try { await navigator.clipboard.writeText(text); setFeedback('Copied') }
        catch { setFeedback(sourceMessage("Select the text below to copy it.")) }
      }}>{feedback === 'Copied' ? <IconCheck /> : <IconCopy />}</Button></Tooltip>
    </div>
    {feedback === 'Copied' && <VisuallyHidden.Root role="status">{systemMessage(feedback)}</VisuallyHidden.Root>}
    {feedback && feedback !== 'Copied' && <p className={styles.feedback} role="status">{systemMessage(feedback)}</p>}
    <pre id={id} className={styles.source} data-wrap={wrap} data-expanded={expanded || undefined} role="region" tabIndex={0} aria-label={label}><code lang={lang}>{displayText}</code></pre>
    {expandable && <Dialog open={show} onOpenChange={setShow} title={label} size="wide" openerRef={opener}
      footer={<DialogActions><DialogClose asChild><Button>{msg('Close')}</Button></DialogClose></DialogActions>}>
      <CodeBlock text={text} label={label} lang={lang} defaultWrap={wrap} expanded verbatim={verbatim} />
    </Dialog>}
  </div>
}
