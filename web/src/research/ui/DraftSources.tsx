import type { ReactNode } from 'react'
import type { ChatAttachment } from '../../chat/store'
import { ReadingDetails, useReadingDetails } from '../../chat/ReadingDetails'
import { SourceReader } from '../../documents/SourceReader'
import { msg, useLocale } from '../../i18n'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import styles from './ResearchAuthoring.module.css'

const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
export const declaredSources = (document: unknown): unknown[] => object(document) && Array.isArray(document.sources) ? document.sources : []
const text = (value: unknown): string => typeof value === 'string' ? value : ''

/** Authored references are distinct from documents retained by Desk and research receipts. */
export function DraftSources({ document, documents = [], files = [], onRead }: {
  document: unknown; documents?: readonly ChatAttachment[]; files?: readonly { name: string; text: string }[]
  onRead?: (node: ReactNode, opener: HTMLElement) => void
}) {
  useLocale()
  const ownRead = useReadingDetails('draft-sources')
  const read = onRead ?? ownRead
  const declared = declaredSources(document)
  return <>
    {(documents.length > 0 || files.length > 0) && <section className={styles.section}>
      <h3>{msg('Attached documents')}</h3>
      <ul className={styles.list}>
        {documents.map(file => <li className={styles.row} key={file.id}><button className={styles.rowButton} type="button" onClick={event => read(<SourceReader name={file.name} reference={file.document!} link={file.link} />, event.currentTarget)}><strong>{file.name}</strong>{file.link && <div className={styles.url}>{file.link.url}</div>}</button></li>)}
        {files.map((file, index) => <li className={styles.row} key={index}><button className={styles.rowButton} type="button" onClick={event => read(<ReadingDetails title={file.name}><CodeBlock text={file.text} label={msg('Text')} /></ReadingDetails>, event.currentTarget)}>{file.name}</button></li>)}
      </ul>
    </section>}
    {declared.length > 0 && <section className={styles.section}>
      <h3>{msg('Pack source references')}</h3>
      <p className={styles.detail}>{msg('References declared in the draft. A reference alone does not verify that a source was retrieved.')}</p>
      <ul className={styles.list}>{declared.map((source, index) => {
        const record = object(source) ? source : {}
        const title = text(record.title) || text(record.id) || msg('Source {{value0}}', { value0: index + 1 })
        const locator = object(record.locator) ? text(record.locator.value) : ''
        const citation = object(record.citation) ? record.citation : {}
        return <li className={styles.row} key={index}><button className={styles.rowButton} type="button" onClick={event => read(<ReadingDetails title={title}>
          {locator && <p className={styles.url}>{locator}</p>}
          {text(record.publisher) && <p>{text(record.publisher)}</p>}
          {text(citation.excerpt) && <blockquote>{text(citation.excerpt)}</blockquote>}
          {text(citation.location) && <p>{text(citation.location)}</p>}
          <Disclosure title={msg('Technical details')}><CodeBlock text={JSON.stringify(source, null, 2)} label="JSON" /></Disclosure>
        </ReadingDetails>, event.currentTarget)}><strong>{title}</strong><div className={styles.url}>{locator}</div></button></li>
      })}</ul>
    </section>}
  </>
}
