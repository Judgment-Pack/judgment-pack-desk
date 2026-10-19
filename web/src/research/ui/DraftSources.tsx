import type {PolicyAuthoringSource} from '../../policies/authoringSource'
import {policySourceKey} from '../../policies/authoringSource'
import {PolicySourceIdentity} from '../../policies/PolicySourceIdentity'
import { useCollectionSelection } from '../../ui/useCollectionSelection'
import { SelectionTable, SelectionHeader, SelectionCell, SelectionActions } from '../../ui/SelectionTable'
import { exportSelectedRows } from '../../ui/selectionExport'
import { CollectionRow } from '../../ui/CollectionRow'
import { useState, type ReactNode } from 'react'
import type { ChatAttachment } from '../../chat/store'
import type { SourceRecord } from '../ledger'
import { citedPage, type Citation } from '../run'
import { SourceReader } from '../../documents/SourceReader'
import { msg, useLocale } from '../../i18n'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import { Button } from '../../ui/Button'
import { CollectionToolbar } from '../../ui/CollectionToolbar'
import { BuilderSplit } from '../../builders/BuilderSplit'
import { CollectionLayout } from '../../ui/CollectionLayout'
import { Pagination } from '../../ui/Pagination'
import { usePagination } from '../../ui/usePagination'
import styles from '../../definitions/DefinitionWorkspace.module.css'

const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
export const declaredSources = (document: unknown): unknown[] => object(document) && Array.isArray(document.sources) ? document.sources : []
const text = (value: unknown): string => typeof value === 'string' ? value : ''

/** Shared saved/draft source browser. A declared reference never implies retrieval. */
export function DraftSources({ document, documents = [], files = [], policySources = [], sources = [], citations = [] }: {
  document: unknown; documents?: readonly ChatAttachment[]; files?: readonly { name: string; text: string }[]
  policySources?:readonly PolicyAuthoringSource[]
  sources?: readonly SourceRecord[]; citations?: readonly Citation[]
  onRead?: (node: ReactNode, opener: HTMLElement) => void
}) {
  useLocale()
  const [query,setQuery]=useState(''),[kind,setKind]=useState('all'),[selected,setSelected]=useState('')
  const [pageCitation,setPageCitation]=useState<Citation>()
  const declared = declaredSources(document)
  const cited = pageCitation && citedPage(pageCitation.location)
  const citedFile = cited && documents.find(file=>file.document?.id===cited.documentId && file.document.digest===cited.digest)
  const citationLinks = (items: readonly Citation[]) => items.map((citation,index)=>{
    const page=citedPage(citation.location)
    const file=page && documents.find(file=>file.document?.id===page.documentId && file.document.digest===page.digest)
    return <div key={index}><span>{citation.traced?msg('Citation traced'):msg('Citation not traced')}</span>{citation.reason && <p>{citation.reason}</p>}
      {page && <Button variant="quiet" disabled={!citation.traced||!file} onClick={()=>setPageCitation(citation)}>{msg('Page {{number}}',{number:page.page})}</Button>}
    </div>
  })
  const uses = (id: string) => !id || !object(document) ? [] : ['rules','exceptions'].flatMap(group=>Array.isArray(document[group]) ? document[group].flatMap((entry:unknown)=>object(entry) && Array.isArray(entry.sourceRefs) && entry.sourceRefs.includes(id) ? [text(entry.id) || msg('Unnamed rule')] : []) : [])
  const rows: {id:string;title:string;kind:string;kindLabel:string;status:string;used:string[];detail:ReactNode}[] = [
    ...policySources.map(source=>({id:'policy:'+policySourceKey(source),title:source.title||msg('Policy'),kind:'policy',kindLabel:msg('Policy'),status:source.publicationId?msg('Selected publication'):msg('Saved revision · no publication selected'),used:[],detail:<PolicySourceIdentity source={source}/>})),
    ...declared.map((source,index)=>{
      const record=object(source)?source:{}, id=text(record.id), title=text(record.title)||id||msg('Source {{value0}}',{value0:index+1})
      const locator=object(record.locator)?text(record.locator.value):'', citation=object(record.citation)?record.citation:{}
      return {id:'declared:'+index,title,kind:'reference',kindLabel:msg('Reference'),status:msg('Declared only'),used:uses(id),detail:<>
        <p className={styles.muted}>{msg('A declared reference does not verify retrieval or the truth of its contents.')}</p>
        {locator && <p>{locator}</p>}{text(record.publisher) && <p>{text(record.publisher)}</p>}
        {text(citation.excerpt) && <blockquote>{text(citation.excerpt)}</blockquote>}{text(citation.location) && <p>{text(citation.location)}</p>}
        {citationLinks(citations.filter(c=>c.sourceId===id))}
        <Disclosure title={msg('Exact reference')}><CodeBlock expandable text={JSON.stringify(source,null,2)} label="JSON"/></Disclosure>
      </>}
    }),
    ...documents.map(file=>({id:'document:'+JSON.stringify([file.id,file.document?.digest,file.document?.pages]),title:file.name,kind:'document',kindLabel:msg('Document'),status:msg('Attached'),used:[],detail:<>{citationLinks(citations.filter(c=>{const page=citedPage(c.location);return page?.documentId===file.document?.id&&page?.digest===file.document?.digest}))}{file.document?<SourceReader name={file.name} reference={file.document} link={file.link}/>:<p>{msg('Document content is unavailable.')}</p>}</>})),
    ...files.map((file,index)=>({id:'file:'+index,title:file.name,kind:'document',kindLabel:msg('Text file'),status:msg('Attached'),used:[],detail:<CodeBlock expandable text={file.text} label={msg('Text')}/>})),
    ...sources.map(record=>({id:'record:'+record.session+':'+record.id,title:record.kind==='search'?msg('Search: {{value0}}',{value0:record.request.query??''}):record.document?.title||record.request.url||record.id,kind:'retrieval',kindLabel:record.kind==='search'?msg('Search'):msg('Page'),
      status:record.failure!==null?msg('Retrieval failed'):record.verification.state==='verified'?msg('Receipt verified'):record.verification.state==='failed'?msg('Receipt failed'):msg('Receipt unchecked'),used:uses(record.id),detail:<>
        {record.failure && <p role="status">{record.failure}</p>}
        {record.document && <CodeBlock expandable text={record.document.text} label={msg('Retrieved text')}/>}
        {record.hits && <Disclosure title={msg('Search results')}><CodeBlock expandable text={JSON.stringify(record.hits,null,2)} label={msg('Search results')}/></Disclosure>}
        {record.excerpts.length>0 && <Disclosure title={msg('Excerpts')}>{record.excerpts.map(excerpt=><blockquote key={excerpt.id}>{excerpt.text}</blockquote>)}</Disclosure>}
        <Disclosure title={msg('Retrieval and receipt')}><p>{msg('Receipt verification establishes byte lineage, not truth, currency or origin.')}</p><CodeBlock expandable text={JSON.stringify({request:record.request,requestedAt:record.requestedAt,acquisition:record.acquisition,verification:record.verification,pageDates:record.document?.pageDates},null,2)} label={msg('Provenance')}/></Disclosure>
      </>}))
  ]
  const filtered=rows.filter(row=>(kind==='all'||row.kind===kind)&&[row.title,...row.used].join(' ').toLocaleLowerCase().includes(query.toLocaleLowerCase()))
  const selection=useCollectionSelection(filtered,row=>row.id,JSON.stringify([document,files.map(file=>file.name)]))
  const pagination=usePagination(filtered.length,JSON.stringify([query,kind]))
  const visible=filtered.slice(pagination.start,pagination.end), detail=visible.find(row=>row.id===selected)
  const detailContent=cited && citedFile?.document ? <><Button variant="quiet" onClick={()=>setPageCitation(undefined)}>{msg('Back to source details')}</Button><SourceReader name={citedFile.name} reference={citedFile.document} link={citedFile.link} citation={{page:cited.page,quote:pageCitation?.quote??''}}/></> : detail?.detail
  return <div className={styles.browser}><BuilderSplit preference="pack" title={detail?.title??msg('Source details')} editor={detailContent} open={!!detail} onClose={()=>{setSelected('');setPageCitation(undefined)}}>
    <CollectionLayout label={msg('Pack sources')} scrollKey={JSON.stringify([pagination.page,pagination.size,query,kind])} footer={<Pagination pagination={pagination} complete/>} toolbar={<>
      <CollectionToolbar label={msg('Source controls')} searchLabel={msg('Search sources')} search={query} onSearch={setQuery}
        filters={[{label:msg('Source kind'), value:kind, onValueChange:setKind, options:[{value:'all',label:msg('All kinds')},{value:'policy',label:msg('Policies')},{value:'reference',label:msg('References')},{value:'document',label:msg('Documents')},{value:'retrieval',label:msg('Retrievals')}]}]}
        actions={()=><SelectionActions selection={selection} onExport={()=>exportSelectedRows('sources',selection.selectedRows.map(({detail,...row})=>row))}/>}/>

      {object(document) && document.sources!==undefined && !Array.isArray(document.sources) && <p role="status">{msg('Source references could not be read. Inspect View JSON for the exact definition.')}</p>}
    </>}>
      {!filtered.length ? <p className={styles.muted}>{rows.length?msg('No matching sources.'):msg('No sources yet. Declared references and retrieved documents appear here.')}</p> : <SelectionTable selection={selection} className={styles.table}><thead><tr><SelectionHeader selection={selection}/><th scope="col">{msg('Name')}</th><th scope="col">{msg('Kind')}</th><th scope="col">{msg('Retrieval')}</th><th scope="col">{msg('Used by')}</th></tr></thead><tbody>
        {visible.map(row=><CollectionRow key={row.id} data-selected={row.id===selected||undefined} data-selection-selected={selection.has(row.id)||undefined}><SelectionCell selection={selection} id={row.id} name={row.title}/><td><Button data-row-primary variant="quiet" className={styles.name} aria-expanded={row.id===selected} onClick={()=>{setSelected(row.id);setPageCitation(undefined)}}>{row.title}</Button></td><td>{row.kindLabel}</td><td>{row.status}</td><td>{row.used.join(', ')||'—'}</td></CollectionRow>)}
      </tbody></SelectionTable>}
    </CollectionLayout>
  </BuilderSplit></div>
}
