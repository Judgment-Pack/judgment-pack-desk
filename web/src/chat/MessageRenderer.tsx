import { msg, useLocale } from '../i18n'
import { isValidElement, useEffect, useState, type ReactNode } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { VisuallyHidden } from 'radix-ui'
import { IconCheck, IconCopy } from '../shell/icons'
import { CodeBlock } from '../ui/CodeBlock'
import { Tooltip } from '../ui/Tooltip'
import styles from './ChatWorkspace.module.css'

function textOf(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(textOf).join('')
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children)
  return ''
}

/** Model text is content: no HTML execution, remote images, or unsafe URLs. */
export function MessageRenderer({ text }: { text: string }) {
  useLocale()
  return <div className={styles.markdown}><Markdown remarkPlugins={[remarkGfm]} skipHtml
    urlTransform={url => /^https?:\/\//i.test(url) ? url : ''}
    components={{
      a: ({ children, href }) => href ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a> : <span>{children}</span>,
      img: ({ alt }) => <span>{alt ? msg("[Image: {{value0}}]", { value0: alt }) : msg("[Image]")}</span>,
      pre: ({ children }) => <CodeBlock text={textOf(children).replace(/\n$/, '')} label={msg("Code")} />,
      table: ({ children }) => <div className={styles.messageTable} role="region" aria-label={msg("Response table")} tabIndex={0}><table>{children}</table></div>
    }}>{text}</Markdown></div>
}

export function CopyMessage({ text }: { text: string }) {
  useLocale()
  const [feedback, setFeedback] = useState('')
  const copied = feedback === 'Copied'
  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setFeedback(''), 2000)
    return () => clearTimeout(timer)
  }, [copied])
  return <div className={styles.messageActions}>
    <Tooltip content={copied ? msg("Copied") : msg("Copy response")}>
      <button type="button" className="desk-icon-button" aria-label={msg("Copy response")} onClick={async () => {
        try { await navigator.clipboard.writeText(text); setFeedback('Copied') }
        catch { setFeedback('Select the response text to copy it.') }
      }}>{copied ? <IconCheck /> : <IconCopy />}</button>
    </Tooltip>
    <VisuallyHidden.Root role="status">{msg(feedback)}</VisuallyHidden.Root>
    {feedback && !copied && <span aria-hidden="true">{msg(feedback)}</span>}
  </div>
}
