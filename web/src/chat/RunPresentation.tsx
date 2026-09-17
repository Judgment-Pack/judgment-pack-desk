import type { AssistantEvent } from '../assistant/engine'
import type { RunState } from '../research/run'
import { statusLine } from '../research/ui/Conversation'
import { TOOL_LABELS } from './toolLabels'
import styles from './ChatWorkspace.module.css'

export interface WorkItem { id: string; name: string; status: 'working' | 'complete' | 'failed' | 'interrupted' }
/** Pair by invocation identity, never by a tool name (parallel calls may repeat). */
export function workItems(events: readonly AssistantEvent[], running: boolean): WorkItem[] {
  const rows: WorkItem[] = []
  const pending = new Map<string, WorkItem>()
  events.forEach((event, index) => {
    if (event.type === 'tool_call' && event.callId) {
      const row: WorkItem = { id: event.callId, name: event.name, status: running ? 'working' : 'interrupted' }
      rows.push(row); pending.set(event.callId, row)
    } else if (event.type === 'tool_result') {
      const row = event.callId ? pending.get(event.callId) : undefined
      const status = event.isError ? 'failed' : 'complete'
      if (row) { row.status = status; pending.delete(event.callId!) }
      else rows.push({ id: `result-${index}`, name: event.name, status })
    }
  })
  return rows
}

export function WorkSummary({ state }: { state: RunState }) {
  const rows = workItems(state.events, state.status === 'running')
  const notices = [...new Set(state.events.flatMap(event =>
    event.type === 'guardrail' && event.action !== 'narrowed' ? [event.detail]
    : event.type === 'thinking_unavailable' ? [event.detail] : []))]
  const critique = [...state.events].reverse().find(event => event.type === 'critique')
  if (!rows.length && !notices.length && !critique) return null
  const failures = rows.filter(row => row.status === 'failed').length
  return <details className={styles.work}><summary>{rows.length ? `Work · ${rows.length} ${rows.length === 1 ? 'step' : 'steps'}${failures ? ` · ${failures} failed` : ''}` : 'Assistant notice'}</summary>
    {rows.length > 0 && <ol>{rows.map(row => <li key={row.id}><span>{TOOL_LABELS[row.name] ?? row.name}</span><span>{row.status === 'complete' ? 'Done' : row.status === 'failed' ? 'Failed' : row.status === 'interrupted' ? 'Interrupted' : 'Working…'}</span></li>)}</ol>}
    {notices.map(notice => <p key={notice}>{notice}</p>)}
    {critique && <p>Adversarial review: {critique.text}</p>}
  </details>
}

/** One visible status. Completed replies have no persistent status furniture. */
export function TaskStatus({ state }: { state: RunState }) {
  if (state.status === 'idle' || state.status === 'complete' || state.status === 'ready') return null
  if (state.streaming) return null
  const active = state.status === 'running' ? workItems(state.events, true).filter(item => item.status === 'working') : []
  const message = active.length ? `${TOOL_LABELS[active[0]!.name] ?? active[0]!.name}…` : state.detail || statusLine(state)
  return <div className={styles.runStatus} role="status">{message}</div>
}

export function candidateSummary(state: RunState): string {
  const candidate = state.candidates.at(-1)
  const check = candidate?.check
  if (state.restored) return 'Saved draft · Recheck needed'
  if (!check || check.documentDigest !== candidate?.digest) return state.status === 'running' ? 'Checking draft…' : 'Not checked'
  if (!check.valid) return 'Structure needs corrections'
  if (!check.cases.length) return 'Structure checked · Tests not run'
  const passed = check.cases.filter(row => row.passed).length
  return `${passed} of ${check.cases.length} tests passed${state.status === 'ready' ? ' · Ready for review' : ' · Review needed'}`
}
