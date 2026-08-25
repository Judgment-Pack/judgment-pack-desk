/**
 * One evaluation trace, rendered as the staged walk it is.
 *
 * Two surfaces show a trace and they must show it the same way, because it is
 * the same artifact: `experimental_evaluate` returns the trace of one
 * evaluation (ADR-0027), and `experimental_test_graphs` asked for traces
 * returns each compared node's own trace, "the same disposition, handoff target
 * and trace a standalone evaluation reports" (ADR-0031). A second renderer for
 * the graph side would be a second reading of one contract, free to drift from
 * the first — so there is one, and both call sites use it.
 *
 * **The trace is informative.** It exists so that an unknown resolution ignored
 * stays visible. It decides nothing: the disposition is the answer, and on a
 * graph node comparison the runtime's own `status` is the verdict. Nothing here
 * derives either.
 *
 * **The order is the evaluator's own walk**, and it is never regrouped across
 * itself: a stage that appears twice appears twice, and the numbering stays the
 * trace's. That matters most on the graph surface, where the list of node
 * comparisons is lexicographic by node name while each trace inside one is
 * walk-ordered — two different orders, and neither may be read off the other.
 */
import type { TraceEntry } from '../mcp/types'
import { Pill, Section, slug } from './primitives'

/** One run of consecutive trace entries sharing a stage, keeping their place. */
export interface TraceStage {
  stage: string
  /** Zero-based position of the first entry in the whole trace. */
  offset: number
  entries: TraceEntry[]
}

/**
 * Split the trace into consecutive same-stage runs. The order is the
 * evaluator's own walk, so entries are never regrouped across it: a stage that
 * appears twice appears twice here, and the numbering stays the trace's.
 */
export function stagesOf(trace: TraceEntry[]): TraceStage[] {
  const stages: TraceStage[] = []
  trace.forEach((entry, index) => {
    const open = stages[stages.length - 1]
    if (open && open.stage === entry.stage) {
      open.entries.push(entry)
      return
    }
    stages.push({ stage: entry.stage, offset: index, entries: [entry] })
  })
  return stages
}

/** The trace as the staged walk it is, in the order the payload carries it. */
export function TracePanel({
  trace,
  /** The section heading. The graph surface names the node the trace is of. */
  title = 'Trace',
  /**
   * One further sentence after the standing note, where the call site has a
   * fact the standing note does not carry — the graph side's two orders, for
   * instance. It is added to the framing and never replaces it.
   */
  context,
  /** What "this payload carries no trace entries" should call the payload. */
  emptyWhat = 'This payload'
}: {
  trace: TraceEntry[]
  title?: string
  context?: string
  emptyWhat?: string
}) {
  const entries = trace ?? []
  return (
    <Section title={title} count={entries.length}>
      <p className="note">
        Informative: what the evaluator walked, in order. It decides nothing —
        the disposition above is the answer.
        {context ? ` ${context}` : ''}
      </p>
      {entries.length === 0 ? (
        <p className="empty">{emptyWhat} carries no trace entries.</p>
      ) : (
        <div className="trace">
          {stagesOf(entries).map((stage) => (
            <section className="trace-stage" key={`${stage.stage}:${stage.offset}`}>
              <h4>{stage.stage}</h4>
              <ol className="trace-list" start={stage.offset + 1}>
                {stage.entries.map((entry, index) => (
                  <TraceRow key={`${entry.id ?? stage.stage}:${stage.offset + index}`} entry={entry} />
                ))}
              </ol>
            </section>
          ))}
        </div>
      )}
    </Section>
  )
}

function TraceRow({ entry }: { entry: TraceEntry }) {
  return (
    <li className={entry.skipped ? 'trace-entry trace-entry-skipped' : 'trace-entry'}>
      <span
        className={`verdict verdict-${slug(entry.condition)}`}
        title={`condition: ${entry.condition}`}
      >
        {entry.condition}
      </span>
      <span className="trace-id">
        {entry.id ? (
          <code className="id">{entry.id}</code>
        ) : (
          <em className="quiet">unnamed {entry.stage} condition</em>
        )}
      </span>
      <span className="trace-badges">
        {entry.effect && <Pill>{entry.effect}</Pill>}
        {entry.outcome && <Pill tone="strong">→ {entry.outcome}</Pill>}
        {entry.skipped && <Pill>skipped</Pill>}
        {entry.suppressed && <Pill>suppressed</Pill>}
        {entry.onUnknown && <Pill tone="quiet">on unknown: {entry.onUnknown}</Pill>}
      </span>
    </li>
  )
}
