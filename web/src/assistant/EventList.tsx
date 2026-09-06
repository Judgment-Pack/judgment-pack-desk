/**
 * What the assistant did, as one line per thing it did.
 *
 * Factored out of the Assistant tab because the Create dialog's **Describe it**
 * section shows the same stream, and a second rendering of one contract is a
 * second vocabulary: the tab would say `rewrote experimental_evaluate` and the
 * dialog would say something almost the same, and a reader comparing the two
 * would be comparing two people's prose about one event.
 *
 * **A tool result is reported as its byte count and the runtime's own
 * `isError`, never as a reading of what it said.** This list has no opinion
 * about a report it did not write; the two answers that matter are quoted whole
 * beside the proposal (`ProposalReport`).
 *
 * **Compact is a filter, not a summary.** The dialog has less room than the
 * tab, so it carries the events a person creating a pack can act on — what was
 * called, what the desk's own guards did, what failed, and that it ended — and
 * leaves out the model's reasoning and its critique, which are about how the
 * answer was reached rather than about what was asked of the runtime. Nothing
 * is condensed and no count replaces a line: an event either has a line or is
 * not in this list.
 */
import type { AssistantEvent } from './engine'
import styles from './AssistantPane.module.css'

/** The bytes of one tool answer, said the way the desk says byte counts. */
function byteCount(text: string): number {
  return new TextEncoder().encode(text).length
}

/** What the compact list carries. See the module doc for why these. */
const COMPACT: ReadonlySet<AssistantEvent['type']> = new Set([
  'tool_call',
  'tool_result',
  'guardrail',
  'thinking_unavailable',
  'proposal',
  'error',
  'end'
])

/**
 * One event as one line.
 *
 * Exported because the pane's own tests read it, and because a caller building
 * a line elsewhere must not write a second sentence for an event this file
 * already has one for.
 */
export function describeEvent(event: AssistantEvent): string {
  switch (event.type) {
    case 'tool_call':
      return `called ${event.name}(${Object.keys((event.args ?? {}) as object).join(', ')})`
    case 'tool_result':
      return `${event.name} answered ${byteCount(event.text)} bytes${
        event.isError ? ' (isError)' : ''
      }${event.structured === undefined ? '' : ' with structured content'}`
    case 'guardrail':
      return `${event.action} ${event.tool}: ${event.detail}`
    case 'thinking_unavailable':
      return event.detail
    case 'reasoning':
      return `${event.text.length} characters of reasoning`
    case 'critique':
      return event.text
    case 'proposal':
      return `proposed a document with ${event.unknowns.length} unknown(s); nothing was written`
    case 'error':
      return event.message
    case 'end':
      return 'the session ended'
  }
}

/** Which of the three colours a line takes. */
export function lineClass(event: AssistantEvent): string {
  if (event.type === 'guardrail' || event.type === 'thinking_unavailable') return styles.guard!
  if (event.type === 'error') return styles.error!
  return styles.line!
}

export function EventList({
  events,
  failure,
  label = 'What the assistant did',
  compact = false
}: {
  events: readonly AssistantEvent[]
  /**
   * What the run failed with **after** its terminal event, where it did.
   *
   * It cannot be on `events` — one `end` is the contract and nothing follows
   * it — so it is passed beside them and printed as the list's last line. A
   * session that fell over while unwinding otherwise reads as one that ended
   * cleanly, which is exactly what it did not do.
   */
  failure?: string | undefined
  /** The list's accessible name. Two of these can be on one page. */
  label?: string
  compact?: boolean
}) {
  /**
   * The events this list has a line for.
   *
   * **One line per reasoning passage, not one per delta.** An engine reports
   * reasoning as it arrives — the contract's `done` is what marks a passage
   * finished — and a model that reasons for a paragraph would otherwise fill
   * this list with a hundred lines saying how many characters had arrived so
   * far. The deltas stay on the run's event list for a fold to read; what is
   * rendered here is the passage.
   */
  const reported = events
    .map((event, index) => ({ event, index }))
    .filter(({ event }) =>
      compact ? COMPACT.has(event.type) : event.type !== 'reasoning' || event.done
    )
  if (reported.length === 0 && failure === undefined) return null
  return (
    <ol
      className={[styles.stream, compact ? styles.compact : undefined].filter(Boolean).join(' ')}
      aria-label={label}
    >
      {reported.map(({ event, index }) => (
        <li key={index} className={lineClass(event)}>
          {/*
            **Reasoning is collapsed, with its count, and opens on a click.**
            The line says how much of it there was — which is what a reader
            scanning a session wants — and the passage itself is one disclosure
            away, because a model's account of its own reasoning is worth
            reading and is not worth a hundred lines of the stream. Nothing is
            summarised: what opens is the passage as the endpoint wrote it.
          */}
          {event.type === 'reasoning' ? (
            <details>
              <summary>{describeEvent(event)}</summary>
              <pre className={styles.reasoning}>{event.text}</pre>
            </details>
          ) : (
            describeEvent(event)
          )}
        </li>
      ))}
      {failure !== undefined && (
        <li key="failure" className={styles.error}>
          the session failed after it ended: {failure}
        </li>
      )}
    </ol>
  )
}
