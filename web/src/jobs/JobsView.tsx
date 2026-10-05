import {SourcePreparations} from './SourcePreparations'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { useConfirmDiscard } from '../shell/UnsavedChanges'
import { loadJobDraft, saveJobDraft, useJobDrafts, type JobDraftValues, type MappedDraft, type SavedJobDraft } from './drafts'
import {TriggerChoice,triggerName} from './TriggerForm'
import {TriggersView,TriggerSummary} from './TriggersView'
import type {TriggerConfig} from './triggerTypes'
import { useDetailsSlot } from '../shell/DetailsSlot'
import { useInspectorControls } from '../shell/InspectorSlot'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { MappedInputFields } from './MappedInputFields'
import { MappingReview, InputLineage } from './MappingReview'
import { isSourceV2, type SourceV2 } from './mappingTypes'
import { deskFetch } from '../files/client'
import { SourceInputFields } from './SourceInputFields'
import { SourceSummary } from './SourceSummary'
import { verifySource } from './sourceInputs'
import type { SourceInput } from './client'
import type { PackDocument } from '../mcp/types'
import { useBriefSubject } from '../briefs/context'
import { useEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { msg, systemMessage, useLocale, formatDate } from '../i18n'
import { PageHeader, PageBody } from '../ui/PageLayout'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { InputFields } from './InputFields'
import { parseInput } from './inputModel'
import { Tooltip } from '../ui/Tooltip'
import { Select } from '../ui/Select'
import { Disclosure } from '../ui/Disclosure'
import { usePack, usePacks } from '../mcp/queries'
import { jobsAPI, JobsRequestError, type Decision, type Job, type Page, type Release, type Run } from './client'
import styles from './JobsView.module.css'
import { readReleaseTests } from './releaseTests'
import { ReleaseReadiness } from './ReleaseReadiness'
import { ReleaseStanding } from './ReleaseStanding'
import { ActivityView } from './ActivityView'
import { stamp } from './activity'
import { RunFields, RunTechnicalDetails } from './RunRecord'

function date(value: string) { return formatDate(new Date(value), { dateStyle: 'medium', timeStyle: 'short' }) }
// A run's submission time, or "Not recorded" where Runner stored none or what
// it stored does not read as a time, decided as the Activity tab decides it,
// rather than throwing and taking the table down with it (#223).
function submitted(at: string | undefined) { const stored = stamp('submitted', at); return stored ? date(stored.at) : msg('Not recorded') }
function errorText(error: unknown) { return error instanceof Error ? error.message : msg('The local runner could not complete this request.') }
function Problem({ error }: { error: unknown }) { return error ? <p className={styles.problem} role="alert">{errorText(error)}</p> : null }
function JSONView({ value, title }: { value: unknown; title: string }) { return <Disclosure title={title}><pre className={styles.json}>{JSON.stringify(value, null, 2)}</pre></Disclosure> }
export function decisionLabel(result?: Decision) { if (!result) return '—'; return result.disposition.outcomeId ?? (result.disposition.kind === 'not-applicable' ? msg('Not applicable') : msg('Unresolved')) }
function stateLabel(state: Run['state']) { return { queued: msg('Queued'), running: msg('Running'), completed: msg('Completed'), failed: msg('Failed'), interrupted: msg('Interrupted') }[state] }
function usePages<T>(path: string) { return useInfiniteQuery({ queryKey: ['jobs-pages', path], initialPageParam: 0, queryFn: ({ pageParam }) => jobsAPI<Page<T>>(`${path}${path.includes('?')?'&':'?'}after=${pageParam}`), getNextPageParam: page => page.next || undefined, refetchInterval: 2500 }) }
function More({ hasNextPage, isFetchingNextPage, fetchNextPage }: { hasNextPage: boolean; isFetchingNextPage: boolean; fetchNextPage: () => unknown }) { return hasNextPage ? <Button disabled={isFetchingNextPage} onClick={() => { void fetchNextPage() }}>{msg('Load more')}</Button> : null }

export function JobsContent() {
  useLocale()
  const { jobId, runId } = useParams()
  if (runId) return <RunView key={runId} runId={runId} />
  if (jobId) return <JobView key={jobId} jobId={jobId} />
  return <JobsIndex />
}
function JobsIndex() {
  const allRuns=useLocation().pathname==='/jobs/runs'
  const drafts = useJobDrafts()
  const [search,setSearch]=useState(''),[query,setQuery]=useState(''),[state,setState]=useState('all'),[review,setReview]=useState(false)
  useEffect(()=>{const timer=setTimeout(()=>setQuery(search.trim()),250);return()=>clearTimeout(timer)},[search])
  const filters=new URLSearchParams();if(query)filters.set('q',query);if(allRuns&&state!=='all')filters.set('state',state);if(allRuns&&review)filters.set('review','true')
  const path=`${allRuns?'runs':'jobs'}${filters.size?'?'+filters.toString():''}`
  const list=usePages<Job|Run>(path),items=list.data?.pages.flatMap(p=>p.items)??[]
  const filtered=Boolean(query||(allRuns&&(state!=='all'||review)))
  const visibleDrafts = allRuns ? [] : (drafts.data ?? []).filter(({draft}) => `${draft.values.name} ${draft.values.packId}`.toLocaleLowerCase().includes(query.toLocaleLowerCase()))
  const hasRows = items.length > 0 || visibleDrafts.length > 0
  const showToolbar=hasRows||Boolean(search)||filtered
  return <>
    <PageHeader variant="collection" title={msg('Jobs')} navigation={<nav className={styles.tabs} aria-label={msg('Jobs')}><Link to="/jobs" aria-current={!allRuns?'page':undefined}>{msg('Jobs')}</Link><Link to="/jobs/runs" aria-current={allRuns?'page':undefined}>{msg('Runs')}</Link></nav>} actions={<ButtonLink to="/jobs/new" variant="primary">{msg('Create job')}</ButtonLink>}/>
    <PageBody width="full"><div className={styles.stack}>
      <RunChainDownload/>
      {showToolbar&&<div className={styles.toolbar}><Input className={styles.search} aria-label={allRuns?msg('Search runs'):msg('Search jobs')} placeholder={allRuns?msg('Search runs…'):msg('Search jobs…')} value={search} onChange={e=>setSearch(e.target.value)}/>{allRuns&&<><Select id="jobs-execution-filter" aria-label={msg('Execution')} value={state} options={[{value:'all',label:msg('All executions')},...(['queued','running','completed','failed','interrupted'] as const).map(value=>({value,label:stateLabel(value)}))]} onValueChange={setState}/><label className="checkbox"><input type="checkbox" checked={review} onChange={e=>setReview(e.target.checked)}/>{msg('Needs attention')}</label></>}</div>}
      <Problem error={list.error}/>{list.isPending&&<p role="status">{msg('Loading…')}</p>}
      {!list.isPending&&!list.error&&!hasRows&&(allRuns||!drafts.isPending)&&<section className={styles.empty} role="status"><h2>{filtered?msg('No matches'):allRuns?msg('No runs yet'):msg('No jobs yet')}</h2><p>{filtered?msg('Try another search or clear the filters.'):allRuns?msg('Run a job to see its results and history here.'):msg('Create a job to run a saved pack manually, on a schedule, or when an event arrives.')}</p></section>}
      {hasRows&&(allRuns?<RunTable runs={items as Run[]} showJob/>:<div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Job')}</th><th>{msg('Pack release')}</th><th>{msg('Trigger')}</th><th>{msg('Recent runs')}</th><th><span className="sr-only">{msg('Actions')}</span></th></tr></thead><tbody>{(items as Job[]).map(j=><tr key={j.id}><td><Link to={`/jobs/${j.id}`}>{j.name}</Link></td><td><span>{j.packTitle??'—'}</span>{j.packVersion&&<span className={styles.cellMeta}>{j.packVersion}</span>}</td><td className="quiet">{j.triggers?.length?j.triggers.map(t=><span className={styles.cellMeta} key={t.id}>{triggerName(t.kind)} · {t.paused?msg('Paused'):t.kind==='cloud'?msg('Listening'):t.kind==='schedule'&&!t.nextAt?msg('Finished'):msg('Active')}</span>):msg('Manual / API')}</td><td><div className={styles.recentRuns}>{j.recentRuns?.length?j.recentRuns.map(r=><Tooltip key={r.id} content={`${stateLabel(r.state)} · ${submitted(r.createdAt)}`}><Link className={styles.runDot} data-state={r.state} to={`/jobs/${j.id}/runs/${r.id}`} aria-label={`${stateLabel(r.state)} · ${submitted(r.createdAt)}`}>{r.state==='completed'?'✓':r.state==='failed'||r.state==='interrupted'?'×':'·'}</Link></Tooltip>):<span className="quiet">{msg('No runs yet')}</span>}</div></td><td><ButtonLink to={`/jobs/${j.id}?run=new`} variant="quiet">{msg('Run')}</ButtonLink></td></tr>)}{visibleDrafts.map(({draft}) => <tr key={`draft-${draft.id}`}><td><div className={styles.jobName}><Link to={`/jobs/new?draft=${draft.id}`}>{draft.values.name || msg('Untitled job')}</Link><span className={styles.draftBadge}>{msg('Draft')}</span></div><span className={styles.cellMeta}>{date(draft.updatedAt)}</span></td><td className="quiet">{draft.values.packId || '—'}</td><td className="quiet">{draft.values.trigger ? triggerName(draft.values.trigger.kind) : msg('Manual / API')}<span className={styles.cellMeta}>{msg('Not active')}</span></td><td className="quiet">—</td><td><ButtonLink to={`/jobs/new?draft=${draft.id}`} variant="quiet">{msg('Resume')}</ButtonLink></td></tr>)}</tbody></table></div>)}

      {!allRuns && <Problem error={drafts.error} />}
      <More {...list}/>
    </div></PageBody>
  </>
}
function RunTable({runs,showJob=false}:{runs:Run[];showJob?:boolean}){return <div className={styles.tableWrap}><table className={styles.table}><thead><tr>{showJob&&<th>{msg('Job')}</th>}<th>{msg('Run')}</th><th>{msg('Execution')}</th><th>{msg('Decision')}</th><th>{msg('Submitted')}</th></tr></thead><tbody>{runs.map(r=><tr key={r.id}>{showJob&&<td><Link to={`/jobs/${r.jobId}`}>{r.jobName??r.jobId}</Link></td>}<td><Link to={`/jobs/${r.jobId}/runs/${r.id}`}>{r.id.slice(-8)}</Link></td><td>{stateLabel(r.state)}</td><td>{decisionLabel(r.result)}</td><td className="quiet">{submitted(r.createdAt)}</td></tr>)}</tbody></table></div>}
export function CreateJobContent() {
  const [params] = useSearchParams(), draftId = params.get('draft')
  const loaded = useQuery({ queryKey: ['job-draft', draftId], queryFn: () => loadJobDraft(draftId!), enabled: !!draftId, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false })
  // Seed once from a fresh read. File-watcher refetches must never replace or
  // unmount a working editor, including when a later read fails.
  const hydrated = useRef<{id: string; value: SavedJobDraft} | undefined>(undefined)
  if (!draftId) hydrated.current = undefined
  if (draftId && hydrated.current?.id !== draftId) {
    if (loaded.isPending || loaded.isFetching) return <p role="status">{msg('Loading draft…')}</p>
    if (loaded.error || !loaded.data || loaded.data.draft.status === 'created') return <><Problem error={loaded.error || Error(msg('This draft has already been used to create a job.'))} /><ButtonLink to="/jobs">{msg('Back to jobs')}</ButtonLink></>
    hydrated.current = {id:draftId,value:loaded.data}
  }
  return <CreateJobEditor key={draftId ?? 'new'} initial={hydrated.current?.value} />
}
function CreateJobEditor({initial}: {initial?: SavedJobDraft}) {
  useLocale()
  const [step,setStep]=useState(0),[inputsValid,setInputsValid]=useState(true)
  const details=useDetailsSlot(),inspector=useInspectorControls(),previousInputs=useRef(false)
  const [trigger,setTrigger]=useState<TriggerConfig | undefined>(initial?.draft.values.trigger),[triggerValid,setTriggerValid]=useState(true)
  const [params] = useSearchParams()
  const [packId, setPackId] = useState(initial?.draft.values.packId ?? params.get('pack') ?? '')
  const packs = usePacks(), pack = usePack(packId)
  const research = useEffectiveConfig().config.research
  const [inputMode, setInputMode] = useState<'manual' | 'mapped'>(initial?.draft.values.inputMode ?? 'mapped'), [source, setSource] = useState<SourceInput | SourceV2>()
  const [name, setName] = useState(initial?.draft.values.name ?? ''), [facts, setFacts] = useState(initial?.draft.values.facts ?? '{}'), [supplied, setSupplied] = useState(initial?.draft.values.supplied ?? false), [evidence, setEvidence] = useState(initial?.draft.values.evidence ?? '{}')
  const [preview, setPreview] = useState<{ signature: string; release: Release; matrix?: string; project: string; packId: string }>(), [reviewed, setReviewed] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>()
  const [refused, setRefused] = useState<{ releaseId: string; message: string }>()
  const navigate = useNavigate(), queryClient = useQueryClient()
  const [mapped, setMapped] = useState<MappedDraft | undefined>(initial?.draft.values.mapped)
  const [transientWork,setTransientWork]=useState(false)
  const [unwritten,setUnwritten]=useState(false)
  const [mappedWork, setMappedWork] = useState(false), [pendingConfig, setPendingConfig] = useState(false)
  const saved = useRef(initial), draftId = useRef(initial?.draft.id ?? crypto.randomUUID())
  const values: JobDraftValues = { name, packId, inputMode, facts, supplied, evidence, ...(mapped ? {mapped} : {}), ...(trigger ? {trigger} : {}) }
  const [baseline, setBaseline] = useState(() => JSON.stringify(values)), [notice,setNotice] = useState('')
  const dirty = JSON.stringify(values) !== baseline || pendingConfig || unwritten || transientWork
  const confirmDiscard = useConfirmDiscard()
  async function saveDraft() {
    if (pendingConfig) throw Error(msg('Apply the source configuration before saving this draft.'))
    if (unwritten || !triggerValid) throw Error(msg('Finish the unfinished fields before saving a draft.'))
    setBusy(true); setError(undefined); setNotice('')
    try {
      saved.current = await saveJobDraft(draftId.current, values, saved.current?.file.sha256)
      setBaseline(JSON.stringify(values)); setTransientWork(false); setNotice(msg('Draft saved locally. Reselect files and preview inputs when you resume.'))
      void queryClient.invalidateQueries({queryKey:['job-drafts']})
    } finally { setBusy(false) }
  }
  const clearGuard = useDirtyGuard(dirty, msg('Leave without saving this job?'), { name, busy, saveDraft: pendingConfig || unwritten || !triggerValid ? undefined : saveDraft, shouldBlock: ({currentLocation,nextLocation}) => currentLocation.pathname !== nextLocation.pathname || new URLSearchParams(currentLocation.search).get('draft') !== new URLSearchParams(nextLocation.search).get('draft') })
  async function changePack(next: string) {
    if (next === packId) return
    if ((mappedWork || mapped || trigger || unwritten) && !await confirmDiscard(msg('Changing the pack discards its input mapping and trigger configuration.'), {name})) return
    setTransientWork(false); setMapped(undefined); setMappedWork(false); setPendingConfig(false); setSource(undefined); setTrigger(undefined); setPackId(next)
  }
  const signature = JSON.stringify([pack.data?.raw, facts, supplied, evidence, inputMode, source])
  const validPreview = preview?.signature === signature ? preview.release : undefined
  useEffect(() => { setReviewed(false) }, [signature,trigger])
  const mappingIdentity = JSON.stringify(source?.mapping)
  const previousMapping = useRef(mappingIdentity)
  useEffect(()=>{if(previousMapping.current && previousMapping.current !== mappingIdentity){setTrigger(undefined);setTriggerValid(true)}previousMapping.current=mappingIdentity},[mappingIdentity])
  const showingInputs=step===1&&inputMode==='mapped'
  useEffect(()=>{if(previousInputs.current&&!showingInputs&&details.open)inspector.close?.();previousInputs.current=showingInputs},[showingInputs,details.open,inspector.close])
  // The runner's refusal replaces the job it did not create, for this release only.
  const refusal = refused && refused.releaseId === validPreview?.id ? refused.message : undefined
  const canCreate = (validPreview?.tests === 'passed' || validPreview?.tests === 'not-run') && !refusal
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
    setBusy(true); setError(undefined); setPreview(undefined); setReviewed(false); setRefused(undefined)
    try {
      if (inputMode !== 'manual' && !source) throw Error(msg('Configure inputs and preview them first.'))
      if (source && !isSourceV2(source) && inputMode !== 'manual') await verifySource(source, research.gateway)
      const input = inputMode === 'manual' ? parseInput(facts, supplied, evidence) : { source }
      const current = await currentInputs()
      const release = await jobsAPI<Release>('previews', { pack: current.pack.raw, input, ...(current.tests.matrix ? { matrix: current.tests.matrix, testSource: current.tests.testSource } : {}) })
      // The decision id the release's bytes were served for: the project's lock is keyed by it.
      setPreview({ signature: JSON.stringify([current.pack.raw, facts, supplied, evidence, inputMode, source]), release, matrix: current.tests.matrix, project: current.tests.project, packId })
    } catch (e) { setError(e) } finally { setBusy(false) }
  }
  async function create() {
    if (!validPreview || !reviewed || !canCreate || !preview || !triggerValid) return
    setBusy(true); setError(undefined)
    try {
      const current = await currentInputs()
      if (current.pack.raw !== validPreview.pack || current.tests.matrix !== preview.matrix || current.tests.project !== preview.project) {
        setPreview(undefined); setReviewed(false)
        throw new Error(msg('The pack or saved tests changed. Check the release again before creating the job.'))
      }
      if (source && !isSourceV2(source) && inputMode !== 'manual') await verifySource(source, research.gateway)
      const job = await jobsAPI<Job>('jobs', { name, releaseId: validPreview.id, reviewed, ...(trigger?{trigger}:{}) })
      if (saved.current) { try { saved.current = await saveJobDraft(draftId.current, values, saved.current.file.sha256, 'created'); void queryClient.invalidateQueries({queryKey:['job-drafts']}) } catch { /* The created job is durable; preserve the draft if cleanup failed. */ } }
      clearGuard(); await queryClient.invalidateQueries({ queryKey: ['jobs-pages', 'jobs'] }); navigate(`/jobs/${job.id}${job.initialTriggerId?'?tab=triggers':''}`)
    } catch (e) {
      if (e instanceof JobsRequestError && e.code === 'release_untested') { setRefused({ releaseId: validPreview.id, message: e.message }); setReviewed(false) }
      else setError(e)
    } finally { setBusy(false) }
  }
  return <>
    <PageHeader title={msg('Jobs')} titleHref="/jobs" context={msg('Create job')} />
    <PageBody fill width="full"><div className={styles.wizard}><div className={styles.wizardContent}>
      <nav aria-label={msg('Create job')}><ol className={styles.steps}>{[msg('Job'),msg('Inputs'),msg('Trigger'),msg('Review')].map((label,index)=><li key={label} aria-current={step===index?'step':undefined}><span>{index+1}</span>{label}</li>)}</ol></nav>
      {step===0&&<p className="quiet">{msg('Choose the pack this job will apply. Later pack edits will not change its release.')}</p>}
      <Problem error={packs.error || pack.error || error} />
      {initial && <p className={styles.note}>{msg('Resumed draft. Reselect files, preview inputs and check the release before creating this job.')}</p>}
      {notice && <p role="status" className={styles.note}>{notice}</p>}
      {pendingConfig && <p className={styles.note}>{msg('Apply the source configuration before saving this draft.')}</p>}
      <section hidden={step!==0} className={styles.stack}><div className={styles.field}><label htmlFor="job-name">{msg('Job name')}</label><Input id="job-name" maxLength={160} value={name} disabled={busy} onChange={e => setName(e.target.value)} /></div>
      <div className={styles.field}><label htmlFor="job-pack">{msg('Pack')}</label><Select id="job-pack" value={packId} disabled={busy} placeholder={msg('Choose a saved pack')} options={(packs.data?.packs ?? []).map(p => ({ value: p.id, label: p.id }))} onValueChange={next=>void changePack(next)} />{pack.data && <p className={styles.note}>{pack.data.document.title} · {pack.data.document.version}</p>}</div>
      </section><section hidden={step!==1} className={styles.stack}><div className={styles.inputHeading}><div><h2>{msg('Where inputs come from')}</h2></div><div className={styles.inputMode}><Select id="job-input-source" aria-label={msg('Input configuration')} value={inputMode} disabled={busy} options={[{value:'mapped',label:msg('Integrations and case inputs')},{value:'manual',label:msg('Manual / API')}]} onValueChange={async v=>{if(trigger&&!await confirmDiscard(msg('Discard unsaved trigger changes?'),{name:trigger.name}))return;setInputMode(v as typeof inputMode);setSource(undefined);setTrigger(undefined)}}/></div></div><p className={styles.note}>{msg('Assign facts and evidence to sources, then preview one sample before creating the job.')}</p>
        <div hidden={inputMode!=='manual'}><InputFields doc={pack.data?.document} onValid={setInputsValid} onUnwritten={setUnwritten} {...{ facts, setFacts, supplied, setSupplied, evidence, setEvidence }} disabled={busy} /></div>
        <div hidden={inputMode!=='mapped'}>{pack.data&&<MappedInputFields active={step===1&&inputMode==='mapped'} key={`${packId}:mapped`} doc={pack.data.document} disabled={busy} onChange={setSource} draft={mapped} onDraftChange={setMapped} onWorkChange={setMappedWork} onPendingChange={setPendingConfig} onTransientChange={()=>setTransientWork(true)}/>}</div>

      </section>
      <section hidden={step!==2} className={styles.stack}>{pack.data&&<TriggerChoice doc={pack.data.document} mapping={inputMode==='manual'?undefined:source?.mapping} value={trigger} onChange={setTrigger} onValid={setTriggerValid} disabled={busy}/>}</section>
      <section hidden={step!==3} className={styles.stack}>
        <h2>{msg('Review job')}</h2>
        <dl className={`${styles.properties} ${styles.reviewSummary}`}>
          <div><dt>{msg('Job')}</dt><dd>{name}</dd></div>
          <div><dt>{msg('Pack')}</dt><dd>{pack.data?.document.title} · {pack.data?.document.version}</dd></div>
          <div><dt>{msg('Inputs')}</dt><dd>{inputMode==='manual'?msg('Manual / API'):source&&isSourceV2(source)?msg('{{count}} configured sources and case inputs',{count:source.mapping.sources?.length??0}):msg('Select and preview sources')}</dd></div>
          <div><dt>{msg('Trigger')}</dt><dd>{trigger?triggerName(trigger.kind):msg('Manual / API')}{trigger&&` · ${msg('Created paused')}`}</dd></div>
          <div><dt>{msg('Execution')}</dt><dd>{msg('Local runner · artifacts stored locally')}</dd></div>
        </dl>
        {!validPreview&&<p className={styles.callout}>{msg('Structure, saved tests and the sample decision have not been checked for these inputs.')}</p>}
        <div><Button onClick={() => { void prepare() }} disabled={busy || inputMode === 'manual' && !inputsValid || !pack.data || inputMode !== 'manual' && !source}>{busy ? msg('Working…') : msg('Check release')}</Button></div>

      {validPreview && <section className={styles.review}><h2>{msg('Review this release')}</h2>{trigger&&<><TriggerSummary config={trigger}/><p className={styles.note}>{msg('The trigger is created paused. Enable it from the Triggers tab after reviewing its timing and inputs.')}</p></>}<dl className={styles.properties}><div><dt>{msg('Pack version')}</dt><dd>{validPreview.packVersion}</dd></div><div><dt>{msg('Sample decision')}</dt><dd>{decisionLabel(validPreview.preview)}</dd></div><div><dt>{msg('Structure')}</dt><dd>{msg('Validated')}</dd></div></dl><ReleaseReadiness release={validPreview} refusal={refusal} />{preview && <ReleaseStanding release={validPreview} packId={preview.packId} />}{validPreview.inputMapping?.version === 2 && <MappingReview mapping={validPreview.inputMapping} profiles={validPreview.inputProfiles} warnings={validPreview.mappingWarnings} />}<p className={styles.note}>{msg('The preview is a rehearsal. Operational runs append audit records. No external actions or schedules are enabled.')}</p><JSONView title={msg('Sample result')} value={validPreview.preview} /><label className="checkbox"><input type="checkbox" checked={reviewed} disabled={busy || !canCreate} onChange={e => setReviewed(e.target.checked)} />{msg('I reviewed this release, its test status and sample result.')}</label></section>}
      </section>
      </div><div className={styles.wizardActions}><ButtonLink to="/jobs" variant="quiet">{msg('Cancel')}</ButtonLink><div className={styles.actions}><Button disabled={busy || pendingConfig || unwritten || !triggerValid || !dirty} onClick={()=>void saveDraft().catch(setError)}>{msg('Save draft')}</Button>{step>0&&<Button disabled={busy} onClick={()=>setStep(step-1)}>{msg('Back')}</Button>}{step<3?<Button variant="primary" disabled={busy||step===0&&(!name.trim()||!pack.data)||step===1&&(inputMode==='manual'?!inputsValid:!source)||step===2&&!triggerValid} onClick={()=>setStep(step+1)}>{msg('Continue')}</Button>:<Button variant="primary" disabled={!validPreview||!canCreate||!reviewed||!triggerValid||!name.trim()||busy} onClick={()=>void create()}>{trigger?msg('Create paused job'):msg('Create job')}</Button>}</div></div>
    </div></PageBody>
  </>
}
function JobView({ jobId }: { jobId: string }) {
  const [hasPreparations,setHasPreparations]=useState(false)
  const query = useQuery({ queryKey: ['job', jobId], queryFn: () => jobsAPI<{ job: Job; release: Release }>(`jobs/${jobId}`) })
  const runs = usePages<Run>(`jobs/${jobId}/runs`)
  const [params,setParams]=useSearchParams()
  const showInputs=params.get('run')==='new',tab=params.get('tab')??'runs'
  function setShowInputs(show:boolean){setParams(show?{run:'new'}:{})}
  const data = query.data
  useBriefSubject({ id: `job:${jobId}`, path: `/api/operations/jobs/${jobId}/briefs`, title: data?.job.name ?? msg('Job brief') })
  return <>
    <PageHeader title={msg('Jobs')} titleHref="/jobs" context={data?.job.name} navigation={data&&!showInputs&&<nav className={styles.tabs} aria-label={msg('Job')}><button aria-current={tab==='runs'?'page':undefined} onClick={()=>setParams({})}>{msg('Runs')}</button><button aria-current={tab==='triggers'?'page':undefined} onClick={()=>setParams({tab:'triggers'})}>{msg('Triggers')}</button><button aria-current={tab==='release'?'page':undefined} onClick={()=>setParams({tab:'release'})}>{msg('Release')}</button><button aria-current={tab==='activity'?'page':undefined} onClick={()=>setParams({tab:'activity'})}>{msg('Activity')}</button></nav>} actions={data && <Button variant="primary" onClick={() => setShowInputs(!showInputs)}>{showInputs ? msg('Hide inputs') : msg('Run job')}</Button>} />
    <PageBody width="wide"><div className={styles.stack}><Problem error={query.error || runs.error} />
      {query.isPending && <p role="status">{msg('Loading job…')}</p>}
      {data && <><div hidden={showInputs||tab!=='release'} className={styles.release}><h2>{data.release.title}</h2><p className="quiet">{msg('Fixed version {{version}}', { version: data.release.packVersion })}</p>{data.release.inputMapping && <p className={styles.note}>{data.release.inputMapping.version === 2 ? msg('Mapped sources') : data.release.inputMapping.provider === 'local-file' ? msg('Local JSON file · Fixed input mapping') : msg('Google Drive · Fixed input mapping')}</p>}<ReleaseReadiness release={data.release} job />{data.release.inputMapping?.version === 2 && <MappingReview mapping={data.release.inputMapping} profiles={data.release.inputProfiles} warnings={data.release.mappingWarnings} />}<Disclosure title={msg('Release details')}><dl className={styles.properties}><div><dt>{msg('Pack digest')}</dt><dd><code>{data.release.packDigest}</code></dd></div><div><dt>{msg('Runtime digest')}</dt><dd><code>{data.release.runtimeDigest}</code></dd></div></dl><p className={styles.note}>{msg('To use a changed pack, create a new job and review its new release.')}</p></Disclosure></div>
        {!showInputs&&tab==='triggers'&&<TriggersView jobId={jobId} release={data.release}/>}
        {!showInputs&&tab==='activity'&&<ActivityView jobId={jobId} release={data.release}/>}
        {showInputs && <RunForm job={data.job} release={data.release} />}
        <section hidden={showInputs||tab!=='runs'} className={styles.stack}>{!showInputs&&tab==='runs'&&<SourcePreparations jobId={jobId} onHasItems={setHasPreparations}/>}<h2>{msg('Run history')}</h2>
          {runs.isPending && <p role="status">{msg('Loading runs…')}</p>}
          {!hasPreparations && !runs.isPending && !runs.error && runs.data?.pages.every(p => p.items.length === 0) && <p className="quiet">{msg('No runs yet. Run this job with a new input to record its first decision.')}</p>}
          {runs.data?.pages.some(p=>p.items.length>0)&&<RunTable runs={runs.data.pages.flatMap(p=>p.items)}/>}
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
  const [facts,setFacts]=useState('{}'),[supplied,setSupplied]=useState(false),[evidence,setEvidence]=useState('{}'),[inputsValid,setInputsValid]=useState(true)
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>()
  const [sourceWork,setSourceWork]=useState(false),[unwritten,setUnwritten]=useState(false)
  const clearGuard = useDirtyGuard(facts !== '{}' || supplied || evidence !== '{}' || sourceWork || unwritten, msg('Discard these run inputs?'), {name:job.name,busy,shouldBlock:({currentLocation,nextLocation})=>currentLocation.pathname!==nextLocation.pathname || new URLSearchParams(nextLocation.search).get('run')!=='new'})
  const key = useRef<{ payload: string; key: string } | undefined>(undefined)
  const navigate = useNavigate(), queryClient = useQueryClient()
  async function submit() {
    setBusy(true); setError(undefined)
    try {
      if (release.inputMapping && !source) throw Error(msg('Configure inputs and preview them first.'))
      if (source && !isSourceV2(source)) await verifySource(source, research.gateway)
      const input = release.inputMapping ? { source } : parseInput(facts, supplied, evidence), payload = JSON.stringify(input)
      if (key.current?.payload !== payload) key.current = { payload, key: crypto.randomUUID() }
      const run = await jobsAPI<Run>(`jobs/${job.id}/runs`, input, key.current.key)
      clearGuard(); await queryClient.invalidateQueries({ queryKey: ['jobs-pages', `jobs/${job.id}/runs`] }); navigate(`/jobs/${job.id}/runs/${run.id}`)
    } catch (e) { setError(e) } finally { setBusy(false) }
  }
  return <section className={styles.runForm}><h2>{msg('New run')}</h2><p className="quiet">{msg('These inputs start an operational run and will be retained with its audit record.')}</p>{release.inputMapping?.version === 2 ? <MappedInputFields doc={JSON.parse(release.pack) as PackDocument} fixed={release.inputMapping} disabled={busy} onChange={setSource} onWorkChange={setSourceWork} /> : release.inputMapping ? <SourceInputFields doc={JSON.parse(release.pack) as PackDocument} provider={release.inputMapping.provider} fixed={release.inputMapping} disabled={busy} onChange={setSource} onWorkChange={setSourceWork} /> : <InputFields doc={JSON.parse(release.pack) as PackDocument} onValid={setInputsValid} onUnwritten={setUnwritten} {...{ facts, setFacts, supplied, setSupplied, evidence, setEvidence }} disabled={busy} />}<Problem error={error} /><div><Button variant="primary" disabled={busy || !inputsValid || Boolean(release.inputMapping && !source)} onClick={() => { void submit() }}>{busy ? msg('Submitting…') : msg('Submit run')}</Button></div></section>
}
function RunView({ runId }: { runId: string }) {
  const query = useQuery({ queryKey: ['job-run', runId], queryFn: () => jobsAPI<Run>(`runs/${runId}`), refetchInterval: q => ['queued', 'running'].includes(q.state.data?.state ?? '') ? 1000 : false })
  const run = query.data
  useBriefSubject({ id: `run:${runId}`, path: `/api/operations/runs/${runId}/briefs`, title: msg('Run brief'), ...(!run || ['queued', 'running'].includes(run.state) ? { unavailable: msg('The run brief is available when execution finishes.') } : {}) })
  return <>
    <PageHeader title={msg('Jobs')} titleHref="/jobs" context={msg('Run {{id}}', { id: runId.slice(-8) })} actions={run && <ButtonLink to={`/jobs/${run.jobId}`} variant="quiet">{msg('Back to job')}</ButtonLink>} />
    <PageBody width="wide"><div className={styles.stack}><Problem error={query.error} />
      {query.isPending && <p role="status">{msg('Loading run…')}</p>}
      {run && <><RunFields run={run} />
        {run.trigger&&<><div className={styles.actions}><span className="quiet">{msg('Trigger')}</span><Link to={`/jobs/${run.jobId}?tab=triggers`}>{triggerName(run.trigger.kind)}</Link>{run.trigger.scheduledAt&&<span className="quiet">{date(run.trigger.scheduledAt)}</span>}</div><JSONView title={msg('Trigger record')} value={run.trigger}/></>}
        {run.problem && <p className={styles.problem}>{run.problem}</p>}
        {run.result && <section className={styles.stack}><h2>{msg('Decision details')}</h2>{run.result.disposition.reasons.length > 0 && <ul>{run.result.disposition.reasons.map(reason => <li key={reason}>{reason}</li>)}</ul>}<p>{run.result.disposition.handoff.state === 'requested' ? msg('Handoff requested: {{target}}', { target: run.result.handoffTarget?.name ?? msg('See full result') }) : msg('No handoff requested')}</p><p className={styles.note}>{msg('This is a recorded decision. No external action or notification was sent.')}</p><JSONView title={msg('Full result')} value={run.result} /></section>}
        {run.input?.source && isSourceV2(run.input.source) ? <><MappingReview mapping={run.input.source.mapping} />{run.input.preparation && <InputLineage preparation={run.input.preparation} />}<JSONView title={msg('Retained inputs')} value={{ facts: run.input.facts, evidence: run.input.evidence }} /><VerificationDownload runId={run.id} /></> : run.input?.source ? <SourceSummary source={run.input.source as SourceInput} /> : <JSONView title={msg('Retained inputs')} value={run.input} />}
        {run.audit && <JSONView title={msg('Runtime audit record')} value={run.audit} />}
        <RunTechnicalDetails run={run} />
      </>}
    </div></PageBody>
  </>
}

type ExportVersion = 2 | 3 | 4 | 5
/** A verification export's version, read from the export itself, and for
 version 4 or 5 the sequence of the run's entry in the runner's chain of runs.
 Desk asks for version 5, and Runner answers an earlier one where the run lacks
 what a later one carries: version 4 for a run whose record is unsigned, 3 for
 a run recorded before Runner chained its runs, 2 for one that holds no exact
 bytes of its record. What was asked for does not say what was saved. */
function readExport(text: string): {version: ExportVersion, sequence?: number} | undefined {
 try {
  const value=JSON.parse(text) as {version?: unknown, chain?: {entry?: unknown}} | null, version=value?.version
  if(version===2||version===3) return {version}
  if(version!==4&&version!==5) return undefined
  // A version that carries a chain entry is named only with the entry's
  // sequence: without one, the clause naming the entry would not be true.
  const sequence=entrySequence(value?.chain?.entry)
  return sequence===undefined ? undefined : {version, sequence}
 } catch { return undefined }
}

/** The sequence the run's chain entry names. The entry is its line's exact
 bytes in base64, one JSON object. It is read, not checked: nothing here holds
 it to the record, to the checkpoint beside it, or to the chain. */
function entrySequence(entry: unknown): number | undefined {
 if(typeof entry!=='string') return undefined
 const line=JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(Uint8Array.from(atob(entry),c=>c.charCodeAt(0)))) as {sequence?: unknown} | null
 const sequence=line?.sequence
 return typeof sequence==='number'&&Number.isSafeInteger(sequence)&&sequence>0 ? sequence : undefined
}

/** What a saved export carries, one clause for each version. */
function exportSentence(file: string, version: ExportVersion) {
 switch(version) {
  case 2: return msg('Saved {{file}}: export version 2, which carries no exact bytes of the audit record to compare with a gateway receipt.', {file})
  case 3: return msg('Saved {{file}}: export version 3, with the audit record’s exact bytes. Their digest, which verify-run reports as recordDigest, can be compared with a gateway receipt’s decision.recordDigest; verify-run does not make that comparison.', {file})
  case 4: return msg('Saved {{file}}: export version 4, with the audit record’s exact bytes and the run’s chain entry and checkpoint; unsigned. The bytes’ digest, which verify-run reports as recordDigest, can be compared with a gateway receipt’s decision.recordDigest; verify-run does not make that comparison.', {file})
  case 5: return msg('Saved {{file}}: export version 5, with the audit record’s exact bytes, the run’s chain entry and checkpoint, and the record’s signatures, not checked here. The bytes’ digest, which verify-run reports as recordDigest, can be compared with a gateway receipt’s decision.recordDigest; verify-run does not make that comparison.', {file})
 }
}

export function VerificationDownload({runId}: {runId: string}) {
 const [error,setError]=useState<unknown>(), [busy,setBusy]=useState(false), [saved,setSaved]=useState<{file: string, version: ExportVersion, sequence?: number}>()
 async function download() {
  setBusy(true);setError(undefined);setSaved(undefined)
  try {
   const response=await deskFetch(`/api/operations/runs/${runId}/verification?version=5`)
   if(!response.ok) throw Error(msg('The local runner could not complete this request.'))
   // Saved as Runner sent it, byte for byte: nothing here encodes the export again.
   const blob=await response.blob(), standing=readExport(await blob.text())
   if(!standing) throw Error(msg('The local runner could not complete this request.'))
   const file=`${runId}-verification-v${standing.version}.json`, url=URL.createObjectURL(blob), a=document.createElement('a')
   a.href=url;a.download=file;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)
   setSaved({file,...standing})
  } catch(e){setError(e)} finally {setBusy(false)}
 }
 return <div className={styles.field}><div><Button disabled={busy} onClick={()=>void download()}>{msg('Download verification record')}</Button></div>
  {saved && <div role="status" className={`${styles.field} ${styles.note}`}><p>{exportSentence(saved.file, saved.version)}</p>
   <p>{saved.sequence===undefined ? msg('Runner’s chain of runs: no chain entry in this export.') : msg('Runner’s chain of runs: chain entry {{sequence}}, not checked.', {sequence: String(saved.sequence)})}</p></div>}
  <Problem error={error}/></div>
}

const RUN_CHAIN_FILE='run-chain.jsonl'
/** Why Desk or the runner refused, in their words where they gave any. */
async function refusalText(response: Response) {
 try {
  const said=(await response.json() as {error?: unknown} | null)?.error
  const text=typeof said==='string' ? said : (said as {message?: unknown} | null | undefined)?.message
  if(typeof text==='string'&&text) return systemMessage(text)
 } catch { /* Not a refusal Desk or the runner wrote. */ }
 return msg('The local runner could not complete this request.')
}

/** The runner's whole chain of runs, saved as the runner sent it: Desk passes
 it on untouched, or fails it whole, and nothing here reads or checks it. */
export function RunChainDownload() {
 const [error,setError]=useState<unknown>(), [busy,setBusy]=useState(false), [chainSaved,setChainSaved]=useState(false)
 async function download() {
  setBusy(true);setError(undefined);setChainSaved(false)
  try {
   const response=await deskFetch('/api/operations/run-chain')
   if(!response.ok) throw Error(await refusalText(response))
   // The whole answer or nothing: a transfer that ends early rejects here,
   // before anything is saved.
   const chain=await response.blob(), href=URL.createObjectURL(chain), link=document.createElement('a')
   link.href=href;link.download=RUN_CHAIN_FILE;link.click();setTimeout(()=>URL.revokeObjectURL(href),1000)
   setChainSaved(true)
  } catch(e){setError(e)} finally {setBusy(false)}
 }
 return <div className={styles.field}><div className={styles.actions}><Button variant="quiet" disabled={busy} onClick={()=>void download()}>{msg('Download the runner’s chain of runs')}</Button>
  <span className={styles.note}>{msg('The file is the runner’s whole chain of runs, byte for byte as the runner sent it, unverified here.')}</span></div>
  {chainSaved && <p role="status" className={styles.note}>{msg('Saved {{file}}.', {file: RUN_CHAIN_FILE})}</p>}
  <Problem error={error}/></div>
}
