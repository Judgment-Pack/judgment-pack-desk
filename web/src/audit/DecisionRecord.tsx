/**
 * Admin → Project → Decision record (ADR-0010, sections 4 and 6).
 *
 * What the runtime's own `jpack audit verify` finds in this desk's trail: the
 * status, the coverage, the segments and discontinuities, each finding by
 * name, and the runtime's sentences about what the result establishes and
 * what it does not, verbatim and in English, as Desk shows the runtime's
 * diagnostics. Desk runs it with no key, no held checkpoint and no stamping
 * roots, and the panel says so.
 *
 * It runs when the panel becomes visible and when the owner asks again: never
 * on a timer, on focus, on a reconnect or on a change to the project. The
 * query is disabled, so nothing reruns it on its own, and marked to run only
 * on request, so `McpProvider` neither cancels nor invalidates it. Admin keeps
 * Project's panel mounted while another section is open, so it says when the
 * panel is visible.
 *
 * With a runtime that has no audit commands, it says one sentence and nothing
 * else: no word of what that runtime cannot do is said as if it were done.
 */
import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ON_REQUEST_ONLY } from '../mcp/projectChange'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { SettingsSection } from '../ui/SettingsSection'
import { AUDIT_KEY, AuditUnavailable, readAuditRecord, type AuditCoverageState, type AuditReport } from './client'
import styles from './DecisionRecord.module.css'
import { TrailDownloads } from './TrailDownloads'

/**
 * The panel's one query: run each time the panel becomes visible, and on the
 * owner's request, and at no other time.
 */
export function useAuditRecord(visible: boolean) {
  const query = useQuery({
    queryKey: AUDIT_KEY,
    queryFn: ({ signal }) => readAuditRecord(signal),
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
  // Joins a run already in flight rather than starting a second.
  useEffect(() => { if (visible) void refetch.current({ cancelRefetch: false }) }, [visible])
  return query
}

export function DecisionRecord({ visible = true }: { visible?: boolean }) {
  useLocale()
  const query = useAuditRecord(visible)
  const record = query.data
  const again = <div><Button onClick={() => void query.refetch()}>{msg('Check again')}</Button></div>
  return <SettingsSection title={msg('Decision record')} description={msg('Jobs runs are recorded by the runner, not in this trail.')} variant="plain">
    <div className={styles.card} data-testid="decision-record">
      {query.isPending || query.isFetching ? <p role="status" className={styles.quiet}>{msg('Asking the runtime…')}</p>
        : query.error instanceof AuditUnavailable ? <p role="alert">{msg('Desk does not check the decision record here.')} {systemMessage(query.error.message)}</p>
          : query.error ? <div role="alert" className={styles.card}><p>{systemMessage(query.error.message)}</p><div><Button onClick={() => void query.refetch()} disabled={query.isFetching}>{msg('Retry')}</Button></div></div>
            : record?.state === 'older-runtime' ? <p>{msg('This runtime (jpack {{version}}) writes an unchained trail and has no audit commands. Chaining, checkpoints, signing and stamping need jpack {{floor}} or later.', { version: record.runtime ?? '?', floor: record.floor })}</p>
              : record?.state === 'no-trail' ? <p>{msg('This project keeps no trail: its jpack.json declares no audit directory, so the runtime records none of its deciding runs.')}</p>
                : record?.state === 'unverified' ? <>
                  <p>{msg('The runtime did not check the trail.')}</p>
                  <ul className={styles.list} aria-label={msg('What the runtime said')}>{record.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>
                  <TrailDownloads files={record.files ?? []} />
                  {again}
                </>
                  : record?.state === 'report' && <>
                    <p className={styles.statement}>{msg('Desk ran this on your machine, over your trail, with no keys and no checkpoints: it checked no signature, no held checkpoint and no stamp. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.')}</p>
                    <Report report={record.report} />
                    <TrailDownloads files={record.files ?? []} />
                    {record.runtime && <p className={styles.quiet}>{msg('Checked by jpack {{version}}.', { version: record.runtime })}</p>}
                    {again}
                  </>}
    </div>
  </SettingsSection>
}

/** The runtime's word for a report's status, in the owner's language. */
function statusWords(status: string): string | undefined {
  switch (status) {
    case 'valid': return msg('Every check the runtime made passed.')
    case 'segmented': return msg('Every check the runtime made passed. A repair started a new segment, and the history is not intact across it.')
    case 'invalid': return msg('The trail failed a check the runtime made.')
  }
}

type Protection = 'signed' | 'checkpointed' | 'stamped'

/** How far one protection reaches, as the report says it. */
function reach(kind: Protection, state: AuditCoverageState) {
  const through = state.through ?? 0
  const words = state.status === 'through'
    ? { signed: () => msg('Signed through record {{sequence}}', { sequence: through }), checkpointed: () => msg('Witnessed through record {{sequence}}', { sequence: through }), stamped: () => msg('Stamped through record {{sequence}}', { sequence: through }) }[kind]()
    : state.status === 'none' && kind === 'signed' ? msg('No record carries a valid signature')
      : state.status === 'none' && kind === 'stamped' ? msg('No trusted stamp covers a record')
        : state.status === 'failed' && kind === 'checkpointed' ? msg('The held checkpoints did not match')
          : state.status === 'not-checked' && kind === 'signed' ? msg('Not checked: Desk gave no public key')
            : state.status === 'not-checked' && kind === 'stamped' ? msg('Not checked: Desk gave no time-stamping roots')
              : state.status === 'not-supplied' && kind === 'checkpointed' ? msg('Not checked: Desk gave no held checkpoint')
                : undefined
  return words ?? <><code>{state.status}</code>{state.detail && <span lang="en"> {state.detail}</span>}</>
}

function Report({ report }: { report: AuditReport }) {
  const { coverage } = report
  const words = statusWords(report.status)
  const listed = (shown: number, total: number) => total > shown && <p className={styles.quiet}>{msg('Listed: {{shown}} of {{total}}.', { shown, total })}</p>
  return <>
    <p role="status" data-status={report.status}>{words ?? <code>{report.status}</code>}</p>
    {!report.snapshotBetweenWrites && <p className={styles.quiet}>{msg('The runtime could take no lock on the trail here, so its last line may be a write still in progress.')}</p>}
    <dl className={styles.facts} aria-label={msg('Coverage')}>
      <div><dt>{msg('Lines')}</dt><dd>{report.lines}</dd></div>
      <div><dt>{msg('Lines before the first chained line')}</dt><dd>{coverage.legacyPrefix}</dd></div>
      <div><dt>{msg('Chained lines')}</dt><dd>{coverage.chained}</dd></div>
      <div><dt>{msg('Unchained lines a later chained line commits to')}</dt><dd>{coverage.unchained}</dd></div>
      <div><dt>{msg('Lines nothing commits to')}</dt><dd>{coverage.uncovered}</dd></div>
      <div><dt>{msg('Lines a repair names as damaged')}</dt><dd>{coverage.damaged}</dd></div>
      <div><dt>{msg('Signatures')}</dt><dd>{reach('signed', coverage.signed)}</dd></div>
      {coverage.signed.status !== 'not-checked' && <>
        <div><dt>{msg('Records with a valid signature of their own')}</dt><dd>{coverage.signedRecords}</dd></div>
        <div><dt>{msg('Chained records without one')}</dt><dd>{coverage.unsignedRecords}</dd></div>
      </>}
      <div><dt>{msg('Held checkpoints')}</dt><dd>{reach('checkpointed', coverage.checkpointed)}</dd></div>
      <div><dt>{msg('Records a held checkpoint witnesses')}</dt><dd>{coverage.witnessed}</dd></div>
      <div><dt>{msg('Records no held checkpoint witnesses')}</dt><dd>{coverage.unwitnessed}</dd></div>
      <div><dt>{msg('Stamps')}</dt><dd>{reach('stamped', coverage.stamped)}</dd></div>
    </dl>
    <section aria-label={msg('Segments')}>
      <h4 className={styles.heading}>{msg('Segments')}</h4>
      <ul className={styles.list}>{report.segments.map((segment, index) => <li key={index}>{msg('Lines {{first}} to {{last}}', { first: segment.firstLine, last: segment.lastLine })}</li>)}</ul>
      {listed(report.segments.length, report.segmentsTotal)}
    </section>
    {report.discontinuitiesTotal > 0 && <section aria-label={msg('Discontinuities')}>
      <h4 className={styles.heading}>{msg('Discontinuities')}</h4>
      <ul className={styles.list}>{report.discontinuities.map((item, index) => <li key={index}>
        {msg('At line {{line}}, a repair names line {{damaged}} as damaged.', { line: item.line, damaged: item.damagedLine })} <code>{item.reason}</code> <code>{item.digest}</code>
      </li>)}</ul>
      {listed(report.discontinuities.length, report.discontinuitiesTotal)}
    </section>}
    <section aria-label={msg('Findings')}>
      <h4 className={styles.heading}>{msg('Findings')}</h4>
      {report.findingsTotal === 0 ? <p>{msg('No check failed.')}</p>
        : <ul className={styles.list}>{report.findings.map((finding, index) => <li key={index}>
          <code>{finding.name}</code> {msg('Line {{line}}', { line: finding.line })}: <span lang="en">{finding.detail}</span>
        </li>)}</ul>}
      {listed(report.findings.length, report.findingsTotal)}
    </section>
    {report.establishes.length > 0 && <section aria-label={msg('What this establishes')}>
      <h4 className={styles.heading}>{msg('What this establishes')}</h4>
      <ul className={styles.list} lang="en">{report.establishes.map((sentence, index) => <li key={index}>{sentence}</li>)}</ul>
    </section>}
    {report.doesNotEstablish.length > 0 && <section aria-label={msg('What this does not establish')}>
      <h4 className={styles.heading}>{msg('What this does not establish')}</h4>
      <ul className={styles.list} lang="en">{report.doesNotEstablish.map((sentence, index) => <li key={index}>{sentence}</li>)}</ul>
    </section>}
  </>
}
