import { SourceRow } from '../ui/SourceRow'
import { IconDetails, IconLink } from '../shell/icons'
import { SearchSources, SearchReferenceIcon } from '../search/SearchSources'
import type { SearchReference } from '../search/results'
import type { ReactNode } from 'react'
import type { ChatAttachment } from './store'
import type { WebsiteReference } from '../documents/website'
import type { ResearchRunBinding } from '../research/useResearchRun'
import { msg, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { Disclosure } from '../ui/Disclosure'
import { SourceReader } from '../documents/SourceReader'
import { WebsiteSources } from '../documents/WebsiteSources'
import { SourceInspector } from '../research/ui/SourceInspector'
import { attachmentKey } from './responseHistory'
import styles from './ChatWorkspace.module.css'

export type ReadMessageDetail = (node: ReactNode, opener: HTMLElement | null) => void
export function SentAttachments({files, onRead}: {files: ChatAttachment[]; onRead: ReadMessageDetail}) {
  useLocale()
  if (!files.length) return null
  return <ul className={styles.sentAttachments} aria-label={msg('Attached files')}>{files.map(file => <li key={attachmentKey(file)}><Button variant="quiet" onClick={event => onRead(file.document
    ? <SourceReader name={file.name} reference={file.document} link={file.link}/>
    : <section><h3>{file.name}</h3><CodeBlock text={file.text} label={msg('Attachment')}/></section>, event.currentTarget)}>{file.name}</Button></li>)}</ul>
}
export function SourceList({chatId, documents, websites = [], searches = [], sourceIds = [], binding, onRead}: {
  chatId: string; documents: ChatAttachment[]; websites?: WebsiteReference[]; searches?: SearchReference[]; sourceIds?: string[]; binding?: ResearchRunBinding; onRead: ReadMessageDetail
}) {
  useLocale()
  return <div className={styles.documentList}>
    {documents.map(file => <SourceRow icon={<IconDetails/>} title={file.name} key={attachmentKey(file)} onClick={event => onRead(<SourceReader name={file.name} reference={file.document!} link={file.link}/>, event.currentTarget)}/>)}
    {sourceIds.map(id => <SourceRow icon={<IconDetails/>} title={binding?.ledger?.byId(id)?.document?.title || id} key={id} onClick={event => binding?.ledger && onRead(<SourceInspector selection={{kind:'source',id}} ledger={binding.ledger} state={binding.state}/>, event.currentTarget)}/>)}
    {searches.map(reference=><SourceRow icon={<SearchReferenceIcon reference={reference}/>} title={reference.request.query} meta={msg('Web search')} key={reference.id} onClick={event=>onRead(<SearchSources reference={reference}/>,event.currentTarget)}/>)}
    {websites.map(reference => <SourceRow icon={<IconLink/>} title={new URL(reference.seed).hostname} meta={msg('Website sources')} key={reference.id} onClick={event => onRead(<WebsiteSources chatId={chatId} reference={reference} documents={documents} onRead={onRead}/>, event.currentTarget)}/>)}
  </div>
}
export function MessageSources(props: Parameters<typeof SourceList>[0]) {
  useLocale()
  const count = props.documents.length + (props.sourceIds?.length ?? 0)
  const searches = props.searches ?? []
  if (!count && !props.websites?.length && !searches.length) return null
  return <>{(count>0||!!props.websites?.length)&&<Disclosure title={count ? `${msg('Sources')} · ${count}` : msg('Website sources')}><SourceList {...props} searches={[]}/></Disclosure>}{searches.length>0&&<Disclosure title={`${msg('Search results')} · ${searches.length}`}><SourceList {...props} documents={[]} sourceIds={[]} websites={[]}/></Disclosure>}</>
}
