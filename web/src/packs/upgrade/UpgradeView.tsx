/**
 * "Turn on this project's gates" (ADR-0009, section 4).
 *
 * The upgrade offer for a project Desk was started on, or a desk made before
 * new desks started gated. It lists each change, what it writes and why, and
 * writes nothing until the owner confirms. One confirmation covers the
 * configuration and the first lock together, and carries the offer's token,
 * so the desk writes exactly what this page showed, or nothing.
 * `requireComparableFacts` is its own item, which the owner can decline while
 * taking the rest.
 *
 * On the project Desk was started on, "Sign this project's decisions" is one
 * more item (ADR-0010, section 1 and question 2): never chosen for the owner,
 * with its costs listed before anything is confirmed, and what a signature
 * does not establish. Where it is not offered, it says why. Once the key is
 * made, the decision record is checked again, with the key's public half.
 */
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Message } from '../../i18n/Message'
import { msg, systemMessage, useLocale } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import { PageHeader } from '../../ui/PageLayout'
import { checkDecisionRecordAgain } from '../../audit/client'
import { REVIEW_KEY } from '../review/client'
import { File } from '../review/ReviewAndLockView'
import { fileFindings, findingWords, otherFindings } from '../review/findings'
import { confirmUpgrade, readUpgrade, StaleUpgrade, UPGRADE_KEY, type Upgrade, type Upgraded } from './client'
import styles from './Upgrade.module.css'

type Outcome = { kind: 'done'; done: Upgraded } | { kind: 'stale' } | { kind: 'error'; message: string }

export function UpgradeView() {
  useLocale()
  const client = useQueryClient()
  // The owner's choice about requireComparableFacts: on unless declined.
  const [facts, setFacts] = useState(true)
  // The owner's choice about the signing key: never made for them.
  const [sign, setSign] = useState(false)
  const query = useQuery({ queryKey: [...UPGRADE_KEY, facts, sign], queryFn: ({ signal }) => readUpgrade(facts, sign, signal), retry: false, staleTime: 0 })
  const [busy, setBusy] = useState(false)
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const upgrade = query.data
  const confirm = async () => {
    if (!upgrade?.token) return
    setBusy(true)
    setOutcome(null)
    try {
      const done = await confirmUpgrade(upgrade.token, upgrade.requireComparableFacts, upgrade.sign === true)
      setOutcome({ kind: 'done', done })
      // The decision record now has a key to check the trail with.
      if (done.signingKey) void checkDecisionRecordAgain(client)
    } catch (cause) {
      setOutcome(cause instanceof StaleUpgrade ? { kind: 'stale' } : { kind: 'error', message: cause instanceof Error ? cause.message : String(cause) })
    } finally {
      setBusy(false)
      await Promise.all([client.invalidateQueries({ queryKey: UPGRADE_KEY }), client.invalidateQueries({ queryKey: REVIEW_KEY })])
    }
  }
  return <article className={styles.page} aria-label={msg('Turn on this project’s gates')}>
    <PageHeader title={msg('Turn on this project’s gates')} actions={<ButtonLink to="/packs">{msg('Back to packs')}</ButtonLink>} />
    <div className={styles.body}>
      {outcome?.kind !== 'done' && upgrade?.state === 'offer' && <p className={styles.statement}>{msg('Nothing is written until you confirm. If you leave this page, every file stays as it is.')}</p>}
      {outcome?.kind === 'done' && <p role="status" className={styles.done}>
        {msg('The gates are on. jpack.json is at configVersion {{version}}, and these {{count}} files are this project’s reviewed set.', { version: outcome.done.configVersion, count: outcome.done.files })}
        {outcome.done.copies === 'not-stored' && <> {msg('Desk could not keep copies of them, so the next review cannot show what changed.')} {systemMessage(outcome.done.copiesProblem ?? '')}</>}
        {outcome.done.signingKey && <> {msg('Desk keeps a signing key for this project now, keyId {{keyId}}, and jpack.json names it: every runtime that reads jpack.json signs each record it adds to the chained trail, if it accepts the key. Its public key is in the decision record, in Admin → Project.', { keyId: outcome.done.signingKey.keyId })}</>}
      </p>}
      {outcome?.kind === 'stale' && <Alert>{msg('The project changed after you reviewed the upgrade, so nothing was written. Review it again.')}</Alert>}
      {outcome?.kind === 'error' && <Alert reason={systemMessage(outcome.message)}>{msg('The gates were not turned on.')}</Alert>}
      {query.isPending ? <p role="status" className={styles.quiet}>{msg('Asking the runtime what would change…')}</p>
        : query.error ? <section role="alert" className={styles.problem}>
          <p>{systemMessage(query.error.message)}</p>
          <Button onClick={() => void query.refetch()} disabled={query.isFetching}>{msg('Retry')}</Button>
        </section>
        : upgrade && <Offer upgrade={upgrade} facts={facts} setFacts={setFacts} sign={sign} setSign={setSign} busy={busy || query.isFetching} confirm={confirm} />}
    </div>
  </article>
}

function Offer({ upgrade, facts, setFacts, sign, setSign, busy, confirm }: {
  upgrade: Upgrade; facts: boolean; setFacts: (facts: boolean) => void; sign: boolean; setSign: (sign: boolean) => void; busy: boolean; confirm: () => Promise<void>
}) {
  if (upgrade.state === 'unavailable') return <Alert reason={systemMessage(upgrade.reason ?? '')}>{msg('Desk offers no upgrade here.')}</Alert>
  const review = upgrade.review
  const covered = review ? review.files.filter(file => file.lock !== 'removed').length : 0
  // The runtime's own findings about the new jpack.json, and any about no
  // file: the file itself is shown under the first item.
  const config = review?.files.find(file => file.kind === 'config')
  const said = review ? [...(config ? fileFindings(review, config) : []), ...otherFindings(review)] : []
  return <>
    {upgrade.state === 'unchanged' && upgrade.gated && <p role="status">{gatesSay(upgrade)}</p>}
    {upgrade.state === 'offer' && <ol className={styles.items}>
      <li className={styles.item} aria-labelledby="upgrade-config">
        <h2 id="upgrade-config" className={styles.heading}><code>jpack.json</code></h2>
        <ul className={styles.changes}>
          {upgrade.changes.includes('configVersion') && (upgrade.changes.includes('signingKey')
            ? <li><Message text="<0/> moves from <1/> to <2/>, the version that names a signing key." slots={[<code>configVersion</code>, <code>{upgrade.from}</code>, <code>{upgrade.to}</code>]} /></li>
            : <li><Message text="<0/> moves from <1/> to <2/>, the version these gates need." slots={[<code>configVersion</code>, <code>{upgrade.from}</code>, <code>{upgrade.to}</code>]} /></li>)}
          {upgrade.changes.includes('requireReviewed') && <li><Message text="<0/>: a deciding run must apply exactly the packs you last locked. Drafts can still be rehearsed and tested." slots={[<code>requireReviewed</code>]} /></li>}
          {upgrade.changes.includes('audit') && <li><Message text="<0/>: each completed deciding run is recorded in <1/>, which is private to this desk and never committed. Desk makes the folder, open only to you, where it is missing." slots={[<code>audit</code>, <code>.desk-private/audit</code>]} /></li>}
          {upgrade.audit?.state === 'kept' && <li><Message text="The audit directory this project already declares, <0/>, is kept." slots={[<code>{upgrade.audit.dir ?? ''}</code>]} /></li>}
          {upgrade.changes.includes('requireComparableFacts') && <li><Message text="<0/>: see the last item." slots={[<code>requireComparableFacts</code>]} /></li>}
          {upgrade.changes.includes('signingKey') && <li><Message text="<0/>: names, by its absolute path, the key Desk makes for this project. See the item on signing." slots={[<code>audit.signingKey</code>]} /></li>}
        </ul>
        <p className={styles.quiet}>{msg('Every other member, and the order of the members, stay as they are.')}</p>
        <CodeBlock text={upgrade.configAfter ?? ''} label={msg('After the upgrade')} />
        <Disclosure title={msg('jpack.json now')}><CodeBlock text={upgrade.configBefore ?? ''} label={msg('Now')} /></Disclosure>
      </li>
      <li className={styles.item} aria-labelledby="upgrade-gitignore">
        <h2 id="upgrade-gitignore" className={styles.heading}><code>.gitignore</code></h2>
        <p>{upgrade.gitignore === 'add' ? <Message text="Desk adds the line <0/> at the end, so that the records are not committed." slots={[<code>.desk-private/</code>]} />
          : upgrade.gitignore === 'create' ? <Message text="This project is in a Git work tree with no .gitignore. Desk creates one holding the line <0/>, so that the records are not committed." slots={[<code>.desk-private/</code>]} />
            : upgrade.gitignore === 'ignored' ? <Message text=".gitignore already ignores <0/>. It is left as it is." slots={[<code>.desk-private/</code>]} />
              : msg('This project is not in a Git work tree, so .gitignore is left as it is.')}</p>
        {(upgrade.gitignore === 'add' || upgrade.gitignore === 'create') && <p className={styles.quiet}>{msg('Desk reads only this project’s own .gitignore. A rule elsewhere that already ignores the folder makes this line redundant, and harmless.')}</p>}
      </li>
      <li className={styles.item} aria-labelledby="upgrade-lock">
        <h2 id="upgrade-lock" className={styles.heading}>{msg('The first review and lock')}</h2>
        <p>{msg('The lock covers {{count}} files: jpack.json as the upgrade writes it, and every pack and graph it declares. Without a lock, requireReviewed would refuse every deciding run, so the configuration and the lock are written together: if the lock fails, Desk puts jpack.json, .gitignore and the lock back as they were.', { count: covered })}</p>
        {upgrade.locked && <section className={styles.warning} aria-label={msg('This project already keeps a lock')}>
          <h3 className={styles.subheading}>{msg('This project already keeps a lock')}</h3>
          <p>{msg('jpack.lock.json is already here, perhaps one a CI step checks. To the runtime the new jpack.json is config-drift: until a lock of it is in place, every deciding run by decision id is refused, and a CI step that runs packs verify fails until the new lock is committed. The upgrade writes the new lock in the same step. Commit jpack.json and jpack.lock.json together.')}</p>
        </section>}
        {said.length > 0 && <ul className={styles.diagnostics} aria-label={msg('What the runtime said')}>{said.map((finding, index) => <li key={index}><strong>{findingWords(finding.name)}</strong>{finding.path && <> <code>{finding.path}</code></>}{finding.detail && <> {finding.detail}</>}</li>)}</ul>}
        {review?.status === 'error' && review.diagnostics.length > 0 && <ul className={styles.diagnostics} aria-label={msg('What the runtime said')}>{review.diagnostics.map((item, index) => <li key={index}><code>{item.code}</code> {item.message}</li>)}</ul>}
        {review && <ul className={styles.files}>{review.files.filter(file => file.kind !== 'config').map(file => <File key={`${file.kind}:${file.id ?? ''}:${file.path}`} review={review} file={file} />)}</ul>}
      </li>
      <li className={styles.item} aria-labelledby="upgrade-facts">
        <h2 id="upgrade-facts" className={styles.heading}><code>requireComparableFacts</code></h2>
        <Facts upgrade={upgrade} facts={facts} setFacts={setFacts} busy={busy} />
      </li>
      {upgrade.signingKey && <li className={styles.item} aria-labelledby="upgrade-signing">
        <h2 id="upgrade-signing" className={styles.heading}>{msg('Sign this project’s decisions')}</h2>
        <Signing upgrade={upgrade} sign={sign} setSign={setSign} busy={busy} />
      </li>}
    </ol>}
    {upgrade.state === 'unchanged' && upgrade.comparableFacts === 'off' && <section className={styles.item} aria-labelledby="upgrade-facts">
      <h2 id="upgrade-facts" className={styles.heading}><code>requireComparableFacts</code></h2>
      <Facts upgrade={upgrade} facts={facts} setFacts={setFacts} busy={busy} />
      {upgrade.signingKey?.state !== 'offered' && <p className={styles.quiet}>{msg('Without it, there is nothing to change.')}</p>}
    </section>}
    {upgrade.state === 'unchanged' && upgrade.signingKey && <section className={styles.item} aria-labelledby="upgrade-signing">
      <h2 id="upgrade-signing" className={styles.heading}>{msg('Sign this project’s decisions')}</h2>
      <Signing upgrade={upgrade} sign={sign} setSign={setSign} busy={busy} />
      {upgrade.signingKey.state === 'offered' && upgrade.comparableFacts !== 'off' && <p className={styles.quiet}>{msg('Without it, there is nothing to change.')}</p>}
    </section>}
    {upgrade.state === 'offer' && upgrade.token && <section className={styles.confirm} aria-label={msg('Confirm')}>
      <p className={styles.statement}>{msg('Locking records that you confirmed these exact files as this project’s reviewed set. It is not a second person’s approval, and it records no name.')}</p>
      <div className={styles.actions}>
        {upgrade.sign
          ? <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Making the key and locking…') : msg('Make the signing key and lock {{count}} files', { count: covered })}</Button>
          : <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Turning the gates on…') : msg('Turn the gates on and lock {{count}} files', { count: covered })}</Button>}
        <ButtonLink to="/packs" variant="quiet">{msg('Not now')}</ButtonLink>
      </div>
    </section>}
  </>
}

/** The fourth item: requireComparableFacts, which the owner can decline on its own. */
function Facts({ upgrade, facts, setFacts, busy }: { upgrade: Upgrade; facts: boolean; setFacts: (facts: boolean) => void; busy: boolean }) {
  if (upgrade.comparableFacts === 'on') return <p>{msg('jpack.json already sets requireComparableFacts.')}</p>
  if (upgrade.comparableFacts === 'unavailable') return <p>{msg('requireComparableFacts needs runtime 0.25.0 or later. The runtime this Desk runs (jpack {{version}}) does not read configuration version 5, so it is not offered.', { version: upgrade.runtime ?? '' })}</p>
  return <>
    <label className="checkbox"><input type="checkbox" checked={facts} disabled={busy} onChange={event => setFacts(event.target.checked)} />{msg('Also refuse a fact of a type no comparison can match')}</label>
    <p className={styles.quiet}>{msg('It refuses an evaluation, rehearsals included, in which a fact has a JSON type that a comparison in the pack can never match: "true" or 1 where the pack compares with true, for example. The refusal names the fact and what the comparison can match. Saved tests and Jobs are not refused. Untick it to take the rest without it; it stays on offer in Admin → Project.')}</p>
  </>
}

/**
 * The fifth item: a signing key for the project Desk was started on, never
 * chosen for the owner. Its costs, as the maintainer's answer to ADR-0010's
 * question 2 lists them, and what a signature does not establish, as section
 * 7 says it, stand before anything is confirmed. Where it is not offered, the
 * chassis's sentence says why.
 */
function Signing({ upgrade, sign, setSign, busy }: { upgrade: Upgrade; sign: boolean; setSign: (sign: boolean) => void; busy: boolean }) {
  const item = upgrade.signingKey
  if (item?.state === 'named') return <p>{msg('jpack.json already names a signing key.')}</p>
  if (item?.state === 'unavailable') return <p>{systemMessage(item.reason)}</p>
  return <>
    <label className="checkbox"><input type="checkbox" checked={sign} disabled={busy} onChange={event => setSign(event.target.checked)} />{msg('Make a signing key for this project, and name it in jpack.json')}</label>
    <p className={styles.quiet}>{msg('Desk has the runtime make an Ed25519 key for this project in Desk’s own configuration folder, outside the project, and jpack.json, at configVersion 6, names it. Every runtime that reads jpack.json then signs each record it adds to the chained trail, if it accepts the key.')}</p>
    <section className={styles.warning} aria-label={msg('What a signing key costs this project')}>
      <h3 className={styles.subheading}>{msg('What a signing key costs this project')}</h3>
      <ul className={styles.changes}>
        <li>{msg('The home path in a committed file: jpack.json names the key by its absolute path, which shows anyone who reads the file where your home folder is.')}</li>
        <li><Message text="<0/> failing in CI: in any checkout where the key is not present, packs validate answers invalid and exits 1." slots={[<code>packs validate</code>]} /></li>
        <li>{msg('Runtimes before the floor refusing the project: a runtime older than {{floor}} refuses a project at configVersion 6, for every command.', { floor: '0.26.0' })}</li>
      </ul>
    </section>
    <p className={styles.quiet}>{msg('A signature establishes that a holder of the key signed these exact bytes. It does not establish anything against you, who hold the key; anything after the key is copied; anything against an agent that can read the key; or that the trail is complete.')}</p>
  </>
}

/** What the gates of a project that has them hold, in one sentence. */
export function gatesSay(upgrade: Upgrade): string {
  if (upgrade.comparableFacts === 'on') return msg('This project’s gates are on: deciding runs are held to its reviewed set and recorded, and a fact of a type no comparison can match is refused.')
  if (upgrade.comparableFacts === 'unavailable') return msg('This project’s gates are on: deciding runs are held to its reviewed set and recorded. requireComparableFacts needs runtime 0.25.0 or later.')
  return msg('This project’s gates are on: deciding runs are held to its reviewed set and recorded. requireComparableFacts is off.')
}
