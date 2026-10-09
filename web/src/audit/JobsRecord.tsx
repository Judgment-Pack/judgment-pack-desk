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
 * with where each run's signature is checked: by `jpack-runner verify-run`
 * on that run's export. Desk passes no public key here, because the chain
 * carries no signature of its own.
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
import { downloadRunChain, JOBS_RECORD_KEY, readJobsRecord, type JobsRecord as JobsAnswer } from './client'
import { Report } from './DecisionRecord'
import styles from './DecisionRecord.module.css'

/** The query that reads, once each time the panel becomes visible, whether this desk has a Runner. */
export const JOBS_RUNNER_KEY = ['desk-jobs-record-runner'] as const
/** The name the chain is saved under, as the Runs page saves it. */
const RUN_CHAIN_FILE = 'run-chain.jsonl'

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

/** Runner's key, as Help & About → Gates says it, and where each run's signature is checked. */
function Signatures({ runnerKey }: { runnerKey?: RunnerKey }) {
  return <section aria-label={msg('Signatures')}>
    <RunnerSignatures runnerKey={runnerKey} />
    <p className={styles.quiet}>{msg("Each run's signature is checked by jpack-runner verify-run on that run's export, not here.")}</p>
  </section>
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
