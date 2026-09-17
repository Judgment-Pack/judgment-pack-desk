import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { parseDisposition } from '../mcp/canonical'
import type { Disposition, MatrixRow } from '../mcp/types'
import { Pill, statusTone } from './primitives'
import { TargetPair, describeTargetAssertion } from './TargetPair'

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
  useLocale()
  return (
    <ul className="rows">
      {rows.map((row) => (
        <MatrixRowItem key={row.id} row={row} />
      ))}
    </ul>
  )
}

function MatrixRowItem({ row }: { row: MatrixRow }) {
  useLocale()
  const dispositionsAgree = row.expected === row.actual
  const expectsRefusal = Boolean(row.expectedErrorClass)
  const assertion = describeTargetAssertion(row)

  return (
    <li className={`row row-${row.status}`}>
      <div className="row-head">
        <code className="row-id">{row.id}</code>
        <Pill tone={statusTone(row.status)}>{row.status}</Pill>
        {row.origin && <Pill tone="quiet"><Message text={"origin <0/>"} slots={[row.origin]} /></Pill>}
        {assertion && <Pill tone="quiet">{assertion}</Pill>}
      </div>

      {expectsRefusal ? (
        <RefusalComparison row={row} />
      ) : (
        <div className="row-compare">
          <DispositionSide label={msg("expected")} text={row.expected} differs={!dispositionsAgree} />
          <DispositionSide label={msg("actual")} text={row.actual} differs={!dispositionsAgree} />
        </div>
      )}

      <TargetPair of={row} />

      {row.detail && <p className="row-detail">{row.detail}</p>}
    </li>
  )
}

/** A row that expected a refusal carries no disposition, so none is shown. */
function RefusalComparison({ row }: { row: MatrixRow }) {
  useLocale()
  return (
    <div className="row-compare">
      <div className="row-side">
        <span className="row-side-label">{msg("expected")}</span>
        <p className="row-refusal"><Message text={"a refused evaluation: <0/><1/>"} slots={[<code>{row.expectedErrorClass}</code>, row.expectedErrorPhase && (
            <><Message text={"<0/>in <1/>"} slots={[' ', <code>{row.expectedErrorPhase}</code>]} /></>
          )]} /></p>
      </div>
      <div
        className={`row-side${row.actualErrorClass === row.expectedErrorClass &&
          (!row.expectedErrorPhase || row.actualErrorPhase === row.expectedErrorPhase)
            ? ''
            : ' row-side-differs'}`}
      >
        <span className="row-side-label">{msg("actual")}</span>
        {row.actualErrorClass ? (
          <p className="row-refusal"><Message text={"a refused evaluation: <0/><1/>"} slots={[<code>{row.actualErrorClass}</code>, row.actualErrorPhase && (
              <><Message text={"<0/>in <1/>"} slots={[' ', <code>{row.actualErrorPhase}</code>]} /></>
            )]} /></p>
        ) : (
          <p className="row-refusal">{msg("a disposition was produced where a refusal was expected")}</p>
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
  useLocale()
  const parsed = parseDisposition(text)
  return (
    <div className={`row-side${differs ? ' row-side-differs' : ''}`}>
      <span className="row-side-label">{label}</span>
      {parsed ? <DispositionSummary disposition={parsed} /> : <code className="row-raw">{text || msg("(none)")}</code>}
    </div>
  )
}

/** The members a §8.3 disposition carries, and no others. */
function DispositionSummary({ disposition }: { disposition: Disposition }) {
  useLocale()
  const reasons = disposition.reasons ?? []
  const handoff = disposition.handoff
  return (
    <div className="row-disposition">
      <span className={`kind kind-${disposition.kind}`}>{disposition.kind}</span>
      {disposition.outcomeId && <code className="row-outcome">{disposition.outcomeId}</code>}
      <span className="row-members"><Message text={"reasons<0/><1/><2/>"} slots={[' ', reasons.length === 0 ? <span className="quiet">{msg("none")}</span> : <code>{reasons.join(', ')}</code>, handoff && (
          <><Message text={"<0/>handoff <1/><2/>"} slots={[' · ', <code>{handoff.state}</code>, handoff.triggeredBy?.length ? (
              <><Message text={"<0/>by <1/>"} slots={[' ', <code>{handoff.triggeredBy.join(', ')}</code>]} /></>
            ) : null]} /></>
        )]} /></span>
    </div>
  )
}
