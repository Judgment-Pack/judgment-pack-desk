/**
 * Admin → Project → Decision record (ADR-0010, sections 4 and 6).
 *
 * What the runtime's own `jpack audit verify` finds in this desk's trail: the
 * status, the coverage, the segments and discontinuities, each finding by
 * name, and the runtime's sentences about what the result establishes and
 * what it does not, verbatim and in English, as Desk shows the runtime's
 * diagnostics. Desk runs it with the public keys it keeps for this desk, if
 * any, the checkpoints it handed over to holders, if any, and the roots of
 * the time-stamping authority the owner set, if any, and the panel says
 * which.
 *
 * Beside it: the public keys Desk keeps, for the owner to hand to a holder,
 * each labelled by its place and the record it signs after; `packs
 * validate`'s word on whether the key named signs this project's records, in
 * the runtime's own sentence; and rotating the key, on the owner's word
 * (ADR-0010, section 1; `RotateSigningKey`); Desk's archive of keys, which
 * holds every key Desk would once have removed, each with why and a Remove on
 * the owner's word (`ArchivedKeys`); and the hand-over of
 * checkpoints to holders, with Desk's own record of it (ADR-0010, section 2;
 * `Handover`). Where the report names the finding `incomplete-last-line`,
 * and only there, the repair, on the owner's word (ADR-0010, section 4;
 * `RepairTrail`). And stamping: the owner's time-stamping authority, kept by
 * Desk, the records pending a stamp, the last stamp run and "Stamp now"
 * (ADR-0010, section 3; `Stamping`).
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
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ON_REQUEST_ONLY } from '../mcp/projectChange'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { SettingsSection } from '../ui/SettingsSection'
import { ArchivedKeys, type ArchiveOutcome } from './ArchivedKeys'
import { archiveOf, AUDIT_KEY, AuditUnavailable, identityOf, readAuditRecord, type AuditArchive, type AuditCoverageState, type AuditIdentity, type AuditKeys, type AuditRecord, type AuditRepair, type AuditReport, type AuditRotation, type AuditSigning, type AuditStamping, type HandedSince } from './client'
import styles from './DecisionRecord.module.css'
import { Handover, NO_HANDOVER, type HandoverState } from './Handover'
import { RepairTrail, type RepairOutcome } from './RepairTrail'
import { ResolveIdentity, type IdentityOutcome } from './ResolveIdentity'
import { RotateSigningKey, type RotationOutcome } from './RotateSigningKey'
import { Stamping, type StampingOutcome } from './Stamping'
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
  // What a rotation answered, kept across the check run after it, which reads
  // the keys afresh: the query is disabled and never stale, so only an
  // explicit run reads them again, and invalidating it would run nothing. The
  // answer goes when the owner checks again or opens the panel again, so it
  // never stands beside a later reading it does not describe.
  const [rotated, setRotated] = useState<RotationOutcome>()
  useEffect(() => { if (visible) setRotated(undefined) }, [visible])
  // What the hand-over's downloads and confirmations answered, kept across
  // the check run after a confirmation, which unmounts the section while it
  // runs; dropped when the panel is opened again.
  const [handover, setHandover] = useState<HandoverState>(NO_HANDOVER)
  useEffect(() => { if (visible) setHandover(NO_HANDOVER) }, [visible])
  // What a repair answered, kept across the check run after it, as a
  // rotation's is, and dropped when the owner checks again or opens the panel
  // again.
  const [repaired, setRepaired] = useState<RepairOutcome>()
  useEffect(() => { if (visible) setRepaired(undefined) }, [visible])
  const repairSection = (repair?: AuditRepair) => <RepairTrail repair={repair} outcome={repaired}
    onOutcome={outcome => { setRepaired(outcome); void query.refetch() }} />
  // What a stamping action answered, kept across the check run after it, as
  // a rotation's is, and dropped when the owner checks again or opens the
  // panel again.
  const [stamped, setStamped] = useState<StampingOutcome>()
  useEffect(() => { if (visible) setStamped(undefined) }, [visible])
  const stampingSection = (stamping?: AuditStamping, report?: AuditReport) => <Stamping stamping={stamping} report={report} outcome={stamped}
    onOutcome={outcome => { setStamped(outcome); void query.refetch() }} />
  const handoverSection = (since?: HandedSince[]) => <Handover checkedAt={query.dataUpdatedAt} since={since} state={handover} onState={setHandover} onConfirmed={() => void query.refetch()} />
  // What the owner's answer on this project's identity did, kept across the
  // check run after it, as a rotation's is, and dropped when the owner checks
  // again or opens the panel again (issue #309).
  const [resolved, setResolved] = useState<IdentityOutcome>()
  useEffect(() => { if (visible) setResolved(undefined) }, [visible])
  const identitySection = (identity?: AuditIdentity) => <ResolveIdentity identity={identity} outcome={resolved}
    onOutcome={outcome => { setResolved(outcome); void query.refetch() }} />
  // What the owner's Remove of an archived key answered, kept across the check
  // run after it, as a rotation's is, and dropped when the owner checks again
  // or opens the panel again.
  const [archived, setArchived] = useState<ArchiveOutcome>()
  useEffect(() => { if (visible) setArchived(undefined) }, [visible])
  const archiveSection = (archive?: AuditArchive) => <ArchivedKeys archive={archive} outcome={archived}
    onOutcome={outcome => { setArchived(outcome); void query.refetch() }} />
  const rotation = (keys?: AuditKeys, rotation?: AuditRotation) => <RotateSigningKey rotation={rotation} keyCount={keys?.state === 'kept' ? keys.public.length : 0}
    outcome={rotated} onOutcome={outcome => { setRotated(outcome); void query.refetch() }} />
  const again = <div><Button onClick={() => { setRotated(undefined); setRepaired(undefined); setStamped(undefined); setResolved(undefined); setArchived(undefined); void query.refetch() }}>{msg('Check again')}</Button></div>
  return <SettingsSection title={msg('Decision record')} description={msg('Jobs runs are recorded by the runner, not in this trail.')} variant="plain">
    <div className={styles.card} data-testid="decision-record">
      {query.isPending || query.isFetching ? <p role="status" className={styles.quiet}>{msg('Asking the runtime…')}</p>
        : query.error instanceof AuditUnavailable ? <><p role="alert">{msg('Desk does not check the decision record here.')} {systemMessage(query.error.message)}</p>{identitySection(identityOf(query.error))}{archiveSection(archiveOf(query.error))}</>
          : query.error ? <><div role="alert" className={styles.card}><p>{systemMessage(query.error.message)}</p><div><Button onClick={() => void query.refetch()} disabled={query.isFetching}>{msg('Retry')}</Button></div></div>{identitySection(identityOf(query.error))}{archiveSection(archiveOf(query.error))}</>
            : record?.state === 'older-runtime' ? <><p>{msg('This runtime (jpack {{version}}) writes an unchained trail and has no audit commands. Chaining, checkpoints, signing and stamping need jpack {{floor}} or later.', { version: record.runtime ?? '?', floor: record.floor })}</p>{identitySection(record.identity)}{archiveSection(record.archive)}</>
              : record?.state === 'no-trail' ? <><p>{msg('This project keeps no trail: its jpack.json declares no audit directory, so the runtime records none of its deciding runs.')}</p>{identitySection(record.identity)}{archiveSection(record.archive)}</>
                : record?.state === 'unverified' ? <>
                  <p>{msg('The runtime did not check the trail.')}</p>
                  <ul className={styles.list} aria-label={msg('What the runtime said')}>{record.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>
                  {record.handoverProblem && <p role="alert">{systemMessage(record.handoverProblem)}</p>}
                  {repairSection()}
                  {identitySection(record.identity)}
                  <SigningKey keys={record.keys} signing={record.signing} />
                  {rotation(record.keys, record.rotation)}
                  {archiveSection(record.archive)}
                  {handoverSection()}
                  {stampingSection(record.stamping)}
                  <TrailDownloads files={record.files ?? []} />
                  {again}
                </>
                  : record?.state === 'report' && <>
                    <p className={styles.statement}>{ranWith(record)}</p>
                    {record.handoverProblem && <p role="alert">{systemMessage(record.handoverProblem)}</p>}
                    {record.expectUnread && <p role="alert">{msg('Desk could not read the checkpoints it keeps as handed over to {{holders}}, so the check ran without them.', { holders: record.expectUnread.join(', ') })}</p>}
                    <Report report={record.report} />
                    {repairSection(record.repair)}
                    {identitySection(record.identity)}
                    <SigningKey keys={record.keys} signing={record.signing} />
                    {rotation(record.keys, record.rotation)}
                    {archiveSection(record.archive)}
                    {handoverSection(record.since)}
                    {stampingSection(record.stamping, record.report)}
                    <TrailDownloads files={record.files ?? []} />
                    {record.runtime && <p className={styles.quiet}>{msg('Checked by jpack {{version}}.', { version: record.runtime })}</p>}
                    {again}
                  </>}
    </div>
  </SettingsSection>
}

/**
 * What Desk says it ran the check with: composed from what it passed to
 * `audit verify`, and from nothing else (line audit, finding 6). Public keys
 * where the keys it keeps were passed (`kept`; never where it keeps none or
 * could not read them); held checkpoints where it passed a holder's file
 * (`expected`); time-stamping roots where it passed them (`passed`). The
 * ADR's sentence (section 4), "with keys and checkpoints you keep", only
 * where both keys and checkpoints were passed; with checkpoints and no key, a
 * sentence that says no public key was passed, as the signing section does.
 */
function ranWith(record: Extract<AuditRecord, { state: 'report' }>): string {
  const keys = record.keys?.state === 'kept'
  const held = (record.expected ?? 0) > 0
  const roots = record.stamping?.passed === true
  if (keys && held) return msg('Desk ran this on your machine, over your trail, with keys and checkpoints you keep. It shows what a holder would see. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.')
  if (keys) {
    return roots
      ? msg('Desk ran this on your machine, over your trail, with the public keys it keeps for this desk, the time-stamping roots you gave and no checkpoints: it checked the signatures against those keys and the stamps against those roots, and no held checkpoint. It is not evidence to anyone who does not trust you: you hold the key. A holder runs the same command on a copy, with what it holds.')
      : msg('Desk ran this on your machine, over your trail, with the public keys it keeps for this desk and no checkpoints: it checked the signatures against those keys, and no held checkpoint and no stamp. It is not evidence to anyone who does not trust you: you hold the key. A holder runs the same command on a copy, with what it holds.')
  }
  if (held) {
    return roots
      ? msg('Desk ran this on your machine, over your trail, with checkpoints you keep, the time-stamping roots you gave and no public key: it checked the trail against those checkpoints and the stamps against those roots, and no signature. It shows what a holder would see. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.')
      : msg('Desk ran this on your machine, over your trail, with checkpoints you keep and no public key: it checked the trail against those checkpoints, and no signature and no stamp. It shows what a holder would see. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.')
  }
  return roots
    ? msg('Desk ran this on your machine, over your trail, with the time-stamping roots you gave and no keys or checkpoints: it checked the stamps against those roots, and no signature and no held checkpoint. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.')
    : msg('Desk ran this on your machine, over your trail, with no keys and no checkpoints: it checked no signature, no held checkpoint and no stamp. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.')
}

/**
 * The keys Desk keeps for this desk, as it passed them, and the runtime's
 * word on the key the project names.
 */
function SigningKey({ keys, signing }: { keys?: AuditKeys; signing?: AuditSigning }) {
  if (!keys && !signing) return null
  return <section aria-label={msg('Signing key')}>
    <h4 className={styles.heading}>{msg('Signing key')}</h4>
    <div className={styles.card}>
      {keys?.state === 'kept' && <>
        <p className={styles.quiet}>{msg('Desk keeps this desk’s signing key in its own configuration folder, outside the project, and passed these public keys to the check, in this order. Hand them to a holder: a holder checks a copy of the trail with jpack audit verify --public-key, one file for each key, in this order.')}</p>
        <p className={styles.quiet}>{msg('A signature establishes that a holder of the key signed these exact bytes. It does not establish anything against the operator, who holds the key; anything after the key is copied; anything against an agent that can read the key; that the trail is complete.')}</p>
        {keys.public.map((key, index) => <CodeBlock key={key.publicKey} text={key.publicKey} label={key.at === 0
          ? msg('Public key {{number}}, keyId {{keyId}}, signing from the first record', { number: index + 1, keyId: key.keyId })
          : msg('Public key {{number}}, keyId {{keyId}}, signing the records after record {{at}}', { number: index + 1, keyId: key.keyId, at: key.at })} />)}
      </>}
      {keys?.state === 'none' && <p>{msg('Desk keeps no signing key for this desk, so it passed no public key.')}</p>}
      {keys?.state === 'startup' && <p>{msg('Desk keeps no signing key for the project it was started on, so it passed no public key.')}</p>}
      {keys?.state === 'unread' && <p role="alert">{systemMessage(keys.problem)}</p>}
      {signing && <SigningCheck signing={signing} />}
    </div>
  </section>
}

/** `packs validate`'s `audit-signing-key` check, with the runtime's own sentence. */
function SigningCheck({ signing }: { signing: AuditSigning }) {
  switch (signing.state) {
    case 'check': {
      const status = { passed: msg('Passed'), failed: msg('Failed'), skipped: msg('Skipped') }[signing.status]
      return <p data-check={signing.status}>{msg('The runtime’s check of the key: {{status}}.', { status })}{signing.detail && <> <span lang="en">{signing.detail}</span></>}</p>
    }
    case 'no-key': return <p>{msg('The runtime’s check of the key: no key is named for this project, so its runtime signs no record.')}</p>
    case 'unread': return <>
      <p>{msg('The runtime did not say whether a key signs this project’s records.')}</p>
      {signing.diagnostics && <ul className={styles.list} aria-label={msg('What the runtime said of the key')}>{signing.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>}
      {signing.problem && <p className={styles.quiet}>{systemMessage(signing.problem)}</p>}
    </>
  }
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

/**
 * The runtime's report, as the panel shows it: its status, coverage,
 * segments, discontinuities, findings, and its own sentences. The Jobs record
 * shows its report the same way (`JobsRecord`).
 */
export function Report({ report }: { report: AuditReport }) {
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
      {report.signatures && <>
        <div><dt>{msg('Key in force at the end of the trail')}</dt><dd><code>{report.signatures.keyInForce}</code></dd></div>
        <div><dt>{msg('Signature lines the runtime could not read')}</dt><dd>{report.signatures.unreadable}</dd></div>
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
