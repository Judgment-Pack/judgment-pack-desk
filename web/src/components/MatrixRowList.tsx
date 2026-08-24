import { describeHandoffTarget, parseDisposition } from '../mcp/canonical'
import type { Disposition, MatrixRow } from '../mcp/types'
import { Pill } from './primitives'

/**
 * One matrix's rows, expected beside actual.
 *
 * A row is judged on canonical bytes, so this view shows the disposition the
 * row wrote and the disposition the evaluator produced side by side and reports
 * the runtime's own verdict rather than recomputing one. Where the two differ,
 * the differing members are marked — but the mark follows the payload's status,
 * never the other way round.
 *
 * Three kinds of row appear here and are kept apart, because the payload keeps
 * them apart:
 *
 * - a row expecting a **disposition**, compared byte for byte;
 * - a row expecting a **refusal**, which carries no disposition at all and is
 *   judged on the §8.4 error class and phase;
 * - a row additionally asserting a **handoff target** (ADR-0025), which is not
 *   part of the disposition and gates separately. That last one is the case
 *   this view most needs to make legible: a pack edit touching only where a
 *   handoff goes leaves every disposition byte identical, so a row can fail
 *   with its expected and actual dispositions matching exactly.
 */
export function MatrixRowList({ rows }: { rows: MatrixRow[] }) {
  return (
    <ul className="rows">
      {rows.map((row) => (
        <MatrixRowItem key={row.id} row={row} />
      ))}
    </ul>
  )
}

function MatrixRowItem({ row }: { row: MatrixRow }) {
  const passed = row.status === 'passed'
  const dispositionsAgree = row.expected === row.actual
  const expectsRefusal = Boolean(row.expectedErrorClass)
  const assertsTarget = row.expectedHandoffTarget !== undefined
  const targetsAgree = row.expectedHandoffTarget === row.actualHandoffTarget

  return (
    <li className={`row row-${row.status}`}>
      <div className="row-head">
        <code className="row-id">{row.id}</code>
        <Pill tone={passed ? 'quiet' : 'strong'}>{row.status}</Pill>
        {row.origin && <Pill tone="quiet">origin {row.origin}</Pill>}
        {assertsTarget && <Pill tone="quiet">asserts a handoff target</Pill>}
      </div>

      {!passed && dispositionsAgree && assertsTarget && !targetsAgree && (
        <p className="note note-warn">
          The dispositions match byte for byte. This row fails on the handoff
          target alone — where the decision goes, which §8.3 keeps outside the
          disposition.
        </p>
      )}

      {expectsRefusal ? (
        <RefusalComparison row={row} />
      ) : (
        <div className="row-compare">
          <DispositionSide label="expected" text={row.expected} differs={!dispositionsAgree} />
          <DispositionSide label="actual" text={row.actual} differs={!dispositionsAgree} />
        </div>
      )}

      {assertsTarget && (
        <div className="row-compare row-targets">
          <TargetSide label="expected target" member={row.expectedHandoffTarget} differs={!targetsAgree} />
          <TargetSide label="actual target" member={row.actualHandoffTarget} differs={!targetsAgree} />
        </div>
      )}

      {row.detail && <p className="row-detail">{row.detail}</p>}
    </li>
  )
}

/** A row that expected a refusal carries no disposition, so none is shown. */
function RefusalComparison({ row }: { row: MatrixRow }) {
  return (
    <div className="row-compare">
      <div className="row-side">
        <span className="row-side-label">expected</span>
        <p className="row-refusal">
          a refused evaluation: <code>{row.expectedErrorClass}</code>
          {row.expectedErrorPhase && (
            <>
              {' '}
              in <code>{row.expectedErrorPhase}</code>
            </>
          )}
        </p>
      </div>
      <div className={`row-side${row.actualErrorClass === row.expectedErrorClass ? '' : ' row-side-differs'}`}>
        <span className="row-side-label">actual</span>
        {row.actualErrorClass ? (
          <p className="row-refusal">
            a refused evaluation: <code>{row.actualErrorClass}</code>
            {row.actualErrorPhase && (
              <>
                {' '}
                in <code>{row.actualErrorPhase}</code>
              </>
            )}
          </p>
        ) : (
          <p className="row-refusal">a disposition was produced where a refusal was expected</p>
        )}
      </div>
    </div>
  )
}

function DispositionSide({
  label,
  text,
  differs
}: {
  label: string
  text: string
  differs: boolean
}) {
  const parsed = parseDisposition(text)
  return (
    <div className={`row-side${differs ? ' row-side-differs' : ''}`}>
      <span className="row-side-label">{label}</span>
      {parsed ? <DispositionSummary disposition={parsed} /> : <code className="row-raw">{text || '(none)'}</code>}
    </div>
  )
}

/** The members a §8.3 disposition carries, and no others. */
function DispositionSummary({ disposition }: { disposition: Disposition }) {
  const reasons = disposition.reasons ?? []
  const handoff = disposition.handoff
  return (
    <div className="row-disposition">
      <span className={`kind kind-${disposition.kind}`}>{disposition.kind}</span>
      {disposition.outcomeId && <code className="row-outcome">{disposition.outcomeId}</code>}
      <span className="row-members">
        reasons{' '}
        {reasons.length === 0 ? <span className="quiet">none</span> : <code>{reasons.join(', ')}</code>}
        {handoff && (
          <>
            {' · '}handoff <code>{handoff.state}</code>
            {handoff.triggeredBy?.length ? (
              <>
                {' '}
                by <code>{handoff.triggeredBy.join(', ')}</code>
              </>
            ) : null}
          </>
        )}
      </span>
    </div>
  )
}

function TargetSide({
  label,
  member,
  differs
}: {
  label: string
  member: string | undefined
  differs: boolean
}) {
  return (
    <div className={`row-side${differs ? ' row-side-differs' : ''}`}>
      <span className="row-side-label">{label}</span>
      <p className="target-name">{member === undefined ? '(not reported)' : describeHandoffTarget(member)}</p>
    </div>
  )
}
