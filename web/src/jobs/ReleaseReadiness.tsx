import { msg, formatDate, useLocale } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import { MatrixRowList } from '../components/MatrixRowList'
import { CoverageReport } from '../components/CoverageReport'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import type { Release } from './client'
import styles from './JobsView.module.css'

/** The startup flag that turns the tested-releases policy off. Not translated. */
const POLICY_OFF = '--runner-require-tested-releases=false'

/** The installation's tested-releases policy as Desk reported it, or undefined where it did not say. */
export function useRequireTestedReleases(): boolean | undefined {
  return useEffectiveConfig().desk?.chassis?.jobs?.requireTestedReleases
}

/**
 * The untested-release note. It names the policy only where Desk said what it
 * is; otherwise it says what it said before the page could know. A release that
 * already backs a job (`job`) is never stopped by the policy, which refuses only
 * a new job.
 */
function untestedNote(requireTested: boolean | undefined, job: boolean) {
  if (job) return requireTested === true
    ? msg('No saved tests were run for this release. This job was created before this installation refused untested releases, and it keeps running. A new job needs a release whose saved tests ran and passed.')
    : msg('No saved tests were run for this release. Review it as untested before creating a job.')
  if (requireTested === true) return msg('No saved tests were run for this release, and this installation refuses to create a job from an untested release. Save tests for this pack, then check a new release. To turn this policy off, restart Desk with {{flag}}.', { flag: POLICY_OFF })
  if (requireTested === false) return msg('No saved tests were run for this release. This installation allows a job from an untested release, because Desk was started with {{flag}}. Review it as untested before creating a job.', { flag: POLICY_OFF })
  return msg('No saved tests were run for this release. Review it as untested before creating a job.')
}

/** `refusal` is the runner's own answer to creating a job from this release; `job` marks a release an existing job runs. */
export function ReleaseReadiness({ release, refusal, job = false }: { release: Release; refusal?: string; job?: boolean }) {
  useLocale()
  const requireTested = useRequireTestedReleases()
  const check = release.testEvidence
  const entry = check?.report?.packs?.[0]
  const counts = entry?.summary
  const complete = release.tests === 'passed' || release.tests === 'failed'
  const gaps = entry?.coverage?.length ? entry.coverage.filter(p => p.status !== 'covered').length : undefined
  const status = release.tests === 'passed' ? msg('Passed') : release.tests === 'failed' ? msg('Failed') : release.tests === 'not-run' ? msg('Not run') : msg('Check incomplete')
  return <div className={styles.readiness}>
    <div className={styles.readinessHeading}><h3>{msg('Release readiness')}</h3><span className={release.tests === 'failed' || release.tests === 'error' ? styles.problem : undefined}>{status}</span></div>
    {release.tests === 'not-run' ? <p className={styles.note}>{untestedNote(requireTested, job)}</p> : <>
      {complete && counts && <p>{msg('Saved cases: {{total}} · {{passed}} passed · {{failed}} failed', { passed: counts.passed, failed: counts.mismatched, total: counts.total })}</p>}
      {check?.problem && <p className={styles.problem}>{check.problem}</p>}
      {(release.tests === 'failed' || release.tests === 'error') && <p className={styles.note}>{msg('Correct the pack or saved cases, then check a new release before creating a job.')}</p>}
      {complete && <p className={styles.note}>{msg('Checked against this exact pack and Runtime snapshot. Later edits do not change this report.')}</p>}
      {check && <p className={styles.note}>{msg('Checked {{date}}', { date: formatDate(new Date(check.checkedAt), { dateStyle: 'medium', timeStyle: 'short' }) })}</p>}
      {check?.source?.exploratoryCount ? <p className={styles.note}>{msg('Cases without saved expectations: {{count}} · not run', { count: check.source.exploratoryCount })}</p> : null}
      {complete && entry && <>
        <Disclosure title={msg('Test results')}><MatrixRowList rows={entry.rows ?? []} names={check?.source?.caseNames} /></Disclosure>
        <Disclosure title={gaps === undefined ? msg('Coverage') : msg('Coverage gaps: {{count}} · advisory', { count: gaps })}><CoverageReport coverage={entry.coverage} /></Disclosure>
      </>}
      {!complete && check?.report && <Disclosure title={msg('Runtime report')}><pre className={styles.json}>{JSON.stringify(check.report, null, 2)}</pre></Disclosure>}
      {check && <Disclosure title={msg('Test snapshot')}><dl className={styles.properties}>
        <div><dt>{msg('Matrix digest')}</dt><dd><code>{check.matrixDigest}</code></dd></div>
        {check.source && <div><dt>{msg('Test suite revision')}</dt><dd>{check.source.suiteRevision}</dd></div>}
      </dl><Disclosure title={msg('Saved expectations (JSON)')}><pre className={styles.json}>{check.matrix}</pre></Disclosure></Disclosure>}
    </>}
    {refusal && <p className={styles.problem} role="alert">{refusal}</p>}
  </div>
}
