import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
import { workItems, summarizeWork, type WorkRecord } from './responseHistory'
import type { RunState } from '../research/run'
import { statusLine } from '../research/ui/Conversation'
import { TOOL_LABELS } from './toolLabels'
import styles from './ChatWorkspace.module.css'
import { Disclosure } from '../ui/Disclosure'
import { RunStatus } from '../ui/RunStatus'

export { workItems } from './responseHistory'
export function WorkSummary({ state, work }: { state?: RunState; work?: WorkRecord }) {
  useLocale()
  const saved = work ?? summarizeWork(state?.events ?? [], state?.status === 'running')
  const rows = saved.items, notices = saved.notices, critique = saved.critique
  if (!rows.length && !notices.length && !critique) return null
  const failures = rows.filter(row => row.status === 'failed').length
  return <Disclosure className={styles.work} title={<>{rows.length ? msg("Work · {{count}} steps", { count: rows.length }) + (failures ? msg(" · {{count}} failed", { count: failures }) : "") : msg("Assistant notice")}</>}>
    {rows.length > 0 && <ol>{rows.map(row => <li key={row.id}><span>{TOOL_LABELS[row.name] ?? row.name}</span><span>{row.status === 'complete' ? msg("Done") : row.status === 'failed' ? msg("Failed") : row.status === 'interrupted' ? msg("Interrupted") : msg("Working…")}</span></li>)}</ol>}
    {notices.map(notice => <p key={notice}>{systemMessage(notice)}</p>)}
    {critique && <p><Message text={"Adversarial review: <0/>"} slots={[critique]} /></p>}
  </Disclosure>
}

/** One visible status. Completed replies have no persistent status furniture. */
export function TaskStatus({ state }: { state: RunState }) {
  useLocale()
  if (state.status === 'idle' || state.status === 'complete' || state.status === 'ready') return null
  const active = state.status === 'running' ? workItems(state.events, true).filter(item => item.status === 'working') : []
  const message = active.length ? `${TOOL_LABELS[active[0]!.name] ?? active[0]!.name}…` : systemMessage(state.detail) || statusLine(state)
  return <RunStatus className={styles.runStatus} running={state.status === 'running'} error={state.status === 'failed'}>{message}</RunStatus>
}

export function candidateSummary(state: RunState): string {
  const candidate = state.candidates.at(-1)
  const check = candidate?.check
  if (state.restored) return msg("Saved draft · Recheck needed")
  if (!check || check.documentDigest !== candidate?.digest) return state.status === 'running' ? msg("Checking draft…") : msg("Not checked")
  if (!check.valid) return msg("Structure needs corrections")
  if (!check.cases.length) return msg("Structure checked · Tests not run")
  const passed = check.cases.filter(row => row.passed).length
  return msg("{{value0}} of {{value1}} tests passed{{value2}}", { value0: passed, value1: check.cases.length, value2: state.status === 'ready' ? msg(' · Ready for review') : msg(' · Review needed') })
}
