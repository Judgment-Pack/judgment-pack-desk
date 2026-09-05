/**
 * A refused evaluation, rendered as the runtime's answer.
 *
 * **A refusal carries no disposition at all.** Not a null one, not an
 * escalation, not a "could not decide" — the §8.4 envelope has a class, a
 * phase and the evaluator's version, and the payload has no `disposition`
 * member. So this panel prints those three, the diagnostics the envelope
 * carries, and nothing that could be read as an outcome. A surface that
 * substituted anything there would be answering a question the runtime
 * declined to answer.
 *
 * It was inside `PackEvaluate`, and the editor's what-if pane needs the same
 * panel. Two spellings of "what a refusal looks like" is two chances for one
 * of them to start looking like a verdict, so there is one — and it lives in
 * `components/`, whose idiom is global classes from `styles.css` rather than a
 * module of its own.
 *
 * Mid-edit a refusal is the **ordinary** answer, not an error state: a draft
 * halfway through a rule is not conformant, and preflight refuses it by class
 * and phase. That is why the pane renders this rather than an error box with a
 * stack trace in it.
 */
import { ErrorBox, Pill, Section } from './primitives'
import { ToolRefusal } from '../mcp/refusal'

export function RefusalPanel({
  error,
  title = 'The runtime refused this evaluation'
}: {
  error: Error
  /** What the box calls it, where the surface's words differ. */
  title?: string
}) {
  const envelope = error instanceof ToolRefusal ? error.envelope : undefined
  const evaluationError = envelope?.evaluationError
  return (
    <Section title="Refused">
      <ErrorBox title={title} error={error} />
      {evaluationError && (
        <p className="meta">
          <Pill tone="strong">class: {evaluationError.class}</Pill>
          <Pill>phase: {evaluationError.phase}</Pill>
          <Pill tone="quiet">evaluator {evaluationError.evaluatorSpecVersion}</Pill>
        </p>
      )}
      {envelope?.diagnostics?.length ? (
        <ul className="cards">
          {envelope.diagnostics.map((diagnostic, index) => (
            <li className="card" key={`${diagnostic.code ?? 'diagnostic'}:${index}`}>
              <div className="card-head">
                {diagnostic.code && <code className="id">{diagnostic.code}</code>}
                {diagnostic.severity && <Pill tone="quiet">{diagnostic.severity}</Pill>}
                {diagnostic.codeStability && <Pill tone="quiet">{diagnostic.codeStability}</Pill>}
              </div>
              {diagnostic.message && <p>{diagnostic.message}</p>}
            </li>
          ))}
        </ul>
      ) : null}
      <p className="note">A refusal carries no disposition. Nothing above is an answer.</p>
    </Section>
  )
}
