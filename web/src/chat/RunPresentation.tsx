import { useEffect, useRef, useState, type ReactNode } from 'react'
import { SearchStepDetails } from '../search/SearchSources'
import { ProviderIcon, providerName } from '../search/ProviderIcon'
import { LinkStepDetails } from './LinkStepDetails'
import type { ChatAttachment } from './store'
import type { ReadMessageDetail } from './MessageSources'
import { Tooltip } from '../ui/Tooltip'
import { searchFailureMessage } from '../search/failures'
import { IconCheck, IconException, IconHistory, IconChevronRight, IconLink } from '../shell/icons'
import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
import { workItems, summarizeWork, type WorkItem, type WorkRecord } from './responseHistory'
import type { RunState } from '../research/run'
import { statusLine } from '../research/ui/Conversation'
import { TOOL_LABELS } from './toolLabels'
import styles from './ChatWorkspace.module.css'
import { Disclosure } from '../ui/Disclosure'
import { RunStatus } from '../ui/RunStatus'

export { workItems } from './responseHistory'
/** Clear selection only when this inspection actually closes, including StrictMode replay. */
function Inspection({children,onClose}:{children:ReactNode;onClose:()=>void}){
 const mounted=useRef(0)
 useEffect(()=>{const version=++mounted.current;return()=>{queueMicrotask(()=>{if(mounted.current===version)onClose()})}},[onClose])
 return children
}
export function WorkSummary({ state, work, documents=[], onRead }: { state?: RunState; work?: WorkRecord; documents?: ChatAttachment[]; onRead?:ReadMessageDetail }) {
  useLocale()
  const saved = work ?? summarizeWork(state?.events ?? [], state?.status === 'running')
  const rows = saved.items, notices = saved.notices.filter(n=>!saved.items.some(row=>searchFailureMessage(row.failure)===n)), critique = saved.critique
  const [selected,setSelected]=useState<{id:string;opener:HTMLElement}|null>(null)
  const inspection=useRef(0)
  const selectedRow=rows.find(row=>row.id===selected?.id)
  const selectedDocument=selectedRow?.link?.documentId?documents.find(file=>file.document?.id===selectedRow.link?.documentId&&file.document?.digest===selectedRow.link?.digest):undefined
  const selectedJSON=selectedRow?JSON.stringify({row:selectedRow,document:selectedDocument}):undefined
  // Keep an open inspection current when a running call completes or fails.
  useEffect(()=>{
   if(!selected || !selectedJSON || !onRead)return
   const token=++inspection.current
   const {row,document}=JSON.parse(selectedJSON) as {row:WorkItem;document?:ChatAttachment}
   onRead(<Inspection key={token} onClose={()=>{if(inspection.current===token)setSelected(null)}}>{row.name==='read_link'?<LinkStepDetails row={row} document={document}/>:<SearchStepDetails row={row}/>}</Inspection>,selected.opener)
  },[selected,selectedJSON,onRead])
  if (!rows.length && !notices.length && !critique) return null
  const failures = rows.filter(row => row.status === 'failed').length
  return <div className={styles.workGroup}>
   {(rows.length>0||notices.length>0||critique)&&<Disclosure className={styles.work} title={<span className={styles.workTitle}>{rows.length ? msg('Work · {{count}} steps', {count:rows.length}) : msg('Assistant notice')}{failures>0&&<span className={styles.workWarning}><IconException/>{msg(' · {{count}} failed',{count:failures})}</span>}</span>}>
    {rows.length > 0 && <ol>{rows.map(row => {
     const search=row.name==='search_sources', link=row.name==='read_link', interactive=(search||link)&&!!onRead
     const status=row.status === 'complete' ? msg('Done') : row.status === 'failed' ? msg('Failed') : row.status === 'interrupted' ? msg('Interrupted') : msg('Working…')
     const label=TOOL_LABELS[row.name] ?? row.name
     const contents=<><span className={styles.workIcon}>{search?<ProviderIcon provider={row.search?.provider}/>:link?<IconLink/>:row.status==='complete'?<IconCheck/>:row.status==='failed'?<IconException/>:<IconHistory/>}</span><div className={styles.workStep}><span>{label}</span>{!interactive&&row.failure&&<small>{systemMessage(searchFailureMessage(row.failure)!)}</small>}</div><span className={styles.workState}>{status}</span>{interactive&&<IconChevronRight/>}</>
     return <li key={row.id} data-status={row.status}>{interactive?<Tooltip side="right" openOnFocus={false} content={link?msg('View link details'):row.search?.provider?msg('View {{provider}} details',{provider:providerName(row.search.provider)!}):msg('View search details')}><button type="button" className={styles.workRow} aria-label={`${label} · ${link?row.link?new URL(row.link.url).hostname:msg('Link'):providerName(row.search?.provider)??msg('Web search')} · ${status}`} aria-pressed={selected?.id===row.id} onClick={event=>setSelected({id:row.id,opener:event.currentTarget})}>{contents}</button></Tooltip>:<div className={styles.workRow}>{contents}</div>}</li>
    })}</ol>}
    {notices.map(notice => <p key={notice}>{systemMessage(notice)}</p>)}
    {critique && <p><Message text={"Adversarial review: <0/>"} slots={[critique]} /></p>}
   </Disclosure>}
  </div>
}

/** One visible status. Completed replies have no persistent status furniture. */
export function TaskStatus({ state }: { state: RunState }) {
  useLocale()
  if (state.status === 'idle' || state.status === 'complete' || state.status === 'ready') return null
  const active = state.status === 'running' ? workItems(state.events, true).filter(item => item.status === 'working') : []
  const message = active.length ? `${TOOL_LABELS[active[0]!.name] ?? active[0]!.name}…` : systemMessage(state.detail) || statusLine(state)
  return <RunStatus className={styles.runStatus} running={state.status === 'running'} error={state.status === 'failed'}>{message}</RunStatus>
}

export function candidateSummary(state: RunState): string {
  const candidate = state.candidates.at(-1)
  const check = candidate?.check
  if (state.restored) return msg("Saved draft · Recheck needed")
  if (!check || check.documentDigest !== candidate?.digest) return state.status === 'running' ? msg("Checking draft…") : msg("Not checked")
  if (!check.valid) return msg("Structure needs corrections")
  if (!check.cases.length) return msg("Structure checked · Tests not run")
  const passed = check.cases.filter(row => row.passed).length
  return msg("{{value0}} of {{value1}} tests passed{{value2}}", { value0: passed, value1: check.cases.length, value2: state.status === 'ready' ? msg(' · Ready for review') : msg(' · Review needed') })
}
