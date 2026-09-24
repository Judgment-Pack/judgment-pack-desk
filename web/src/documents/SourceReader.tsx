import { useEffect, useRef, useState, type ReactNode } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { Button } from '../ui/Button'
import { Popover } from '../ui/Popover'
import { Disclosure } from '../ui/Disclosure'
import { ReadingDetails } from '../chat/ReadingDetails'
import type { ChatLink } from '../chat/store'
import { loadDocument, type DocumentReference, type VerifiedDocument } from './client'
import { needsPartialConsent, usablePages } from './record'
import { quoteRange } from './quote'
import styles from './SourceReader.module.css'

type Citation = { page: number; quote: string }
function useSource(reference: DocumentReference, enabled: boolean, citation?: Citation) {
  const pin = useEffectiveConfig().config.research.gateway
  const [attempt, retry] = useState(0)
  const [state, setState] = useState<{ key: string; value?: VerifiedDocument; error?: string }>({ key: '' })
  const key = JSON.stringify([reference, pin?.authority, pin?.signer.public, citation, attempt])
  useEffect(() => {
    if (!enabled) return
    const operation = new AbortController()
    setState({ key })
    if (!pin) { setState({ key, error: msg('Configure the gateway in Admin → Storage & data to use attached PDFs.') }); return }
    void loadDocument(reference, pin, operation.signal).then(value => {
      if (operation.signal.aborted) return
      const page = citation && usablePages(value.record).find(page => page.number === citation.page)
      if (citation && (!reference.pages.includes(citation.page) || !page || !quoteRange(page.text, citation.quote))) setState({ key, error: msg('This quote does not match the cited page.') })
      else setState({ key, value })
    }, error => { if (!operation.signal.aborted) setState({ key, error: (error as Error).message }) })
    return () => operation.abort()
  }, [key, enabled]) // key includes every input affecting verification or selection
  return { ...(enabled && state.key === key ? state : {}), retry: () => retry(value => value + 1) }
}
function Warnings({ value }: { value: VerifiedDocument }) {
  const { record } = value
  return <>
    {needsPartialConsent(record) && <p className={styles.warning}>{msg('Some pages are missing or unreadable.')}</p>}
    {(record.content.extraction === 'ocr' || record.content.extraction === 'mixed') && <p className={styles.warning}>{msg('OCR was used. Check the page text against the original.')}</p>}
    {record.content.truncated && <p className={styles.warning}>{msg('Processing stopped before the whole document was read.')}</p>}
  </>
}
function Status({ error, retry }: { error?: string; retry: () => void }) {
  return error ? <div><p role="alert">{systemMessage(error)}</p><Button onClick={retry}>{msg('Retry')}</Button></div> : <p role="status">{msg('Verifying document pages…')}</p>
}
function Highlight({ text, quote, compact = false }: { text: string; quote?: string; compact?: boolean }) {
  const range = quote && quoteRange(text, quote)
  if (!range) return <>{text}</>
  const start = compact ? Math.max(0, range.start - 120) : 0
  const end = compact ? Math.min(text.length, range.start + 480) : text.length
  return <>{start > 0 ? '…' : ''}{text.slice(start, range.start)}<mark>{text.slice(range.start, Math.min(range.end, end))}</mark>{text.slice(Math.min(range.end, end), compact ? Math.min(end, range.end + 120) : end)}{compact && Math.min(end, range.end + 120) < text.length ? '…' : ''}</>
}
export function CitationPreview({ name, reference, citation, number, onRead, link }: {
  name: string; reference: DocumentReference; citation: Citation; number: number; onRead: (node: ReactNode, opener: HTMLElement | null) => void
  /** The link a web document was read from, where the chat kept one, for its anchor. */
  link?: ChatLink
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const moving = useRef(false)
  const source = useSource(reference, open, citation)
  const page = source.value?.record.content.pages.find(page => page.number === citation.page)
  return <>{citation.quote}<Popover title={name} variant="list" align="start" dismissible open={open} onOpenChange={setOpen}
    onCloseAutoFocus={event => { if (moving.current) { event.preventDefault(); moving.current = false } }}
    triggerTooltip={`${name} · ${msg('Page {{number}}', { number: citation.page })}`}
    trigger={<button ref={trigger} type="button" className={styles.citation} aria-label={msg('View source {{number}}: {{name}}, page {{page}}', { number, name, page: citation.page })}>[{number}]</button>}>
    <div className={styles.preview}>{!source.value || !page ? <Status error={source.error} retry={source.retry} /> : <>
      <p className={styles.meta}>{msg('Quote found on page {{number}}', { number: citation.page })}</p>
      <Warnings value={source.value} />
      <p className={styles.excerpt}><Highlight text={page.text} quote={citation.quote} compact /></p>
      <Button variant="inline" onClick={() => { moving.current = true; setOpen(false); onRead(<SourceReader name={name} reference={reference} citation={citation} link={link} />, trigger.current) }}>{msg('Open source text')}</Button>
    </>}</div>
  </Popover></>
}
export function SourceReader({ name, reference, citation, link }: { name: string; reference: DocumentReference; citation?: Citation; link?: ChatLink }) {
  useLocale()
  const source = useSource(reference, true, citation)
  const selected = useRef<HTMLElement>(null)
  useEffect(() => {
    const page = selected.current
    if (!source.value || !page) return
    const body = page.parentElement
    const target = page.querySelector('mark') ?? page
    if (body) body.scrollTop += target.getBoundingClientRect().top - body.getBoundingClientRect().top - 16
  }, [source.value])
  const original = source.value?.object.original
  const download = () => {
    if (!original) return
    const url = URL.createObjectURL(new Blob([Uint8Array.from(atob(original.bytes), c => c.charCodeAt(0))], { type: 'application/octet-stream' }))
    const link = document.createElement('a'); link.href = url; link.download = name; link.click(); setTimeout(() => URL.revokeObjectURL(url), 0)
  }
  return <ReadingDetails title={name} actions={original && <Button variant="quiet" onClick={download}>{source.value?.record.provenance.source.kind === 'web' && source.value.record.provenance.source.format === 'static-text-v1' ? msg('Download snapshot') : msg('Download original')}</Button>}>
    {!source.value ? <Status error={source.error} retry={source.retry} /> : <>
      <Warnings value={source.value} />
      {source.value.record.provenance.source.kind === 'connection-resource' && source.value.record.provenance.source.url && <p className={styles.meta}><a href={source.value.record.provenance.source.url} target="_blank" rel="noopener noreferrer">{msg('Open source')}</a></p>}
      {source.value.record.provenance.source.kind === 'connected-source' && <p className={styles.meta}><a href={source.value.record.provenance.source.url} target="_blank" rel="noopener noreferrer">{msg('Open in {{provider}}', { provider: source.value.record.provenance.source.provider === 'notion' ? 'Notion' : 'Obsidian' })}</a></p>}
      {source.value.record.provenance.source.kind === 'web' && <p className={styles.meta}><a href={source.value.record.provenance.source.url} target="_blank" rel="noopener noreferrer">{msg('Open original source')}</a>{link?.anchor && <> · <a href={`${link.url}#${link.anchor}`} target="_blank" rel="noopener noreferrer">{msg('Open at {{anchor}}', { anchor: `#${link.anchor}` })}</a></>}{source.value.record.provenance.source.format === 'static-text-v1' && <> · {msg('Static text snapshot')}</>}</p>}
      <Disclosure title={msg('Technical details')}><p>{msg('Receipt verified. This confirms byte lineage, not accuracy or authority.')}</p><code className={styles.identity}>{source.value.digest}</code></Disclosure>
      {source.value.record.content.pages.filter(page => reference.pages.includes(page.number)).map(page => <section key={page.number} ref={citation?.page === page.number ? selected : undefined} className={styles.page}>
        <h3>{msg('Page {{number}}', { number: page.number })}</h3>
        <p className={styles.sourceText}><Highlight text={page.text} quote={citation?.page === page.number ? citation.quote : undefined} /></p>
      </section>)}
    </>}
  </ReadingDetails>
}
