/**
 * Whether a run stands behind what it produced — asked once, for everybody.
 *
 * Two surfaces act on a proposal: the Assistant tab accepts one into a draft,
 * and the Create dialog writes one to a file. Both have to answer the same
 * question first — *is this run clean?* — and they used to answer it separately.
 * The dialog learned that a failure reported after the terminal event withdraws
 * a proposal; the tab did not, so the same engine sequence produced a document
 * the tab would have written into somebody's draft with no reason shown. A rule
 * kept in two places is two rules.
 *
 * **Three ways a run fails to stand behind a proposal**, and this is the whole
 * list:
 *
 * 1. It never made one.
 * 2. It made one and then put an `error` on the stream. The contract does not
 *    make `proposal` an engine's last non-terminal event, and an engine that
 *    proposes a document and then fails a final check has shown its work and
 *    then said the work does not stand. An error *before* a proposal is not
 *    this: a tool call that failed and was retried is an ordinary run.
 * 3. It made one and then failed **after** its terminal event — a `finally`
 *    that threw, a transport that rejected on close. That cannot go on the
 *    stream, because exactly one `end` is the contract and nothing follows it,
 *    so the run hook reports it beside the stream and this reads it there.
 *
 * What each surface *does* about it is its own: the dialog offers no source to
 * write, and the tab keeps the session on screen with Accept refused and the
 * reason on the control. What may be acted on, and why not, is decided here.
 */
import type { AssistantEvent } from './engine'

export type ProposalEvent = Extract<AssistantEvent, { type: 'proposal' }>

export interface RunOutcome {
  /**
   * The proposal a caller may act on.
   *
   * Undefined where the run made none **or** where it did not stand behind the
   * one it made. A caller that wants to *show* an unactionable proposal reads
   * the event list for itself; this member is the permission, not the payload.
   */
  proposal: ProposalEvent | undefined
  /**
   * Why this run is not one to act on, in its own words, or `''`.
   *
   * Empty for a clean run **and** for a run that simply proposed nothing
   * without failing: "there is nothing here" is not a failure, and a caller
   * that needs a sentence for it supplies its own.
   */
  failure: string
}

export function outcomeOf(run: {
  events: readonly AssistantEvent[]
  /** What the run failed with after its terminal event, where it did. */
  failure?: string | undefined
}): RunOutcome {
  const events = run.events
  const proposedAt = events.findIndex((event) => event.type === 'proposal')
  const said = (from: readonly AssistantEvent[]): string | undefined => {
    const errors = from.filter(
      (event): event is Extract<AssistantEvent, { type: 'error' }> => event.type === 'error'
    )
    return errors[errors.length - 1]?.message
  }
  // Before a proposal exists, every error on the stream is the run's account of
  // itself; after one, only the errors that came after it withdraw it.
  const spoiled =
    run.failure ?? (proposedAt === -1 ? said(events) : said(events.slice(proposedAt + 1)))
  return {
    proposal:
      proposedAt === -1 || spoiled !== undefined ? undefined : (events[proposedAt] as ProposalEvent),
    failure: spoiled ?? ''
  }
}
