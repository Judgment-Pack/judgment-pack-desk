import { Message } from '../../i18n/Message'
import { msg, useLocale } from '../../i18n'
import type { ReactNode } from 'react'
import type { TraceEntry } from '../../mcp/types'
import { InspectionRow } from '../../ui/InspectionRow'
import { Disclosure } from '../../ui/Disclosure'
import { CodeBlock } from '../../ui/CodeBlock'
import { InfoHelp } from '../../ui/InfoHelp'
import { fieldLabel, packTerm, PACK_TERMS, TERM_HELP, valueLabel } from '../terminology'
import { MemberValue } from './MemberValue'
import { ConditionTree } from '../document/ConditionTree'
import { isRecord } from '../document/MisshapenMember'
import { valueAt } from '../pointers'
import { itemTrace, outcomeLabel, selectedItem, text, type LogicGroup, type LogicItem, type LogicProjection } from '../logicModel'
import styles from './LogicInspector.module.css'

export function LogicInspector({ model, at, groupId, onSelect, trace, advanced, mainContent = false, conditionsVisible = true }: {
  model: LogicProjection; at: string | null
  groupId?: string | null
  onSelect: (pointer: string) => void; mainContent?: boolean; conditionsVisible?: boolean
  trace?: readonly TraceEntry[]; advanced: ReactNode
}) {
  useLocale()
  const selected = selectedItem(model, at)
  const row = (group: LogicGroup, item: LogicItem) => {
    const observation = itemTrace(group, item, trace)
    return <InspectionRow key={item.pointer} label={item.label}
    aria-label={msg("View details: {{value0}}", { value0: item.label })} current={selected?.item.pointer === item.pointer}
    data-outline-pointer={item.pointer} onClick={() => onSelect(item.pointer)}
    description={item.effect || observation ? <>{item.effect}{observation && <span className={styles.observation}>{observation}</span>}</> : undefined} />
  }
  const inspectedGroup = model.groups.find(group => group.id === groupId)
  if (inspectedGroup) return <div className={styles.details}>
    <h2>{inspectedGroup.label} · {inspectedGroup.items.length}</h2>
    <p className={styles.meta}>{inspectedGroup.description}</p>
    {inspectedGroup.items.map(item => row(inspectedGroup, item))}
    {!inspectedGroup.items.length && <p>{msg("None declared.")}</p>}
  </div>

  if (at === null) return <div className={styles.details}><h2>{msg("Pack details")}</h2>{advanced}</div>
  const value = selected?.item.value ?? valueAt(model.document, at)
  const condition = selected?.group.id === 'applicability' ? value : isRecord(value) ? value.when : undefined
  const pointer = selected?.item.pointer ?? at
  const observed = selected && itemTrace(selected.group, selected.item, trace)
  const group = model.groups.find(g => '/' + g.id === at)
  return <div className={styles.details}>
    <h2>{selected?.item.label ?? group?.label ?? fieldLabel(at.slice(1) || 'Document')}</h2>
    {!mainContent && <p className={styles.meta}>{packTerm(pointer.slice(1))?.description ?? selected?.group.description}</p>}
    {!mainContent && observed && <p className={styles.observation}><Message text={"Recorded condition: <0/>"} slots={[<strong>{observed}</strong>]} /></p>}
    {condition !== undefined && (!mainContent || (!conditionsVisible && selected?.group.id !== 'applicability')) && <section className={styles.group}><h3>{msg("Condition")}</h3>
      <ConditionTree readOnly structured condition={condition} at={selected?.group.id === 'applicability' ? "/applicability" : `${pointer}/when`} />
    </section>}
    {!mainContent && isRecord(value) && (value.outcome !== undefined || value.effect !== undefined) && <section className={styles.group}>
      <h3>{value.effect === undefined ? msg("Contributes outcome") : msg("Effect")}</h3>
      <p>{selected?.item.effect}</p>
      <h3>{PACK_TERMS.onUnknown.label} <InfoHelp title={PACK_TERMS.onUnknown.label}>{TERM_HELP.onUnknown}</InfoHelp></h3>
      <p>{valueLabel('onUnknown', text(value.onUnknown))}</p>
    </section>}
    {!mainContent && pointer === '/fallbackOutcome' && <section className={styles.group}><h3>{PACK_TERMS.fallbackOutcome.label} <InfoHelp title={PACK_TERMS.fallbackOutcome.label}>{TERM_HELP.fallbackOutcome}</InfoHelp></h3><p>{outcomeLabel(model.document, value)}</p><p className={styles.meta}>{msg("A fallback does not itself request a handoff.")}</p></section>}
    {isRecord(value) && typeof value.description === 'string' && (!mainContent || !['outcomes', 'sources'].includes(selected?.group.id ?? '')) && <Disclosure className={styles.group} title={msg("Author description")}><p>{value.description}</p></Disclosure>}
    {isRecord(value) && typeof value.rationale === 'string' && <section className={styles.group}><h3>{msg("Reasoning")}</h3><p>{value.rationale}</p></section>}
    {group && !selected && <section className={styles.group}>{group.items.map(item => row(group, item))}{!group.items.length && <p>{msg("None declared.")}</p>}</section>}
    {!mainContent && condition === undefined && pointer !== '/fallbackOutcome' && !group && <Definition value={value} />}
    <Disclosure className={styles.group} title={msg("Technical details")}>
      <p className={styles.meta}><Message text={"Document path: <0/>"} slots={[<code>{pointer || '/'}</code>]} /></p>
      <h3><Message text={"Exact <0/> JSON"} slots={[condition !== undefined ? msg("condition") : msg("definition")]} /></h3>
      <CodeBlock text={JSON.stringify(condition ?? value, null, 2) ?? "Not declared"} />
    </Disclosure>
    <section className={styles.group} aria-label={msg("References, checks and metadata")}>{advanced}</section>
  </div>
}

function Definition({ value }: { value: unknown }) {
  useLocale()
  if (value === undefined) return <p>{msg("Not declared.")}</p>
  if (!isRecord(value)) return <MemberValue value={value} />
  return <dl className={styles.definition}>{Object.entries(value).filter(([key]) => !['description', 'extensions'].includes(key)).map(([key, child]) => <div key={key}>
    <dt>{fieldLabel(key)}</dt><dd>{typeof child === 'string' ? <span>{valueLabel(key, child)}</span> : <MemberValue value={child} />}</dd>
  </div>)}</dl>
}
