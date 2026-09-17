import { isValidElement, useState, type ReactNode } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import styles from './ChatWorkspace.module.css'

function textOf(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(textOf).join('')
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children)
  return ''
}

/** Model text is content: no HTML execution, remote images, or unsafe URLs. */
export function MessageRenderer({ text }: { text: string }) {
  return <div className={styles.markdown}><Markdown remarkPlugins={[remarkGfm]} skipHtml
    urlTransform={url => /^https?:\/\//i.test(url) ? url : ''}
    components={{
      a: ({ children, href }) => href ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a> : <span>{children}</span>,
      img: ({ alt }) => <span>{alt ? `[Image: ${alt}]` : '[Image]'}</span>,
      pre: ({ children }) => <CodeBlock text={textOf(children).replace(/\n$/, '')} label="Code" />,
      table: ({ children }) => <div className={styles.messageTable} role="region" aria-label="Response table" tabIndex={0}><table>{children}</table></div>
    }}>{text}</Markdown></div>
}

export function CopyMessage({ text }: { text: string }) {
  const [feedback, setFeedback] = useState('')
  return <div className={styles.messageActions}><Button variant="quiet" onClick={async () => {
    try { await navigator.clipboard.writeText(text); setFeedback('Copied') }
    catch { setFeedback('Select the response text to copy it.') }
  }}>Copy response</Button><span role="status">{feedback}</span></div>
}
