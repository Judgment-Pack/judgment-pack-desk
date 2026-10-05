/**
 * What a run's record says, shown the same way on the run page and in the
 * Activity tab's detail pane (#213): when Runner recorded each step, who
 * initiated the run, which release ran, and what the record holds.
 *
 * Nothing here checks anything. "Present" means Runner returned the member;
 * the sizes and digests are of the bytes Runner returned, computed in this
 * browser, and compared with nothing.
 */
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { formatDate, formatNumber, msg } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import { jobsAPI, type Decision, type Job, type Release, type Run, type SourceInput } from './client'
import { decodeBase64, evidenceOf, requesterOf, sha256, stamp, type Requester, type StampName } from './activity'
import { isSourceV2 } from './mappingTypes'
import { InputLineage, MappingReview } from './MappingReview'
import { SourceSummary } from './SourceSummary'
import { triggerName } from './TriggerForm'
import type { Trigger } from './triggerTypes'
import styles from './JobsView.module.css'

export function when(value: string) { return formatDate(new Date(value), { dateStyle: 'medium', timeStyle: 'medium' }) }
/**
 * A stored time as the run page shows it: `absent` where Runner stored none, and
 * "Not recorded" where what it stored does not read as a time, so neither
 * throws (#223). The Activity tab shows no stamp for either.
 */
function storedTime(name: StampName, at: string | undefined, absent: string) { const stored = stamp(name, at); return stored ? when(stored.at) : at ? msg('Not recorded') : absent }

/** The job and its release, as `GET jobs/{job}` returns them: the query the job page reads. */
export function useJobRecord(jobId: string) {
  return useQuery({ queryKey: ['job', jobId], queryFn: () => jobsAPI<{ job: Job; release: Release }>(`jobs/${jobId}`) })
}
/** The job's triggers: the query the Triggers tab reads. */
export function useJobTriggers(jobId: string) {
  return useQuery({ queryKey: ['job-triggers', jobId], queryFn: () => jobsAPI<{ items: Trigger[]; localFiles?: boolean }>(`jobs/${jobId}/triggers`) })
}

export function runStateLabel(state: Run['state']) { return { queued: msg('Queued'), running: msg('Running'), completed: msg('Completed'), failed: msg('Failed'), interrupted: msg('Interrupted') }[state] }
export function decisionText(result?: Decision) { if (!result) return '—'; return result.disposition.outcomeId ?? (result.disposition.kind === 'not-applicable' ? msg('Not applicable') : msg('Unresolved')) }

/** The name of a stored time: what Runner recorded at it. */
export function stampLabel(name: StampName): string {
  return {
    submitted: msg('Submitted'),
    started: msg('Started'),
    finished: msg('Finished'),
    // Runner sets an interrupted run's finishedAt when it records the
    // interruption, at its restart for a run it found running.
    interruption: msg('Interruption recorded by the runner'),
    received: msg('Received'),
    scheduled: msg('Scheduled for'),
    'preparation-started': msg('Preparation started')
  }[name]
}

/** An installation or a trigger, never a person. */
export function requesterText(by: Requester, triggers?: Trigger[]): string {
  if (by.kind === 'installation') return msg('This installation')
  if (by.kind === 'unrecorded') return msg('Not recorded')
  const trigger = triggers?.find(t => t.id === by.triggerId)
  const name = trigger?.config.name ?? (by.triggerKind ? triggerName(by.triggerKind) : msg('Trigger {{id}}', { id: by.triggerId.slice(-8) }))
  return by.revision === undefined
    ? msg('{{name}}, trigger revision not recorded', { name })
    : msg('{{name}}, trigger revision {{revision}}', { name, revision: by.revision })
}

/** The fixed release a record names, linking to the Release tab; `short` shows the version alone, as a table cell does. */
export function ReleaseName({ jobId, releaseId, release, short = false }: { jobId: string; releaseId: string; release?: Release; short?: boolean }) {
  if (release?.id !== releaseId) return <code>{releaseId}</code>
  const name = msg('Fixed version {{version}}', { version: release.packVersion })
  return <Link to={`/jobs/${jobId}?tab=release`} aria-label={short ? name : undefined}>{short ? release.packVersion : name}</Link>
}

// Not the pack's evidence availability ("Present", "Absent"): whether the
// runner's record carries the item, and nothing about the item itself.
const presence = (held: boolean) => held ? msg('Present in the record') : msg('Absent from the record')

/** The run's own fields: the run page's, and the Activity pane's. */
export function RunFields({ run }: { run: Run }) {
  const job = useJobRecord(run.jobId), triggers = useJobTriggers(run.jobId)
  const release = job.data?.release.id === run.releaseId ? job.data.release : undefined
  const evidence = evidenceOf(run)
  return <dl className={styles.properties}>
    <div><dt>{msg('Execution')}</dt><dd role={run.state === 'queued' || run.state === 'running' ? 'status' : undefined}>{runStateLabel(run.state)}</dd></div>
    <div><dt>{msg('Decision')}</dt><dd>{decisionText(run.result)}</dd></div>
    <div><dt>{msg('Submitted')}</dt><dd>{storedTime('submitted', run.createdAt, msg('Not recorded'))}</dd></div>
    <div><dt>{msg('Started')}</dt><dd>{storedTime('started', run.startedAt, '—')}</dd></div>
    <div><dt>{run.state === 'interrupted' ? stampLabel('interruption') : msg('Finished')}</dt><dd>{storedTime(run.state === 'interrupted' ? 'interruption' : 'finished', run.finishedAt, '—')}</dd></div>
    <div><dt>{msg('Requested by')}</dt><dd>{requesterText(requesterOf(run), triggers.data?.items)}</dd></div>
    <div><dt>{msg('Release')}</dt><dd><ReleaseName jobId={run.jobId} releaseId={run.releaseId} release={release} /></dd></div>
    <div><dt>{msg('Pack digest')}</dt><dd>{release ? <code>{release.packDigest}</code> : '—'}</dd></div>
    <div><dt>{msg('Runtime digest')}</dt><dd>{release ? <code>{release.runtimeDigest}</code> : '—'}</dd></div>
    <div><dt>{msg('Retained inputs')}</dt><dd>{presence(evidence.inputs)}</dd></div>
    <div><dt>{msg('Exact record bytes')}</dt><dd>{presence(evidence.recordBytes)}</dd></div>
    <div><dt>{msg('Signature sidecar')}</dt><dd>{presence(evidence.sidecar)}</dd></div>
    <div><dt>{msg('Acquisition receipts')}</dt><dd>{presence(evidence.receipts)}</dd></div>
  </dl>
}

/** The size and SHA-256 of a base64 member's bytes, once computed here. */
function useBytes(base64?: string) {
  const [measured, setMeasured] = useState<{ source: string; size: number; digest: string }>()
  useEffect(() => {
    if (!base64) return
    let current = true
    const bytes = decodeBase64(base64)
    void sha256(bytes).then(digest => { if (current) setMeasured({ source: base64, size: bytes.length, digest }) })
    return () => { current = false }
  }, [base64])
  return base64 && measured?.source === base64 ? measured : undefined
}
function BytesFacts({ base64, size, digest }: { base64?: string; size: string; digest: string }) {
  const measured = useBytes(base64)
  return <>
    <div><dt>{size}</dt><dd>{!base64 ? msg('Absent from the record') : measured ? formatNumber(measured.size) : '…'}</dd></div>
    <div><dt>{digest}</dt><dd>{!base64 ? msg('Absent from the record') : measured ? <code>{measured.digest}</code> : '…'}</dd></div>
  </>
}

/** A record's JSON, rendered only once opened: a run carries its record's bytes twice. */
export function RecordJSON({ title, value }: { title: string; value: unknown }) {
  const [open, setOpen] = useState(false)
  return <Disclosure title={title} onToggle={event => setOpen(event.currentTarget.open)}>{open && <pre className={styles.json}>{JSON.stringify(value, null, 2)}</pre>}</Disclosure>
}

export function RunTechnicalDetails({ run }: { run: Run }) {
  return <Disclosure title={msg('Technical details')}>
    <dl className={styles.properties}>
      <div><dt>{msg('Run ID')}</dt><dd><code>{run.id}</code></dd></div>
      <div><dt>{msg('Release ID')}</dt><dd><code>{run.releaseId}</code></dd></div>
      <div><dt>{msg('Revision')}</dt><dd>{run.revision}</dd></div>
      <div><dt>{msg('Attempts')}</dt><dd>{run.attempt}</dd></div>
      <div><dt>{msg('Requester as recorded')}</dt><dd>{run.requestedBy ? <code>{run.requestedBy}</code> : msg('Not recorded')}</dd></div>
      <BytesFacts base64={run.auditBytes} size={msg('Exact record bytes: size in bytes')} digest={msg('Exact record bytes: SHA-256')} />
      <BytesFacts base64={run.auditSignatures} size={msg('Signature sidecar: size in bytes')} digest={msg('Signature sidecar: SHA-256')} />
    </dl>
    <p className={styles.note}>{msg('Sizes and SHA-256 digests are computed in this browser from the bytes the runner returned. Nothing here compares them with anything.')}</p>
    <RecordJSON title={msg('Run record (JSON)')} value={run} />
  </Disclosure>
}

function JSONView({ value, title }: { value: unknown; title: string }) { return <Disclosure title={title}><pre className={styles.json}>{JSON.stringify(value, null, 2)}</pre></Disclosure> }

/**
 * The run page's disclosures, for the Activity pane: the trigger record, the
 * problem, the decision, the retained inputs and the runtime's record. The
 * verification download stays on the run page, which the pane links to.
 */
export function RunDisclosures({ run }: { run: Run }) {
  return <>
    {run.trigger && <JSONView title={msg('Trigger record')} value={run.trigger} />}
    {run.problem && <p className={styles.problem}>{run.problem}</p>}
    {run.result && <section className={styles.stack}><h3>{msg('Decision details')}</h3>{run.result.disposition.reasons.length > 0 && <ul>{run.result.disposition.reasons.map(reason => <li key={reason}>{reason}</li>)}</ul>}<p>{run.result.disposition.handoff.state === 'requested' ? msg('Handoff requested: {{target}}', { target: run.result.handoffTarget?.name ?? msg('See full result') }) : msg('No handoff requested')}</p><p className={styles.note}>{msg('This is a recorded decision. No external action or notification was sent.')}</p><JSONView title={msg('Full result')} value={run.result} /></section>}
    {run.input?.source && isSourceV2(run.input.source) ? <><MappingReview mapping={run.input.source.mapping} />{run.input.preparation && <InputLineage preparation={run.input.preparation} />}<JSONView title={msg('Retained inputs')} value={{ facts: run.input.facts, evidence: run.input.evidence }} /></> : run.input?.source ? <SourceSummary source={run.input.source as SourceInput} /> : <JSONView title={msg('Retained inputs')} value={run.input} />}
    {run.audit !== undefined && <JSONView title={msg('Runtime audit record')} value={run.audit} />}
  </>
}
