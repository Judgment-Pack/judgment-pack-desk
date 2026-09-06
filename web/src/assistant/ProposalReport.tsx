/**
 * A proposal, reported: what it is, what it left open, and what the runtime
 * said about it.
 *
 * Factored out of the Assistant tab for the Create dialog's **Describe it**
 * section, which needs the same three things and must not describe them in
 * words of its own. Two renderings of one proposal are two accounts of it.
 *
 * **The checks are quoted whole and never summarised.** A summary of a verdict
 * is a second verdict, and only the runtime is entitled to the first. What is
 * shown is the exact text of the `validate` report and of the rehearsal
 * evaluation, with the runtime's own `isError` beside the name.
 *
 * **The summary line counts and never judges.** It says which document this is
 * and how many rules and outcomes it carries — facts anybody could count off
 * the JSON below — and says nothing about whether it is any good. Where a
 * member is missing or is not the shape it should be, the line says so rather
 * than reporting a zero, because "no rules member" and "an empty rules array"
 * are different documents.
 */
import { CodeArea } from '../ui/CodeArea'
import type { AssistantEvent } from './engine'
import styles from './AssistantPane.module.css'

/** The two answers a proposal is reported with, in the order they are shown. */
export const QUOTED_CHECKS = ['validate', 'experimental_evaluate'] as const

/** What one proposal says about itself, counted rather than read. */
export interface ProposalSummary {
  id: string | undefined
  title: string | undefined
  /** Undefined where the member is absent or is not an array. */
  rules: number | undefined
  outcomes: number | undefined
}

export function summariseProposal(document: unknown): ProposalSummary {
  const held =
    typeof document === 'object' && document !== null && !Array.isArray(document)
      ? (document as Record<string, unknown>)
      : {}
  const text = (value: unknown) => (typeof value === 'string' && value !== '' ? value : undefined)
  const count = (value: unknown) => (Array.isArray(value) ? value.length : undefined)
  return {
    id: text(held.id),
    title: text(held.title),
    rules: count(held.rules),
    outcomes: count(held.outcomes)
  }
}

/** The one line a proposal is introduced by. */
export function ProposalSummaryLine({ document }: { document: unknown }) {
  const summary = summariseProposal(document)
  const some = (count: number | undefined, one: string, many: string) =>
    count === undefined ? `no ${many} member` : `${count} ${count === 1 ? one : many}`
  return (
    <p className={styles.honesty}>
      <strong>{summary.title ?? 'a document with no title'}</strong>
      {' — '}
      <code>{summary.id ?? 'no id'}</code>
      {' · '}
      {some(summary.rules, 'rule', 'rules')}
      {' · '}
      {some(summary.outcomes, 'outcome', 'outcomes')}
    </p>
  )
}

/** What the assistant said it did not know, listed as it wrote them. */
export function ProposalUnknowns({ unknowns }: { unknowns: readonly string[] }) {
  return (
    <>
      <p className={styles.label}>Unknowns the assistant declared</p>
      {unknowns.length === 0 ? (
        <p className={styles.honesty}>It declared none.</p>
      ) : (
        <ul className={styles.unknowns}>
          {unknowns.map((unknown) => (
            <li key={unknown}>{unknown}</li>
          ))}
        </ul>
      )}
    </>
  )
}

/**
 * The refutation pass, reported: what the runtime said, and what the critic
 * said about it.
 *
 * **The verdict is the runtime's and the prose is the model's, and the two are
 * never mixed.** The `critique` event carries a verdict this desk computed from
 * the runtime's own statuses (`assistant/refutation.ts`); the critic's own
 * words are shown beneath it, labelled, because they are worth reading and are
 * not evidence.
 *
 * A pass that reached no runtime check says so and states no verdict — which is
 * ADR-0001's first rule: a non-empty list of checks before "not refuted" is
 * rendered.
 */
export function RefutationReport({ events }: { events: readonly AssistantEvent[] }) {
  const critique = [...events]
    .reverse()
    .find(
      (event): event is Extract<AssistantEvent, { type: 'critique' }> => event.type === 'critique'
    )
  if (critique === undefined) return null
  return (
    <div>
      <p className={styles.label}>The refutation pass</p>
      {critique.checks.length === 0 ? (
        <p className={styles.honesty}>{critique.text}</p>
      ) : (
        <>
          <p className={critique.refuted ? styles.notice : styles.honesty}>
            {critique.refuted
              ? 'The runtime refuted this proposal.'
              : 'The runtime did not refute this proposal.'}{' '}
            {critique.text}
          </p>
          <ul className={styles.unknowns}>
            {critique.checks.map((check, index) => (
              <li key={`${check.tool}:${index}`}>
                <code>{check.tool}</code> → <strong>{check.status}</strong>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  )
}

/**
 * The runtime's two answers, quoted.
 *
 * The **last** answer for each name, because a session that validated a draft
 * and then validated the document it proposed has two, and the one that is
 * about the proposal is the later one. A name the session never called has
 * nothing here rather than a line saying so: the stream above already says
 * every call that was made.
 */
export function RuntimeChecks({ events }: { events: readonly AssistantEvent[] }) {
  const results = events.filter(
    (event): event is Extract<AssistantEvent, { type: 'tool_result' }> =>
      event.type === 'tool_result'
  )
  return (
    <>
      {QUOTED_CHECKS.map((name) => {
        const result = [...results].reverse().find((entry) => entry.name === name)
        if (result === undefined) return null
        return (
          <div key={name}>
            <p className={styles.label}>
              {name} — the runtime’s answer, quoted{result.isError ? ' (isError)' : ''}
            </p>
            <CodeArea value={result.text} readOnly aria-label={`${name}, as the runtime wrote it`} />
          </div>
        )
      })}
    </>
  )
}
