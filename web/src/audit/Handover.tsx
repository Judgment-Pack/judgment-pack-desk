/**
 * Admin → Project → Decision record → Hand-over (ADR-0010, section 2; the
 * maintainer's answer to question 4: download or copy first).
 *
 * The holders the owner added, each by a label and a channel of their own
 * words, and what Desk recorded as handed over to each for the trail as the
 * runtime gives it now: through which record, when by Desk's clock, the
 * SHA-256 of what was handed over, and what follows it. That is the chained
 * records since, as the decision record's report counts them, where the
 * holder's checkpoints were passed to it and the report says how many; and
 * otherwise the lines since, said as lines: a line a repair names as damaged
 * is not a record (line audit, finding 7). Beside the list, the ADR's
 * sentence: this is Desk's own record, and proves nothing to anyone; only the
 * holder's own copy counts.
 *
 * "Download checkpoints" saves the checkpoints after the holder's cursor as
 * the bytes Desk served, kept as a Blob from the response to the saved file,
 * under the name the answer gave: nothing here reads them as text. A download
 * moves nothing. The owner then sends the file by any channel the holder
 * keeps, and confirms that it went to the holder; Desk records it only where
 * the trail still gives those bytes, and otherwise says the file is stale.
 * After a confirmation the holders and the decision record are read again: the
 * check then runs with what was handed over.
 *
 * Where this desk has a Runner, each holder shows two rows: the decision
 * record's, and Runner's chain of runs (ADR-0010, section 5), each with
 * what Desk recorded of it, its own download and its own confirmation. The
 * chain's row says where Runner is not running, where no run is chained yet,
 * and where Desk could not read the chain, and then offers nothing; a
 * confirmation of the chain checks the Jobs record again.
 *
 * The query runs when the section is shown, with each reading of the
 * decision record, and after an action: never on a timer, on focus, on a
 * reconnect or on a change to the project. What a
 * download or a confirmation answered is kept by the panel, which unmounts
 * this section while it checks the trail again.
 */
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ON_REQUEST_ONLY } from '../mcp/projectChange'
import { formatDate, msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { addHolder, checkJobsRecordAgain, confirmHandover, downloadCheckpoints, HOLDERS_KEY, readHolders, StaleHandover, type Checkpoints, type HandedSince, type HandoverChain, type Holder, type HolderTrail, type HandoverTrail, type JobsChain } from './client'
import styles from './DecisionRecord.module.css'

/** A download waiting for the owner's word that it went to the holder. */
export type PendingHandover = Omit<Checkpoints, 'blob'>
/** What the last download or confirmation for a holder answered. */
export type HandoverNotice =
  | { kind: 'nothing-new' | 'stale' }
  | { kind: 'recorded'; through: number }
  | { kind: 'failed'; message: string }
/**
 * Each holder's download waiting for a confirmation, and what its last action
 * answered, by row: the holder's id for the decision record's, and the id and
 * ":jobs" for the chain of runs' (`rowKey`).
 */
export type HandoverState = { pending: Record<string, PendingHandover>; notices: Record<string, HandoverNotice> }
export const NO_HANDOVER: HandoverState = { pending: {}, notices: {} }

/** The key of a holder's row for a chain, in HandoverState. */
export const rowKey = (holder: string, chain: HandoverChain) => chain === 'jobs' ? `${holder}:jobs` : holder

/**
 * The section's one query: run each time it is shown, each time the decision
 * record beside it is read (`checkedAt`, when it was), and after an action,
 * and at no other time.
 */
export function useHolders(checkedAt: number) {
  const query = useQuery({
    queryKey: HOLDERS_KEY,
    queryFn: ({ signal }) => readHolders(signal),
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
  useEffect(() => { void refetch.current({ cancelRefetch: false }) }, [checkedAt])
  return query
}

/** Save the bytes as served, under the name the answer gave, as the trail's own downloads are saved. */
function save(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob), link = document.createElement('a')
  link.href = url; link.download = name
  document.body.append(link); link.click(); link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

const when = (seconds: number) => formatDate(seconds * 1000, { dateStyle: 'medium', timeStyle: 'short' })
const messageOf = (cause: unknown) => cause instanceof Error ? cause.message : String(cause)

export function Handover({ checkedAt, since, state, onState, onConfirmed }: {
  /** When the decision record beside this was last read: the holders are read again with it. */
  checkedAt: number
  /** What the decision record's report says follows the last record handed over to each holder whose checkpoints it was given. */
  since?: HandedSince[]
  state: HandoverState
  onState: (update: (state: HandoverState) => HandoverState) => void
  /** Run the decision record's check again: it runs with what was handed over. */
  onConfirmed: () => void
}) {
  useLocale()
  const client = useQueryClient()
  const query = useHolders(checkedAt)
  const [busy, setBusy] = useState<string>()
  const data = query.data
  const set = (holder: string, pending: PendingHandover | undefined, notice: HandoverNotice | undefined) => onState(previous => {
    const next = { pending: { ...previous.pending }, notices: { ...previous.notices } }
    if (pending) next.pending[holder] = pending
    else delete next.pending[holder]
    if (notice) next.notices[holder] = notice
    else delete next.notices[holder]
    return next
  })
  async function download(holder: Holder, chain: HandoverChain) {
    const key = rowKey(holder.id, chain)
    setBusy(key)
    try {
      const checkpoints = await downloadCheckpoints(holder.id, chain)
      if (!checkpoints) return set(key, undefined, { kind: 'nothing-new' })
      const { blob, ...named } = checkpoints
      save(blob, named.name)
      set(key, named, undefined)
    } catch (cause) {
      set(key, undefined, { kind: 'failed', message: messageOf(cause) })
    } finally {
      setBusy(undefined)
    }
  }
  async function confirm(holder: Holder, pending: PendingHandover) {
    const key = rowKey(holder.id, pending.chain)
    setBusy(key)
    try {
      await confirmHandover(holder.id, pending)
      set(key, undefined, { kind: 'recorded', through: pending.through })
      // The decision record is read again, and the holders with it.
      onConfirmed()
      // A hand-over of the chain of runs: the Jobs record is checked again.
      if (pending.chain === 'jobs') void checkJobsRecordAgain(client)
    } catch (cause) {
      if (cause instanceof StaleHandover) set(key, undefined, { kind: 'stale' })
      else set(key, pending, { kind: 'failed', message: messageOf(cause) })
    } finally {
      setBusy(undefined)
    }
  }
  return <section aria-label={msg('Hand-over')}>
    <h4 className={styles.heading}>{msg('Hand-over')}</h4>
    <div className={styles.card}>
      <p className={styles.quiet}>{msg("This is Desk's own record. You keep it, and you can change it, so it proves nothing to a holder or to anyone else. Only the holder's own copy counts.")}</p>
      <p className={styles.quiet}>{msg('A held checkpoint establishes that the records up to it are the ones that existed when it was handed over, against an operator who does not hold the holder’s copy. It does not establish anything after it; that the holder kept every checkpoint; when it was made or handed over.')}</p>
      <p className={styles.quiet}>{msg('Download a holder’s checkpoints and send the file by any channel the holder keeps: an e-mail attachment, a ticket, or a folder the holder controls. Then confirm that it went to them.')}</p>
      {query.isPending || (query.isFetching && !data) ? <p role="status" className={styles.quiet}>{msg('Asking the runtime…')}</p>
        : query.error ? <div role="alert" className={styles.card}><p>{systemMessage(query.error.message)}</p><div><Button onClick={() => void query.refetch()}>{msg('Retry')}</Button></div></div>
          : data && <>
            {data.trail === null && (data.diagnostics ? <>
              <p>{msg('The runtime gives no checkpoint of this trail now, so nothing can be handed over.')}</p>
              <ul className={styles.list} aria-label={msg('What the runtime said')}>{data.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>
            </> : <p>{msg('The trail has no chained record yet, so there is nothing to hand over.')}</p>)}
            {data.jobs?.state === 'no-runner' && <p className={styles.quiet}>{msg('This desk has no Runner, so it has no chain of runs to hand over.')}</p>}
            {data.holders.length === 0 ? <p>{msg('No holder yet.')}</p>
              : <ul className={styles.list} aria-label={msg('Holders')}>{data.holders.map(holder => <HolderItem key={holder.id} holder={holder}
                trail={data.trail} since={since} jobs={data.jobs?.state === 'no-runner' ? undefined : data.jobs} state={state} busy={busy !== undefined}
                onDownload={chain => void download(holder, chain)} onConfirm={pending => void confirm(holder, pending)} />)}</ul>}
          </>}
      <AddHolder onAdded={() => void query.refetch()} />
    </div>
  </section>
}

/** The record Desk shows for a holder of one chain: the current identity's, or with none current the last confirmed. */
function shownRecord(entries: Record<string, HolderTrail>, trail: HandoverTrail | null): HolderTrail | undefined {
  if (trail) return entries[trail.identity]
  return Object.values(entries).sort((a, b) => b.confirmedAt - a.confirmedAt)[0]
}

/** What follows the last record handed over to a holder: chained records, as a report counts them, or lines. */
type Since = { records: number } | { lines: number }

/**
 * What the row says follows the record handed over: what the decision
 * record's report says for this holder, this trail and this very record,
 * where it says it; and otherwise the lines Desk's record of hand-overs gives.
 */
function sinceOf(record: HolderTrail | undefined, counted: HandedSince | undefined): Since | undefined {
  if (counted?.records !== undefined) return { records: counted.records }
  if (counted?.lines !== undefined) return { lines: counted.lines }
  return record?.linesSince !== undefined ? { lines: record.linesSince } : undefined
}

/** A holder: the decision record's row, and, where this desk has a Runner, the chain of runs' row beside it. */
function HolderItem({ holder, trail, since, jobs, state, busy, onDownload, onConfirm }: {
  holder: Holder
  trail: HandoverTrail | null
  since?: HandedSince[]
  /** Runner's chain of runs as it is now; undefined where this desk has no Runner. */
  jobs?: JobsChain
  state: HandoverState
  busy: boolean
  onDownload: (chain: HandoverChain) => void
  onConfirm: (pending: PendingHandover) => void
}) {
  const head = jobs?.state === 'chain' ? jobs.chain : null
  const record = shownRecord(holder.trails, trail)
  const counted = record && trail ? since?.find(item => item.holder === holder.id && item.trail === trail.identity && item.through === record.through) : undefined
  const jobsRecord = shownRecord(holder.jobs ?? {}, head)
  return <li aria-label={holder.label}>
    <div className={styles.card}>
      <p><strong>{holder.label}</strong> <span className={styles.quiet}>{holder.channel}</span></p>
      <ChainRow holder={holder} title={jobs ? msg('Decision record') : undefined} trail={trail} record={record} since={sinceOf(record, counted)}
        moved={holder.otherTrail ? msg('The trail was moved aside since: this holder starts at 0 for the new trail') : undefined}
        pending={state.pending[rowKey(holder.id, 'trail')]} notice={state.notices[rowKey(holder.id, 'trail')]} busy={busy}
        onDownload={() => onDownload('trail')} onConfirm={onConfirm} />
      {jobs && <ChainRow holder={holder} title={msg('Jobs runs')} trail={head} record={jobsRecord} since={sinceOf(jobsRecord, undefined)}
        moved={holder.otherJobsChain ? msg('The chain of runs has another identity now: this holder starts at 0 for it') : undefined}
        pending={state.pending[rowKey(holder.id, 'jobs')]} notice={state.notices[rowKey(holder.id, 'jobs')]} busy={busy}
        onDownload={() => onDownload('jobs')} onConfirm={onConfirm}>
        <JobsChainWords chain={jobs} />
      </ChainRow>}
    </div>
  </li>
}

/** Why the chain of runs' row offers nothing, where it does not. */
function JobsChainWords({ chain }: { chain: JobsChain }) {
  switch (chain.state) {
    case 'not-running': return <p>{msg('Runner is not running on this desk now, so Desk could not read its chain of runs.')}</p>
    case 'empty': return <p>{msg('No run is chained yet, so there is nothing to hand over.')}</p>
    case 'unread': return <>
      <p>{msg('Desk could not read the runner’s chain of runs now, so nothing can be handed over from it.')}</p>
      {chain.diagnostics && <ul className={styles.list} aria-label={msg('What the runtime said')}>{chain.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>}
      {chain.problem && <p className={styles.quiet}>{systemMessage(chain.problem)}</p>}
    </>
    default: return null
  }
}

/**
 * One chain's row for a holder: what Desk recorded of it, its download, a
 * download waiting for its confirmation, and what its last action answered.
 * Titled, and a group of its own, where the holder shows two.
 */
function ChainRow({ holder, title, trail, record, since, moved, pending, notice, busy, onDownload, onConfirm, children }: {
  holder: Holder
  title?: string
  /** The chain as it is now: null where it has no chained record, or was not read. */
  trail: HandoverTrail | null
  record?: HolderTrail
  /** What follows the record handed over: records where a report counts them, lines otherwise. */
  since?: Since
  /** Where Desk's record names only another identity of the chain, what to say of it. */
  moved?: string
  pending?: PendingHandover
  notice?: HandoverNotice
  busy: boolean
  onDownload: () => void
  onConfirm: (pending: PendingHandover) => void
  children?: ReactNode
}) {
  return <div role={title ? 'group' : undefined} aria-label={title}>
    <div className={styles.card}>
      {title && <h5 className={styles.heading}>{title}</h5>}
      {children}
      {record ? <>
        <p>{msg('Handed over through record {{through}} on {{date}}', { through: record.through, date: when(record.confirmedAt) })}</p>
        <CodeBlock text={record.digest} label={msg('SHA-256 of what was handed over')} />
        {trail && since && ('records' in since ? <p>{msg('Records since: {{number}}', { number: since.records })}</p>
          : <p>{msg('Lines since: {{number}}', { number: since.lines })}</p>)}
      </> : moved ? <p>{moved}</p>
        : <p>{msg('Nothing handed over yet')}</p>}
      {trail && <div className={styles.actions}><Button disabled={busy} onClick={onDownload}>{msg('Download checkpoints')}</Button></div>}
      {pending && <div className={styles.card}>
        <p>{msg('Saved {{file}}: the checkpoints after record {{from}}, through record {{through}}.', { file: pending.name, from: pending.from, through: pending.through })}</p>
        <CodeBlock text={pending.digest} label={msg('SHA-256 of the file')} />
        {pending.more && <p className={styles.quiet}>{msg('More records remain: download again once this is confirmed.')}</p>}
        <p>{msg('Confirm: the file went to {{holder}}', { holder: holder.label })}</p>
        <div className={styles.actions}><Button variant="primary" disabled={busy} onClick={() => onConfirm(pending)}>{msg('Confirm')}</Button></div>
      </div>}
      {notice?.kind === 'nothing-new' && <p role="status">{msg('Nothing new to hand over to this holder.')}</p>}
      {notice?.kind === 'recorded' && <p role="status">{msg('Recorded: the file went to {{holder}}, through record {{through}}.', { holder: holder.label, through: notice.through })}</p>}
      {notice?.kind === 'stale' && <p role="alert">{msg('What you downloaded is not what the trail gives now. Download it again and hand over that file.')}</p>}
      {notice?.kind === 'failed' && <p role="alert">{systemMessage(notice.message)}</p>}
    </div>
  </div>
}

/** A holder, added by the owner's label and channel. */
function AddHolder({ onAdded }: { onAdded: () => void }) {
  const [label, setLabel] = useState('')
  const [channel, setChannel] = useState('')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string>()
  async function add() {
    setAdding(true); setError(undefined)
    try {
      await addHolder(label, channel)
      setLabel(''); setChannel('')
      onAdded()
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setAdding(false)
    }
  }
  return <form aria-label={msg('Add holder')} className={styles.card} onSubmit={event => { event.preventDefault(); void add() }}>
    <Field label={msg('Label')} hint={msg('Who holds the checkpoints, in your words.')}>
      {wiring => <Input {...wiring} value={label} placeholder={msg('Counterparty: procurement desk')} onChange={event => setLabel(event.target.value)} />}
    </Field>
    <Field label={msg('Channel')} hint={msg('How the file reaches them.')}>
      {wiring => <Input {...wiring} value={channel} placeholder={msg('e-mail to records@…')} onChange={event => setChannel(event.target.value)} />}
    </Field>
    <div className={styles.actions}><Button type="submit" disabled={adding || !label.trim() || !channel.trim()}>{msg('Add holder')}</Button></div>
    {error && <p role="alert">{systemMessage(error)}</p>}
  </form>
}
