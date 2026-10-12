/**
 * Admin → Workspace → Decision safeguards → Jobs record (ADR-0010, section 4, "A Jobs record panel",
 * and section 5; issue #216).
 *
 * Beside the decision record, where this desk has a Runner: what the
 * runtime's own `jpack audit verify --trail` finds in Desk's private copy of
 * Runner's chain of runs, taken fresh for each check, with the checkpoints of
 * the chain Desk handed over to holders and nothing else held. It shows the
 * status, the coverage, each finding by name and the runtime's sentences of
 * what the result establishes and what it does not, verbatim and in English,
 * through the decision record's own `Report`; the ADR's sentence on whose
 * copy it is; and Runner's key, in the words Help & About → Gates uses,
 * with the panel's sentence on what is signed: the chain of runs carries no
 * signature sidecar of its own, and Runner's key signs each run's record, in
 * that run's own attempt. Desk passes no public key here.
 *
 * **Each run's signing** (issue #345), read on request only, a page of
 * Runner's completed runs at a time: each run's `signing` member as Runner
 * records it (Runner v0.8.0), and how it holds against the list of public
 * keys Desk keeps for this Runner. A run signed under a key the list does not
 * name is named in Desk's one sentence of its own; a failed signing, in
 * Runner's words; a run Runner recorded no signing for is never shown as
 * signed; and where the list could not be read now, no run is held to it.
 * Each run's export is checked by `jpack-runner verify-run --public-key <the
 * list's key in force> --require-signed` on request only, and what it said
 * is shown as it said it, with the command Desk ran.
 *
 * It runs on request only: when the owner checks, and again after a
 * hand-over of the chain is confirmed (`checkJobsRecordAgain`). Never when it
 * opens, on a timer, on focus, on a reconnect or on a change to the project.
 * Whether this desk has a Runner is read once each time the panel becomes
 * visible, from `GET /api/runner-key`, and not asked again on an interval.
 *
 * "Download the chain" saves Runner's chain of runs as the Runs page does:
 * `run-chain.jsonl`, the bytes Desk passed on, kept as a Blob from the
 * response to the saved file.
 */
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ON_REQUEST_ONLY } from '../mcp/projectChange'
import { msg, systemMessage, useLocale } from '../i18n'
import { loadRunnerKey, type RunnerKey } from '../jobs/runnerKey'
import { RunnerSignatures } from '../routes/GatesHelp'
import { Button } from '../ui/Button'
import { SettingsSection } from '../ui/SettingsSection'
import { CodeBlock } from '../ui/CodeBlock'
import { downloadRunChain, JOBS_RECORD_KEY, readJobsRecord, readJobsSigning, verifyJobsRun, type JobsRecord as JobsAnswer, type JobsSigning, type RunnerKeyList, type RunSigning, type RunVerification } from './client'
import { Report } from './DecisionRecord'
import styles from './DecisionRecord.module.css'

/** The query that reads, once each time the panel becomes visible, whether this desk has a Runner. */
export const JOBS_RUNNER_KEY = ['desk-jobs-record-runner'] as const
/** The name the chain is saved under, as the Runs page saves it. */
const RUN_CHAIN_FILE = 'run-chain.jsonl'

/**
 * The panel's sentence on what is signed (ADR-0010, section 4, as amended
 * 2026-10-06, and row 12): the chain has no signature sidecar of its own;
 * each run's record is what Runner's key signs.
 */
const SIGNED_STATEMENT = "The chain of runs carries no signature sidecar of its own. Runner's key signs each run's record, in that run's own attempt, and the signature travels with that run's export."

/** Desk's one sentence of its own on a run's signing: signed, under a key the list it keeps does not name. */
const UNLISTED = 'Signed under a key the list does not name.'

/** The command Desk runs on a run's export, with what each path and the digest are; the key is the list's in force. */
const verifyCommand = (publicKey: string) =>
  `jpack-runner verify-run --file <Desk's copy of the run's export> --profiles <this desk's input profiles> --release-digest <the export's own releaseDigest> --public-key ${publicKey} --require-signed`

/** The ADR's sentence (section 4, "A Jobs record panel"), without "the keys and": Desk passes no key here. */
const STATEMENT = "Desk ran this over its own copy of the runner's chain of runs, with the checkpoints it keeps. It shows what a holder would see. It is not evidence to anyone who does not trust this installation."

/** The panel's one check: disabled, so that only the owner's request, or a confirmed hand-over of the chain, runs it. */
export function useJobsRecord() {
  return useQuery({
    queryKey: JOBS_RECORD_KEY,
    queryFn: ({ signal }) => readJobsRecord(signal),
    enabled: false,
    meta: ON_REQUEST_ONLY,
    retry: false,
    staleTime: Infinity,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: false
  })
}

/** Whether this desk has a Runner: `GET /api/runner-key`, once each time the panel becomes visible. */
function useRunner(visible: boolean) {
  const query = useQuery({
    queryKey: JOBS_RUNNER_KEY,
    queryFn: ({ signal }) => loadRunnerKey(signal),
    enabled: false,
    meta: ON_REQUEST_ONLY,
    retry: false,
    staleTime: Infinity,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: false
  })
  const refetch = useRef(query.refetch)
  refetch.current = query.refetch
  useEffect(() => { if (visible) void refetch.current({ cancelRefetch: false }) }, [visible])
  return query
}

export function JobsRecord({ visible = true }: { visible?: boolean }) {
  useLocale()
  const runner = useRunner(visible)
  const query = useJobsRecord()
  // Shown where this desk has a Runner, or where Desk could not say whether
  // it has one: the check then says.
  if (runner.data === null || runner.isPending) return null
  const record = query.data
  return <SettingsSection title={msg('Jobs record')} description={msg('Runner’s chain of runs: one entry for each completed Jobs run. Desk checks a copy of it when you ask.')} variant="plain">
    <div className={styles.card} data-testid="jobs-record">
      <div className={styles.actions}><Button onClick={() => void query.refetch()} disabled={query.isFetching}>{msg('Check the chain of runs')}</Button></div>
      {query.isFetching ? <p role="status" className={styles.quiet}>{msg('Asking the runtime…')}</p>
        : query.error ? <p role="alert">{systemMessage(query.error.message)}</p>
          : record && <Answer record={record} />}
      <ChainDownload />
      <RunSignings />
    </div>
  </SettingsSection>
}

/** What the check answered, in each of its states. */
function Answer({ record }: { record: JobsAnswer }) {
  switch (record.state) {
    case 'no-runner': return <p>{msg('This desk has no Runner, so it has no chain of runs to check.')}</p>
    case 'not-running': return <p>{msg('Runner is not running on this desk now, so Desk could not read its chain of runs.')}</p>
    case 'older-runtime': return <p>{msg('This runtime (jpack {{version}}) writes an unchained trail and has no audit commands. Chaining, checkpoints, signing and stamping need jpack {{floor}} or later.', { version: record.runtime ?? '?', floor: record.floor })}</p>
    case 'unverified': return <>
      <p>{msg('The runtime did not check the chain of runs.')}</p>
      <ul className={styles.list} aria-label={msg('What the runtime said')}>{record.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>
      {record.handoverProblem && <p role="alert">{systemMessage(record.handoverProblem)}</p>}
      <Signatures runnerKey={record.runnerKey} />
      {record.runtime && <p className={styles.quiet}>{msg('Checked by jpack {{version}}.', { version: record.runtime })}</p>}
    </>
    case 'report': return <>
      <p className={styles.statement}>{msg(STATEMENT)}</p>
      {record.handoverProblem && <p role="alert">{systemMessage(record.handoverProblem)}</p>}
      {record.expectUnread && <p role="alert">{msg('Desk could not read the checkpoints it keeps as handed over to {{holders}}, so the check ran without them.', { holders: record.expectUnread.join(', ') })}</p>}
      <Report report={record.report} />
      <p className={styles.quiet}>{msg('Lines in Desk’s copy of the chain: {{number}}', { number: record.chainLines })}</p>
      <Signatures runnerKey={record.runnerKey} />
      {record.runtime && <p className={styles.quiet}>{msg('Checked by jpack {{version}}.', { version: record.runtime })}</p>}
    </>
  }
}

/** Runner's key, as Help & About → Gates says it, and the panel's sentence on what is signed. */
function Signatures({ runnerKey }: { runnerKey?: RunnerKey }) {
  return <section aria-label={msg('Signatures')}>
    <RunnerSignatures runnerKey={runnerKey} />
    <p className={styles.quiet}>{msg(SIGNED_STATEMENT)}</p>
  </section>
}

/** Each completed run's signing, a page at a time, on request only. */
function RunSignings() {
  const [pages, setPages] = useState<Extract<JobsSigning, { state: 'runs' }>[]>([])
  const [other, setOther] = useState<Exclude<JobsSigning, { state: 'runs' }>>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  async function read(after?: number) {
    setBusy(true); setError(undefined)
    try {
      const page = await readJobsSigning(after)
      if (page.state === 'runs') {
        setOther(undefined)
        setPages(before => after === undefined ? [page] : [...before, page])
      } else {
        setOther(page)
        setPages([])
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      if (after === undefined) { setPages([]); setOther(undefined) }
    } finally {
      setBusy(false)
    }
  }
  const last = pages.at(-1)
  const runs = pages.flatMap(page => page.runs)
  const named = runs.filter(run => run.held === 'unlisted' || run.held === 'failed' || run.held === 'other')
  return <section className={styles.card} aria-label={msg('Each run’s signing')}>
    <div className={styles.actions}><Button disabled={busy} onClick={() => void read()}>{msg('Read each run’s signing')}</Button></div>
    {error && <p role="alert">{systemMessage(error)}</p>}
    {other?.state === 'no-runner' && <p>{msg('This desk has no Runner, so it has no run to check.')}</p>}
    {other?.state === 'not-running' && <p>{msg('Runner is not running on this desk now, so Desk could not read its runs.')}</p>}
    {last && <>
      <KeyList list={last.list} />
      {named.length > 0 && <div role="alert" aria-label={msg('Runs to look at')}>
        <ul className={styles.list}>{named.map(run => <li key={run.id}><code>{run.id}</code> <Named run={run} /></li>)}</ul>
      </div>}
      {runs.length === 0
        ? <p>{msg('Runner lists no completed run.')}</p>
        : <ul className={styles.list} aria-label={msg('Completed runs')}>{runs.map(run => <RunSigningRow key={run.id} run={run} />)}</ul>}
      {last.next !== undefined && <div className={styles.actions}><Button disabled={busy} onClick={() => void read(last.next)}>{msg('Show older runs')}</Button></div>}
    </>}
  </section>
}

/** The list Desk keeps for this Runner, as the page of runs was held to it. */
function KeyList({ list }: { list: RunnerKeyList }) {
  if (list.state === 'read') return <CodeBlock text={list.current} label={msg('The key in force in Desk’s list for this Runner')} />
  if (list.state === 'none') return <p>{msg('Desk keeps no list of public keys for this desk’s Runner.')}</p>
  return <p role="alert">{msg('Desk could not read its list of Runner’s public keys now, so no run’s key was held to it: {{problem}}', { problem: list.problem })}</p>
}

/** Why a run is named: Desk's one sentence, Runner's own words, or a member in neither of Runner's forms. */
function Named({ run }: { run: RunSigning }) {
  if (run.held === 'unlisted') return <>{msg(UNLISTED)}</>
  if (run.held === 'failed') return <span lang="en">{(run.signing as { reason: string }).reason}</span>
  return <>{msg('Runner’s signing member for this run is in neither of the forms Runner documents.')}</>
}

/** One run: its signing, as Runner records it, and its export checked on request. */
function RunSigningRow({ run }: { run: RunSigning }) {
  const [check, setCheck] = useState<RunVerification>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  async function verify() {
    setBusy(true); setCheck(undefined); setError(undefined)
    try {
      setCheck(await verifyJobsRun(run.id))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }
  return <li aria-label={run.id}>
    <p><code>{run.id}</code>{run.jobName ? <> · {run.jobName}</> : null}{run.finishedAt ? <> · {run.finishedAt}</> : null}</p>
    {run.signing === undefined
      ? <p className={styles.quiet}>{msg('Runner recorded no signing for this run.')}</p>
      : <p>{msg('Signing, as Runner records it:')} <code lang="en">{JSON.stringify(run.signing)}</code></p>}
    {run.held === 'not-held' && <p className={styles.quiet}>{msg('Not held to the list: Desk could not read it now.')}</p>}
    <div className={styles.actions}><Button disabled={busy} onClick={() => void verify()}>{msg('Check this run’s export with jpack-runner verify-run')}</Button></div>
    {busy && <p role="status" className={styles.quiet}>{msg('Asking jpack-runner verify-run…')}</p>}
    {error && <p role="alert">{systemMessage(error)}</p>}
    {check && <Verification check={check} />}
  </li>
}

/** What verify-run said, as it said it, with the command Desk ran; or Runner's refusal to export the run. */
function Verification({ check }: { check: RunVerification }) {
  if (check.state === 'export-refused') return <section aria-label={msg('What Runner said')}>
    <p>{msg('Runner did not export the run: it answered {{status}}.', { status: check.status })}</p>
    {check.said && <CodeBlock text={check.said} label={msg('Runner’s answer')} lang="en" />}
  </section>
  return <>
    <CodeBlock text={verifyCommand(check.publicKey)} label={msg('The command Desk ran')} lang="en" />
    <section aria-label={msg('What jpack-runner verify-run said')}>
      <p>{msg('jpack-runner verify-run exited with status {{exit}}.', { exit: check.exit })}</p>
      {check.stdout && <CodeBlock text={check.stdout} label={msg('Its standard output')} lang="en" />}
      {check.stderr && <CodeBlock text={check.stderr} label={msg('Its standard error')} lang="en" />}
    </section>
  </>
}

/** The chain, saved as Desk passed it on: kept as a Blob from the response to the saved file, never read as text. */
function ChainDownload() {
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string>()
  async function download() {
    setBusy(true); setSaved(false); setError(undefined)
    try {
      const chain = await downloadRunChain()
      const url = URL.createObjectURL(chain), link = document.createElement('a')
      link.href = url; link.download = RUN_CHAIN_FILE
      document.body.append(link); link.click(); link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      setSaved(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }
  return <div className={styles.card}>
    <div className={styles.actions}><Button disabled={busy} onClick={() => void download()}>{msg('Download the chain')}</Button></div>
    {saved && <p role="status">{msg('Saved {{file}}.', { file: RUN_CHAIN_FILE })}</p>}
    {error && <p role="alert">{systemMessage(error)}</p>}
  </div>
}
