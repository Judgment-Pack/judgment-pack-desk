import type { PackDocument, TraceEntry } from '../mcp/types'
import { isRecord } from './document/MisshapenMember'
import { PACK_TERMS, valueLabel } from './terminology'

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
  if (value.effect === 'suppress-rule') return `Exclude rule ${text(value.targetRule)}`
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
    { id: 'applicability', ...PACK_TERMS.applicability, description: document.applicability === undefined ? 'No scope restriction is set.' : PACK_TERMS.applicability.description, items: [{ pointer: '/applicability', label: PACK_TERMS.applicability.label, value: document.applicability }] },
    { id: 'evidenceRequirements', ...PACK_TERMS.evidenceRequirements, description: `${entries(document.evidenceRequirements).filter(x => isRecord(x) && x.required === true).length} required evidence items`, items: rows('evidenceRequirements') },
    { id: 'rules', ...PACK_TERMS.rules, items: rows('rules') },
    { id: 'exceptions', ...PACK_TERMS.exceptions, items: rows('exceptions') },
    { id: 'resolution', ...PACK_TERMS.resolution, items: [
      { pointer: '/fallbackOutcome', label: PACK_TERMS.fallbackOutcome.label, value: document.fallbackOutcome },
      { pointer: '/escalation', label: PACK_TERMS.escalation.label, value: document.escalation }
    ] },
    { id: 'outcomes', ...PACK_TERMS.outcomes, items: rows('outcomes') },
    { id: 'sources', ...PACK_TERMS.sources, items: rows('sources') }
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
  return found ? `${valueLabel('condition', found.condition)}${found.suppressed ? ' · excluded by a special case' : ''}${found.skipped && found.condition !== 'not-evaluated' ? ' · not evaluated' : ''}` : 'Unreported'
}
