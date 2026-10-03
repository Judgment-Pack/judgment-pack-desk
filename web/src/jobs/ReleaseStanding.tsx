import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { msg, systemMessage, useLocale } from '../i18n'
import { REVIEW_KEY } from '../packs/review/client'
import { findingWords } from '../packs/review/findings'
import type { ReviewFinding } from '../packs/review/client'
import type { Release } from './client'
import { readReleaseStanding, type LockedBytes, type ReleaseStanding as Standing } from './reviewedSet'
import styles from './JobsView.module.css'

/** What the lock holds for the decision id, beside the bytes the release is made from. */
function comparison(bytes: LockedBytes, id: string) {
  return bytes === 'same' ? msg('The lock pins these exact bytes for {{id}}.', { id })
    : bytes === 'other' ? msg('The lock pins other bytes for {{id}}.', { id })
      : msg('The lock pins no bytes for {{id}}.', { id })
}

function label(standing: Standing | undefined) {
  switch (standing?.state) {
    case undefined: return msg('Checking…')
    case 'reviewed': return msg('In the reviewed set')
    case 'draft': return msg('Draft')
    case 'no-lock': return msg('No lock')
    case 'config-drift': return msg('Project file changed')
    case 'unreadable': return msg('Not known')
  }
}

function Findings({ findings, id }: { findings: ReviewFinding[]; id: string }) {
  if (findings.length === 0) return <p className={styles.note}>{msg('The runtime’s packs verify finds nothing about {{id}} in the project now.', { id })}</p>
  return <div className={styles.field}>
    <p className={styles.note}>{msg('What the runtime’s packs verify finds in the project now:')}</p>
    <ul className={styles.standingFindings}>{findings.map((finding, index) => <li key={index} data-finding={finding.name}>
      {findingWords(finding.name)}{finding.detail && <span className={styles.note}> · {finding.detail}</span>}
    </li>)}</ul>
  </div>
}

/**
 * Whether the pack bytes this release is made from are in the project's
 * reviewed set (ADR-0009, question 4). It shows; it refuses nothing, and
 * nothing in the wizard reads it.
 */
export function ReleaseStanding({ release, packId }: { release: Release; packId: string }) {
  useLocale()
  // One reading per release: a release checked again is compared afresh, and
  // a lock confirmed in the review step invalidates this with the review.
  const query = useQuery({ queryKey: [...REVIEW_KEY, 'release', release.id, packId], queryFn: ({ signal }) => readReleaseStanding(release.pack, packId, signal), retry: false, staleTime: 0, refetchOnWindowFocus: true })
  const standing: Standing | undefined = query.error ? { state: 'unreadable', reason: query.error.message } : query.data
  const review = <Link to="/packs/_review">{msg('Review and lock')}</Link>
  return <div className={styles.readiness} data-standing={standing?.state ?? 'checking'}>
    <div className={styles.readinessHeading}><h3>{msg('Project reviewed set')}</h3><span data-label>{label(standing)}</span></div>
    {!standing ? <p role="status" className={styles.note}>{msg('Comparing the pack bytes of this release with the project’s lock…')}</p>
      : standing.state === 'unreadable' ? <>
        <p>{msg('Desk could not read the project’s review, so this does not say whether the pack bytes of this release are in its reviewed set.')}</p>
        {standing.reason && <p className={styles.note}>{systemMessage(standing.reason)}</p>}
      </>
        : standing.state === 'no-lock' ? <>
          <p>{msg('This project keeps no reviewed-set lock, so the pack bytes of this release are in no reviewed set.')}</p>
          <p>{review}</p>
        </>
          : <>
            <p>{msg('Desk compared the SHA-256 of the pack bytes this release is made from with the project’s lock.')} {comparison(standing.bytes, packId)}</p>
            {standing.state === 'draft' && <p>{msg('This release is made from a draft: bytes that are not in the project’s reviewed set.')}</p>}
            {standing.state === 'config-drift' && <p>{msg('The project’s jpack.json changed after that lock, so until the next lock the runtime refuses every deciding run by decision id in this project, {{id}} included.', { id: packId })}</p>}
            <Findings findings={standing.findings} id={packId} />
            {standing.state !== 'reviewed' && <p>{review}</p>}
          </>}
    <p className={styles.note}>{msg('“In the reviewed set” means that the project’s jpack.lock.json pins these exact bytes for this decision id. It does not say the pack is right, or who reviewed it. It is shown, not enforced: the job is created the same way either way.')}</p>
    <p className={styles.note}>{msg('Runner locks each release on its own and runs it under that lock, so the audit record of every run of this job says reviewed: true. That field is about Runner’s lock of this release, not this project’s reviewed set.')}</p>
  </div>
}
