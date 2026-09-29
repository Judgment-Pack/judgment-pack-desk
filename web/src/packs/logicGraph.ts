import { msg } from '../i18n'
import type { RelationshipEdge } from '../components/RelationshipMap'
import { isRecord } from './document/MisshapenMember'
import type { LogicGroup, LogicItem, LogicProjection } from './logicModel'

export interface LogicGraphNode { id: string; group: LogicGroup; items: LogicItem[]; column: number; title: string }

/** Only explicit rule outcomes and exception targets form graph edges. References
 * to evidence/sources are documentary, never inferred execution prerequisites. */
export function projectLogicGraph(model: LogicProjection, grouped = false, expanded: ReadonlySet<string> = new Set()) {
  const nodes: LogicGraphNode[] = []
  const edges: RelationshipEdge[] = []
  const exceptions = model.groups.find(g => g.id === 'exceptions')!
  const rules = model.groups.find(g => g.id === 'rules')!
  const outcomes = model.groups.find(g => g.id === 'outcomes')!
  const targetedRules = new Set(exceptions.items.flatMap(item => isRecord(item.value) && item.value.effect === 'suppress-rule' ? [item.value.targetRule] : []))
  const ruleColumn = exceptions.items.length ? 1 : 0
  const itemNodes = new Map<string, string>()
  const buckets = new Map<string, LogicItem[]>()
  for (const item of rules.items) {
    const outcome = isRecord(item.value) ? item.value.outcome : undefined
    const key = typeof outcome === 'string' ? JSON.stringify(outcome) : item.pointer
    buckets.set(key, [...(buckets.get(key) ?? []), item])
  }
  const exceptionBuckets = new Map<string, LogicItem[]>()
  for (const item of exceptions.items) {
    if (!isRecord(item.value) || item.value.effect !== 'force-outcome' || typeof item.value.outcome !== 'string') continue
    const key=JSON.stringify(item.value.outcome)
    exceptionBuckets.set(key,[...(exceptionBuckets.get(key)??[]),item])
  }
  for (const group of [exceptions, rules, outcomes]) {
    const seen = new Set<string>()
    for (const item of group.items) {
      const outcome = isRecord(item.value) ? item.value.outcome : undefined
      const key = typeof outcome === 'string' ? JSON.stringify(outcome) : item.pointer
      const id = `${group.id}:${key}`
      const bucket = (group === exceptions ? exceptionBuckets : buckets).get(key) ?? [item]
      // An exclusion must point to its exact rule, never a group suggesting
      // every rule contributing that outcome is excluded.
      const aggregate = grouped && bucket.length > 1 && !expanded.has(id) && (
        group === rules && !bucket.some(child => isRecord(child.value) && targetedRules.has(child.value.id)) ||
        group === exceptions && isRecord(item.value) && item.value.effect === 'force-outcome')
      if (aggregate && seen.has(key)) continue
      seen.add(key)
      const items = aggregate ? bucket : [item]
      const nodeId = aggregate ? id : item.pointer
      nodes.push({ id: nodeId, group, items, title: aggregate ? group === exceptions ? msg('{{effect}} · {{count}} special cases', {effect:item.effect,count:items.length}) : msg('{{outcome}} · {{count}} rules', { outcome: item.effect, count: items.length }) : item.label,
        column: group === exceptions ? 0 : group === rules ? ruleColumn : ruleColumn + 1 })
      items.forEach(child => itemNodes.set(child.pointer, nodeId))
    }
  }
  for (const group of [exceptions, rules]) for (const item of group.items) {
    if (!isRecord(item.value)) continue
    const suppressed = group === exceptions && item.value.effect === 'suppress-rule'
    const targetItems = suppressed ? rules.items : outcomes.items
    const targetId = suppressed ? item.value.targetRule : item.value.outcome
    if (typeof targetId !== 'string') continue
    const targetItem = targetItems.find(child => isRecord(child.value) && child.value.id === targetId)
    if (!targetItem) continue
    const source = itemNodes.get(item.pointer)!, target = itemNodes.get(targetItem.pointer)!
    const id = JSON.stringify([source, target])
    if (!edges.some(e => e.id === id)) edges.push({ id, source, target,
      semantic: suppressed ? 'excludes' : group === exceptions ? 'forces' : 'contributes',
      label: suppressed ? msg('Excludes') : group === exceptions ? msg('Forces') : msg('Contributes') })
  }
  const handoff = model.groups.find(group => group.id === 'resolution')?.items.find(item => item.pointer === '/escalation')
  const target = model.document.escalation?.target
  const requests = exceptions.items.filter(item => isRecord(item.value) && item.value.effect === 'escalate')
  if (handoff && isRecord(target) && typeof target.name === 'string' && requests.length) {
    const group: LogicGroup = { id: 'handoff', label: msg('Handoff'), description: '', items: [handoff] }
    nodes.push({ id: handoff.pointer, group, items: [handoff], title: target.name, column: ruleColumn + 1 })
    for (const item of requests) {
      const source = itemNodes.get(item.pointer)!
      edges.push({ id: JSON.stringify([source, handoff.pointer]), source, target: handoff.pointer, semantic: 'requests-handoff', label: msg('Requests handoff') })
    }
  }
  return { nodes, edges }
}
