import { Fragment, useId, useRef, useState } from 'react'
import { historyRecordField } from './historyRecord'
import { DraftComparison } from '../packs/DraftComparison'
import { useInfiniteQuery, useQueries, useQuery } from '@tanstack/react-query'
import { useChats } from '../chat/ChatProvider'
import { useFileListing } from '../files/queries'
import { answer, deskFetch } from '../files/client'
import { testHistoryEntry } from './historyPresentation'
import type { HistoryEntry } from './historyPresentation'
import historyStyles from './DefinitionHistory.module.css'
import { readTests, TESTS_KEY } from '../packs/test-workspace/store'
import { decisionText, type Reply as BriefReply } from '../briefs/model'
import type { Candidate } from '../research/run'
import type { GraphDraft } from '../graphs/drafts'
import { msg, systemMessage } from '../i18n'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { MenuChoice } from '../ui/Menu'
import { Disclosure } from '../ui/Disclosure'
import { CodeBlock } from '../ui/CodeBlock'
import { Pagination } from '../ui/Pagination'
import { usePagination } from '../ui/usePagination'
import styles from './DefinitionWorkspace.module.css'

interface RecordPage {state: string; records: {raw: string; digest: string}[]; next?: number; unread: number}
const json = (value: unknown) => JSON.stringify(value, null, 2)
const text = (value: unknown) => typeof value === 'string' ? value : ''
const recordedTime = (value: unknown) => text(value) && Number.isFinite(Date.parse(text(value))) ? text(value) : undefined
/** Candidate time is not recorded; never borrow the chat's modification time. */
export function candidateHistory(candidates: readonly Candidate[], namespace='draft'): HistoryEntry[] {
  return candidates.flatMap(candidate => {
    const record = candidate.check?.record ?? candidate.previousCheck?.record
    const rows: HistoryEntry[] = [{id:namespace+':candidate:'+candidate.revision+':'+candidate.digest,kind:'changes',title:msg('Draft revision {{revision}}',{revision:candidate.revision}),detail:msg('Saved draft'),raw:candidate.text,digest:candidate.digest,note:msg('A saved copy of this draft. Who changed it and when were not recorded.') }]
    if(record)rows.push({id:'check:'+record.digest,kind:'checks',title:msg('Draft checks recorded'),detail:msg('Revision {{revision}} · Draft checks',{revision:candidate.revision}),at:record.at,raw:json(record),note:msg('These are the checks saved for this draft at that time. They do not approve the pack for use.')})
    return rows
  })
}
export function DefinitionHistory({kind, owner, documentId, draft=false, candidates, graphDraft, observation}: {
  kind: 'pack' | 'graph'; owner: string; documentId?: string; draft?: boolean; candidates?: readonly Candidate[]; graphDraft?: GraphDraft; observation?: unknown
}) {
  const project=useFileListing().data?.root, chats=useChats()
  const linked=kind==='pack' ? (chats.packDrafts??[]).filter(item=>item.id===owner || item.finalized?.id===owner) : []
  const subjects=owner ? [...new Set([`${kind}${draft?'-draft':''}:${owner}`, ...linked.map(item=>'pack-draft:'+item.id)])] : []
  const briefs=useQueries({queries:subjects.map(subject=>({queryKey:['briefs',project,'/api/briefs?subject='+encodeURIComponent(subject),subject],enabled:!!project,queryFn:async({signal}:{signal:AbortSignal})=>answer<BriefReply>(await deskFetch('/api/briefs?subject='+encodeURIComponent(subject),{signal})),staleTime:0}))})
  const tests=useQuery({queryKey:[...TESTS_KEY,project??null],queryFn:readTests,enabled:!!project&&!!owner&&kind==='pack'})
  const records=useInfiniteQuery({queryKey:['definition-history',project,kind,documentId],enabled:!!project&&!!documentId&&!draft,initialPageParam:undefined as number|undefined,
    queryFn:async({pageParam,signal})=>answer<RecordPage>(await deskFetch('/api/definitions/history?'+new URLSearchParams({kind,id:documentId!,...(pageParam===undefined?{}:{before:String(pageParam)})}),{signal})),
    getNextPageParam:page=>page.next})
  const revisions=candidates??(linked.length===1?linked[0]!.checkpoint.state.candidates:[])
  const entries: HistoryEntry[]=candidates?candidateHistory(candidates,owner):linked.flatMap(item=>candidateHistory(item.checkpoint.state.candidates,item.id))
  for(const id of kind==='pack' ? new Set([owner,...linked.map(item=>item.id)]) : [])for(const run of tests.data?.content.suites[id]?.runs??[])entries.push(testHistoryEntry(run))
  briefs.forEach((query,index)=>{for(const brief of query.data?.content.subjects[subjects[index]!]?.revisions??[])entries.push({id:'brief:'+brief.id,kind:'briefs',title:msg('Brief revision {{revision}}',{revision:brief.revision}),detail:msg('Generated with {{model}}',{model:brief.model}),at:brief.at,raw:json(brief),note:msg('An AI-generated summary saved at this time. It does not approve a decision.'),sections:[{title:msg('Context'),text:brief.text.context},{title:msg('Findings'),text:brief.text.findings},{title:msg('Open questions'),text:brief.text.uncertainty},{title:msg('Next action'),text:brief.text.nextAction}]})})
  for(const page of records.data?.pages??[])for(const record of page.records) {
    const value=JSON.parse(record.raw)
    entries.push({id:'decision:'+record.digest,kind:'decisions',title:msg('Decision recorded'),detail:[decisionText(value.disposition),text(value.graph?.node)].filter(Boolean).join(' · '),at:recordedTime(value.at),raw:record.raw,digest:record.digest})
  }
  if(graphDraft)entries.push({id:'graph-draft:'+graphDraft.draftId,kind:'changes',title:msg('Saved graph draft'),detail:graphDraft.id,raw:graphDraft.content,note:msg('The latest saved copy of this graph draft. Earlier edits may not have been recorded.')})
  if(observation)entries.push({id:'observation',kind:'checks',title:msg('Graph tests observed'),detail:msg('Current session'),raw:json(observation),note:msg('This test observation is held in this session. It is not a durable decision record.')})
  const historyId=useId()
  const [search,setSearch]=useState(''),[filter,setFilter]=useState('all'),[selected,setSelected]=useState<string>()
  const searchField=useRef<HTMLInputElement>(null),heading=useRef<HTMLHeadingElement>(null)
  const unique=[...new Map(entries.map(entry=>[entry.id,entry])).values()]
  const filtered=unique.filter(entry=>(filter==='all'||entry.kind===filter)&&[entry.title,entry.detail,entry.digest].join(' ').toLowerCase().includes(search.toLowerCase())).sort((a,b)=>(Date.parse(b.at??'')||0)-(Date.parse(a.at??'')||0))
  const pagination=usePagination(filtered.length,kind+owner+filter+search), shown=filtered.slice(pagination.start,pagination.end)
  const detail=unique.find(entry=>entry.id===selected)
  const loading=briefs.some(query=>query.isPending)||(kind==='pack'&&!!owner&&tests.isPending)||(!draft&&!!documentId&&records.isPending)
  const errors=[...briefs.map(query=>query.error),kind==='pack'&&!!owner&&tests.error,!draft&&!!documentId&&records.error,chats.error].filter(Boolean)
  const recordDetails=detail?.kind==='decisions'?JSON.parse(detail.raw):undefined
  const download=()=>{const url=URL.createObjectURL(new Blob([detail?.raw??json(filtered)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download=detail?'history-record.json':'loaded-definition-history.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
  return <section className={`${styles.history} ${historyStyles.root}`} aria-label={msg('Definition history')}>
    {detail?<>
      <div className={`${styles.toolbar} ${historyStyles.toolbar}`}><Button variant="quiet" onClick={()=>{setSelected(undefined);requestAnimationFrame(()=>searchField.current?.focus())}}>{msg('Back to history')}</Button><div className={styles.actions}><Button onClick={download}>{msg('Export record')}</Button></div></div>
      <div className={styles.reading}>
        <header className={historyStyles.detailHeading}>
          <h2 ref={heading} tabIndex={-1}>{detail.title}</h2>
          <p className={historyStyles.subtitle}>{detail.detail}</p>
          <p className={historyStyles.timestamp}>{detail.at?<time dateTime={detail.at}>{new Date(detail.at).toLocaleString()}</time>:msg('Time not recorded')}</p>
        </header>
        {detail.facts?.length ? <dl className={styles.metadata}>{detail.facts.map(fact=><Fragment key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></Fragment>)}</dl> : null}
        {detail.sections?.map(section=><section className={styles.section} key={section.title}><h3 className={historyStyles.sectionTitle}>{section.title}</h3><p>{section.text}</p></section>)}
      {detail.note&&<p className={styles.muted}>{detail.note}</p>}
      <Disclosure title={msg('Technical details')}>
        <div className={historyStyles.technical}>
          <p className={styles.muted}>{msg('Original saved data and fingerprints for troubleshooting or independent verification.')}</p>
          {recordDetails&&<>
            <dl className={styles.metadata}><dt>{msg('Run')}</dt><dd>{text(recordDetails.run)||'—'}</dd><dt>{msg('Definition version')}</dt><dd>{text(recordDetails.pack?.version)||text(recordDetails.graph?.version)||'—'}</dd><dt>{msg('Runtime')}</dt><dd>{text(recordDetails.tool?.version)||msg('Not recorded')}</dd></dl>
            <Disclosure title={msg('Inputs as evaluated')}><CodeBlock expandable text={historyRecordField(detail.raw,'inputs')??msg('Inspect the saved data for this field.')} label={msg('Recorded inputs')}/></Disclosure>
            <Disclosure title={msg('Receipt references')}><p className={styles.muted}>{msg('These are recorded citations. Receipts and their bindings have not been checked in this view.')}</p><CodeBlock expandable text={historyRecordField(detail.raw,'cites')??msg('Not recorded')} label={msg('Receipt references')}/></Disclosure>
            <Disclosure title={msg('Evaluation notes')}><p className={styles.muted}>{msg('The audit record retains selected trace notes, not the full evaluation trace.')}</p><CodeBlock expandable text={[historyRecordField(detail.raw,'unknownCauses'),historyRecordField(detail.raw,'typeMismatches')].filter(Boolean).join('\n')||msg('Not recorded')} label={msg('Evaluation notes')}/></Disclosure>
            <section className={styles.section}><h3 className={historyStyles.sectionTitle}>{msg('Record verification')}</h3><p className={styles.muted}>{msg('Signatures, checkpoints and timestamps have not been checked in this view. A record digest alone does not authenticate it.')}</p><ButtonLink variant="quiet" to="/admin/safeguards">{msg('Open decision safeguards')}</ButtonLink></section>
          </>}
          {detail.digest&&<section className={styles.section}>
            <h3 className={historyStyles.sectionTitle}>{detail.kind==='decisions'?msg('Event fingerprint'):kind==='pack'?msg('Pack content fingerprint'):msg('Graph content fingerprint')}</h3>
            <p className={styles.muted}>{detail.kind==='decisions'?msg('Identifies the exact saved event data. This fingerprint alone does not verify who recorded it.'):msg('Identifies the exact content saved with this event, even when two revisions have the same version number. It does not indicate approval.')}</p>
            <CodeBlock text={detail.digest} label={msg('SHA-256 fingerprint')}/>
          </section>}
          <section className={styles.section}><h3 className={historyStyles.sectionTitle}>{msg('Saved data')}</h3><p className={styles.muted}>{msg('The original data saved for this event, including any inputs and results it contains.')}</p><CodeBlock expandable text={detail.raw} label={msg('Saved data (JSON)')}/></section>
        </div>
      </Disclosure></div>
    </>:<>
      <div className={`${styles.toolbar} ${historyStyles.toolbar}`}><Input ref={searchField} type="search" aria-label={msg('Search history')} placeholder={msg('Search history…')} value={search} onChange={e=>setSearch(e.target.value)}/><MenuChoice label={msg('Event type')} value={filter} onValueChange={setFilter} options={[{value:'all',label:msg('All events')},{value:'changes',label:msg('Changes')},{value:'checks',label:msg('Checks')},{value:'briefs',label:msg('Briefs')},{value:'decisions',label:msg('Decisions')}]}/><div className={styles.actions}><Button variant="quiet" onClick={()=>{if(!draft&&documentId)void records.refetch();if(kind==='pack'&&owner)void tests.refetch();briefs.forEach(query=>void query.refetch())}}>{msg('Refresh')}</Button><Button disabled={!filtered.length} onClick={download}>{msg('Export loaded events')}</Button></div></div>
      <p className={historyStyles.intro}>{msg('Saved changes, test results, briefs and decisions. Some activity may not have been recorded.')}</p>
      {!!errors.length&&<Disclosure title={msg('Some history is unavailable')} open>{errors.map((error,i)=><p className={styles.muted} key={i}>{systemMessage(error instanceof Error?error.message:String(error))}</p>)}</Disclosure>}
      {(records.data?.pages.some(page=>page.unread>0))&&<p role="status" className={styles.notice}>{msg('Some trail lines could not be read as completed decisions. Inspect the full trail in Decision safeguards.')}</p>}
      {!draft&&!documentId&&<p role="status" className={styles.notice}>{msg('Decision history is unavailable until the definition identity can be read.')}</p>}
      {records.data?.pages[0]?.state==='no-trail'&&<p className={styles.notice}>{msg('This project does not keep a Runtime decision trail.')}</p>}
      {loading&&<p role="status">{msg('Loading history…')}</p>}
      {!loading&&!filtered.length&&<p className={styles.muted}>{search||filter!=='all'?msg('No matching events in the loaded history.'):msg('No retained history is available for this definition.')}</p>}
      <div className={styles.tableScroll}><table className={`${styles.table} ${historyStyles.table}`}><thead><tr><th scope="col">{msg('Event')}</th><th scope="col">{msg('Recorded')}</th></tr></thead><tbody>{shown.map((entry,index)=><tr key={entry.id}><td><Button variant="inline" className={historyStyles.event} aria-label={entry.title} aria-describedby={`${historyId}-${index}`} onClick={()=>{setSelected(entry.id);requestAnimationFrame(()=>heading.current?.focus())}}><span className={historyStyles.eventTitle}>{entry.title}</span><span id={`${historyId}-${index}`} className={historyStyles.subtitle}>{entry.detail}</span></Button></td><td>{entry.at?<time dateTime={entry.at}>{new Date(entry.at).toLocaleString()}</time>:msg('Time not recorded')}</td></tr>)}</tbody></table></div>
      {revisions.length>1&&<Disclosure title={msg('Compare draft revisions')}><DraftComparison candidates={revisions}/></Disclosure>}
      <Pagination pagination={pagination} complete={!loading&&!errors.length&&!records.hasNextPage&&(draft||!!documentId)}/>
      {records.hasNextPage&&<Button disabled={records.isFetchingNextPage} onClick={()=>void records.fetchNextPage()}>{msg('Load older decision records')}</Button>}
    </>}
  </section>
}
