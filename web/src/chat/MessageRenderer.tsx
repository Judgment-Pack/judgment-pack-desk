import { CitationPreview } from '../documents/SourceReader'
import { useReadingDetails } from './ReadingDetails'
import type { ChatAttachment } from './store'
import { msg, useLocale } from '../i18n'
import { isValidElement, memo, useEffect, useMemo, useState, type ReactNode } from 'react'
import Markdown, { type Components } from 'react-markdown'
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

interface MarkdownNode { type: string; url?: string; children?: MarkdownNode[]; data?: { hProperties?: Record<string, unknown> } }
function numberSources() {
  return (tree: MarkdownNode) => {
    let number = 0
    const visit = (node: MarkdownNode) => {
      if (node.type === 'link' && node.url?.startsWith('attachment:')) node.data = { ...node.data, hProperties: { ...node.data?.hProperties, 'data-source-number': ++number } }
      node.children?.forEach(visit)
    }
    visit(tree)
  }
}
const NO_DOCUMENTS: ChatAttachment[] = []
/** Model text is content: no HTML execution, remote images, or unsafe URLs.
 * Stable renderers preserve citations and code controls as streamed text grows;
 * typing in the composer must not remount completed answers. */
export const MessageRenderer = memo(function MessageRenderer({ text, documents = NO_DOCUMENTS, scope = 'response', onRead }: { text: string; documents?: ChatAttachment[]; scope?: string; onRead?: (node: ReactNode, opener: HTMLElement | null) => void }) {
  const locale = useLocale()
  const ownRead = useReadingDetails(scope)
  const read = onRead ?? ownRead
  const components = useMemo<Components>(() => ({
      a: ({ children, href, node }) => {
        if (href?.startsWith('attachment:')) {
          const match = /^attachment:([a-f0-9-]{36})\/(sha256:[a-f0-9]{64})\/page\/([1-9][0-9]*)$/.exec(href)
          const file = match && documents.find(file => file.document?.id === match[1] && file.document.digest === match[2])
          return file?.document ? <CitationPreview name={file.name} reference={file.document} link={file.link} citation={{ page: Number(match![3]), quote: textOf(children) }} number={Number(node?.properties['data-source-number']) || 1} onRead={read} /> : <span>{children} <Tooltip content={msg('Source unavailable')}><span tabIndex={0} className={styles.caption} aria-label={msg('Source unavailable')}>[{String(node?.properties['data-source-number'] || '?')}]</span></Tooltip></span>
        }
        return href ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a> : <span>{children}</span>
      },
      img: ({ alt }) => <span>{alt ? msg("[Image: {{value0}}]", { value0: alt }) : msg("[Image]")}</span>,
      pre: ({ children }) => {
        const code = Array.isArray(children) ? children[0] : children
        const language = isValidElement<{ className?: string }>(code) ? /^language-(\S+)$/.exec(code.props.className ?? '')?.[1] : undefined
        return <CodeBlock text={textOf(children).replace(/\n$/, '')} label={language || msg('Code')} />
      },
      table: ({ children }) => <div className={styles.messageTable} role="region" aria-label={msg("Response table")} tabIndex={0}><table>{children}</table></div>
    }), [documents, locale, read])
  return <div className={styles.markdown}><Markdown remarkPlugins={[remarkGfm, numberSources]} skipHtml
    urlTransform={url => /^https?:\/\//i.test(url) || /^attachment:[a-f0-9-]{36}\/sha256:[a-f0-9]{64}\/page\/[1-9][0-9]*$/.test(url) ? url : ''}
    components={components}>{text}</Markdown></div>
})

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
