/**
 * Admin → Project → Decision record → Stamping (ADR-0010, section 3; the
 * maintainer's answer to question 5: Desk schedules stamping once the owner
 * sets an authority, and there is none by default).
 *
 * Beside the settings, ADR-0010 section 7's row for a stamp, verbatim: what a
 * stamp establishes and what it does not. Then this desk's authority, as
 * Desk keeps it, outside the project and never in jpack.json: its address,
 * the interval, the roots, the policies and the revocation lists; or that
 * none is set; or why Desk could not read what it keeps.
 *
 * Setting or changing the authority is the owner's action behind a
 * confirmation in the rotation's pattern: Desk first holds the proposal to
 * its rules and shows what it would keep, and the confirmation states,
 * before anything is kept, that choosing an authority is a trust decision,
 * that each stamp sends that party the checkpoint's digest, and that nothing
 * on the decision path waits for a stamp. Removing the authority is
 * confirmed too, and keeps the stamps already in the trail.
 *
 * Under the settings: the records pending a stamp, where the runtime checked
 * the stamps with the roots; what the runtime said of the stamps it accepted
 * and the lag it measured, in its own numbers; and the last stamp run since
 * Desk started, by Desk's clock, in the runtime's words where it refused.
 * Where the runtime did not check the stamps, the page says so, and the
 * sequence the last run named is labelled as the authority's answer to
 * Desk's request, not as a stamp the runtime checked. "Stamp now" runs one
 * stamp on request, in the desk's one turn, and the decision record is
 * checked again after it.
 */
import { useRef, useState } from 'react'
import { formatDate, msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { TextArea } from '../ui/TextArea'
import { checkStamping, removeStamping, setStamping, stampNow, type AuditReport, type AuditStamping, type StampingChecked, type StampingProposal, type StampingSettings, type StampRun } from './client'
import styles from './DecisionRecord.module.css'

/** What the last stamping action asked here answered, kept by the panel across the check run after it. */
export type StampingOutcome = { kind: 'set' | 'removed' | 'ran' } | { kind: 'failed'; message: string }

const when = (seconds: number) => formatDate(seconds * 1000, { dateStyle: 'medium', timeStyle: 'medium' })
const messageOf = (cause: unknown) => cause instanceof Error ? cause.message : String(cause)

/** A file's bytes in base64, read as bytes and never as text. */
async function base64Of(file: File): Promise<string> {
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  for (let at = 0; at < bytes.length; at += 0x8000) binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000))
  return btoa(binary)
}

export function Stamping({ stamping, report, outcome, onOutcome }: {
  stamping?: AuditStamping
  /** The runtime's report beside it, where there is one: its coverage and stamps. */
  report?: AuditReport
  outcome?: StampingOutcome
  /** What an action answered; the decision record is checked again after each. */
  onOutcome: (outcome: StampingOutcome) => void
}) {
  useLocale()
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [busy, setBusy] = useState(false)
  const remover = useRef<HTMLButtonElement>(null)
  if (!stamping) return null
  const { settings } = stamping
  const stamped = report?.coverage.stamped
  const named = stamping.last && (stamping.last.status === 'stamped' || stamping.last.status === 'already-stamped') ? stamping.last : undefined
  async function run() {
    setBusy(true)
    try {
      // The run's outcome is the decision record's to show, checked again.
      await stampNow()
      onOutcome({ kind: 'ran' })
    } catch (cause) {
      onOutcome({ kind: 'failed', message: messageOf(cause) })
    } finally {
      setBusy(false)
    }
  }
  async function remove() {
    if (!stamping?.removeToken) return
    setBusy(true)
    try {
      await removeStamping(stamping.removeToken)
      onOutcome({ kind: 'removed' })
    } catch (cause) {
      onOutcome({ kind: 'failed', message: messageOf(cause) })
    } finally {
      setBusy(false)
      setRemoving(false)
    }
  }
  return <section aria-label={msg('Stamping')}>
    <h4 className={styles.heading}>{msg('Stamping')}</h4>
    <div className={styles.card}>
      <dl className={styles.facts} aria-label={msg('What a stamp establishes')}>
        <div><dt>{msg('Establishes')}</dt><dd>{msg('the checkpoint, and every line before it, existed by the authority’s stated time, as far as that authority is independent of the operator')}</dd></div>
        <div><dt>{msg('Does not establish')}</dt><dd>{msg('when any record was made: a stamp is an upper bound on existence; anything against an authority that colludes; revocation, where no supplied list speaks for it; anything after the last checkpoint stamped')}</dd></div>
      </dl>
      {outcome?.kind === 'set' && <p role="status">{msg('The time-stamping authority is set. Desk stamps this desk’s trail at the interval, while it is running.')}</p>}
      {outcome?.kind === 'removed' && <p role="status">{msg('The time-stamping authority is removed. Desk stamps nothing more; the stamps already in the trail are kept.')}</p>}
      {outcome?.kind === 'failed' && <p role="alert">{systemMessage(outcome.message)}</p>}
      {stamping.state === 'none' && <p>{msg('No time-stamping authority is set for this desk, so Desk stamps nothing.')}</p>}
      {(stamping.state === 'unread' || stamping.state === 'unavailable') && <p role="alert">{systemMessage(stamping.problem ?? '')}</p>}
      {settings && <Settings settings={settings} />}
      {stamping.passProblem && <p role="alert">{systemMessage(stamping.passProblem)}</p>}
      {stamping.state !== 'unavailable' && <div className={styles.actions}>
        {stamping.state === 'set' && <Button disabled={busy || stamping.running} onClick={() => void run()}>{busy ? msg('Stamping…') : msg('Stamp now')}</Button>}
        <Button disabled={busy} onClick={() => setEditing(true)}>{settings ? msg('Change the authority') : msg('Set an authority')}</Button>
        {stamping.removeToken && <Button ref={remover} disabled={busy} onClick={() => setRemoving(true)}>{msg('Remove the authority')}</Button>}
      </div>}
      {stamping.running && <p role="status">{msg('A stamp run is in progress.')}</p>}
      {stamping.pending !== undefined && <dl className={styles.facts} aria-label={msg('Pending')}>
        <div><dt>{msg('Records pending a stamp')}</dt><dd>{stamping.pending}</dd></div>
      </dl>}
      {report?.stamps && <Stamps report={report} />}
      {stamped?.status === 'not-checked' && (named || stamping.state === 'set') && <p>{msg('The runtime did not check the stamps.')}{stamped.detail && <> <span lang="en">{stamped.detail}</span></>}</p>}
      {stamped?.status === 'not-checked' && named?.sequence !== undefined && <p>{msg('The last stamp run named the checkpoint at record {{sequence}}: that is the authority’s answer to Desk’s request, not a stamp the runtime checked.', { sequence: named.sequence })}</p>}
      {stamping.last ? <LastRun run={stamping.last} /> : stamping.state === 'set' && <p className={styles.quiet}>{msg('No stamp run since Desk started.')}</p>}
      {editing && <AuthorityForm settings={settings} onCancel={() => setEditing(false)} onSet={() => { setEditing(false); onOutcome({ kind: 'set' }) }} />}
    </div>
    <Dialog open={removing} onOpenChange={value => { if (!busy) setRemoving(value) }} openerRef={remover}
      title={msg('Remove this desk’s time-stamping authority?')}
      description={msg('Desk stops stamping this desk and removes the settings it keeps for it.')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setRemoving(false)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void remove()}>{busy ? msg('Removing…') : msg('Remove the authority')}</Button>
      </DialogActions>}>
      <p>{msg('The stamps already in the trail’s stamps file are kept as they are.')}</p>
    </Dialog>
  </section>
}

/** The settings Desk keeps, or would keep. */
function Settings({ settings }: { settings: StampingSettings }) {
  return <>
    <dl className={styles.facts} aria-label={msg('Time-stamping authority')}>
      <div><dt>{msg('Authority')}</dt><dd><code>{settings.authority}</code></dd></div>
      <div><dt>{msg('Interval')}</dt><dd>{msg('{{minutes}} minutes', { minutes: settings.intervalMinutes })}</dd></div>
      <div><dt>{msg('Policies')}</dt><dd>{settings.policies.length ? settings.policies.map(policy => <code key={policy}>{policy} </code>) : msg('Any')}</dd></div>
      <div><dt>{msg('Revocation lists')}</dt><dd>{settings.crls.length}</dd></div>
      {settings.setAt !== undefined && <div><dt>{msg('Set on')}</dt><dd>{when(settings.setAt)}</dd></div>}
    </dl>
    <ul className={styles.list} aria-label={msg('Root certificates')}>{settings.roots.map(root => <li key={root.sha256}>
      <span>{root.subject}</span> <CodeBlock text={root.sha256} label={msg('SHA-256 of the certificate')} />
    </li>)}</ul>
    {settings.crls.map(crl => <CodeBlock key={crl.sha256} text={crl.sha256} label={msg('SHA-256 of a revocation-list file')} />)}
  </>
}

/** What the runtime said of the stamps it checked, in its own numbers. */
function Stamps({ report }: { report: AuditReport }) {
  const stamps = report.stamps!
  const { lag } = stamps
  return <dl className={styles.facts} aria-label={msg('Stamps the runtime checked')}>
    <div><dt>{msg('Stamp lines')}</dt><dd>{stamps.lines}</dd></div>
    <div><dt>{msg('Stamps that hold under the roots given')}</dt><dd>{stamps.trusted}</dd></div>
    <div><dt>{msg('Stamp lines the runtime could not read')}</dt><dd>{stamps.unreadable}</dd></div>
    <div><dt>{msg('Revocation checked against a list given')}</dt><dd>{stamps.revocationChecked}</dd></div>
    <div><dt>{msg('Revocation not checked')}</dt><dd>{stamps.revocationNotChecked}</dd></div>
    {stamps.coveredBy && <div><dt>{msg('Existed by, as the authority states')}</dt><dd><code>{stamps.coveredBy}</code></dd></div>}
    {lag.records > 0 && <>
      <div><dt>{msg('Records whose lag was measured')}</dt><dd>{lag.records}</dd></div>
      <div><dt>{msg('Longest lag')}</dt><dd>{msg('{{seconds}} s, record {{sequence}}', { seconds: lag.maxSeconds, sequence: lag.maxSequence ?? 0 })}</dd></div>
      <div><dt>{msg('Shortest lag')}</dt><dd>{msg('{{seconds}} s, record {{sequence}}', { seconds: lag.minSeconds, sequence: lag.minSequence ?? 0 })}</dd></div>
    </>}
    {lag.atAfterStamp && <div><dt>{msg('A record’s at is later than its stamp’s time')}</dt><dd>{msg('Yes')}</dd></div>}
    {lag.atUnreadable > 0 && <div><dt>{msg('Records whose at could not be read')}</dt><dd>{lag.atUnreadable}</dd></div>}
  </dl>
}

/** The last stamp run since Desk started, by Desk's clock. */
function LastRun({ run }: { run: StampRun }) {
  const date = when(run.at)
  switch (run.status) {
    case 'stamped': return <p data-run="stamped">{msg('Last stamp run, {{date}} by Desk’s clock: the authority stamped the checkpoint at record {{sequence}}, stating it existed by {{existedBy}}, under policy {{policy}}.', { date, sequence: run.sequence ?? 0, existedBy: run.existedBy ?? '', policy: run.policy ?? '' })}</p>
    case 'already-stamped': return <p data-run="already-stamped">{msg('Last stamp run, {{date}} by Desk’s clock: the checkpoint at record {{sequence}} was stamped already, and nothing was asked.', { date, sequence: run.sequence ?? 0 })}</p>
    case 'refused': return <>
      <p data-run="refused">{msg('Last stamp run, {{date}} by Desk’s clock: the runtime did not stamp. It said:', { date })}</p>
      <ul className={styles.list} aria-label={msg('What the runtime said of the stamp')}>{(run.diagnostics ?? []).map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>
      <p className={styles.quiet}>{msg('Desk tries again at the next interval.')}</p>
    </>
    case 'problem': return <p data-run="problem">{msg('Last stamp run, {{date}} by Desk’s clock: it did not stamp.', { date })} {systemMessage(run.problem ?? '')}</p>
  }
}

/** The owner's proposal: checked by Desk, shown, and kept only on the confirmation. */
function AuthorityForm({ settings, onCancel, onSet }: { settings?: StampingSettings; onCancel: () => void; onSet: () => void }) {
  const [authority, setAuthority] = useState(settings?.authority ?? '')
  const [minutes, setMinutes] = useState(String(settings?.intervalMinutes ?? 60))
  const [roots, setRoots] = useState('')
  const [policies, setPolicies] = useState(settings?.policies.join(' ') ?? '')
  const [lists, setLists] = useState<File[]>([])
  const [checked, setChecked] = useState<{ proposal: StampingProposal; answer: StampingChecked }>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const opener = useRef<HTMLButtonElement>(null)
  async function review() {
    setBusy(true); setError(undefined)
    try {
      const proposal: StampingProposal = {
        authority: authority.trim(), intervalMinutes: /^\d+$/.test(minutes.trim()) ? Number(minutes.trim()) : -1, roots,
        policies: policies.split(/[\s,]+/).filter(Boolean), crls: await Promise.all(lists.map(base64Of))
      }
      setChecked({ proposal, answer: await checkStamping(proposal) })
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }
  async function confirm() {
    if (!checked) return
    setBusy(true)
    try {
      await setStamping(checked.proposal, checked.answer.token)
      setChecked(undefined)
      onSet()
    } catch (cause) {
      setChecked(undefined)
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }
  return <form aria-label={msg('Time-stamping authority')} className={styles.card} onSubmit={event => { event.preventDefault(); void review() }}>
    <Field label={msg('Authority’s address')} hint={msg('An http or https address. Desk passes it to jpack audit stamp as --tsa, and never writes it into jpack.json.')}>
      {wiring => <Input {...wiring} value={authority} onChange={event => setAuthority(event.target.value)} />}
    </Field>
    <Field label={msg('Interval, in minutes')} hint={msg('From 5 to 1440. Desk stamps at this interval, only when records were added, and only while it is running.')}>
      {wiring => <Input {...wiring} inputMode="numeric" value={minutes} onChange={event => setMinutes(event.target.value)} />}
    </Field>
    <Field label={msg('Root certificates (PEM)')} hint={msg('The roots you trust for this authority. The decision record checks the stamps against them.')}>
      {wiring => <TextArea {...wiring} rows={4} spellCheck={false} value={roots} onChange={event => setRoots(event.target.value)} />}
    </Field>
    <Field label={msg('Policy OIDs (optional)')} hint={msg('Separated by spaces. Without any, a stamp under any policy is checked.')}>
      {wiring => <Input {...wiring} value={policies} onChange={event => setPolicies(event.target.value)} />}
    </Field>
    <Field label={msg('Revocation lists (optional)')} hint={msg('PEM or DER files. Revocation is checked only where a list you give speaks for a stamp’s time.')}>
      {wiring => <input {...wiring} type="file" multiple onChange={event => { setLists([...event.target.files ?? []]) }} />}
    </Field>
    <div className={styles.actions}>
      <Button ref={opener} type="submit" disabled={busy || !authority.trim() || !roots.trim()}>{msg('Review')}</Button>
      <Button disabled={busy} onClick={onCancel}>{msg('Cancel')}</Button>
    </div>
    {error && <p role="alert">{systemMessage(error)}</p>}
    <Dialog open={!!checked} onOpenChange={value => { if (!busy && !value) setChecked(undefined) }} openerRef={opener}
      title={msg('Set this time-stamping authority?')}
      description={msg('Desk keeps these settings in its own configuration folder, outside the project, and passes the address to jpack audit stamp as --tsa: jpack.json is not changed.')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setChecked(undefined)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Setting…') : msg('Set the authority')}</Button>
      </DialogActions>}>
      <ul className={styles.list} aria-label={msg('What setting an authority means')}>
        <li>{msg('Choosing an authority is a trust decision.')}</li>
        <li>{msg('Each stamp sends this authority the SHA-256 digest of the trail’s checkpoint, a nonce and a request for its certificate, and nothing else of the trail.')}</li>
        <li>{msg('Nothing on the decision path waits for a stamp: a deciding run is recorded at once, and stamped later, while Desk is running.')}</li>
      </ul>
      {checked && <Settings settings={checked.answer.shown} />}
    </Dialog>
  </form>
}
