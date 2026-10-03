/**
 * "Review and lock" (ADR-0009, section 2).
 *
 * Every file a lock would cover, and what only the lock names, from one
 * reading of the project: each with the runtime's `packs verify` findings in
 * plain words, and a diff only where the desk kept a copy of exactly the bytes
 * the lock names. One confirmation covers every file, and says how many: a
 * lock covers the whole set, so there is no locking one pack. It carries the
 * review's token, and the desk locks exactly that reading, or nothing. Where
 * a file cannot be shown, nothing can be confirmed.
 */
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, systemMessage, useLocale } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import { PageHeader } from '../../ui/PageLayout'
import { SnapshotComparison } from '../SnapshotComparison'
import { confirmLock, REVIEW_KEY, StaleReview, type Locked, type Review, type ReviewFile } from './client'
import { fileFindings, findingWords, otherFindings } from './findings'
import { useReview } from './ReviewContext'
import styles from './ReviewAndLock.module.css'

type Outcome = { kind: 'locked'; locked: Locked } | { kind: 'stale' } | { kind: 'error'; message: string }

export function ReviewAndLockView() {
  useLocale()
  const query = useReview()
  const client = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const review = query?.data
  const lock = async () => {
    if (!review?.token) return
    setBusy(true)
    setOutcome(null)
    try {
      setOutcome({ kind: 'locked', locked: await confirmLock(review.token) })
    } catch (cause) {
      setOutcome(cause instanceof StaleReview ? { kind: 'stale' } : { kind: 'error', message: cause instanceof Error ? cause.message : String(cause) })
    } finally {
      setBusy(false)
      await client.invalidateQueries({ queryKey: REVIEW_KEY })
    }
  }
  const covered = review ? review.files.filter(file => file.lock !== 'removed').length : 0
  const matches = review?.locked && review.status === 'valid' && review.findings.length === 0
  const others = review ? otherFindings(review) : []
  return <article className={styles.page} aria-label={msg('Review and lock')}>
    <PageHeader title={msg('Review and lock')} actions={<ButtonLink to="/packs">{msg('Back to packs')}</ButtonLink>} />
    <div className={styles.body}>
      <p className={styles.statement}>{msg('Locking records that you confirmed these exact files as this project’s reviewed set. It is not a second person’s approval, and it records no name.')}</p>
      <p className={styles.quiet}>{msg('A change to jpack.json holds every pack: until the next lock, the runtime refuses every deciding run by decision id. Rehearsals and tests are not affected.')}</p>
      {outcome?.kind === 'locked' && <p role="status" className={styles.done}>
        {msg('Locked. These {{count}} files are now this project’s reviewed set.', { count: outcome.locked.files })}
        {outcome.locked.copies === 'not-stored' && <> {msg('Desk could not keep copies of them, so the next review cannot show what changed.')} {systemMessage(outcome.locked.copiesProblem ?? '')}</>}
      </p>}
      {outcome?.kind === 'stale' && <Alert>{msg('The project changed after you reviewed it, so nothing was locked. Review the changes again.')}</Alert>}
      {outcome?.kind === 'error' && <Alert reason={systemMessage(outcome.message)}>{msg('Nothing was locked.')}</Alert>}
      {!query || query.isPending ? <p role="status" className={styles.quiet}>{msg('Asking the runtime what changed…')}</p>
        : query.error ? <section role="alert" className={styles.problem}>
          <p>{systemMessage(query.error.message)}</p>
          <Button onClick={() => void query.refetch()} disabled={query.isFetching}>{msg('Retry')}</Button>
        </section>
        : review && <>
          {!review.locked && <p>{msg('This project has no reviewed-set lock yet, so every file below is new to it. Read each one before you lock.')}</p>}
          {review.status === 'error' && review.diagnostics.length > 0 && <section aria-label={msg('What the runtime said')}>
            <h2 className={styles.heading}>{msg('What the runtime said')}</h2>
            <ul className={styles.diagnostics}>{review.diagnostics.map((item, index) => <li key={index}><code>{item.code}</code> {item.message}</li>)}</ul>
          </section>}
          {matches && <p role="status">{msg('Every file matches the reviewed set. There is nothing to lock.')}</p>}
          {review.files.length > 0 && <section aria-label={msg('Files this lock covers')}>
            <h2 className={styles.heading}>{msg('Files this lock covers')}</h2>
            <ul className={styles.findings}>{review.files.map(file => <File key={`${file.kind}:${file.id ?? ''}:${file.path}`} review={review} file={file} />)}</ul>
          </section>}
          {others.length > 0 && <section aria-label={msg('What the runtime said')}>
            <ul className={styles.findings}>{others.map((finding, index) => <li key={index} className={styles.finding}>
              <h3 className={styles.words}>{findingWords(finding.name)}</h3>
              <p className={styles.where}>{finding.id && <code>{finding.id}</code>}{finding.path && <code>{finding.path}</code>}</p>
              {finding.detail && <p className={styles.quiet}>{finding.detail}</p>}
            </li>)}</ul>
          </section>}
          {review.blocked ? <Alert reason={systemMessage(review.blocked)}>{msg('Desk cannot lock this project here until it can show you every file a lock would cover.')}</Alert>
            : review.token && !matches && <section className={styles.confirm} aria-label={msg('Confirm')}>
              <p>{msg('This lock covers {{count}} files: jpack.json and every pack and graph it declares.', { count: covered })}</p>
              <Button variant="primary" disabled={busy || query.isFetching} onClick={() => void lock()}>
                {busy ? msg('Locking…') : review.locked && review.findings.length > 0 ? msg('Confirm and lock {{count}} differences', { count: review.findings.length }) : msg('Confirm and lock {{count}} files', { count: covered })}
              </Button>
            </section>}
        </>}
    </div>
  </article>
}

/** One file of the review: what the runtime found, and the file itself. */
function File({ review, file }: { review: Review; file: ReviewFile }) {
  const found = fileFindings(review, file)
  const { earlier, now } = file
  // A first lock reads every file; after one, a file the lock already holds
  // stays closed, and can still be opened.
  const open = !review.locked || found.length > 0 || file.lock !== 'same'
  return <li className={styles.finding} data-file={file.path}>
    <h3 className={styles.words}>{found.length > 0 ? found.map(finding => findingWords(finding.name)).join(' · ')
      : file.lock === 'same' ? msg('Matches the last lock')
        : file.lock === 'removed' ? findingWords('locked-but-undeclared')
          : msg('Not in a lock yet')}</h3>
    <p className={styles.where}>{file.id && <code>{file.id}</code>}<code>{file.path}</code></p>
    {found.map((finding, index) => finding.detail && <p key={index} className={styles.quiet}>{finding.detail}</p>)}
    {earlier?.state === 'text' && now.state === 'text'
      ? <>
        <SnapshotComparison before={earlier.text!} after={now.text!} beforeLabel={msg('Last locked')} afterLabel={msg('Now')} />
        <Disclosure title={msg('The whole file now')}><CodeBlock text={now.text!} label={msg('Now')} /></Disclosure>
      </>
      : <>
        {earlier?.state === 'no-copy' && <p className={styles.quiet}>{msg('Desk has no earlier copy of the locked version to compare with.')}</p>}
        {earlier?.state === 'not-shown' && <p className={styles.quiet}>{msg('The locked version is too large to show here.')}</p>}
        {earlier?.state === 'text' && <Disclosure title={msg('Last locked')} open={open}><CodeBlock text={earlier.text!} label={msg('Last locked')} /></Disclosure>}
        {now.state === 'text' && <Disclosure title={msg('The whole file now')} open={open}><CodeBlock text={now.text!} label={msg('Now')} /></Disclosure>}
        {now.state === 'not-shown' && <p className={styles.quiet}>{msg('The current file is too large to show here.')}</p>}
      </>}
  </li>
}
