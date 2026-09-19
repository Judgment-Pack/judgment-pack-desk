import { useEffect, useState } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { Button } from '../ui/Button'
import { Popover } from '../ui/Popover'
import { loadDocument, matchesPageQuote, type DocumentReference, type VerifiedDocument } from './client'
import { usablePages, needsPartialConsent } from './record'
import styles from './Documents.module.css'

export function DocumentPreview({ name, reference, disabled, onChange, citation }: { name: string; reference: DocumentReference; disabled: boolean; onChange?: (ref: DocumentReference) => void; citation?: { page: number; quote: string } }) {
  useLocale()
  const pin = useEffectiveConfig().config.research.gateway
  const [open, setOpen] = useState(false), [document, setDocument] = useState<VerifiedDocument>(), [error, setError] = useState('')
  useEffect(() => {
    if (!open) return
    const operation = new AbortController()
    setDocument(undefined); setError('')
    if (!pin) { setError(msg('Configure the gateway in Admin → Storage & data to use attached PDFs.')); return }
    void loadDocument(reference, pin, operation.signal).then(value => { if (!operation.signal.aborted) {
      if (citation && (!reference.pages.includes(citation.page) || !matchesPageQuote(value, citation.page, citation.quote))) setError(msg('This quote does not match the cited page.'))
      else setDocument(value)
    } }, cause => { if (!operation.signal.aborted) setError((cause as Error).message) })
    return () => operation.abort()
  }, [open, reference.id, reference.digest, pin?.authority, pin?.signer.public, citation?.page, citation?.quote])
  const record = document?.record
  const canUse = record ? new Set(usablePages(record).map(page => page.number)) : new Set<number>()
  const exportOriginal = () => {
    if (!document) return
    const url = URL.createObjectURL(new Blob([Uint8Array.from(atob(document.object.original.bytes), c => c.charCodeAt(0))], { type: 'application/octet-stream' }))
    const link = window.document.createElement('a'); link.href = url; link.download = name; link.click(); setTimeout(() => URL.revokeObjectURL(url), 0)
  }
  return <Popover title={name} open={open} onOpenChange={setOpen} trigger={<Button variant="quiet" className={citation ? styles.citation : undefined}>{citation ? citation.quote : name}</Button>}>
    <div className={styles.preview}>
      {error ? <p role="alert">{systemMessage(error)}</p> : !record ? <p role="status">{msg('Verifying document pages…')}</p> : <>
        <p>{msg('Receipt verified. This confirms byte lineage, not accuracy or authority.')}</p>
        <p>{record.processing.status === 'failed' ? msg('Extraction failed. No pages can be sent.') : needsPartialConsent(record) ? msg('Some pages are missing or unreadable. Only selected readable pages can be sent.') : msg('Extraction complete.')}</p>
        {record.content.extraction === 'ocr' || record.content.extraction === 'mixed' ? <p>{msg('OCR was used. Check the page text against the original.')}</p> : null}
        {record.processing.errors.map((error, index) => <p key={index} className={styles.problem}><code>{error.code}</code>{error.page !== null && <> · {msg('Page {{number}}', { number: error.page })}</>}<br />{error.message}</p>)}
        {record.content.truncated && <p>{msg('Processing stopped before the whole document was read.')}</p>}
        <div className={styles.pages}>{record.content.pages.filter(page => !citation || page.number === citation.page).map(page => <section key={page.number}>
          <div className={styles.pageHeading}><label><input type="checkbox" checked={canUse.has(page.number) && reference.pages.includes(page.number)} disabled={disabled || !onChange || !canUse.has(page.number)} onChange={e => onChange?.({ ...reference, pages: e.target.checked ? [...reference.pages, page.number].sort((a,b) => a-b) : reference.pages.filter(n => n !== page.number) })} /> {msg('Page {{number}}', { number: page.number })}</label>
            <span>{page.unmapped > 0 ? msg('Unreadable characters') : page.status === 'ok' ? page.extraction === 'ocr' ? msg('OCR') : msg('Text') : page.status === 'no-text' ? msg('Blank page') : page.status === 'needs-ocr' ? msg('Needs OCR') : msg('Failed')}</span></div>
          {page.text && <p className={styles.pageText}>{page.text}</p>}
        </section>)}</div>
        {onChange && needsPartialConsent(record) && canUse.size > 0 && <label className={styles.consent}><input type="checkbox" disabled={disabled} checked={reference.allowPartial} onChange={e => onChange({ ...reference, allowPartial: e.target.checked })} />{msg('Use the selected readable pages despite the missing content.')}</label>}
        <Button variant="quiet" onClick={exportOriginal}>{msg('Download original')}</Button>
        <details><summary>{msg('Record identity')}</summary><code className={styles.identity}>{document!.digest}</code></details>
      </>}
    </div>
  </Popover>
}
