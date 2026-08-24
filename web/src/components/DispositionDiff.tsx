import type { Evaluation } from '../mcp/types'
import { Section } from './primitives'

/**
 * What changed in the disposition between two runs of the same pack.
 *
 * Only the §8.3 disposition members are compared, plus the handoff target the
 * runtime reports beside one — those are what a what-if question is asking
 * about. The trace is not diffed: it is informative, and a trace that moved
 * while the disposition held is not a change in the answer.
 *
 * Both sides are the runtime's own words. Nothing here is derived beyond
 * "these two strings differ" and "this reason is in one set and not the other".
 */
export function DispositionDiff({
  previous,
  current
}: {
  previous: Evaluation
  current: Evaluation
}) {
  const rows: DiffRow[] = [
    scalarRow('Kind', previous.disposition?.kind, current.disposition?.kind),
    scalarRow('Outcome id', previous.disposition?.outcomeId, current.disposition?.outcomeId),
    setRow('Reasons', previous.disposition?.reasons, current.disposition?.reasons),
    scalarRow('Handoff state', previous.disposition?.handoff?.state, current.disposition?.handoff?.state),
    setRow(
      'Handoff triggered by',
      previous.disposition?.handoff?.triggeredBy,
      current.disposition?.handoff?.triggeredBy
    ),
    scalarRow('Handoff target', targetOf(previous), targetOf(current))
  ]
  const changed = rows.filter((row) => row.changed)

  return (
    <Section title="What changed" count={changed.length}>
      {changed.length === 0 ? (
        <p className="note">
          The disposition is unchanged from the previous run: same kind, outcome,
          reasons, handoff and target.
        </p>
      ) : (
        <p className="note">
          Previous run on the left, this run on the right. Unchanged members are
          listed too, so the diff never hides what held.
        </p>
      )}
      <table className="diff">
        <thead>
          <tr>
            <th scope="col">Member</th>
            <th scope="col">Previous</th>
            <th scope="col">This run</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.label} className={row.changed ? 'diff-changed' : undefined}>
              <th scope="row">{row.label}</th>
              <td>
                <Value text={row.before} />
              </td>
              <td>
                <Value text={row.after} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </Section>
  )
}

interface DiffRow {
  label: string
  before: string | undefined
  after: string | undefined
  changed: boolean
}

function scalarRow(label: string, before: string | undefined, after: string | undefined): DiffRow {
  return { label, before, after, changed: before !== after }
}

/**
 * A reason or trigger set. The runtime emits both sorted and duplicate-free, so
 * the rendering here is the payload's order and the comparison is set equality.
 */
function setRow(label: string, before: string[] | undefined, after: string[] | undefined): DiffRow {
  const beforeText = (before ?? []).join(', ')
  const afterText = (after ?? []).join(', ')
  return { label, before: beforeText, after: afterText, changed: beforeText !== afterText }
}

/** The target as one string, or undefined where the payload reported none. */
function targetOf(payload: Evaluation): string | undefined {
  if (!payload.handoffTarget) return undefined
  return `${payload.handoffTarget.name} (${payload.handoffTarget.kind})`
}

/** An absent member reads as absent, never as an empty cell that looks like a bug. */
function Value({ text }: { text: string | undefined }) {
  if (text === undefined) return <span className="quiet">absent</span>
  if (text === '') return <span className="quiet">empty</span>
  return <code>{text}</code>
}
