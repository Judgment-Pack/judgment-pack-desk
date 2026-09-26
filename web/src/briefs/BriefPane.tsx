import { selectedAssistant } from '../assistant/target'
import { assistantReady } from '../assistant/useAssistantSlot'
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { DropdownMenu } from 'radix-ui'
import { msg, useLocale } from '../i18n'
import { jobsAPI } from '../jobs/client'
import { answer, deskFetch } from '../files/client'
import { useFileListing } from '../files/queries'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { usePickedModel } from '../assistant/pickedModel'
import { useAssistantRun } from '../assistant/useAssistantRun'
import { useProposalGenerator } from '../assistant/useProposalGenerator'
import { Button } from '../ui/Button'
import { Select } from '../ui/Select'
import { Tooltip } from '../ui/Tooltip'
import { RunStatus } from '../ui/RunStatus'
import { IconMore } from '../shell/icons'
import { useDetailsSlot } from '../shell/DetailsSlot'
import { SourceReader } from '../documents/SourceReader'
import type { ChatAttachment } from '../chat/store'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { documentContext, loadDocument } from '../documents/client'
import type { BriefSubject } from './context'
import { briefText, evidenceFor, briefResults, sourceIdentity, type Reply, type Snapshot } from './model'
import { printBrief } from './print'
import styles from './BriefPane.module.css'
const EMPTY: readonly string[] = []
async function request(path: string, body?: Record<string, unknown>): Promise<Reply> {
  if (path.startsWith('/api/operations/')) return jobsAPI<Reply>(path.slice('/api/operations/'.length), body)
  return answer<Reply>(await deskFetch(path, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : undefined))
}
export function BriefPane({ subject, headerTarget }: { subject: BriefSubject; headerTarget: HTMLElement | null }) {
  useLocale()
  const config = useEffectiveConfig()
  const listing = useFileListing(), project = listing.data?.root, slot = useAssistantSlot(), details = useDetailsSlot()
  const selected = selectedAssistant(slot)
  const picked = usePickedModel(selected?.models ?? EMPTY, selected?.model ?? null, project)
  const run = useAssistantRun({ purpose: 'brief', endpoint: slot.endpoint, agent: slot.agent ? { ...slot.agent, tools: [] } : undefined, model: picked.model, engine: slot.engine, thinking: slot.thinking })
  const generate = useProposalGenerator(run), cache = useQueryClient()
  const key = ['briefs', project, subject.path, subject.id]
  const query = useQuery({ queryKey: key, enabled: !!project && !subject.unavailable, queryFn: () => request(subject.path), refetchInterval: q => q.state.data?.content.subjects[subject.id]?.pending ? 4000 : false })
  const history = query.data?.content.subjects[subject.id]
  const revisions = history?.revisions ?? []
  const source = subject.snapshot ?? query.data?.snapshot
  const [revision, setRevision] = useState('latest'), [showHistory, setHistory] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(''), [phase, setPhase] = useState('')
  const active = useRef<AbortController | null>(null), page = useRef<HTMLDivElement>(null)
  useEffect(() => () => active.current?.abort(), [])
  const held = revision === 'latest' ? revisions.at(-1) : revisions.find(r => r.id === revision)
  const stale = held && source && sourceIdentity(held.snapshot) !== sourceIdentity(source)
  const [, updateClock] = useState(0)
  useEffect(() => {
    if (!history?.pending) return
    const timeout = setTimeout(() => updateClock(n => n + 1), Math.max(0, Date.parse(history.pending.expires) - Date.now()) + 50)
    return () => clearTimeout(timeout)
  }, [history?.pending?.expires])
  const pending = history?.pending && Date.parse(history.pending.expires) > Date.now()
  const snapshot = held?.snapshot
  async function post(body: Record<string, unknown>) {
    const response = await request(subject.path, { ...body, project, subject: subject.id, owner: subject.owner, caseId: subject.caseId })
    if (response.project && response.project !== project) throw Error(msg('The project changed. Reopen the brief before continuing.'))
    cache.setQueryData(key, response)
    return response
  }
  async function start() {
    if (active.current || !source || !picked.model || !assistantReady(slot)) return
    const controller = new AbortController(); active.current = controller; setBusy(true); setError('')
    let token = ''
    try {
      setPhase(msg('Reading sources…'))
      const frozen = structuredClone(source)
      const documents: { id: string; context: string }[] = []
      for (const file of (frozen.record.case as { sources?: ChatAttachment[] })?.sources ?? []) {
        if (!file.document) continue
        if (!config.config.research.gateway) throw Error(msg('Configure document processing to read these sources.'))
        const doc = await loadDocument(file.document, config.config.research.gateway, controller.signal)
        documents.push({ id: file.id, context: documentContext(doc, file.document) })
      }
      if (documents.length) frozen.documents = documents
      if (new TextEncoder().encode(JSON.stringify(frozen)).length > 1000000) throw Error(msg('Select fewer source pages before continuing.'))
      controller.signal.throwIfAborted()
      const reservation = await post({ action: 'begin', expectedRevision: revisions.length, snapshot: frozen, model: picked.model })
      const pending = reservation.content.subjects[subject.id]?.pending
      if (!pending) throw Error(msg('The brief reservation could not be read.'))
      token = pending.token
      controller.signal.throwIfAborted()
      setPhase(msg('Writing brief…'))
      const result = await generate('Write the one-page brief from this frozen snapshot. Required evidence and recorded results are also rendered separately from your prose. Keep all four sections together below 220 words.\n' + JSON.stringify(pending.snapshot), controller.signal)
      controller.signal.throwIfAborted()
      if (result.failure) throw Error(result.failure)
      const text = briefText(result.document)
      try { await post({ action: 'complete', token, text }) }
      catch (saveError) {
        const saved = await request(subject.path)
        if (!saved.content.subjects[subject.id]?.revisions.some(r => r.id === token)) throw saveError
        cache.setQueryData(key, saved)
      }
      token = ''; setRevision('latest')
    } catch (e) { if (!controller.signal.aborted) setError((e as Error).message) }
    finally {
      if (token) { try { await post({ action: 'cancel', token }) } catch { /* Server lease expires; existing revisions remain readable. */ } }
      active.current = null; setBusy(false); void query.refetch()
    }
  }
  function print() { try { if (page.current && held) printBrief(page.current, held.snapshot.title) } catch(e) { setError((e as Error).message) } }
  const actions = <DropdownMenu.Root><Tooltip content={msg('Brief actions')}><DropdownMenu.Trigger asChild><button className="desk-icon-button" aria-label={msg('Brief actions')}><IconMore /></button></DropdownMenu.Trigger></Tooltip><DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" sideOffset={6}>
    <DropdownMenu.Item className="desk-menu-item" disabled={busy || !!pending || !source || !held || !picked.model} onSelect={() => { void start() }}>{msg('Regenerate brief')}</DropdownMenu.Item>
    <DropdownMenu.Item className="desk-menu-item" disabled={!revisions.length} onSelect={() => setHistory(v => !v)}>{msg('Revision history')}</DropdownMenu.Item>
    <DropdownMenu.Item className="desk-menu-item" disabled={!held} onSelect={print}>{msg('Print / Save PDF')}</DropdownMenu.Item>
  </DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>
  return <div className={styles.pane}>
    {headerTarget && createPortal(actions, headerTarget)}
    <div className={styles.page}>
      {subject.unavailable ? <p className={styles.meta}>{subject.unavailable}</p> : <>
        {query.isPending && <RunStatus>{msg('Loading brief…')}</RunStatus>}
        {(error || query.error) && <RunStatus error>{error || (query.error as Error).message}</RunStatus>}
        {query.error && <div><Button onClick={() => { void query.refetch() }}>{msg('Retry')}</Button></div>}
        {busy && <div className={styles.actions}><RunStatus running>{phase}</RunStatus><Button variant="quiet" onClick={() => active.current?.abort()}>{msg('Stop')}</Button></div>}
        {!busy && pending && <RunStatus>{msg('A brief is being generated in another window. The saved version remains available.')}</RunStatus>}
        {stale && <p className={styles.notice}>{msg('Inputs have changed since this brief was generated. Regenerate when you are ready.')}</p>}
        {showHistory && revisions.length > 0 && <Select id="brief-revision" aria-label={msg('Brief revision')} value={revision} onValueChange={setRevision} options={[{ value: 'latest', label: msg('Latest revision') }, ...[...revisions].reverse().map(r => ({ value: r.id, label: msg('Revision {{revision}} · {{date}}', { revision: r.revision, date: new Date(r.at).toLocaleString() }) }))]} />}
        {!held && !query.isPending && !query.error && <section className={styles.empty}><h2>{msg('A one-page brief')}</h2><p className={styles.meta}>{msg('Summarize this saved record, its required evidence and what needs attention. Generate once, then reopen the saved version at any time.')}</p><div><Button variant="primary" disabled={busy || !!pending || !source || !picked.model || !assistantReady(slot)} onClick={() => { void start() }}>{msg('Generate brief')}</Button></div></section>}
        {!busy && !held && <div data-no-print>{selected && <Select id="brief-model" aria-label={msg('Brief model')} value={picked.model} options={selected.models.map(m => ({ value: m, label: m }))} onValueChange={picked.pick} />}{(!assistantReady(slot) || !picked.model) && <p className={styles.meta}>{msg('Configure an Assistant model in Admin to generate a brief.')}</p>}</div>}
        {held && snapshot && <div ref={page} className={styles.page}>
          <header><h2>{snapshot.title}</h2><p className={styles.meta}>{msg('Brief · Revision {{revision}}', { revision: held.revision })} · {new Date(held.at).toLocaleString()}</p><p className={styles.meta}>{msg('Version')} {String(JSON.parse(snapshot.pack).version ?? '—')}{snapshot.kind === 'run' && <> · {msg('Run {{id}}', { id: String((snapshot.record.run as { id: string }).id).slice(-8) })}</>}</p></header>
          <section><h3>{msg('Context')}</h3><p>{held.text.context}</p></section>
          <RecordedResult snapshot={snapshot} />
          <section><h3>{msg('Evidence needed')}</h3><ul className={styles.evidence}>{evidenceFor(snapshot).map(e => <li key={e.id}><span>{e.label}{!e.required && <small> · {msg('Optional')}</small>}</span><span>{e.state === 'present' ? msg('Declared present') : e.state === 'absent' ? msg('Absent') : msg('Unknown')}</span></li>)}</ul>{!evidenceFor(snapshot).length && <p className={styles.meta}>{msg('No evidence requirements declared.')}</p>}<p className={styles.meta}>{msg('Availability is recorded input, not verification of an evidence document.')}</p></section>
          <section><h3>{msg('Findings')}</h3><p>{held.text.findings}</p></section>
          <section><h3>{msg('Uncertainty and disagreement')}</h3><p>{held.text.uncertainty}</p></section>
          <section><h3>{msg('Next action')}</h3><p>{held.text.nextAction}</p></section>
          {snapshot.kind === 'case' && <div className={styles.sources}>{((snapshot.record.case as { sources?: ChatAttachment[] })?.sources ?? []).map(a => <Button key={a.id} variant="quiet" onClick={e => details.inspect?.(a.document ? <SourceReader reference={a.document} name={a.name} /> : <article className={styles.pane}><h2>{a.name}</h2><p>{a.text}</p></article>, e.currentTarget)}>{a.name}</Button>)}</div>}
          <footer className={`${styles.footer} ${styles.meta}`}>{msg('AI-generated summary · {{model}}. Read with the retained evidence and results; this brief does not approve or change a decision.', { model: held.model })}</footer>
        </div>}
      </>}
    </div>
  </div>
}
function RecordedResult({ snapshot }: { snapshot: Snapshot }) {
  const results = briefResults(snapshot)
  return <section><h3>{snapshot.kind === 'job' ? msg('Sample result') : msg('Recorded result')}</h3>{results.expected && <p>{msg('Expected')}: {results.expected}</p>}<p>{msg('Actual')}: {results.actual}</p>{results.state && <p className={styles.meta}>{results.state}</p>}{results.actual === '—' && <p className={styles.meta}>{msg('No matching result is recorded for these inputs.')}</p>}</section>
}
