import {privateQuery} from '../data/privateQuery'
import {LoadingState} from '../ui/LoadingState'
import { AIConnectionPicker } from '../assistant/AIConnectionPicker'
import { selectedAssistant } from '../assistant/target'
import { assistantReady } from '../assistant/useAssistantSlot'
import { useEffect, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Menu as DropdownMenu } from '../ui/Menu'
import { msg, systemMessage, useLocale } from '../i18n'
import { jobsAPI } from '../jobs/client'
import { answer, deskFetch } from '../files/client'
import { useFileListing } from '../files/queries'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { usePickedModel } from '../assistant/pickedModel'
import { serverRunClient } from '../assistant/serverRuns/client'
import type { RunSnapshot } from '../assistant/serverRuns/types'
import { ExecutionProgress } from '../assistant/execution/ExecutionProgress'
import { ResponseRecovery } from '../chat/ResponseRecovery'
import { AIFailureError, normalizeFailure, readFailure, type AIFailure } from '@judgment-pack/harness/failures'
import { observeBrief, savedBrief } from './serverGeneration'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Button } from '../ui/Button'
import { Select } from '../ui/Select'
import { Tooltip } from '../ui/Tooltip'
import { RunStatus } from '../ui/RunStatus'
import { IconMore } from '../shell/icons'
import { useDetailsSlot } from '../shell/DetailsSlot'
import { SourceReader } from '../documents/SourceReader'
import type { ChatAttachment } from '../chat/store'
import type { BriefSubject } from './context'
import { evidenceFor, briefResults, sourceIdentity, type Reply, type Snapshot } from './model'
import { printBrief } from './print'
import styles from './BriefPane.module.css'
const EMPTY: readonly string[] = []
async function request(path: string, body?: Record<string, unknown>): Promise<Reply> {
  if (path.startsWith('/api/operations/')) return jobsAPI<Reply>(path.slice('/api/operations/'.length), body)
  const reply = await answer<Reply>(await deskFetch(path, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : undefined))
  if (reply?.content?.version !== 1 || !reply.content.subjects) throw Error(msg('The saved brief could not be read. Try again.'))
  return reply
}
export function BriefPane({ subject, headerTarget = null, embedded = false, sourceKey, content, generationUnavailable }: {
  subject: BriefSubject; headerTarget?: HTMLElement | null; embedded?: boolean; sourceKey?: string; content?: string; generationUnavailable?: string
}) {
  const id = useId()
  const [connectionId, setConnectionId] = useState<string>()
  const [regenerating, setRegenerating] = useState(false)
  useLocale()
  const listing = useFileListing(), project = listing.data?.root, slot = useAssistantSlot(connectionId), details = useDetailsSlot()
  const selected = selectedAssistant(slot)
  const picked = usePickedModel(selected?.models ?? EMPTY, selected?.model ?? null, project, slot.connectionId, 'brief')
  const cache = useQueryClient()
  const key = ['briefs', project, subject.path, subject.id]
  const query = useQuery({ ...privateQuery, queryKey: key, enabled: !!project && !subject.unavailable, queryFn: () => request(subject.path), refetchInterval: q => q.state.data?.content.subjects[subject.id]?.pending ? 4000 : privateQuery.refetchInterval })
  const previousSource = useRef(sourceKey)
  useEffect(() => { if (previousSource.current !== sourceKey) { previousSource.current = sourceKey; void query.refetch() } }, [sourceKey, query.refetch])
  const history = query.data?.content.subjects[subject.id]
  const revisions = history?.revisions ?? []
  const source = subject.snapshot ?? query.data?.snapshot
  const [revision, setRevision] = useState('latest'), [showHistory, setHistory] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(''), [phase, setPhase] = useState('')
  const [serverRun,setServerRun] = useState<RunSnapshot>()
  const [runFailure,setRunFailure] = useState<AIFailure>()
  const active = useRef<AbortController | null>(null), page = useRef<HTMLDivElement>(null)
  useEffect(() => {
    setServerRun(undefined);setBusy(false);setError('');setRunFailure(undefined)
    return () => {active.current?.abort();active.current=null}
  }, [subject.id,subject.path,project])
  const held = revision === 'latest' ? revisions.at(-1) : revisions.find(r => r.id === revision)
  const sourceMismatch = content !== undefined && source !== undefined && source.pack !== content
  const blocked = generationUnavailable || query.data?.snapshotIssue || (sourceMismatch ? msg('Save the current changes before generating a brief.') : undefined)
  const stale = held && (sourceMismatch || source && sourceIdentity(held.snapshot) !== sourceIdentity(source))
  const [, updateClock] = useState(0)
  useEffect(() => {
    if (!history?.pending) return
    const timeout = setTimeout(() => updateClock(n => n + 1), Math.max(0, Date.parse(history.pending.expires) - Date.now()) + 50)
    return () => clearTimeout(timeout)
  }, [history?.pending?.expires])
  const pending = history?.pending && Date.parse(history.pending.expires) > Date.now()
  const snapshot = held?.snapshot
  const definition = snapshot?.kind === 'pack' || snapshot?.kind === 'graph'
  async function post(body: Record<string, unknown>) {
    const response = await request(subject.path, { ...body, project, subject: subject.id, owner: subject.owner, caseId: subject.caseId })
    if (response.project && response.project !== project) throw Error(msg('The project changed. Reopen the brief before continuing.'))
    cache.setQueryData(key, response)
    return response
  }
  async function follow(client:Awaited<ReturnType<typeof serverRunClient>>,initial:RunSnapshot,controller:AbortController) {
    const run=await observeBrief(client,initial,controller.signal,setServerRun)
    if(run.state==='complete') {
      setPhase(msg('Writing brief…'))
      try { cache.setQueryData(key,savedBrief(await client.commitBrief(run,controller.signal),run)) }
      catch(saveError) {
        try {cache.setQueryData(key,savedBrief(await client.briefReceipt(run,controller.signal),run))}
        catch {throw saveError}
      }
      setRevision('latest')
    }
  }
  // Mount/route changes detach only the reader. An exact prior admission may
  // finish or be inspected, but this effect never starts a model request.
  useEffect(()=>{
    const token=history?.pending?.token
    if(!token||active.current)return
    const controller=new AbortController();active.current=controller
    void (async()=>{
      try {
        const client=await serverRunClient(),reference=await client.briefReference(subject.id,token,controller.signal)
        if(reference.run){setBusy(['admitted','running'].includes(reference.run.state));await follow(client,reference.run,controller)}
      }catch(cause){if(!controller.signal.aborted){setRunFailure(normalizeFailure(cause,'desk'));setError(cause instanceof AIFailureError?cause.message:(cause as Error).message)}}
      finally{if(active.current===controller){active.current=null;setBusy(false)}}
    })()
    return ()=>{controller.abort();if(active.current===controller)active.current=null}
  },[history?.pending?.token,subject.id,subject.path,project])
  async function start() {
    if (active.current || blocked || !source || !picked.model || !assistantReady(slot) || !slot.connectionId || !slot.connectionRevision) return
    const controller = new AbortController(); active.current = controller; setBusy(true); setError('');setServerRun(undefined);setRunFailure(undefined)
    try {
      setPhase(msg('Reading sources…'))
      const frozen = structuredClone(source)
      // Source selections are verified by the canonical server document service.
      delete frozen.documents
      if (new TextEncoder().encode(JSON.stringify(frozen)).length > 1000000) throw Error(msg('Select fewer source pages before continuing.'))
      const reservation = await post({ action: 'begin', expectedRevision: revisions.length, snapshot: frozen, model: picked.model })
      const pending = reservation.content.subjects[subject.id]?.pending
      if (!pending) throw Error(msg('The brief reservation could not be read.'))
      controller.signal.throwIfAborted()
      setPhase(msg('Writing brief…'))
      const client=await serverRunClient(),reference=await client.briefReference(subject.id,pending.token,controller.signal)
      let started=reference.run
      if(!started) {
        try { started=await client.start(reference.start,controller.signal,{connectionId:slot.connectionId,connectionRevision:slot.connectionRevision}) }
        catch(cause) {
          controller.signal.throwIfAborted()
          // Resolve only the same admission after lost acknowledgement; never
          // resend or allocate a replacement generation automatically.
          started=(await client.briefReference(subject.id,pending.token,controller.signal)).run
          if(!started)throw cause
        }
      }
      await follow(client,started,controller)
    } catch(cause) {
      if(!controller.signal.aborted){setRunFailure(normalizeFailure(cause,'desk'));setError(cause instanceof AIFailureError?cause.message:(cause as Error).message)}
    } finally {
      if(active.current===controller){active.current=null;setBusy(false)}
      void query.refetch()
    }
  }
  async function stop() {
    const token=history?.pending?.token
    active.current?.abort()
    let stopError:unknown
    try {
      const client=await serverRunClient()
      const retained=token?(await client.briefReference(subject.id,token)).run:serverRun
      if(retained&&!retained.historical&&!['complete','stopped','interrupted','failed'].includes(retained.state))setServerRun(await client.action(retained,{id:crypto.randomUUID(),kind:'cancel',revision:retained.revision}))
    }catch(cause){stopError=cause}
    try {if(token)await post({action:'cancel',token})}catch(cause){stopError=cause}
    if(stopError)setError((stopError as Error).message)
    active.current=null;setBusy(false);void query.refetch()
  }
  function print() { try { if (page.current && held) printBrief(page.current, held.snapshot.title) } catch(e) { setError((e as Error).message) } }
  const modelControls = <><AIConnectionPicker value={slot.connectionId} onValueChange={setConnectionId} disabled={busy}/>{selected && <Select id={id + '-model'} aria-label={msg('Brief model')} disabled={busy} value={picked.model} options={selected.models.map(m => ({ value: m, label: m }))} onValueChange={picked.pick} />}</>
  const actions = <DropdownMenu.Root><Tooltip content={msg('Brief actions')}><DropdownMenu.Trigger asChild><button className="desk-icon-button" aria-label={msg('Brief actions')}><IconMore /></button></DropdownMenu.Trigger></Tooltip><DropdownMenu.Portal><DropdownMenu.Content align="end" sideOffset={6}>
    <DropdownMenu.Item disabled={busy || !!pending || !!blocked || !source || !held} onSelect={() => setRegenerating(true)}>{msg('Regenerate brief')}</DropdownMenu.Item>
    <DropdownMenu.Item disabled={!revisions.length} onSelect={() => setHistory(v => !v)}>{msg('Revision history')}</DropdownMenu.Item>
    <DropdownMenu.Item disabled={!held} onSelect={print}>{msg('Print / Save PDF')}</DropdownMenu.Item>
  </DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>
  return <div className={embedded ? styles.embedded : styles.pane}>
    <Dialog open={regenerating} onOpenChange={setRegenerating} title={msg('Regenerate brief')} description={msg('Choose the AI connection and model for this brief.')} footer={<DialogActions><Button variant="quiet" onClick={()=>setRegenerating(false)}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy || !!pending || !!blocked || !source || !picked.model || !assistantReady(slot)} onClick={()=>{setRegenerating(false);void start()}}>{msg('Regenerate brief')}</Button></DialogActions>}>
      <div className={styles.generate}>{modelControls}</div>{slot.unusable&&<p className={styles.meta}>{systemMessage(slot.unusable)}</p>}
    </Dialog>
    {embedded && <header className={styles.sectionHeader}><h2>{msg('Brief')}</h2><span className={styles.meta}>{msg('Optional summary')}</span>{actions}</header>}
    {!embedded && headerTarget && createPortal(actions, headerTarget)}
    <div className={styles.page}>
      {subject.unavailable ? <p className={styles.meta}>{subject.unavailable}</p> : <>
        {query.isPending && <LoadingState>{msg('Loading brief…')}</LoadingState>}
        {(error || query.error) && <RunStatus error>{error || (query.error as Error).message}</RunStatus>}
        {query.error && <div><Button onClick={() => { void query.refetch() }}>{msg('Retry')}</Button></div>}
        {busy && <div className={styles.actions}><RunStatus running>{phase}</RunStatus><Button variant="quiet" onClick={() => {void stop()}}>{msg('Stop')}</Button></div>}
        {!busy && pending && serverRun?.state !== 'complete' && <div className={styles.actions}><Button variant="quiet" onClick={()=>{void stop()}}>{msg('Stop')}</Button></div>}
        {serverRun && <ExecutionProgress execution={serverRun.checkpoint} onStop={()=>{void stop()}}/>}
        {serverRun && (['interrupted','failed','stopped'].includes(serverRun.state) || !!error) && <ResponseRecovery detail={error} failure={readFailure([...serverRun.events].reverse().find(e=>e.event.type==='error')?.event.failure)??runFailure} disabled={busy}/>}
        {!busy && pending && !serverRun && <RunStatus>{msg('A brief is being generated in another window. The saved version remains available.')}</RunStatus>}
        {blocked && <p className={styles.notice}>{blocked}</p>}
        {stale && <p className={styles.notice}>{msg('Dialects have changed since this brief was generated. Regenerate when you are ready.')}</p>}
        {showHistory && revisions.length > 0 && <Select id={id + "-revision"} aria-label={msg('Brief revision')} value={revision} onValueChange={setRevision} options={[{ value: 'latest', label: msg('Latest revision') }, ...[...revisions].reverse().map(r => ({ value: r.id, label: msg('Revision {{revision}} · {{date}}', { revision: r.revision, date: new Date(r.at).toLocaleString() }) }))]} />}
        {!held && !query.isPending && !query.error && <section className={styles.empty}>{!embedded && <h2>{msg('A one-page brief')}</h2>}<p className={styles.meta}>{source?.kind === 'pack' || source?.kind === 'graph' ? msg('Summarize this definition, its saved tests and open questions. This brief is optional and does not approve or finalize anything.') : msg('Summarize this saved record, its required evidence and what needs attention. Generate once, then reopen the saved version at any time.')}</p><div className={styles.generate}>{modelControls}<Button variant="primary" disabled={busy || !!pending || !!blocked || !source || !picked.model || !assistantReady(slot)} onClick={() => { void start() }}>{msg('Generate brief')}</Button></div></section>}
        {!busy && !held && <div data-no-print>{(!assistantReady(slot) || !picked.model) && <p className={styles.meta}>{slot.unusable ? systemMessage(slot.unusable) : msg('Configure an Assistant model in Admin to generate a brief.')}</p>}</div>}
        {held && snapshot && <div ref={page} className={styles.page}>
          <header><h2>{snapshot.title}</h2><p className={styles.meta}>{msg('Brief · Revision {{revision}}', { revision: held.revision })} · {new Date(held.at).toLocaleString()}</p><p className={styles.meta}>{msg('Version')} {String(JSON.parse(snapshot.pack).version ?? '—')}{snapshot.kind === 'run' && <> · {msg('Run {{id}}', { id: String((snapshot.record.run as { id: string }).id).slice(-8) })}</>}</p></header>
          <section><h3>{msg('Context')}</h3><p>{held.text.context}</p></section>
          {snapshot.kind === 'pack' || snapshot.kind === 'graph' ? <p className={styles.meta}>{msg('Definition summary. Recorded tests are historical observations, not an operational decision or proof of current readiness.')}</p> : <RecordedResult snapshot={snapshot} />}
          <section><h3>{msg('Evidence needed')}</h3><ul className={styles.evidence}>{evidenceFor(snapshot).map(e => <li key={e.id}><span>{e.label}{!e.required && !definition && <small> · {msg('Optional')}</small>}</span><span>{definition ? e.required ? msg('Required') : msg('Optional') : e.state === 'present' ? msg('Declared present') : e.state === 'absent' ? msg('Absent') : msg('Unknown')}</span></li>)}</ul>{!evidenceFor(snapshot).length && <p className={styles.meta}>{msg('No evidence requirements declared.')}</p>}{!definition && <p className={styles.meta}>{msg('Availability is recorded input, not verification of an evidence document.')}</p>}</section>
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
  return <section><h3>{snapshot.kind === 'job' ? msg('Sample result') : msg('Recorded result')}</h3>{results.expected && <p>{msg('Expected')}: {results.expected}</p>}<p>{msg('Actual')}: {results.actual}</p>{results.state && <p className={styles.meta}>{results.state}</p>}{results.actual === '—' && <p className={styles.meta}>{msg('No matching result is recorded for this dialect.')}</p>}</section>
}
