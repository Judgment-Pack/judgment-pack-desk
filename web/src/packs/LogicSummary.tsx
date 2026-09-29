import { msg } from '../i18n'
import type { PackDocument } from '../mcp/types'
import { isRecord } from './document/MisshapenMember'
import { humanId, text, type LogicItem } from './logicModel'
import { decisionAccent, lookupAppearance, type OutcomeAppearances } from './decisionAppearance'
import styles from './LogicDetails.module.css'

/** Describe the declared operator without flattening nested ALL/ANY logic. */
export function conditionSummary(value: unknown): string {
  if (!isRecord(value)) return msg('Condition not declared')
  if (value.op === 'all' || value.op === 'any') {
    const count = Array.isArray(value.conditions) ? value.conditions.length : 0
    return value.op === 'all' ? msg('All {{count}} conditions', { count }) : msg('Any of {{count}} conditions', { count })
  }
  if (value.op === 'not') return msg('Negated condition')
  if (value.op === 'evidence-present') return msg('Evidence present: {{name}}', { name: humanId(value.evidenceRequirement, msg('Not declared')) })
  if (value.op === 'fact') return msg('Fact condition: {{path}}', { path: text(value.path) })
  return msg('Condition: {{operator}}', { operator: text(value.op) })
}

export function LogicSummary({ document, group, item, appearances = {} }: { document: PackDocument; group: string; item: LogicItem; appearances?: OutcomeAppearances }) {
  const value = item.value
  if (!isRecord(value)) return <p className={styles.note}>{msg('Unrecognized entry')}</p>
  if (group === 'outcomes') return document.fallbackOutcome === value.id ? <span className={styles.fallback}>{msg('Fallback')}</span> : null
  const appearance = lookupAppearance(appearances, value.outcome)
  return <div className={styles.content}><p className={styles.note}>{conditionSummary(value.when)}</p><p style={appearance ? { color: decisionAccent(appearance) } : undefined}>{item.effect}</p></div>
}
