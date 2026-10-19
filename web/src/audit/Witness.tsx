/**
 * Admin → Workspace → Decision safeguards → Decision record → Witness
 * (ADR-0012, accepted 2026-10-12; ADR-0010's delivery row 10).
 *
 * A witness is a gateway another party runs, which signs the checkpoints Desk
 * hands it, chains its statements per trail, and serves them to anyone who
 * names the trail. Beside the settings, ADR-0012 section 5's rows, in
 * ADR-0013's words: what a statement establishes and what it does not, and
 * the cost of sending the lines since the cursor when only the last is
 * countersigned. Then this desk's witness, as Desk keeps it: its URL, its
 * public key, the interval, the trail identity the witness's operator
 * registers, and the cursor, the last record the witness countersigned for
 * this desk; or that none is set; or why Desk could not read what it keeps.
 * The submitter's token is pasted once and never shown again.
 *
 * Setting a witness is the owner's action behind a confirmation in the
 * stamping settings' pattern: Desk holds the proposal to its rules, the
 * runtime's key rule among them, and the confirmation states, before anything
 * is kept, that choosing a witness is a trust decision, what each submission
 * sends, that its reads are open, that only the last line of a submission is
 * countersigned, that whoever holds the token can poison the trail there for
 * good, and that the witness's operator registers the trail first, with the
 * trail identity to give them.
 *
 * What the reading found is the runtime's report: where it began, current to
 * the head Desk fetched or historical, as far as the statements were
 * supplied, the latest checkpoint statement, the conflicts and the
 * retirement. Where the witness did not answer as asked, its own reason and
 * sentence, verbatim; Desk's own words are only "Desk could not reach the
 * witness". "Send now" makes one submission, and the decision record is
 * checked again after it. "Check once" asks the runtime once to require the
 * countersigned coverage through a record, and keeps nothing of it.
 */
import { useRef, useState } from 'react'
import { formatDate, msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { checkCountersignedOnce, checkWitness, removeWitness, sendToWitness, setWitness, type AuditRecord, type AuditReport, type AuditWitness, type AuditWitnessHolder, type WitnessAttempt, type WitnessChecked, type WitnessFetch, type WitnessProposal, type WitnessSettings } from './client'
import styles from './DecisionRecord.module.css'

/** What the last witness action asked here answered, kept by the panel across the check run after it. */
export type WitnessOutcome = { kind: 'set' | 'removed' | 'sent' } | { kind: 'failed'; message: string }

/** Desk's own words for a witness that did not answer as asked: these, and no others. */
export const UNREACHED = 'Desk could not reach the witness'

/** Desk's only words for a witness that did not answer as asked, with the HTTP status where there was one. */
function unreached(status?: number): string {
  return status ? `${msg(UNREACHED)} (HTTP ${status}).` : `${msg(UNREACHED)}.`
}

const when = (seconds: number) => formatDate(seconds * 1000, { dateStyle: 'medium', timeStyle: 'medium' })
const messageOf = (cause: unknown) => cause instanceof Error ? cause.message : String(cause)

export function Witness({ witness, report, outcome, onOutcome }: {
  witness?: AuditWitnessHolder
  /** The runtime's report beside it, where there is one: its countersigned coverage and witness section. */
  report?: AuditReport
  outcome?: WitnessOutcome
  /** What an action answered; the decision record is checked again after each. */
  onOutcome: (outcome: WitnessOutcome) => void
}) {
  useLocale()
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [busy, setBusy] = useState(false)
  const remover = useRef<HTMLButtonElement>(null)
  const state = witness?.state ?? 'none'
  const settings = witness?.settings
  async function send() {
    setBusy(true)
    try {
      // The submission's outcome is the decision record's to show, checked again.
      await sendToWitness()
      onOutcome({ kind: 'sent' })
    } catch (cause) {
      onOutcome({ kind: 'failed', message: messageOf(cause) })
    } finally {
      setBusy(false)
    }
  }
  async function remove() {
    if (!witness?.removeToken) return
    setBusy(true)
    try {
      await removeWitness(witness.removeToken)
      onOutcome({ kind: 'removed' })
    } catch (cause) {
      onOutcome({ kind: 'failed', message: messageOf(cause) })
    } finally {
      setBusy(false)
      setRemoving(false)
    }
  }
  return <section aria-label={msg('Witness')}>
    <h4 className={styles.heading}>{msg('Witness')}</h4>
    <div className={styles.card}>
      <WhatAStatementEstablishes />
      <p className={styles.quiet}>{msg('Desk sends the lines after its cursor, ending with the head’s. Only the last line of a submission is countersigned: nothing the witness keeps shows that the lines before it were sent, and the statement for the head covers them through the trail’s own chain.')}</p>
      {outcome?.kind === 'set' && <p role="status">{msg('The witness is set. Desk submits this desk’s new checkpoints to it at the interval, while it is running.')}</p>}
      {outcome?.kind === 'removed' && <p role="status">{msg('The witness is removed. Desk submits nothing more to it; the statements it signed are kept.')}</p>}
      {outcome?.kind === 'failed' && <p role="alert">{systemMessage(outcome.message)}</p>}
      {state === 'none' && <p>{msg('No witness is set for this desk, so Desk submits nothing.')}</p>}
      {(state === 'unread' || state === 'unavailable') && <p role="alert">{systemMessage(witness?.problem ?? '')}</p>}
      {settings && <Settings settings={settings} />}
      {witness?.trail && <CodeBlock text={witness.trail} label={msg('Trail identity, which the witness’s operator registers')} />}
      {settings && <Cursor witness={witness!} />}
      {witness?.passProblem && <p role="alert">{systemMessage(witness.passProblem)}</p>}
      {state !== 'unavailable' && <div className={styles.actions}>
        {state === 'set' && <Button disabled={busy || witness?.running} onClick={() => void send()}>{busy ? msg('Sending…') : msg('Send now')}</Button>}
        <Button disabled={busy} onClick={() => setEditing(true)}>{settings ? msg('Change the witness') : msg('Add a witness')}</Button>
        {witness?.removeToken && <Button ref={remover} disabled={busy} onClick={() => setRemoving(true)}>{msg('Remove the witness')}</Button>}
      </div>}
      {witness?.running && <p role="status">{msg('A submission is in progress.')}</p>}
      {witness?.head && <HeadFetch head={witness.head} />}
      {report?.witness && <Reading section={report.witness} report={report} />}
      {witness?.noStatements && <p className={styles.quiet}>{msg('Desk keeps no statement of the witness for this trail yet, so it passed none to the check: the runtime reads a chain from its first statement.')}</p>}
      {witness?.steps && <p className={styles.quiet}>{msg('Desk keeps more statements than one reading takes: this reading read the earliest, and the next reads on from it.')}</p>}
      {witness?.last ? <LastAttempt attempt={witness.last} /> : state === 'set' && <p className={styles.quiet}>{msg('No submission since Desk started.')}</p>}
      {witness?.setAside && witness.setAside.length > 0 && <ul className={styles.list} aria-label={msg('Files Desk moved aside')}>
        {witness.setAside.map(entry => <li key={entry.name}><code>{entry.name}</code> {systemMessage(entry.reason)}</li>)}
      </ul>}
      {state === 'set' && witness?.passed && <CheckOnce />}
      {editing && <WitnessForm settings={settings} onCancel={() => setEditing(false)} onSet={() => { setEditing(false); onOutcome({ kind: 'set' }) }} />}
    </div>
    <Dialog open={removing} onOpenChange={value => { if (!busy) setRemoving(value) }} openerRef={remover}
      title={msg('Remove this desk’s witness?')}
      description={msg('Desk stops submitting to it, and removes the submitter’s token and the witness’s settings.')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setRemoving(false)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void remove()}>{busy ? msg('Removing…') : msg('Remove the witness')}</Button>
      </DialogActions>}>
      <p>{msg('The statements the witness signed, its key and Desk’s cursor are kept as they are, in the witness’s folder.')}</p>
    </Dialog>
  </section>
}

/** ADR-0012 section 5's rows, in ADR-0013's words: what each part establishes, and what it does not. */
function WhatAStatementEstablishes() {
  const rows: [string, string, string][] = [
    [msg('A checkpoint statement, verified under the pinned key'), msg('the key’s holder was given this checkpoint and signed it as statement index of its chain for the trail, and states that it did so at witnessedAt'), msg('that the checkpoint names a real record; that it extends the one before; that the submitter was the trail’s operator; that witnessedAt is true; anything against a witness that colludes, or whose key is stolen')],
    [msg('The same, held against the trail'), msg('lines 1 to its sequence are the lines that existed when the witness signed, if the witness is independent of the operator'), msg('anything after that sequence; that this is the project’s only trail; that the records are true, or that every decision was recorded')],
    [msg('The chain read to a head Desk fetched'), msg('Desk holds every statement that head commits to, as the witness signed them: none missing, out of order or altered'), msg('that the head is still the witness’s after the fetch; that the witness signed no second chain for another audience; that it will serve these tomorrow')],
    [msg('A conflict statement'), msg('a submitter the witness allowed offered another record for a sequence the witness held'), msg('which of the two is the trail’s; who that submitter was')],
    [msg('A retirement statement'), msg('the witness accepts no more statements for the trail, and the chain ends there'), msg('why; whether the trail’s operator went on under another trail, as it does after a retirement, registered anew')],
    [msg('The submitter’s token'), msg('to the witness, an issuer and a subject; it opens the witness and nothing else'), msg('that its holder is the trail’s operator: a credential is enough to poison a trail for good, by a checkpoint signed at a sequence no honest one will exceed, which revoking the token does not unsign; recovery is retirement, not erasure')],
    [msg('Desk’s cursor and its record of submissions'), '—', msg('anything: they are the operator’s')]
  ]
  return <dl className={styles.facts} aria-label={msg('What a witness establishes')}>
    {rows.map(([part, establishes, not]) => <div key={part}>
      <dt>{part}</dt>
      <dd>{msg('Establishes: {{what}}', { what: establishes })} {msg('Does not establish: {{what}}', { what: not })}</dd>
    </div>)}
  </dl>
}

/** The settings Desk keeps, or would keep. */
function Settings({ settings }: { settings: WitnessSettings }) {
  return <>
    <dl className={styles.facts} aria-label={msg('Witness')}>
      <div><dt>{msg('URL')}</dt><dd><code>{settings.url}</code></dd></div>
      <div><dt>{msg('Interval')}</dt><dd>{msg('{{minutes}} minutes', { minutes: settings.intervalMinutes })}</dd></div>
      {settings.setAt !== undefined && <div><dt>{msg('Set on')}</dt><dd>{when(settings.setAt)}</dd></div>}
    </dl>
    <CodeBlock text={settings.publicKey} label={msg('The witness’s public key, pinned')} />
  </>
}

/** Desk's cursor at the witness, for the trail as it is now. */
function Cursor({ witness }: { witness: AuditWitnessHolder }) {
  const cursor = witness.cursor
  return <>
    {/* **Desk's own record of its submissions, never a claim of what the
        witness countersigned**: that is the runtime's report alone (review
        round 1 of #382, finding 5). */}
    {cursor && cursor.through > 0
      ? <p data-cursor={cursor.through}>{msg('Desk’s record of its submissions: its cursor is at record {{record}}, statement index {{index}}, moved on {{date}} by Desk’s clock.', { record: cursor.through, index: cursor.index, date: when(cursor.at) })}</p>
      : <p data-cursor={0}>{msg('Desk’s record of its submissions: no statement for this trail has moved its cursor yet.')}</p>}
    <p className={styles.quiet}>{msg('The cursor is Desk’s own record, and establishes nothing; how far the witness’s signature reaches is the runtime’s report, in the coverage above.')}</p>
    {cursor?.belowHead !== undefined && cursor.belowHead > 0 && <p>{msg("The witness holds a later checkpoint for this trail, at record {{record}}, so Desk sends again only once the trail's head is past it.", { record: cursor.belowHead })}</p>}
    {cursor?.retired && <p>{msg("The witness retired this trail, so Desk submits nothing more for it. A trail's operator goes on under a new trail, registered anew, which Desk does not make.")}</p>}
  </>
}

/** The decision record's fetch of the head: the witness's words, or Desk's only words. */
function HeadFetch({ head }: { head: WitnessFetch }) {
  switch (head.state) {
    case 'fetched': return <p className={styles.quiet} data-head="fetched">{msg('Desk fetched the witness’s head for this trail, statement index {{index}}, and passed it to the check.', { index: head.index ?? 0 })}</p>
    case 'refused': return <p role="alert" data-head="refused">{msg('The witness answered HTTP {{status}}:', { status: head.status ?? 0 })} <code>{head.reason}</code> <span lang="en">{head.error}</span></p>
    case 'unreached': return <p role="alert" data-head="unreached">{unreached(head.status)}</p>
    case 'withheld': return <p role="alert" data-head="withheld">{msg('The witness answered HTTP {{status}}, and its answer carried a credential, so Desk keeps and shows none of its words.', { status: head.status ?? 0 })}{head.reason && <> <code>{head.reason}</code></>}</p>
  }
}

/** The runtime's reading of the witness's chain, in its own values. */
function Reading({ section, report }: { section: AuditWitness; report: AuditReport }) {
  const latest = section.latestCheckpoint
  return <dl className={styles.facts} aria-label={msg('The runtime’s reading of the witness’s chain')}>
    <div><dt>{msg('Reading')}</dt><dd data-reading={section.reading ?? section.status}>{section.reading === 'current'
      ? msg('Current, to the head Desk fetched, index {{index}}', { index: section.headIndex ?? 0 })
      : section.reading === 'historical' ? msg('Historical: read only as far as it was supplied, with no head')
        : <code>{section.status}</code>}</dd></div>
    <div><dt>{msg('Began')}</dt><dd>{section.began === 'continued' ? msg('After index {{index}}, from Desk’s own continuation', { index: section.continuedAfter ?? 0 }) : msg('At index 0')}</dd></div>
    <div><dt>{msg('Statements read')}</dt><dd>{section.statementsRead}</dd></div>
    {latest && <div><dt>{msg('Latest checkpoint statement')}</dt><dd>{msg('Index {{index}}, record {{sequence}}, stated at {{at}}', { index: latest.index, sequence: latest.sequence, at: latest.witnessedAt })}</dd></div>}
    <div><dt>{msg('Conflict statements')}</dt><dd>{section.conflictsTotal === 0 ? msg('None') : <>{section.conflictsTotal}: <code>{(section.conflicts ?? []).join(', ')}</code></>}</dd></div>
    <div><dt>{msg('Retired')}</dt><dd>{section.retired ? msg('Yes') : msg('No')}</dd></div>
    <div><dt>{msg('Continuation saved')}</dt><dd>{section.continuationSaved ? msg('Yes') : msg('No')}</dd></div>
    {section.saveRefused && <div><dt>{msg('Continuation refused')}</dt><dd lang="en">{section.saveRefused}</dd></div>}
    {report.requiredCountersigned && <div><dt>{msg('Countersigned coverage required')}</dt><dd>{report.requiredCountersigned.through}: <code>{report.requiredCountersigned.status}</code></dd></div>}
  </dl>
}

/** The last submission since Desk started, or the last one kept, by Desk's clock. */
function LastAttempt({ attempt }: { attempt: WitnessAttempt }) {
  const date = when(attempt.at)
  const sent = attempt.through ? <p className={styles.quiet}>{(attempt.first ?? attempt.through) === attempt.through
    ? msg('Sent the checkpoint of record {{record}}.', { record: attempt.through })
    : msg('Sent the checkpoints of records {{first}} to {{through}}, {{lines}} lines.', { first: attempt.first ?? attempt.through, through: attempt.through, lines: attempt.lines ?? 0 })}
    {attempt.unsent ? <> {msg('{{count}} earlier lines since the cursor were left unsent: the statement for the head covers them through the trail’s own chain.', { count: attempt.unsent })}</> : null}</p> : null
  switch (attempt.outcome) {
    case 'countersigned': return <>
      <p data-attempt="countersigned">{msg('Last submission, {{date}} by Desk’s clock: the witness answered a statement for the checkpoint at record {{record}}, statement index {{index}}, which the runtime’s reading with it as the head confirmed, and Desk’s cursor moved to it.', { date, record: attempt.through ?? 0, index: attempt.index ?? 0 })}</p>
      {sent}
    </>
    case 'kept': return <>
      <p data-attempt="kept">{msg('Last submission, {{date}} by Desk’s clock: the witness answered a statement, statement index {{index}}, which Desk keeps.', { date, index: attempt.index ?? 0 })} {systemMessage(attempt.problem ?? '')}</p>
      {attempt.findings && attempt.findings.length > 0 && <ul className={styles.list} aria-label={msg('What the runtime found')}>{attempt.findings.map((finding, index) => <li key={index}><code>{finding.name}</code> <span lang="en">{finding.detail}</span></li>)}</ul>}
      {attempt.diagnostics && <ul className={styles.list} aria-label={msg('What the runtime said')}>{attempt.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>}
      {sent}
    </>
    case 'refused': return <>
      <p data-attempt="refused">{msg('Last submission, {{date}} by Desk’s clock: the witness answered HTTP {{status}}:', { date, status: attempt.status ?? 0 })} <code>{attempt.reason}</code> <span lang="en">{attempt.error}</span></p>
      {sent}
      <p className={styles.quiet}>{msg('Desk’s cursor did not move. Desk tries again at the next interval.')}{retry(attempt)}</p>
    </>
    case 'withheld': return <>
      <p data-attempt="withheld">{msg('Last submission, {{date}} by Desk’s clock: the witness answered HTTP {{status}}, and its answer carried a credential, so Desk keeps and shows none of its words.', { date, status: attempt.status ?? 0 })}{attempt.reason && <> <code>{attempt.reason}</code></>}</p>
      {sent}
      <p className={styles.quiet}>{msg('Desk’s cursor did not move. Desk tries again at the next interval.')}{retry(attempt)}</p>
    </>
    case 'unreached': return <>
      <p data-attempt="unreached">{msg('Last submission, {{date}} by Desk’s clock:', { date })} {unreached(attempt.status)}</p>
      {sent}
      <p className={styles.quiet}>{msg('Desk’s cursor did not move. Desk tries again at the next interval.')}{retry(attempt)}</p>
    </>
    case 'nothing': case 'problem': return <>
      <p data-attempt={attempt.outcome}>{msg('Last submission, {{date}} by Desk’s clock: nothing was sent.', { date })} {systemMessage(attempt.problem ?? '')}</p>
      {attempt.diagnostics && <ul className={styles.list} aria-label={msg('What the runtime said')}>{attempt.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>}
    </>
  }
}

/** The time the witness's Retry-After names, which every submission waits for. */
function retry(attempt: WitnessAttempt) {
  return attempt.retryUntil ? <> {msg('The witness asked Desk to send nothing before {{date}}, by Desk’s clock.', { date: when(attempt.retryUntil) })}</> : null
}

/** The one check the owner asks for: the countersigned coverage required through a record, once, and kept nowhere. */
function CheckOnce() {
  const [through, setThrough] = useState('')
  const [busy, setBusy] = useState(false)
  const [answer, setAnswer] = useState<AuditRecord>()
  const [error, setError] = useState<string>()
  async function check() {
    setBusy(true); setError(undefined); setAnswer(undefined)
    try {
      setAnswer(await checkCountersignedOnce(Number(through.trim())))
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }
  const report = answer?.state === 'report' ? answer.report : undefined
  const missing = report?.findings.filter(finding => finding.name === 'countersigned-coverage-missing') ?? []
  return <form aria-label={msg('Check the countersigned coverage once')} className={styles.card} onSubmit={event => { event.preventDefault(); void check() }}>
    <p className={styles.quiet}>{msg('A reader’s demand, not the operator’s: Desk asks the runtime once to require the countersigned coverage through the record you give, and keeps nothing of it.')}</p>
    <Field label={msg('Countersigned through record')}>
      {wiring => <Input {...wiring} inputMode="numeric" value={through} onChange={event => setThrough(event.target.value)} />}
    </Field>
    <div className={styles.actions}><Button type="submit" disabled={busy || !/^[1-9][0-9]*$/.test(through.trim())}>{busy ? msg('Checking…') : msg('Check once')}</Button></div>
    {error && <p role="alert">{systemMessage(error)}</p>}
    {report?.requiredCountersigned && <p role="status" data-required={report.requiredCountersigned.status}>
      {report.requiredCountersigned.status === 'met'
        ? msg('The runtime found every record through record {{record}} countersigned.', { record: report.requiredCountersigned.through })
        : msg('The runtime did not find every record through record {{record}} countersigned.', { record: report.requiredCountersigned.through })}
      {missing.map((finding, index) => <span key={index} lang="en"> {finding.detail}</span>)}
    </p>}
    {answer && answer.state !== 'report' && <p role="alert">{msg('The runtime did not check the trail.')}</p>}
  </form>
}

/** The owner's proposal: checked by Desk, shown, and kept only on the confirmation. */
function WitnessForm({ settings, onCancel, onSet }: { settings?: WitnessSettings; onCancel: () => void; onSet: () => void }) {
  const [url, setUrl] = useState(settings?.url ?? '')
  const [key, setKey] = useState(settings?.publicKey ?? '')
  const [token, setToken] = useState('')
  const [minutes, setMinutes] = useState(String(settings?.intervalMinutes ?? 60))
  const [checked, setChecked] = useState<{ proposal: WitnessProposal; answer: WitnessChecked }>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const opener = useRef<HTMLButtonElement>(null)
  async function review() {
    setBusy(true); setError(undefined)
    try {
      const proposal: WitnessProposal = { url: url.trim(), publicKey: key, intervalMinutes: /^\d+$/.test(minutes.trim()) ? Number(minutes.trim()) : -1 }
      setChecked({ proposal, answer: await checkWitness(proposal) })
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
      await setWitness(checked.proposal, token, checked.answer.token)
      setChecked(undefined)
      setToken('')
      onSet()
    } catch (cause) {
      setChecked(undefined)
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }
  return <form aria-label={msg('Witness')} className={styles.card} onSubmit={event => { event.preventDefault(); void review() }}>
    <Field label={msg('The witness’s URL')} hint={msg('https, or http to a loopback address. Desk appends the witness’s paths to it.')}>
      {wiring => <Input {...wiring} value={url} autoComplete="off" spellCheck={false} onChange={event => setUrl(event.target.value)} />}
    </Field>
    <Field label={msg('The witness’s public key')} hint={msg('64 hexadecimal characters, from the witness’s operator, out of band: never from the witness it checks. The runtime holds it to its key rule before Desk keeps it.')}>
      {wiring => <Input {...wiring} value={key} autoComplete="off" spellCheck={false} readOnly={!!settings} onChange={event => setKey(event.target.value)} />}
    </Field>
    <Field label={msg('The submitter’s token')} hint={msg('Issued by the witness’s operator for the witness’s audience. Pasted once for each URL you confirm: Desk keeps it in its own custody, outside the project, bound to that URL’s origin, and never shows it again.')}>
      {wiring => <Input {...wiring} type="password" autoComplete="new-password" spellCheck={false} value={token} onChange={event => setToken(event.target.value)} />}
    </Field>
    <Field label={msg('Interval, in minutes')} hint={msg('From 5 to 1440. Desk submits at this interval, only when records were added, and only while it is running.')}>
      {wiring => <Input {...wiring} inputMode="numeric" value={minutes} onChange={event => setMinutes(event.target.value)} />}
    </Field>
    <div className={styles.actions}>
      <Button ref={opener} type="submit" disabled={busy || !url.trim() || !key.trim() || !token.trim()}>{msg('Review')}</Button>
      <Button disabled={busy} onClick={onCancel}>{msg('Cancel')}</Button>
    </div>
    {error && <p role="alert">{systemMessage(error)}</p>}
    <Dialog open={!!checked} onOpenChange={value => { if (!busy && !value) setChecked(undefined) }} openerRef={opener}
      title={msg('Set this witness?')}
      description={msg('Desk keeps the witness’s URL and key with this desk’s record of hand-overs, and the submitter’s token in its own custody, outside the project.')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setChecked(undefined)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Setting…') : msg('Set the witness')}</Button>
      </DialogActions>}>
      <ul className={styles.list} aria-label={msg('What setting a witness means')}>
        <li>{msg('Choosing a witness is a trust decision.')}</li>
        <li>{msg('Each submission sends the witness the trail identity, sequences and record digests, never a record’s contents.')}</li>
        <li>{msg('Its reads are open: whoever holds one of the trail’s checkpoints can follow the trail there.')}</li>
        <li>{msg('Only the last line of a submission is countersigned.')}</li>
        <li>{msg('Whoever holds the token can poison the trail at the witness for good: a checkpoint signed at a sequence no honest one will exceed is not unsigned by revoking the token, and the trail is then retired, not repaired.')}</li>
        <li>{msg('The witness’s operator registers the trail first.')}</li>
      </ul>
      {checked?.answer.trail
        ? <CodeBlock text={checked.answer.trail} label={msg('Trail identity, which the witness’s operator registers')} />
        : <p>{msg('The trail has no chained record yet: its identity is known after the first deciding run.')}</p>}
      {checked && <Settings settings={checked.answer.shown} />}
    </Dialog>
  </form>
}
