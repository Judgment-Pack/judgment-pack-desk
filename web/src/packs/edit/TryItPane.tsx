/**
 * Running the draft in the editor, without saving it first.
 *
 * `experimental_evaluate` takes `pack` as JSON text **XOR** `pack_id`, so the
 * unsaved buffer can be evaluated as it stands. That is the whole point of the
 * pane: an author trying a rule they are halfway through writing does not have
 * to put it on disk to find out what it does.
 *
 * Four facts about that call, each of which changes what this pane may claim:
 *
 * - **A text pack never touches the reviewed set.** `applied` is built only
 *   where a `pack_id` was supplied, and the consult is gated on `applied`
 *   being non-empty, so a draft run is `lock.DraftRun` — never refused for
 *   being unlocked, and proving nothing about a recorded decision.
 * - **The audit writer runs for every call, including a text pack.** Only
 *   `rehearsal: true` suppresses the record. So the declaration is sent
 *   wherever the connected runtime advertises the argument, and where it does
 *   **not**, no draft is probed silently: the pane says the run would be
 *   recorded in a project that declares an audit directory, and asks for a
 *   second, explicit click.
 * - **Preflight refuses a draft that is not conformant**, by class and phase,
 *   with no disposition. Mid-edit that is the ordinary answer rather than an
 *   error, and it is rendered as the runtime's own answer.
 * - **The payload echoes the pack document's own `id`**, which is a URI, not
 *   the project's decision id. The foot prints what the payload carries; the
 *   desk substitutes nothing.
 *
 * The result is stale the moment the buffer moves, and says so. A disposition
 * is the answer to the bytes that were sent, and there is no way to tell from
 * a disposition which of the bytes since then it would still be the answer to.
 */
import { useMemo, useState } from 'react'
import { EvaluationView } from '../../components/EvaluationView'
import { RefusalPanel } from '../../components/RefusalPanel'
import { Button } from '../../ui/Button'
import { SegmentedControl } from '../../ui/SegmentedControl'
import { TextArea } from '../../ui/TextArea'
import { useEvaluate } from '../../mcp/queries'
import type { EvaluationRun } from '../../mcp/types'
import { valueAt } from '../pointers'
import styles from './TryItPane.module.css'

/** What one declared requirement may be said to be. */
const AVAILABILITY = ['present', 'absent', 'unknown'] as const
type Availability = (typeof AVAILABILITY)[number]

export function TryItPane({
  buffer,
  packId,
  rehearsalSupported,
  connected,
  onClose
}: {
  /** The bytes in the editor, which are what a draft run sends. */
  buffer: string
  /** The project's decision id, for the other source. */
  packId: string
  /** Whether this runtime advertises the `rehearsal` argument. */
  rehearsalSupported: boolean
  connected: boolean
  onClose: () => void
}) {
  const evaluate = useEvaluate()
  const [source, setSource] = useState<'pack' | 'pack_id'>('pack')
  const [facts, setFacts] = useState('{}')
  const [availability, setAvailability] = useState<Record<string, Availability>>({})
  const [ran, setRan] = useState<{ run: EvaluationRun; bytes: string } | null>(null)
  // The second click, where the runtime does not take the declaration. It is
  // armed by the first and disarmed by anything that changes what would be
  // sent, so a confirmation cannot outlive the thing it confirmed.
  //
  // **It remembers the bytes rather than being a flag.** The buffer is most of
  // what would be sent and it is the one input this pane does not own: the
  // author is typing in the editor beside it, and a flag stayed armed through
  // every keystroke — so the second press recorded a run over bytes nobody was
  // asked about. Comparing the bytes makes the confirmation about the thing it
  // confirmed; the three handlers below still clear it, because the facts, the
  // evidence and the source are sent too and none of them is in `buffer`.
  const [armedFor, setArmedFor] = useState<string | null>(null)
  const armed = armedFor !== null && armedFor === buffer

  /**
   * The requirements the **draft** declares, not the served pack's.
   *
   * A requirement added in the editor has to appear, and one deleted has to
   * stop appearing — a row that survived its own deletion would send an
   * evidence key naming a requirement the pack no longer has.
   */
  const requirements = useMemo(() => draftRequirements(buffer), [buffer])

  const factsError = jsonError(facts)
  const stale = ran !== null && ran.bytes !== buffer

  const evidence = useMemo(() => {
    // Only the rows the author actually set, and only for requirements the
    // draft still declares. Every other requirement is unknown, which is what
    // omitting it means, so there is nothing to send for it.
    const stated: Record<string, Availability> = {}
    for (const id of requirements) {
      const said = availability[id]
      if (said !== undefined) stated[id] = said
    }
    return Object.keys(stated).length === 0 ? undefined : JSON.stringify(stated)
  }, [availability, requirements])

  const needsConfirmation = source === 'pack' && !rehearsalSupported
  const runnable = connected && factsError === null && !evaluate.isPending

  const run = () => {
    if (!runnable) return
    if (needsConfirmation && !armed) {
      setArmedFor(buffer)
      return
    }
    setArmedFor(null)
    const bytes = buffer
    evaluate.mutate(
      source === 'pack'
        ? { source: 'pack', pack: bytes, facts, evidence }
        : { source: 'pack_id', packId, facts, evidence },
      { onSuccess: (completed) => setRan({ run: completed, bytes }) }
    )
  }

  return (
    <aside className={styles.pane} aria-label="Try it">
      <div className={styles.head}>
        <h2 className={styles.heading}>Try it</h2>
        <Button variant="quiet" onClick={onClose}>
          Close
        </Button>
      </div>

      <SegmentedControl
        label="What to run"
        value={source}
        onValueChange={(next) => {
          setSource(next === 'pack' ? 'pack' : 'pack_id')
          setArmedFor(null)
        }}
        segments={[
          { value: 'pack', label: 'these edits' },
          { value: 'pack_id', label: 'the saved pack' }
        ]}
      />

      <p className={styles.honesty}>
        {source === 'pack'
          ? rehearsalSupported
            ? 'Runs the unsaved draft, declared a rehearsal. Nothing is saved, nothing is recorded, and no reviewed set is consulted.'
            : 'Runs the unsaved draft. Nothing is saved and no reviewed set is consulted — but this runtime does not take the rehearsal declaration, so in a project that declares an audit directory this run appends one record.'
          : 'Runs the pack on disk, not these edits.'}
      </p>

      <label className={styles.label} htmlFor="tryit-facts">
        Facts
      </label>
      <TextArea
        id="tryit-facts"
        rows={6}
        value={facts}
        spellCheck={false}
        onChange={(event) => {
          setFacts(event.target.value)
          setArmedFor(null)
        }}
      />
      <p className={styles.status}>{factsError ?? 'valid JSON'}</p>

      {requirements.length > 0 && (
        <>
          <p className={styles.label}>Evidence</p>
          <div className={styles.rows}>
            {requirements.map((id) => (
              <div className={styles.row} key={id}>
                <code className={styles.rowId}>{id}</code>
                <SegmentedControl
                  label={id}
                  value={availability[id] ?? 'unknown'}
                  onValueChange={(next) => {
                    setAvailability((was) => ({ ...was, [id]: next as Availability }))
                    setArmedFor(null)
                  }}
                  segments={AVAILABILITY.map((word) => ({ value: word, label: word }))}
                />
              </div>
            ))}
          </div>
        </>
      )}

      <div className={styles.actions}>
        <Button variant="primary" disabled={!runnable} onClick={run}>
          {evaluate.isPending ? 'Running…' : armed ? 'Run and record it' : 'Run'}
        </Button>
        {!connected && <span className={styles.status}>waiting for the runtime connection</span>}
      </div>
      {armed && (
        <p className={styles.confirm} role="status">
          This runtime does not take the rehearsal declaration. In a project whose{' '}
          <code>jpack.json</code> declares an audit directory, running this appends one record to
          it. Press again to run it anyway.
        </p>
      )}

      {evaluate.error && <RefusalPanel error={evaluate.error} title="The runtime refused this run" />}

      {ran !== null && (
        <div className={styles.result}>
          <p className={styles.resultHead}>
            <span className={styles.pill}>
              {ran.run.payload.rehearsal === true ? 'rehearsal' : 'recorded where audit is declared'}
            </span>
            <span className={styles.pill}>
              {source === 'pack' ? 'from the draft in the editor' : 'from the pack on disk'}
            </span>
          </p>
          {stale && (
            <p className={styles.staleNote} role="status">
              The editor has moved since this ran. This is the answer to the bytes that were sent,
              and nothing here is about the bytes on screen now.
            </p>
          )}
          <EvaluationView payload={ran.run.payload} />
          <p className={styles.foot}>
            <code>{ran.run.payload.packId}</code>
            <span>packVersion {ran.run.payload.packVersion}</span>
          </p>
        </div>
      )}
    </aside>
  )
}

/**
 * The evidence requirement ids the buffer declares.
 *
 * Read out of the bytes on screen rather than out of the served document, and
 * read defensively: mid-edit the buffer may not scan, may hold a
 * `evidenceRequirements` that is not an array, or may hold entries with no
 * `id`. None of those is an error here — it is a document being written — so
 * what cannot be read contributes no row.
 */
export function draftRequirements(buffer: string): string[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(buffer)
  } catch {
    return []
  }
  const declared = valueAt(parsed, '/evidenceRequirements')
  if (!Array.isArray(declared)) return []
  return declared
    .map((entry) =>
      typeof entry === 'object' && entry !== null
        ? (entry as { id?: unknown }).id
        : undefined
    )
    .filter((id): id is string => typeof id === 'string' && id !== '')
}

function jsonError(text: string): string | null {
  try {
    JSON.parse(text)
    return null
  } catch (cause) {
    return cause instanceof Error ? cause.message : String(cause)
  }
}
