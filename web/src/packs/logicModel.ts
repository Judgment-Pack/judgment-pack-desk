import type { PackDocument, TraceEntry } from '../mcp/types'
import { isRecord } from './document/MisshapenMember'

export interface LogicItem {
  pointer: string
  label: string
  value: unknown
  effect?: string
}
export interface LogicGroup { id: string; label: string; items: LogicItem[]; description: string }
export interface LogicProjection { document: PackDocument; groups: LogicGroup[] }
export const text = (value: unknown, fallback = 'Not declared'): string => typeof value === 'string' ? value : fallback
export const humanId = (value: unknown, fallback: string): string => text(value, fallback).replace(/[-_]/g, ' ')
export const entries = (value: unknown): unknown[] => Array.isArray(value) ? value : []
export function outcomeLabel(doc: PackDocument, id: unknown): string {
  const found = entries(doc.outcomes).find(x => isRecord(x) && x.id === id)
  return isRecord(found) ? text(found.label, text(id)) : text(id)
}
export function effectLabel(doc: PackDocument, value: unknown): string {
  if (!isRecord(value)) return 'Unrecognized entry'
  if (value.effect === 'force-outcome') return `Force ${outcomeLabel(doc, value.outcome)}`
  if (value.effect === 'suppress-rule') return `Suppress ${text(value.targetRule)}`
  if (value.effect === 'escalate') return 'Request handoff'
  if (value.effect !== undefined) return text(value.effect, 'Unrecognized effect')
  return outcomeLabel(doc, value.outcome)
}

/** A single projection of declared members. No conditions are evaluated here. */
export function projectLogic(document: PackDocument): LogicProjection {
  const rows = (key: 'rules' | 'exceptions' | 'outcomes' | 'sources' | 'evidenceRequirements'): LogicItem[] =>
    entries(document[key]).map((value, index) => ({
      pointer: `/${key}/${index}`, value,
      label: isRecord(value) ? text(value.label, text(value.title, humanId(value.id, `Unrecognized entry ${index + 1}`))) : `Unrecognized entry ${index + 1}`,
      effect: key === 'rules' || key === 'exceptions' ? effectLabel(document, value) : undefined
    }))
  return { document, groups: [
    { id: 'applicability', label: 'Applicability', description: document.applicability === undefined ? 'No scope condition declared' : 'Declared scope condition', items: [{ pointer: '/applicability', label: 'Applicability', value: document.applicability }] },
    { id: 'evidenceRequirements', label: 'Evidence', description: `${entries(document.evidenceRequirements).filter(x => isRecord(x) && x.required === true).length} required`, items: rows('evidenceRequirements') },
    { id: 'rules', label: 'Rules', description: 'Candidate outcomes · no first-match priority', items: rows('rules') },
    { id: 'exceptions', label: 'Exceptions', description: [...new Set(entries(document.exceptions).map(x => isRecord(x) ? text(x.effect) : 'Unrecognized effect'))].join(' · '), items: rows('exceptions') },
    { id: 'resolution', label: 'Resolution', description: 'Outcome · not applicable · unresolved. Handoff is separate.', items: [
      { pointer: '/fallbackOutcome', label: 'Fallback outcome', value: document.fallbackOutcome },
      { pointer: '/escalation', label: 'Handoff', value: document.escalation }
    ] },
    { id: 'outcomes', label: 'Declared outcomes', description: 'Possible outcome labels', items: rows('outcomes') },
    { id: 'sources', label: 'Sources', description: 'Inspect citations on demand', items: rows('sources') }
  ] }
}
export function selectedItem(model: LogicProjection, pointer: string | null): { group: LogicGroup; item: LogicItem } | undefined {
  if (pointer === null) return undefined
  for (const group of model.groups) for (const item of group.items) {
    if (pointer === item.pointer || pointer.startsWith(item.pointer + '/')) return { group, item }
  }
  return undefined
}
export function matchingItems(group: LogicGroup, query: string): LogicItem[] {
  const needle = query.trim().toLocaleLowerCase()
  return group.items.filter(item => !needle || `${item.label} ${item.pointer} ${item.effect ?? ''} ${JSON.stringify(item.value) ?? ''}`.toLocaleLowerCase().includes(needle))
}
/** Absent observations stay unreported; skipped/suppressed are never false. */
export function itemTrace(group: LogicGroup, item: LogicItem, trace?: readonly TraceEntry[]): string | undefined {
  if (!trace) return undefined
  const stage = group.id === 'rules' ? 'rule' : group.id === 'exceptions' ? 'exception' : group.id === 'applicability' ? 'applicability' : undefined
  if (!stage) return undefined
  const id = isRecord(item.value) ? item.value.id : undefined
  const found = trace.find(t => t.stage === stage && (stage === 'applicability' || t.id === id))
  return found ? `${found.condition}${found.suppressed ? ' · suppressed' : ''}${found.skipped ? ' · skipped' : ''}` : 'Unreported'
}
