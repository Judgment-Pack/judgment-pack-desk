import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { MappedInputFields } from './MappedInputFields'
import { MappingReview, InputLineage } from './MappingReview'
import { isSourceV2, type SourceV2 } from './mappingTypes'
import { deskFetch } from '../files/client'
import { SourceInputFields } from './SourceInputFields'
import { SourceSummary } from './SourceSummary'
import { verifySource } from './sourceInputs'
import type { SourceInput, InputMapping } from './client'
import type { PackDocument } from '../mcp/types'
import { useBriefSubject } from '../briefs/context'
import { useEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { msg, useLocale, formatDate } from '../i18n'
import { PageHeader, PageBody } from '../ui/PageLayout'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { TextArea } from '../ui/TextArea'
import { Select } from '../ui/Select'
import { Disclosure } from '../ui/Disclosure'
import { usePack, usePacks } from '../mcp/queries'
import { jobsAPI, type Decision, type Job, type JobInput, type Page, type Release, type Run } from './client'
import styles from './JobsView.module.css'
import { readReleaseTests } from './releaseTests'
import { ReleaseReadiness } from './ReleaseReadiness'

function date(value: string) { return formatDate(new Date(value), { dateStyle: 'medium', timeStyle: 'short' }) }
function errorText(error: unknown) { return error instanceof Error ? error.message : msg('The local runner could not complete this request.') }
function Problem({ error }: { error: unknown }) { return error ? <p className={styles.problem} role="alert">{errorText(error)}</p> : null }
function JSONView({ value, title }: { value: unknown; title: string }) { return <Disclosure title={title}><pre className={styles.json}>{JSON.stringify(value, null, 2)}</pre></Disclosure> }
export function decisionLabel(result?: Decision) { if (!result) return '—'; return result.disposition.outcomeId ?? (result.disposition.kind === 'not-applicable' ? msg('Not applicable') : msg('Unresolved')) }
function stateLabel(state: Run['state']) { return { queued: msg('Queued'), running: msg('Running'), completed: msg('Completed'), failed: msg('Failed'), interrupted: msg('Interrupted') }[state] }
function usePages<T>(path: string) { return useInfiniteQuery({ queryKey: ['jobs-pages', path], initialPageParam: 0, queryFn: ({ pageParam }) => jobsAPI<Page<T>>(`${path}?after=${pageParam}`), getNextPageParam: page => page.next || undefined, refetchInterval: 2500 }) }
function More({ hasNextPage, isFetchingNextPage, fetchNextPage }: { hasNextPage: boolean; isFetchingNextPage: boolean; fetchNextPage: () => unknown }) { return hasNextPage ? <Button disabled={isFetchingNextPage} onClick={() => { void fetchNextPage() }}>{msg('Load more')}</Button> : null }

export function JobsContent() {
  useLocale()
  const { jobId, runId } = useParams()
  if (runId) return <RunView key={runId} runId={runId} />
  if (jobId) return <JobView key={jobId} jobId={jobId} />
  return <JobsIndex />
}
function JobsIndex() {
  const jobs = usePages<Job>('jobs')
  const [search, setSearch] = useState('')
  const items = jobs.data?.pages.flatMap(page => page.items) ?? []
  return <>
    <PageHeader title={msg('Jobs')} actions={<ButtonLink to="/jobs/new" variant="primary">{msg('Create job')}</ButtonLink>} />
    <PageBody width="wide"><div className={styles.stack}>
      <p className="quiet">{msg('Apply a fixed pack release to new inputs. Every run keeps its decision and audit record.')}</p>
      <Problem error={jobs.error} />
      {jobs.isPending && <p role="status">{msg('Loading jobs…')}</p>}
      {items.length > 0 && <Input className={styles.search} aria-label={msg('Search jobs')} placeholder={msg('Search jobs…')} value={search} onChange={e => setSearch(e.target.value)} />}
      {!jobs.isPending && !jobs.error && items.length === 0 && <div className={styles.empty}><h2>{msg('Put a pack to work')}</h2><p className="quiet">{msg('Choose a saved pack, check its release, then create a job. Start runs manually or through the authenticated API.')}</p></div>}
      {items.length > 0 && <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Job')}</th><th>{msg('Trigger')}</th><th>{msg('Created')}</th></tr></thead><tbody>{items.filter(j => j.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(j => <tr key={j.id}><td><Link to={`/jobs/${j.id}`}>{j.name}</Link></td><td className="quiet">{msg('Manual / API')}</td><td className="quiet">{date(j.createdAt)}</td></tr>)}</tbody></table></div>}
      <More {...jobs} />
      <p className={styles.note}>{msg('Local runner · Desk must be running. Closing the browser does not stop accepted runs.')}</p>
    </div></PageBody>
  </>
}
function parseInput(facts: string, supplied: boolean, evidence: string): JobInput {
  let value: unknown
  try { value = JSON.parse(facts) } catch { throw new Error(msg('Facts must be valid JSON.')) }
  if (!supplied) return { facts: value }
  let availability: unknown
  try { availability = JSON.parse(evidence) } catch { throw new Error(msg('Evidence must be valid JSON.')) }
  if (!availability || Array.isArray(availability) || typeof availability !== 'object' || Object.values(availability).some(v => !['present', 'absent', 'unknown'].includes(v as string))) throw new Error(msg('Evidence must map requirement IDs to present, absent or unknown.'))
  return { facts: value, evidence: availability as JobInput['evidence'] }
}
function InputFields({ facts, setFacts, supplied, setSupplied, evidence, setEvidence, disabled }: { facts: string; setFacts: (s: string) => void; supplied: boolean; setSupplied: (v: boolean) => void; evidence: string; setEvidence: (s: string) => void; disabled: boolean }) {
  return <fieldset className={styles.fields} disabled={disabled}>
    <div className={styles.field}><label htmlFor="job-facts">{msg('Facts (JSON)')}</label><TextArea id="job-facts" rows={7} value={facts} onChange={e => setFacts(e.target.value)} spellCheck={false} /><p className={styles.note}>{msg('Use nested values at the paths the pack reads. Omitted values remain unknown.')}</p></div>
    <label className="checkbox"><input type="checkbox" checked={supplied} onChange={e => setSupplied(e.target.checked)} />{msg('Supply evidence availability')}</label>
    {supplied && <div className={styles.field}><label htmlFor="job-evidence">{msg('Evidence availability (JSON)')}</label><TextArea id="job-evidence" rows={4} value={evidence} onChange={e => setEvidence(e.target.value)} spellCheck={false} /><p className={styles.note}>{msg('Declare present, absent or unknown for each requirement. These declarations do not verify a document or connect a source.')}</p></div>}
  </fieldset>
}
export function CreateJobContent() {
  useLocale()
  const [params] = useSearchParams()
  const [packId, setPackId] = useState(params.get('pack') ?? '')
  const packs = usePacks(), pack = usePack(packId)
  const research = useEffectiveConfig().config.research
  const [inputMode, setInputMode] = useState<'manual' | 'mapped' | InputMapping['provider']>('manual'), [source, setSource] = useState<SourceInput | SourceV2>()
  const [name, setName] = useState(''), [facts, setFacts] = useState('{}'), [supplied, setSupplied] = useState(false), [evidence, setEvidence] = useState('{}')
  const [preview, setPreview] = useState<{ signature: string; release: Release; matrix?: string; project: string }>(), [reviewed, setReviewed] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>()
  const navigate = useNavigate(), queryClient = useQueryClient()
  const signature = JSON.stringify([pack.data?.raw, facts, supplied, evidence, inputMode, source])
  const validPreview = preview?.signature === signature ? preview.release : undefined
  useEffect(() => { setReviewed(false) }, [signature])
  const canCreate = validPreview?.tests === 'passed' || validPreview?.tests === 'not-run'
  async function currentInputs() {
    const [freshPack, freshPacks] = await Promise.all([pack.refetch(), packs.refetch()])
    if (freshPack.error) throw freshPack.error
    if (freshPacks.error) throw freshPacks.error
    if (!freshPack.data) throw new Error(msg('The pack could not be loaded.'))
    const selected = freshPacks.data?.packs?.find(p => p.id === packId)
    if (!selected) throw new Error(msg('Choose a saved pack'))
    return { pack: freshPack.data, tests: await readReleaseTests(packId, selected.matrixPath) }
  }
  async function prepare() {
    if (!pack.data) return
    setBusy(true); setError(undefined); setPreview(undefined); setReviewed(false)
    try {
      if (inputMode !== 'manual' && !source) throw Error(msg('Select a file and preview its mapping first.'))
      if (source && !isSourceV2(source) && inputMode !== 'manual') await verifySource(source, research.gateway)
      const input = inputMode === 'manual' ? parseInput(facts, supplied, evidence) : { source }
      const current = await currentInputs()
      const release = await jobsAPI<Release>('previews', { pack: current.pack.raw, input, ...(current.tests.matrix ? { matrix: current.tests.matrix, testSource: current.tests.testSource } : {}) })
      setPreview({ signature: JSON.stringify([current.pack.raw, facts, supplied, evidence, inputMode, source]), release, matrix: current.tests.matrix, project: current.tests.project })
    } catch (e) { setError(e) } finally { setBusy(false) }
  }
  async function create() {
    if (!validPreview || !reviewed || !canCreate || !preview) return
    setBusy(true); setError(undefined)
    try {
      const current = await currentInputs()
      if (current.pack.raw !== validPreview.pack || current.tests.matrix !== preview.matrix || current.tests.project !== preview.project) {
        setPreview(undefined); setReviewed(false)
        throw new Error(msg('The pack or saved tests changed. Check the release again before creating the job.'))
      }
      if (source && !isSourceV2(source) && inputMode !== 'manual') await verifySource(source, research.gateway)
      const job = await jobsAPI<Job>('jobs', { name, releaseId: validPreview.id, reviewed })
      await queryClient.invalidateQueries({ queryKey: ['jobs-pages', 'jobs'] }); navigate(`/jobs/${job.id}`)
    } catch (e) { setError(e) } finally { setBusy(false) }
  }
  return <>
    <PageHeader title={msg('Jobs')} titleHref="/jobs" context={msg('Create job')} />
    <PageBody width="form"><div className={styles.stack}>
      <p className="quiet">{msg('Check saved tests and sample inputs against a fixed pack release. Later pack edits will not change this job.')}</p>
      <Problem error={packs.error || pack.error || error} />
      <div className={styles.field}><label htmlFor="job-name">{msg('Job name')}</label><Input id="job-name" maxLength={160} value={name} disabled={busy} onChange={e => setName(e.target.value)} /></div>
      <div className={styles.field}><label htmlFor="job-pack">{msg('Pack')}</label><Select id="job-pack" value={packId} disabled={busy} placeholder={msg('Choose a saved pack')} options={(packs.data?.packs ?? []).map(p => ({ value: p.id, label: p.id }))} onValueChange={setPackId} />{pack.data && <p className={styles.note}>{pack.data.document.title} · {pack.data.document.version}</p>}</div>
      <section className={styles.stack}><h2>{msg('Sample inputs')}</h2>
        <div className={styles.field}><label htmlFor="job-input-source">{msg('Input source')}</label><Select id="job-input-source" value={inputMode} disabled={busy} options={[{ value: 'manual', label: msg('Manual / API') }, { value: 'mapped', label: msg('Mapped sources') }, { value: 'local-file', label: msg('Local JSON file') }, { value: 'google-drive', label: msg('Google Drive') }]} onValueChange={v => { setInputMode(v as typeof inputMode); setSource(undefined) }} /></div>
        {inputMode === 'manual' ? <InputFields {...{ facts, setFacts, supplied, setSupplied, evidence, setEvidence }} disabled={busy} /> : pack.data && inputMode === 'mapped' ? <MappedInputFields key={`${packId}:mapped`} doc={pack.data.document} disabled={busy} onChange={setSource} /> : pack.data && <SourceInputFields key={`${packId}:${inputMode}`} doc={pack.data.document} provider={inputMode as InputMapping['provider']} disabled={busy} onChange={setSource} />}
        <div><Button onClick={() => { void prepare() }} disabled={busy || !pack.data || inputMode !== 'manual' && !source}>{busy ? msg('Working…') : msg('Check release')}</Button></div>
      </section>
      {validPreview && <section className={styles.review}><h2>{msg('Review this release')}</h2><dl className={styles.properties}><div><dt>{msg('Pack version')}</dt><dd>{validPreview.packVersion}</dd></div><div><dt>{msg('Sample decision')}</dt><dd>{decisionLabel(validPreview.preview)}</dd></div><div><dt>{msg('Structure')}</dt><dd>{msg('Validated')}</dd></div></dl><ReleaseReadiness release={validPreview} />{validPreview.inputMapping?.version === 2 && <MappingReview mapping={validPreview.inputMapping} profiles={validPreview.inputProfiles} warnings={validPreview.mappingWarnings} />}<p className={styles.note}>{msg('The preview is a rehearsal. Operational runs append audit records. No external actions or schedules are enabled.')}</p><JSONView title={msg('Sample result')} value={validPreview.preview} /><label className="checkbox"><input type="checkbox" checked={reviewed} disabled={busy || !canCreate} onChange={e => setReviewed(e.target.checked)} />{msg('I reviewed this release, its test status and sample result.')}</label></section>}
      <div className={styles.actions}><Button variant="primary" disabled={!validPreview || !canCreate || !reviewed || !name.trim() || busy} onClick={() => { void create() }}>{msg('Create job')}</Button><ButtonLink to="/jobs" variant="quiet">{msg('Cancel')}</ButtonLink></div>
    </div></PageBody>
  </>
}
function JobView({ jobId }: { jobId: string }) {
  const query = useQuery({ queryKey: ['job', jobId], queryFn: () => jobsAPI<{ job: Job; release: Release }>(`jobs/${jobId}`) })
  const runs = usePages<Run>(`jobs/${jobId}/runs`)
  const [showInputs, setShowInputs] = useState(false)
  const data = query.data
  useBriefSubject({ id: `job:${jobId}`, path: `/api/operations/jobs/${jobId}/briefs`, title: data?.job.name ?? msg('Job brief') })
  return <>
    <PageHeader title={msg('Jobs')} titleHref="/jobs" context={data?.job.name} actions={data && <Button variant="primary" onClick={() => setShowInputs(!showInputs)}>{showInputs ? msg('Hide inputs') : msg('Run job')}</Button>} />
    <PageBody width="wide"><div className={styles.stack}><Problem error={query.error || runs.error} />
      {query.isPending && <p role="status">{msg('Loading job…')}</p>}
      {data && <><div className={styles.release}><h2>{data.release.title}</h2><p className="quiet">{msg('Fixed version {{version}} · Manual / API', { version: data.release.packVersion })}</p>{data.release.inputMapping && <p className={styles.note}>{data.release.inputMapping.version === 2 ? msg('Mapped sources') : data.release.inputMapping.provider === 'local-file' ? msg('Local JSON file · Fixed input mapping') : msg('Google Drive · Fixed input mapping')}</p>}<ReleaseReadiness release={data.release} />{data.release.inputMapping?.version === 2 && <MappingReview mapping={data.release.inputMapping} profiles={data.release.inputProfiles} warnings={data.release.mappingWarnings} />}<Disclosure title={msg('Release details')}><dl className={styles.properties}><div><dt>{msg('Pack digest')}</dt><dd><code>{data.release.packDigest}</code></dd></div><div><dt>{msg('Runtime digest')}</dt><dd><code>{data.release.runtimeDigest}</code></dd></div></dl><p className={styles.note}>{msg('To use a changed pack, create a new job and review its new release.')}</p></Disclosure></div>
        {showInputs && <RunForm job={data.job} release={data.release} />}
        <section className={styles.stack}><h2>{msg('Run history')}</h2>
          {runs.isPending && <p role="status">{msg('Loading runs…')}</p>}
          {!runs.isPending && !runs.error && runs.data?.pages.every(p => p.items.length === 0) && <p className="quiet">{msg('No runs yet. Run this job with a new input to record its first decision.')}</p>}
          {(runs.data?.pages.some(p => p.items.length > 0)) && <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Run')}</th><th>{msg('Execution')}</th><th>{msg('Decision')}</th><th>{msg('Started')}</th></tr></thead><tbody>{runs.data.pages.flatMap(p => p.items).map(r => <tr key={r.id}><td><Link to={`/jobs/${jobId}/runs/${r.id}`}>{r.id.slice(-8)}</Link></td><td>{stateLabel(r.state)}</td><td>{decisionLabel(r.result)}</td><td className="quiet">{date(r.createdAt)}</td></tr>)}</tbody></table></div>}
          <More {...runs} />
        </section>
        <Disclosure title={msg('Submit through the API')}><p className="quiet">{data.release.inputMapping ? msg('Send a retained source snapshot with the exact release mapping and a unique idempotency key. File inputs use the source contract documented by the runner.') : msg('Send facts and optional evidence availability with your Desk bearer and a unique idempotency key. Retry the same submission with the same key.')}</p><pre className={styles.json}>{`POST /api/operations/jobs/${jobId}/runs\nAuthorization: Bearer <your Desk bearer>\nIdempotency-Key: <unique submission ID>\nContent-Type: application/json\n\n${data.release.inputMapping ? JSON.stringify({ source: { mapping: data.release.inputMapping, ...(data.release.inputMapping.version === 2 ? { case: {}, sources: '<named snapshots and signed responses>' } : { snapshot: '<retained source snapshot>' }) } }, null, 2) : '{"facts": {}}'}`}</pre></Disclosure>
        <p className={styles.note}>{msg('Runs are stored by the local runner, separately from chat backups. Keep Desk running to process the queue.')}</p>
      </>}
    </div></PageBody>
  </>
}
function RunForm({ job, release }: { job: Job; release: Release }) {
  const research = useEffectiveConfig().config.research
  const [source, setSource] = useState<SourceInput | SourceV2>()
  const [facts, setFacts] = useState(JSON.stringify(release.sample.facts, null, 2)), [supplied, setSupplied] = useState(release.sample.evidence !== undefined), [evidence, setEvidence] = useState(JSON.stringify(release.sample.evidence ?? {}, null, 2))
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>()
  const key = useRef<{ payload: string; key: string } | undefined>(undefined)
  const navigate = useNavigate(), queryClient = useQueryClient()
  async function submit() {
    setBusy(true); setError(undefined)
    try {
      if (release.inputMapping && !source) throw Error(msg('Select a file and preview its mapping first.'))
      if (source && !isSourceV2(source)) await verifySource(source, research.gateway)
      const input = release.inputMapping ? { source } : parseInput(facts, supplied, evidence), payload = JSON.stringify(input)
      if (key.current?.payload !== payload) key.current = { payload, key: crypto.randomUUID() }
      const run = await jobsAPI<Run>(`jobs/${job.id}/runs`, input, key.current.key)
      await queryClient.invalidateQueries({ queryKey: ['jobs-pages', `jobs/${job.id}/runs`] }); navigate(`/jobs/${job.id}/runs/${run.id}`)
    } catch (e) { setError(e) } finally { setBusy(false) }
  }
  return <section className={styles.runForm}><h2>{msg('New run')}</h2><p className="quiet">{msg('These inputs start an operational run and will be retained with its audit record.')}</p>{release.inputMapping?.version === 2 ? <MappedInputFields doc={JSON.parse(release.pack) as PackDocument} fixed={release.inputMapping} disabled={busy} onChange={setSource} /> : release.inputMapping ? <SourceInputFields doc={JSON.parse(release.pack) as PackDocument} provider={release.inputMapping.provider} fixed={release.inputMapping} disabled={busy} onChange={setSource} /> : <InputFields {...{ facts, setFacts, supplied, setSupplied, evidence, setEvidence }} disabled={busy} />}<Problem error={error} /><div><Button variant="primary" disabled={busy || Boolean(release.inputMapping && !source)} onClick={() => { void submit() }}>{busy ? msg('Submitting…') : msg('Submit run')}</Button></div></section>
}
function RunView({ runId }: { runId: string }) {
  const query = useQuery({ queryKey: ['job-run', runId], queryFn: () => jobsAPI<Run>(`runs/${runId}`), refetchInterval: q => ['queued', 'running'].includes(q.state.data?.state ?? '') ? 1000 : false })
  const run = query.data
  useBriefSubject({ id: `run:${runId}`, path: `/api/operations/runs/${runId}/briefs`, title: msg('Run brief'), ...(!run || ['queued', 'running'].includes(run.state) ? { unavailable: msg('The run brief is available when execution finishes.') } : {}) })
  return <>
    <PageHeader title={msg('Jobs')} titleHref="/jobs" context={msg('Run {{id}}', { id: runId.slice(-8) })} actions={run && <ButtonLink to={`/jobs/${run.jobId}`} variant="quiet">{msg('Back to job')}</ButtonLink>} />
    <PageBody width="wide"><div className={styles.stack}><Problem error={query.error} />
      {query.isPending && <p role="status">{msg('Loading run…')}</p>}
      {run && <><dl className={styles.properties}><div><dt>{msg('Execution')}</dt><dd role={run.state === 'queued' || run.state === 'running' ? 'status' : undefined}>{stateLabel(run.state)}</dd></div><div><dt>{msg('Decision')}</dt><dd>{decisionLabel(run.result)}</dd></div><div><dt>{msg('Submitted')}</dt><dd>{date(run.createdAt)}</dd></div><div><dt>{msg('Finished')}</dt><dd>{run.finishedAt ? date(run.finishedAt) : '—'}</dd></div></dl>
        {run.problem && <p className={styles.problem}>{run.problem}</p>}
        {run.result && <section className={styles.stack}><h2>{msg('Decision details')}</h2>{run.result.disposition.reasons.length > 0 && <ul>{run.result.disposition.reasons.map(reason => <li key={reason}>{reason}</li>)}</ul>}<p>{run.result.disposition.handoff.state === 'requested' ? msg('Handoff requested: {{target}}', { target: run.result.handoffTarget?.name ?? msg('See full result') }) : msg('No handoff requested')}</p><p className={styles.note}>{msg('This is a recorded decision. No external action or notification was sent.')}</p><JSONView title={msg('Full result')} value={run.result} /></section>}
        {run.input?.source && isSourceV2(run.input.source) ? <><MappingReview mapping={run.input.source.mapping} />{run.input.preparation && <InputLineage preparation={run.input.preparation} />}<JSONView title={msg('Retained inputs')} value={{ facts: run.input.facts, evidence: run.input.evidence }} /><VerificationDownload runId={run.id} /></> : run.input?.source ? <SourceSummary source={run.input.source as SourceInput} /> : <JSONView title={msg('Retained inputs')} value={run.input} />}
        {run.audit && <JSONView title={msg('Runtime audit record')} value={run.audit} />}
        <Disclosure title={msg('Technical details')}><dl className={styles.properties}><div><dt>{msg('Run ID')}</dt><dd><code>{run.id}</code></dd></div><div><dt>{msg('Release ID')}</dt><dd><code>{run.releaseId}</code></dd></div><div><dt>{msg('Revision')}</dt><dd>{run.revision}</dd></div><div><dt>{msg('Attempts')}</dt><dd>{run.attempt}</dd></div></dl></Disclosure>
      </>}
    </div></PageBody>
  </>
}

function VerificationDownload({runId}: {runId: string}) {
 const [error,setError]=useState<unknown>(), [busy,setBusy]=useState(false)
 async function download() {
  setBusy(true);setError(undefined)
  try {
   const response=await deskFetch(`/api/operations/runs/${runId}/verification`)
   if(!response.ok) throw Error(msg('The local runner could not complete this request.'))
   const url=URL.createObjectURL(await response.blob()), a=document.createElement('a')
   a.href=url;a.download=`${runId}-verification.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)
  } catch(e){setError(e)} finally {setBusy(false)}
 }
 return <div><Button disabled={busy} onClick={()=>void download()}>{msg('Download verification record')}</Button><Problem error={error}/></div>
}
