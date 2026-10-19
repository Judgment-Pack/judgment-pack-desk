import type { ChildFigures, FiguresTotal, TokenFigures } from '@judgment-pack/harness/execution/state'
import { msg, useLocale } from '../i18n'
import { profileLabel } from '../chat/DelegatedTaskDetails'
import type { RunReport } from './report'
import styles from './RunReportLines.module.css'

const seconds = (ms: number | undefined) => (ms === undefined ? '—' : (ms / 1000).toFixed(1))
const tokensOf = (tokens: TokenFigures | undefined) =>
  tokens === undefined || (tokens.inputTokens === undefined && tokens.outputTokens === undefined) ? '—' : msg('in {{input}}, out {{output}}', { input: tokens.inputTokens ?? 0, output: tokens.outputTokens ?? 0 })

/**
 * What a response reports about itself: how many requests it made, the
 * provider's own token counts where it returned them, how many requests were
 * retried and why, and, where it delegated, each child's model, requests, tool
 * calls, tokens, time to first byte and duration with each batch's total beside
 * the parent's own figures. **Every figure is the harness's, from the
 * execution checkpoint**, so a reloaded chat shows the same lines. A figure the
 * provider did not return is a dash, never a zero or an estimate.
 */
export function RunReportLines({ report, titles = {}, className }: { report: RunReport; titles?: Readonly<Record<string, string>>; className?: string }) {
  useLocale()
  const delegation = report.delegation
  return (
    <section className={className ?? styles.report} aria-label={msg('Run report')}>
      <p>
        {report.unit === 'provider-managed' ? msg('Agent runs: {{requests}}', { requests: report.requests }) : msg('Model requests: {{requests}}', { requests: report.requests })}
        {report.retries && msg(' · retried: {{retried}} ({{reasons}})', { retried: report.retries.count, reasons: report.retries.reasons.join(', ') })}
        {report.tokens && msg(' · tokens reported by the provider: {{tokens}}', { tokens: tokensOf(report.tokens) })}
      </p>
      {delegation && (
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">{msg('Task')}</th><th scope="col">{msg('Model')}</th><th scope="col">{msg('Requests')}</th><th scope="col">{msg('Tool calls')}</th>
              <th scope="col">{msg('Tokens')}</th><th scope="col">{msg('First answer (s)')}</th><th scope="col">{msg('Time (s)')}</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <th scope="row">{msg('Assistant (its own)')}</th><td>{delegation.parent.model}</td><td>{delegation.parent.modelRequests}</td><td>—</td>
              <td>{tokensOf(delegation.parent.tokens)}</td><td>—</td><td>—</td>
            </tr>
            {delegation.batches.map((batch) => (
              <BatchRows key={batch.batch} batch={batch.batch} children={batch.children} total={batch.total} titles={titles} />
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function ChildRow({ child, titles }: { child: ChildFigures; titles: Readonly<Record<string, string>> }) {
  useLocale()
  const marks = [child.partial ? msg('partial') : '', child.reused ? msg('reused') : '', child.state !== 'complete' ? stateLabel(child.state) : ''].filter(Boolean)
  return (
    <tr>
      <th scope="row">
        {titles[child.task] ?? profileLabel(child.profile)}
        {marks.length > 0 && <span className={styles.mark}> ({marks.join(', ')})</span>}
      </th>
      <td>{child.model ?? '—'}</td>
      <td>{child.agentRuns ?? child.modelRequests}</td>
      <td>{child.toolCalls}</td>
      <td>{tokensOf(child.tokens)}</td>
      <td>{seconds(child.firstByteMs)}</td>
      <td>{seconds(child.durationMs)}</td>
    </tr>
  )
}

function stateLabel(state: ChildFigures['state']): string {
  return state === 'failed' ? msg('failed') : state === 'interrupted' ? msg('interrupted') : state === 'running' ? msg('running') : msg('queued')
}

function BatchRows({ batch, children, total, titles }: { batch: number; children: ChildFigures[]; total: FiguresTotal; titles: Readonly<Record<string, string>> }) {
  useLocale()
  return (
    <>
      {children.map((child) => <ChildRow key={child.task} child={child} titles={titles} />)}
      <tr className={styles.total}>
        <th scope="row">{msg('Batch {{batch}} total', { batch })}</th><td>—</td>
        <td>{total.agentRuns ?? total.modelRequests}</td><td>{total.toolCalls}</td>
        <td>{tokensOf(total.tokens)}</td><td>—</td><td>{seconds(total.durationMs)}</td>
      </tr>
    </>
  )
}
