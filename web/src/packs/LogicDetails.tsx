import type { PackDocument } from '../mcp/types'
import { ConditionTree } from './document/ConditionTree'
import { isRecord } from './document/MisshapenMember'
import { entries, evidenceSummary, outcomeLabel, text, type LogicItem } from './logicModel'
import { valueLabel } from './terminology'
import styles from './LogicDetails.module.css'

/** Shared reading content. No evaluation, inferred policy, or interactive children. */
export function LogicDetails({ document, group, item, conditions = true }: {
  document: PackDocument; group: string; item: LogicItem; conditions?: boolean
}) {
  const value = item.value
  if (group === 'applicability') return value === undefined ? <p className={styles.note}>No scope restriction is set.</p>
    : <ConditionTree readOnly structured condition={value} at={item.pointer} />
  if (item.pointer === '/fallbackOutcome') return <div className={styles.content}>
    <p>{value === undefined ? 'No fallback outcome' : outcomeLabel(document, value)}</p>
    <p className={styles.note}>Used only when no rule contributes an outcome and nothing blocks the result.</p>
  </div>
  if (value === undefined) return <p className={styles.note}>Not declared.</p>
  if (!isRecord(value)) return <pre className={styles.raw}>{JSON.stringify(value, null, 2)}</pre>
  if (group === 'rules' || group === 'exceptions') return <div className={styles.content}>
    {conditions ? <ConditionTree readOnly structured condition={value.when} at={`${item.pointer}/when`} />
      : <p className={styles.note}>{isRecord(value.when) ? valueLabel('op', text(value.when.op)) : 'Condition'} · conditions hidden</p>}
    <dl className={styles.facts}>
      <div><dt>{group === 'rules' ? 'Contributes outcome' : 'Effect'}</dt><dd>{item.effect}</dd></div>
      <div><dt>If unknown</dt><dd>{valueLabel('onUnknown', text(value.onUnknown))}</dd></div>
    </dl>
  </div>
  if (group === 'evidenceRequirements') return <p className={styles.note}>{evidenceSummary(value)}</p>
  if (item.pointer === '/escalation') return <div className={styles.content}>
    <p>{isRecord(value.target) ? text(value.target.name) : 'Recipient not declared'}</p>
    <ul className={styles.triggers}>{entries(value.triggers).map((trigger, index) => <li key={index}>{valueLabel('triggers', text(trigger))}</li>)}</ul>
    <p className={styles.note}>Requests a handoff; delivery is not confirmed.</p>
  </div>
  return typeof value.description === 'string' ? <p className={styles.note}>{value.description}</p> : null
}
