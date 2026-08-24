import { describeHandoffTarget, parseDisposition } from '../mcp/canonical'
import type { Disposition, MatrixRow } from '../mcp/types'
import { Pill, statusTone } from './primitives'

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
 * - a row additionally asserting a **handoff-target state** (ADR-0025), which
 *   is not part of the disposition and gates separately. Its two members are
 *   display renderings — a long one is truncated with a digest tail — so this
 *   view never compares them: the comparator decides on decoded targets, and
 *   the row's own status is the only verdict shown.
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
  const dispositionsAgree = row.expected === row.actual
  const expectsRefusal = Boolean(row.expectedErrorClass)
  const assertsTarget = row.expectedHandoffTarget !== undefined

  return (
    <li className={`row row-${row.status}`}>
      <div className="row-head">
        <code className="row-id">{row.id}</code>
        <Pill tone={statusTone(row.status)}>{row.status}</Pill>
        {row.origin && <Pill tone="quiet">origin {row.origin}</Pill>}
        {assertsTarget && (
          <Pill tone="quiet">
            {row.expectedHandoffTarget === 'null'
              ? 'asserts no handoff target'
              : 'asserts a handoff-target state'}
          </Pill>
        )}
      </div>

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
          <TargetSide label="expected target" member={row.expectedHandoffTarget} />
          <TargetSide label="actual target" member={row.actualHandoffTarget} />
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
      <div
        className={`row-side${
          row.actualErrorClass === row.expectedErrorClass &&
          (!row.expectedErrorPhase || row.actualErrorPhase === row.expectedErrorPhase)
            ? ''
            : ' row-side-differs'
        }`}
      >
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

/**
 * One side of the target pair, shown and never compared: the members are
 * display renderings the comparator does not decide on, so no mark here may
 * claim a difference — the row's status already said what the runtime decided.
 */
function TargetSide({ label, member }: { label: string; member: string | undefined }) {
  return (
    <div className="row-side">
      <span className="row-side-label">{label}</span>
      <p className="target-name">{member === undefined ? '(not reported)' : describeHandoffTarget(member)}</p>
    </div>
  )
}
