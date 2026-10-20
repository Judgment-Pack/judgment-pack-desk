import { msg, useLocale } from '../i18n'
import type { CriticRecord } from './critic'

/**
 * What the critic's pass said about the declared cases and what it cost, as
 * lines beside the critique's own words. The verdict stays the runtime's: this
 * counts comparisons and names the cases nothing compared. A critique with a
 * case left uncompared says so, and says what that means for the verdict.
 */
export function CriticDetails({ record, className }: { record: CriticRecord; className?: string }) {
  useLocale()
  const figures = record.figures
  const tokens = figures?.inputTokens !== undefined || figures?.outputTokens !== undefined
  const shown = record.incomplete.slice(0, 8)
  return (
    <ul className={className} aria-label={msg('Review of the declared cases')}>
      {record.compared > 0 && (
        <li>{msg('Declared cases: {{compared}} compared, {{disagreed}} differ from the expected outcome.', { compared: record.compared, disagreed: record.disagreed })}</li>
      )}
      {record.incomplete.length > 0 && (
        <li role="status">
          {msg('Some declared cases were not compared, so this review cannot say the proposal was not refuted. Not compared: {{cases}}', {
            cases: shown.join(', ') + (record.incomplete.length > shown.length ? '…' : '')
          })}
        </li>
      )}
      {figures && (
        <li>
          {figures.unit === 'agent-runs'
            ? msg('Review agent runs: {{requests}} on {{model}}', { requests: figures.requests, model: figures.model })
            : msg('Review requests: {{requests}} on {{model}}', { requests: figures.requests, model: figures.model })}
          {tokens && msg(' · tokens in {{input}}, out {{output}}', { input: figures.inputTokens ?? 0, output: figures.outputTokens ?? 0 })}
        </li>
      )}
      {figures && record.runModel !== undefined && figures.model !== record.runModel && (
        <li>{msg('The review ran on {{model}}, the model this connection names for reviews, and the answer on {{runModel}}.', { model: figures.model, runModel: record.runModel })}</li>
      )}
    </ul>
  )
}
